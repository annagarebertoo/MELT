//go:build catalogointegration && catalogosearchintegration

package main

// L3 — OpenSearch y Memcached contra contenedores descartables (compose.test.yaml):
// sincronización de índices, filtros y paginación equivalentes a MongoDB,
// caché fría/caliente, invalidación, hit/miss y fallback ante fallos.
// Ejecutar con scripts/test-consistencia-integracion.sh l3.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var hostsCompartidosCatalogo = []string{"mongodb.net", "supabase.co", "supabase.com", "render.com", "onrender.com", "aiven", "bonsai.io", "amazonaws.com"}

func exigirDependenciasDescartables(t *testing.T) (searchURL, cacheAddress string) {
	t.Helper()
	searchURL = strings.TrimSpace(os.Getenv("CATALOGO_TEST_OPENSEARCH_URL"))
	cacheAddress = strings.TrimSpace(os.Getenv("CATALOGO_TEST_MEMCACHED_ADDRESS"))
	if searchURL == "" || cacheAddress == "" {
		t.Fatal("definí CATALOGO_TEST_OPENSEARCH_URL y CATALOGO_TEST_MEMCACHED_ADDRESS (ver scripts/test-consistencia-integracion.sh)")
	}
	for nombre, valor := range map[string]string{"CATALOGO_TEST_URI": os.Getenv("CATALOGO_TEST_URI"), "CATALOGO_TEST_OPENSEARCH_URL": searchURL, "CATALOGO_TEST_MEMCACHED_ADDRESS": cacheAddress} {
		host := strings.ToLower(valor)
		if parsed, err := url.Parse(valor); err == nil && parsed.Host != "" {
			host = strings.ToLower(parsed.Hostname())
		}
		for _, prohibido := range hostsCompartidosCatalogo {
			if strings.Contains(host, prohibido) {
				t.Fatalf("%s apunta a un entorno compartido (%s); usá compose.test.yaml", nombre, prohibido)
			}
		}
	}
	return searchURL, cacheAddress
}

// fixtureBusqueda arma Catálogo con alias e índices propios y un namespace de
// caché exclusivo, para que las pruebas no compartan estado entre sí.
func fixtureBusqueda(t *testing.T) (*Servidor, *gin.Engine) {
	t.Helper()
	searchURL, cacheAddress := exigirDependenciasDescartables(t)
	s, router := catalogFixture(t)
	sufijo := fmt.Sprintf("l3-%d", time.Now().UnixNano())
	s.search = newSearchClient(searchURL)
	s.search.treatmentAlias = "melt-" + sufijo + "-tratamientos-read"
	s.search.professionalAlias = "melt-" + sufijo + "-profesionales-read"
	s.cache = newMemcacheClient(cacheAddress)
	s.cache.namespace = "melt:catalogo:" + sufijo
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cliente := newSearchClient(searchURL)
		_ = cliente.deleteAliasAndIndices(ctx, "melt-"+sufijo+"-tratamientos-read", "melt-"+sufijo+"-tratamientos-*")
		_ = cliente.deleteAliasAndIndices(ctx, "melt-"+sufijo+"-profesionales-read", "melt-"+sufijo+"-profesionales-*")
	})
	return s, router
}

func reindexar(t *testing.T, s *Servidor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.rebuildSearchIndices(ctx); err != nil {
		t.Fatalf("reconstrucción del índice: %v", err)
	}
}

type paginaCatalogo struct {
	Tratamientos []Tratamiento `json:"tratamientos"`
	Pagination   struct {
		Page       int   `json:"page"`
		Total      int64 `json:"total"`
		TotalPages int64 `json:"totalPages"`
	} `json:"pagination"`
}

func listar(t *testing.T, router http.Handler, path string) (paginaCatalogo, string) {
	t.Helper()
	res := catalogRequest(router, http.MethodGet, path, "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, res.Code, res.Body.String())
	}
	var page paginaCatalogo
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page, res.Body.String()
}

func idsDe(page paginaCatalogo) []string {
	ids := make([]string, 0, len(page.Tratamientos))
	for _, item := range page.Tratamientos {
		ids = append(ids, item.ID)
	}
	return ids
}

func sembrarCatalogo(t *testing.T, router http.Handler) {
	t.Helper()
	type semilla struct {
		id, nombre, descripcion, categoria string
		precio                             float64
	}
	semillas := []semilla{}
	for i := 0; i < 9; i++ {
		semillas = append(semillas, semilla{fmt.Sprintf("l3-facial-%02d", i), fmt.Sprintf("Facial basico %02d", i), "limpieza de piel profunda", "Facial", float64(100 + 25*i)})
	}
	for i := 0; i < 5; i++ {
		semillas = append(semillas, semilla{fmt.Sprintf("l3-premium-%02d", i), fmt.Sprintf("Facial premium %02d", i), "piel luminosa con serum", "Facial Premium", float64(150 + 50*i)})
	}
	for i := 0; i < 6; i++ {
		semillas = append(semillas, semilla{fmt.Sprintf("l3-corporal-%02d", i), fmt.Sprintf("Masaje corporal %02d", i), "relajacion muscular y piel", "Corporal", 200})
	}
	catalogCall(t, router, http.MethodPost, "/categorias", `{"id":"facial-premium","nombre":"Facial Premium"}`, http.StatusCreated)
	for _, item := range semillas {
		body := fmt.Sprintf(`{"id":%q,"nombre":%q,"descripcion":%q,"categoria":%q,"precio":%v,"moneda":"ARS","duracionMinutos":45,"keywords":["estetica"]}`, item.id, item.nombre, item.descripcion, item.categoria, item.precio)
		catalogCall(t, router, http.MethodPost, "/tratamientos", body, http.StatusCreated)
	}
}

func TestL3FiltrosYPaginacionDeOpenSearchCoincidenConMongo(t *testing.T) {
	s, router := fixtureBusqueda(t)
	sembrarCatalogo(t, router)
	reindexar(t, s)
	cache, search := s.cache, s.search
	s.cache = nil // compara fuentes, no respuestas cacheadas
	consultas := []string{
		"q=piel",
		"q=piel&categoria=Facial",
		"q=piel&categoria=facial",
		"q=piel&categoria=Facial%20Premium",
		"q=piel&precioMax=200",
		"q=piel&precioMax=199.99",
		"q=piel&categoria=Corporal&sort=precio&order=desc",
		"q=piel&sort=precio&order=asc",
		"q=piel&sort=precio&order=desc",
		"q=piel&sort=nombre&order=desc",
		"q=serum",
		"q=inexistente",
	}
	for _, consulta := range consultas {
		for _, limite := range []int{4, 7} {
			var desdeBusqueda, desdeMongo []string
			var totalBusqueda, totalMongo int64
			for _, fuente := range []string{"opensearch", "mongo"} {
				if fuente == "mongo" {
					s.search = nil
				} else {
					s.search = search
				}
				vistos := map[string]bool{}
				ids := []string{}
				var total int64
				for pagina := 1; ; pagina++ {
					page, _ := listar(t, router, fmt.Sprintf("/tratamientos?%s&limit=%d&page=%d", consulta, limite, pagina))
					total = page.Pagination.Total
					if len(page.Tratamientos) == 0 {
						break
					}
					for _, id := range idsDe(page) {
						if vistos[id] {
							t.Fatalf("%s (%s, limit=%d): %s aparece en más de una página", consulta, fuente, limite, id)
						}
						vistos[id] = true
						ids = append(ids, id)
					}
					if pagina > 30 {
						t.Fatal("paginación sin fin")
					}
				}
				if int64(len(ids)) != total {
					t.Fatalf("%s (%s, limit=%d): las páginas suman %d elementos pero total=%d", consulta, fuente, limite, len(ids), total)
				}
				if fuente == "opensearch" {
					desdeBusqueda, totalBusqueda = ids, total
				} else {
					desdeMongo, totalMongo = ids, total
				}
			}
			if totalBusqueda != totalMongo || strings.Join(desdeBusqueda, ",") != strings.Join(desdeMongo, ",") {
				t.Fatalf("%s (limit=%d): OpenSearch y MongoDB difieren\nopensearch(%d)=%v\nmongo(%d)=%v", consulta, limite, totalBusqueda, desdeBusqueda, totalMongo, desdeMongo)
			}
		}
	}
	s.search, s.cache = search, cache
	if s.searchFailures.Load() != 0 {
		t.Fatalf("hubo %d fallos de búsqueda: la comparación usó el fallback", s.searchFailures.Load())
	}
}

func TestL3IndiceSeSincronizaConAltasCambiosYBajas(t *testing.T) {
	s, router := fixtureBusqueda(t)
	catalogCall(t, router, http.MethodPost, "/tratamientos", `{"id":"l3-sync","nombre":"Peeling enzimatico","descripcion":"renovacion","categoria":"Facial","precio":300,"moneda":"ARS","duracionMinutos":30,"keywords":["exfoliacion"]}`, http.StatusCreated)
	catalogCall(t, router, http.MethodPost, "/profesionales", `{"id":"l3-prof","nombre":"Profesional sincronizada"}`, http.StatusCreated)
	if s.searchIndexFresh() {
		t.Fatal("sin una reconstrucción exitosa el índice no puede considerarse fresco")
	}
	// Sin índice fresco la búsqueda usa MongoDB y ya ve el alta.
	if page, _ := listar(t, router, "/tratamientos?q=exfoliacion"); page.Pagination.Total != 1 {
		t.Fatalf("fallback sin índice: %+v", page)
	}
	antes := time.Now().Add(-time.Second)
	reindexar(t, s)
	if !s.searchIndexFresh() || time.Unix(s.lastIndexSuccess.Load(), 0).Before(antes) {
		t.Fatal("la reconstrucción no actualizó la marca de frescura")
	}
	var control struct {
		LastSuccessAt time.Time `bson:"lastSuccessAt"`
	}
	if err := s.control.FindOne(context.Background(), bson.M{"_id": "opensearch_indexer"}).Decode(&control); err != nil || control.LastSuccessAt.Before(antes) {
		t.Fatalf("la marca compartida de frescura no se guardó: %+v %v", control, err)
	}
	if page, _ := listar(t, router, "/tratamientos?q=exfoliacion"); page.Pagination.Total != 1 || page.Tratamientos[0].ID != "l3-sync" {
		t.Fatalf("alta no indexada: %+v", page)
	}
	ctx := context.Background()
	doc, err := s.search.request(ctx, http.MethodGet, "/"+s.search.professionalAlias+"/_doc/l3-prof", nil)
	if err != nil || !strings.Contains(string(doc), `"found":true`) {
		t.Fatalf("profesional activa ausente del índice: %s %v", doc, err)
	}

	catalogCall(t, router, http.MethodPut, "/tratamientos/l3-sync", `{"nombre":"Peeling quimico","descripcion":"renovacion","categoria":"Facial","precio":320,"moneda":"ARS","duracionMinutos":30,"keywords":["acidos"]}`, http.StatusOK)
	reindexar(t, s)
	if page, _ := listar(t, router, "/tratamientos?q=exfoliacion"); page.Pagination.Total != 0 {
		t.Fatalf("el término anterior siguió indexado: %+v", page)
	}
	if page, _ := listar(t, router, "/tratamientos?q=acidos"); page.Pagination.Total != 1 || page.Tratamientos[0].Precio != 320 {
		t.Fatalf("cambio no indexado: %+v", page)
	}

	catalogCall(t, router, http.MethodDelete, "/tratamientos/l3-sync", "", http.StatusNoContent)
	catalogCall(t, router, http.MethodPut, "/profesionales/l3-prof", `{"nombre":"Profesional sincronizada","activo":false}`, http.StatusOK)
	reindexar(t, s)
	if page, _ := listar(t, router, "/tratamientos?q=acidos"); page.Pagination.Total != 0 {
		t.Fatalf("baja todavía indexada: %+v", page)
	}
	if _, err := s.search.request(ctx, http.MethodGet, "/"+s.search.professionalAlias+"/_doc/l3-prof", nil); err == nil {
		t.Fatal("profesional inactiva siguió en el índice")
	}
	// Las reconstrucciones no acumulan índices viejos.
	data, err := s.search.request(ctx, http.MethodGet, "/_cat/indices/"+strings.TrimSuffix(s.search.treatmentAlias, "-read")+"-*?format=json&h=index", nil)
	var indices []map[string]string
	if err != nil || json.Unmarshal(data, &indices) != nil || len(indices) != 1 {
		t.Fatalf("índices de tratamientos tras 3 reconstrucciones: %s %v", data, err)
	}
}

func TestL3SoloUnaReplicaReconstruyeElIndiceALaVez(t *testing.T) {
	s, _ := fixtureBusqueda(t)
	resultados := make([]bool, 8)
	duenos := make([]string, 8)
	var wg sync.WaitGroup
	for i := range resultados {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, owner, err := s.acquireIndexerLease(context.Background())
			if err != nil {
				t.Error(err)
			}
			resultados[i], duenos[i] = ok, owner
		}(i)
	}
	wg.Wait()
	ganadores := 0
	for i, ok := range resultados {
		if ok {
			ganadores++
			s.releaseIndexerLease(duenos[i])
		}
	}
	if ganadores != 1 {
		t.Fatalf("réplicas que obtuvieron el lease simultáneamente=%d", ganadores)
	}
	if ok, owner, err := s.acquireIndexerLease(context.Background()); err != nil || !ok {
		t.Fatalf("el lease liberado no pudo volver a tomarse: %v %v", ok, err)
	} else {
		s.releaseIndexerLease(owner)
	}
}

func TestL3CacheFriaYCalienteConHitMissYTiempos(t *testing.T) {
	s, router := fixtureBusqueda(t)
	sembrarCatalogo(t, router)
	reindexar(t, s)
	for _, path := range []string{"/tratamientos?categoria=Corporal&sort=precio&limit=3", "/tratamientos?q=piel&categoria=Facial&limit=5&page=2"} {
		antes := s.cache.stats()
		inicio := time.Now()
		_, fria := listar(t, router, path)
		tiempoFrio := time.Since(inicio)
		despuesFria := s.cache.stats()
		if despuesFria["misses"] != antes["misses"]+1 || despuesFria["hits"] != antes["hits"] {
			t.Fatalf("%s: la primera lectura debía ser un miss: antes=%v después=%v", path, antes, despuesFria)
		}
		const lecturas = 20
		inicio = time.Now()
		for i := 0; i < lecturas; i++ {
			if _, caliente := listar(t, router, path); caliente != fria {
				t.Fatalf("%s: la respuesta cacheada difiere de la original", path)
			}
		}
		tiempoCaliente := time.Since(inicio) / lecturas
		despuesCaliente := s.cache.stats()
		if despuesCaliente["hits"] != despuesFria["hits"]+lecturas || despuesCaliente["misses"] != despuesFria["misses"] {
			t.Fatalf("%s: se esperaban %d hits sin misses: %v -> %v", path, lecturas, despuesFria, despuesCaliente)
		}
		t.Logf("%s: fría=%s caliente(promedio)=%s", path, tiempoFrio, tiempoCaliente)
	}
	// Otra página u otro filtro es otra clave.
	antes := s.cache.stats()
	listar(t, router, "/tratamientos?categoria=Corporal&sort=precio&limit=3&page=2")
	if s.cache.stats()["misses"] != antes["misses"]+1 {
		t.Fatal("una página distinta reutilizó la clave de otra")
	}
	// Una respuesta caliente se sirve sin consultar MongoDB.
	if _, err := s.coleccion.DeleteMany(context.Background(), bson.M{"categoria": "Corporal"}); err != nil {
		t.Fatal(err)
	}
	if page, _ := listar(t, router, "/tratamientos?categoria=Corporal&sort=precio&limit=3"); page.Pagination.Total != 6 {
		t.Fatalf("la lectura caliente debía venir de Memcached: %+v", page.Pagination)
	}
	if err := s.cache.bumpGeneration(); err != nil {
		t.Fatal(err)
	}
	if page, _ := listar(t, router, "/tratamientos?categoria=Corporal&sort=precio&limit=3"); page.Pagination.Total != 0 {
		t.Fatalf("tras invalidar se siguió sirviendo la entrada vieja: %+v", page.Pagination)
	}
}

func TestL3LecturasConcurrentesEnFrioSonConsistentes(t *testing.T) {
	s, router := fixtureBusqueda(t)
	sembrarCatalogo(t, router)
	reindexar(t, s)
	const lectores = 16
	cuerpos := make([]string, lectores)
	var wg sync.WaitGroup
	for i := 0; i < lectores; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res := catalogRequest(router, http.MethodGet, "/tratamientos?q=piel&sort=precio&limit=5", "")
			if res.Code == http.StatusOK {
				cuerpos[i] = res.Body.String()
			}
		}(i)
	}
	wg.Wait()
	for i := range cuerpos {
		if cuerpos[i] == "" || cuerpos[i] != cuerpos[0] {
			t.Fatalf("lecturas concurrentes en frío devolvieron respuestas distintas o fallidas")
		}
	}
	antes := s.cache.stats()
	listar(t, router, "/tratamientos?q=piel&sort=precio&limit=5")
	if s.cache.stats()["hits"] != antes["hits"]+1 {
		t.Fatal("tras el arranque en frío la entrada no quedó cacheada")
	}
}

func TestL3MutacionInvalidaLaCacheDeListados(t *testing.T) {
	s, router := fixtureBusqueda(t)
	sembrarCatalogo(t, router)
	path := "/tratamientos?categoria=Facial&sort=precio&order=desc&limit=2"
	listar(t, router, path)
	if page, _ := listar(t, router, path); page.Tratamientos[0].ID != "l3-facial-08" {
		t.Fatalf("orden inicial inesperado: %v", idsDe(page))
	}
	catalogCall(t, router, http.MethodPut, "/tratamientos/l3-facial-00", `{"nombre":"Facial basico 00","descripcion":"limpieza de piel profunda","categoria":"Facial","precio":9999,"moneda":"ARS","duracionMinutos":45,"keywords":["estetica"]}`, http.StatusOK)
	antes := s.cache.stats()
	page, _ := listar(t, router, path)
	if page.Tratamientos[0].ID != "l3-facial-00" || s.cache.stats()["misses"] != antes["misses"]+1 {
		t.Fatalf("la edición no invalidó el listado cacheado: %v", idsDe(page))
	}
	catalogCall(t, router, http.MethodDelete, "/tratamientos/l3-facial-00", "", http.StatusNoContent)
	if page, _ := listar(t, router, path); page.Tratamientos[0].ID == "l3-facial-00" || page.Pagination.Total != 8 {
		t.Fatalf("la baja no invalidó el listado cacheado: %v total=%d", idsDe(page), page.Pagination.Total)
	}
}

func TestL3EscrituraTardiaNoCacheaDatosViejosTrasInvalidar(t *testing.T) {
	s, _ := fixtureBusqueda(t)
	ctx := context.Background()
	query := catalogListQuery{Category: "Facial", Page: 1, Limit: 10, Sort: "nombre", Order: "asc"}
	// Lector lento: miss, lee la base, y antes de guardar ocurre una edición que invalida.
	if _, generacion, ok := s.cache.get(ctx, query); ok {
		t.Fatal("namespace nuevo con hit")
	} else {
		if err := s.cache.bumpGeneration(); err != nil {
			t.Fatal(err)
		}
		s.cache.set(ctx, query, generacion, []byte(`{"version":"vieja"}`))
	}
	if valor, _, ok := s.cache.get(ctx, query); ok {
		t.Fatalf("se sirvió un listado leído antes de la invalidación: %s", valor)
	}
}

func borrarClaveMemcached(t *testing.T, address, key string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "delete %s\r\n", key)
	if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
}

func TestL3EvictionDeLaGeneracionNoResucitaEntradasViejas(t *testing.T) {
	s, router := fixtureBusqueda(t)
	sembrarCatalogo(t, router)
	path := "/tratamientos?categoria=Corporal&limit=10"
	// Memcached recién iniciado: todavía no existe la clave de generación.
	borrarClaveMemcached(t, s.cache.address, s.cache.namespace+":generation")
	listar(t, router, path) // cacheado con la generación inicial
	catalogCall(t, router, http.MethodDelete, "/tratamientos/l3-corporal-00", "", http.StatusNoContent)
	// Memcached expulsa la clave de generación (LRU o reinicio parcial).
	borrarClaveMemcached(t, s.cache.address, s.cache.namespace+":generation")
	if page, _ := listar(t, router, path); page.Pagination.Total != 5 {
		t.Fatalf("una entrada anterior a la invalidación volvió a servirse: total=%d", page.Pagination.Total)
	}
}

func TestL3FallbackSinMemcachedYRecuperacion(t *testing.T) {
	s, router := fixtureBusqueda(t)
	sembrarCatalogo(t, router)
	reindexar(t, s)
	real := s.cache
	s.cache = newMemcacheClient("127.0.0.1:1")
	s.cache.namespace = real.namespace
	for _, path := range []string{"/tratamientos?categoria=Facial&limit=3", "/tratamientos?q=serum&limit=3"} {
		inicio := time.Now()
		page, _ := listar(t, router, path)
		if page.Pagination.Total == 0 {
			t.Fatalf("%s sin resultados con Memcached caído", path)
		}
		if demora := time.Since(inicio); demora > 2*time.Second {
			t.Fatalf("%s: Memcached caído agregó %s de latencia", path, demora)
		}
	}
	if stats := s.cache.stats(); stats["errors"] == 0 || stats["hits"] != 0 {
		t.Fatalf("los errores de Memcached no quedaron contabilizados: %v", stats)
	}
	s.cache = real
	listar(t, router, "/tratamientos?categoria=Facial&limit=3")
	antes := s.cache.stats()
	listar(t, router, "/tratamientos?categoria=Facial&limit=3")
	if s.cache.stats()["hits"] != antes["hits"]+1 {
		t.Fatal("la caché no volvió a usarse tras recuperarse Memcached")
	}
}

func TestL3FallbackSinOpenSearchUsaMongoYNoCacheaResultadosDegradados(t *testing.T) {
	s, router := fixtureBusqueda(t)
	sembrarCatalogo(t, router)
	reindexar(t, s)
	_, esperado := listar(t, router, "/tratamientos?q=serum&sort=precio&limit=3")
	real := s.search
	caido := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "caido", http.StatusServiceUnavailable)
	}))
	defer caido.Close()
	s.search = newSearchClient(caido.URL)
	s.search.treatmentAlias, s.search.professionalAlias = real.treatmentAlias, real.professionalAlias
	if err := s.cache.bumpGeneration(); err != nil {
		t.Fatal(err)
	}
	fallos := s.searchFailures.Load()
	page, degradado := listar(t, router, "/tratamientos?q=serum&sort=precio&limit=3")
	if page.Pagination.Total != 5 || s.searchFailures.Load() != fallos+1 {
		t.Fatalf("el fallback a MongoDB falló: %+v fallos=%d", page.Pagination, s.searchFailures.Load()-fallos)
	}
	if degradado != esperado {
		t.Fatalf("el fallback devolvió otro resultado\nopensearch=%s\nmongo=%s", esperado, degradado)
	}
	// Un índice vencido tampoco se usa, ni su caché.
	s.search = real
	s.lastIndexSuccess.Store(time.Now().Add(-2 * searchFreshnessTarget).Unix())
	if _, err := s.control.UpdateOne(context.Background(), bson.M{"_id": "opensearch_indexer"}, bson.M{"$set": bson.M{"lastSuccessAt": time.Now().Add(-2 * searchFreshnessTarget)}}); err != nil {
		t.Fatal(err)
	}
	antes := s.cache.stats()
	listar(t, router, "/tratamientos?q=serum&sort=precio&limit=3")
	if despues := s.cache.stats(); despues["hits"] != antes["hits"] {
		t.Fatal("con índice vencido se sirvió una búsqueda cacheada")
	}
	// Recuperación: tras reindexar vuelve a usarse OpenSearch y la caché.
	reindexar(t, s)
	fallos = s.searchFailures.Load()
	listar(t, router, "/tratamientos?q=serum&sort=precio&limit=3")
	antes = s.cache.stats()
	if _, body := listar(t, router, "/tratamientos?q=serum&sort=precio&limit=3"); body != esperado || s.cache.stats()["hits"] != antes["hits"]+1 || s.searchFailures.Load() != fallos {
		t.Fatal("la búsqueda no se recuperó tras volver OpenSearch")
	}
}
