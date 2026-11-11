//go:build catalogointegration && catalogosearchintegration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenSearchAndMemcachedEndToEnd(t *testing.T) {
	searchURL := strings.TrimSpace(os.Getenv("CATALOGO_TEST_OPENSEARCH_URL"))
	cacheAddress := strings.TrimSpace(os.Getenv("CATALOGO_TEST_MEMCACHED_ADDRESS"))
	if searchURL == "" || cacheAddress == "" {
		t.Fatal("set isolated/test CATALOGO_TEST_OPENSEARCH_URL and CATALOGO_TEST_MEMCACHED_ADDRESS")
	}
	s, router := catalogFixture(t)
	s.search = newSearchClient(searchURL)
	s.search.treatmentAlias = fmt.Sprintf("melt-catalog-test-%d-treatments-read", time.Now().UnixNano())
	s.search.professionalAlias = fmt.Sprintf("melt-catalog-test-%d-professionals-read", time.Now().UnixNano())
	s.cache = newMemcacheClient(cacheAddress)
	s.cache.namespace = "melt:catalogo:test"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.search.deleteAliasAndIndices(ctx, s.search.treatmentAlias, strings.TrimSuffix(s.search.treatmentAlias, "-read")+"-*")
		_ = s.search.deleteAliasAndIndices(ctx, s.search.professionalAlias, strings.TrimSuffix(s.search.professionalAlias, "-read")+"-*")
	})

	catalogCall(t, router, http.MethodPost, "/tratamientos", `{"id":"search-facial-1","nombre":"Limpieza facial suave","descripcion":"Cuidado de piel sensible","categoria":"Facial","precio":100,"moneda":"ARS","duracionMinutos":60,"keywords":["piel"]}`, http.StatusCreated)
	catalogCall(t, router, http.MethodPost, "/tratamientos", `{"id":"search-facial-2","nombre":"Hidratación facial","descripcion":"Tratamiento de piel","categoria":"Facial","precio":50,"moneda":"ARS","duracionMinutos":45,"keywords":["hidratación"]}`, http.StatusCreated)
	catalogCall(t, router, http.MethodPost, "/tratamientos", `{"id":"search-body","nombre":"Masaje corporal","descripcion":"Relajación","categoria":"Corporal","precio":20,"moneda":"ARS","duracionMinutos":45,"keywords":["piel"]}`, http.StatusCreated)
	catalogCall(t, router, http.MethodPost, "/profesionales", `{"id":"search-person","nombre":"Profesional buscable"}`, http.StatusCreated)
	catalogCall(t, router, http.MethodPost, "/profesionales", `{"id":"search-inactive-person","nombre":"Profesional inactivo","activo":false}`, http.StatusCreated)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.rebuildSearchIndices(ctx); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}

	before := s.cache.stats()
	first := catalogRequest(router, http.MethodGet, "/tratamientos?q=piel&categoria=facial&precioMax=150&sort=precio&order=asc&limit=1&page=1", "")
	if first.Code != http.StatusOK {
		t.Fatalf("search: status=%d body=%s", first.Code, first.Body.String())
	}
	var result struct {
		Treatments []Tratamiento `json:"tratamientos"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Pagination.Total != 2 || len(result.Treatments) != 1 || result.Treatments[0].ID != "search-facial-2" {
		t.Fatalf("filter/order/page mismatch: %#v", result)
	}
	searching := s.search
	brokenRebuild := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "offline", http.StatusServiceUnavailable) }))
	s.search = newSearchClient(brokenRebuild.URL)
	if err := s.rebuildSearchIndices(ctx); err == nil {
		t.Fatal("expected failed generation rebuild")
	}
	brokenRebuild.Close()
	s.search = searching
	retained := catalogRequest(router, http.MethodGet, "/tratamientos?q=sensible", "")
	if retained.Code != http.StatusOK || !strings.Contains(retained.Body.String(), "search-facial-1") {
		t.Fatalf("failed rebuild replaced the last valid index: %s", retained.Body.String())
	}
	second := catalogRequest(router, http.MethodGet, "/tratamientos?q=piel&categoria=facial&precioMax=150&sort=precio&order=asc&limit=1&page=1", "")
	if second.Code != http.StatusOK {
		t.Fatal(second.Body.String())
	}
	stats := s.cache.stats()
	if stats["misses"] <= before["misses"] || stats["hits"] <= before["hits"] {
		t.Fatalf("expected cache miss and hit, before=%v after=%v", before, stats)
	}
	page2 := catalogRequest(router, http.MethodGet, "/tratamientos?q=piel&categoria=facial&precioMax=150&sort=precio&order=asc&limit=1&page=2", "")
	if page2.Code != http.StatusOK || !strings.Contains(page2.Body.String(), "search-facial-1") || s.cache.stats()["misses"] <= stats["misses"] {
		t.Fatalf("pagination key/result mismatch: %s", page2.Body.String())
	}
	peopleDoc, err := s.search.request(ctx, http.MethodGet, "/"+s.search.professionalAlias+"/_doc/search-person", nil)
	if err != nil || !strings.Contains(string(peopleDoc), `"activo":true`) {
		t.Fatalf("active professional missing from search index: %s %v", peopleDoc, err)
	}
	if _, err := s.search.request(ctx, http.MethodGet, "/"+s.search.professionalAlias+"/_doc/search-inactive-person", nil); err == nil {
		t.Fatal("inactive professional must be excluded from search index")
	}

	missesBeforeUpdate := s.cache.stats()["misses"]
	catalogCall(t, router, http.MethodPut, "/tratamientos/search-facial-1", `{"nombre":"Limpieza piel renovada","descripcion":"Cuidado sensible","categoria":"Facial","precio":100,"moneda":"ARS","duracionMinutos":60,"keywords":["piel"]}`, http.StatusOK)
	invalidated := catalogRequest(router, http.MethodGet, "/tratamientos?q=piel&categoria=facial&precioMax=150&sort=precio&order=asc&limit=1&page=1", "")
	if invalidated.Code != http.StatusOK || s.cache.stats()["misses"] <= missesBeforeUpdate {
		t.Fatalf("mutation did not invalidate cached listing: %s", invalidated.Body.String())
	}
	if err := s.rebuildSearchIndices(ctx); err != nil {
		t.Fatalf("rebuild after update: %v", err)
	}
	updated := catalogRequest(router, http.MethodGet, "/tratamientos?q=renovada", "")
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "search-facial-1") {
		t.Fatalf("update not searchable: %s", updated.Body.String())
	}
	catalogCall(t, router, http.MethodDelete, "/tratamientos/search-facial-1", "", http.StatusNoContent)
	if err := s.rebuildSearchIndices(ctx); err != nil {
		t.Fatalf("rebuild after delete: %v", err)
	}
	deleted := catalogRequest(router, http.MethodGet, "/tratamientos?q=renovada", "")
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"total":0`) {
		t.Fatalf("inactive treatment remained indexed: %s", deleted.Body.String())
	}

	key, err := s.cache.key(catalogListQuery{Q: "ttl-proof", Page: 1, Limit: 10, Sort: "nombre", Order: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cache.setRaw(key, []byte("ttl"), time.Second); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, ok, err := s.cache.getRaw(key); err != nil || ok {
		t.Fatalf("TTL not applied: hit=%v err=%v", ok, err)
	}

	brokenSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "offline", http.StatusServiceUnavailable) }))
	defer brokenSearch.Close()
	s.search = newSearchClient(brokenSearch.URL)
	failure := catalogRequest(router, http.MethodGet, "/tratamientos?q=unique-unavailable", "")
	if failure.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected controlled search failure, got %d: %s", failure.Code, failure.Body.String())
	}
	s.cache = newMemcacheClient("127.0.0.1:1")
	fallback := catalogRequest(router, http.MethodGet, "/tratamientos?sort=nombre", "")
	if fallback.Code != http.StatusOK {
		t.Fatalf("Mongo read failed with cache offline: %s", fallback.Body.String())
	}
}

func BenchmarkCatalogListCache(b *testing.B) {
	if strings.TrimSpace(os.Getenv("CATALOGO_TEST_URI")) == "" || strings.TrimSpace(os.Getenv("CATALOGO_TEST_MEMCACHED_ADDRESS")) == "" {
		b.Fatal("set CATALOGO_TEST_URI and CATALOGO_TEST_MEMCACHED_ADDRESS")
	}
	s, router := catalogFixture(b)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	seed := fmt.Sprintf("bench-%d", time.Now().UnixNano())
	docs := make([]any, 100)
	for i := range docs {
		docs[i] = Tratamiento{ID: fmt.Sprintf("%s-%03d", seed, i), Nombre: fmt.Sprintf("Tratamiento de catálogo %03d", i), Descripcion: "Cuidado de piel", Categoria: "Facial", Precio: float64(i + 1), Moneda: "ARS", DuracionMinutos: 30, Keywords: []string{"piel", "cuidado"}, Imagenes: []string{}, ProfesionalesIDs: []string{}}
	}
	if _, err := s.coleccion.InsertMany(ctx, docs); err != nil {
		b.Fatal(err)
	}
	path := "/tratamientos?categoria=Facial&page=1&limit=20&sort=nombre&order=asc"
	b.Run("mongo_uncached", func(b *testing.B) {
		s.cache = nil
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if response := catalogRequest(router, http.MethodGet, path, ""); response.Code != http.StatusOK {
				b.Fatalf("request failed: %s", response.Body.String())
			}
		}
	})
	b.Run("memcached_repeated_hit", func(b *testing.B) {
		s.cache = newMemcacheClient(os.Getenv("CATALOGO_TEST_MEMCACHED_ADDRESS"))
		s.cache.namespace = "melt:catalogo:benchmark"
		if err := s.cache.bumpGeneration(); err != nil {
			b.Fatal(err)
		}
		if response := catalogRequest(router, http.MethodGet, path, ""); response.Code != http.StatusOK {
			b.Fatalf("warm request failed: %s", response.Body.String())
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if response := catalogRequest(router, http.MethodGet, path, ""); response.Code != http.StatusOK {
				b.Fatalf("request failed: %s", response.Body.String())
			}
		}
		b.StopTimer()
		stats := s.cache.stats()
		b.ReportMetric(float64(stats["hits"]), "cache-hit")
		b.ReportMetric(float64(stats["misses"]), "cache-miss")
	})
}
