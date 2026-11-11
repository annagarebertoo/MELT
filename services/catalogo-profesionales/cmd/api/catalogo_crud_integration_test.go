//go:build catalogointegration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func catalogFixture(t testing.TB) (*Servidor, *gin.Engine) {
	t.Helper()
	uri := os.Getenv("CATALOGO_TEST_URI")
	if uri == "" {
		t.Fatal("Set CATALOGO_TEST_URI to an isolated MongoDB replica set")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("sofi_catalogo_test_%d", time.Now().UnixNano())
	db := client.Database(name)
	pdb := client.Database(name + "_profesionales")
	s := &Servidor{coleccion: db.Collection("tratamientos"), profesionales: pdb.Collection("profesionales"), categorias: db.Collection("categorias"), control: db.Collection("control_catalogo")}
	if err = s.inicializarCatalogo(ctx); err != nil {
		t.Fatal(err)
	}
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test-admin" {
			w.WriteHeader(204)
		} else {
			w.WriteHeader(403)
		}
	}))
	s.authVerifyAdminURL = auth.URL
	t.Cleanup(func() {
		auth.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.Drop(ctx); err != nil {
			t.Error(err)
		}
		if err := pdb.Drop(ctx); err != nil {
			t.Error(err)
		}
		_ = client.Disconnect(ctx)
	})
	return s, crearRouter(s, func(ctx context.Context) error { return client.Ping(ctx, nil) })
}
func catalogRequest(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func catalogCall(t *testing.T, r http.Handler, method, path, body string, status int) map[string]any {
	t.Helper()
	w := catalogRequest(r, method, path, body)
	if w.Code != status {
		t.Fatalf("%s %s: want %d got %d %s", method, path, status, w.Code, w.Body.String())
	}
	var data map[string]any
	if status != 204 {
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
	}
	return data
}
func treatmentJSON(id, category string, professionals ...string) string {
	p, _ := json.Marshal(professionals)
	if len(professionals) == 0 {
		p = []byte("[]")
	}
	return fmt.Sprintf(`{"id":%q,"nombre":"Tratamiento %s","descripcion":"Descripción","categoria":%q,"precio":100,"moneda":"ARS","duracionMinutos":30,"keywords":["piel"],"profesionalesIds":%s}`, id, id, category, p)
}
func assertIDs(t *testing.T, data map[string]any, field string, want ...string) {
	t.Helper()
	values, ok := data[field].([]any)
	if !ok || len(values) != len(want) {
		t.Fatalf("%s=%v want %v", field, data[field], want)
	}
	for _, v := range want {
		found := false
		for _, value := range values {
			if value == v {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s missing %s", field, v)
		}
	}
}
func TestCatalogCRUDMongo(t *testing.T) {
	s, r := catalogFixture(t)
	cats := catalogCall(t, r, "GET", "/categorias", "", 200)
	if len(cats["categorias"].([]any)) != 4 {
		t.Fatal(cats)
	}
	catalogCall(t, r, "POST", "/categorias", `{"id":"capilar","nombre":"Capilar"}`, 201)
	catalogCall(t, r, "POST", "/categorias", `{"id":"otra","nombre":"CAPILAR"}`, 409)
	catalogCall(t, r, "POST", "/profesionales", `{"id":"p1","nombre":"Profesional Uno"}`, 201)
	catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("t1", "Capilar", "p1"), 201)
	assertIDs(t, catalogCall(t, r, "GET", "/profesionales/p1", "", 200), "tratamientosIds", "t1")
	catalogCall(t, r, "DELETE", "/categorias/capilar", "", 409)
	catalogCall(t, r, "PUT", "/categorias/capilar", `{"nombre":"Cuidado capilar","descripcion":"Nueva descripción"}`, 200)
	if catalogCall(t, r, "GET", "/tratamientos/t1", "", 200)["categoria"] != "Cuidado capilar" {
		t.Fatal("category rename did not propagate")
	}
	// Duplicate-name failure must roll back category and treatment changes.
	catalogCall(t, r, "PUT", "/categorias/capilar", `{"nombre":"Facial"}`, 409)
	if catalogCall(t, r, "GET", "/tratamientos/t1", "", 200)["categoria"] != "Cuidado capilar" {
		t.Fatal("rename rollback failed")
	}
	catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("t2", "Facial"), 201)
	catalogCall(t, r, "PUT", "/profesionales/p1", `{"nombre":"Profesional editado","tratamientosIds":["t2","t2"]}`, 200)
	assertIDs(t, catalogCall(t, r, "GET", "/tratamientos/t1", "", 200), "profesionalesIds")
	assertIDs(t, catalogCall(t, r, "GET", "/tratamientos/t2", "", 200), "profesionalesIds", "p1")
	catalogCall(t, r, "PUT", "/tratamientos/t1", treatmentJSON("t1", "Cuidado capilar", "p1"), 200)
	assertIDs(t, catalogCall(t, r, "GET", "/profesionales/p1", "", 200), "tratamientosIds", "t1", "t2")
	// Invalid edit cannot partially clear valid associations.
	catalogCall(t, r, "PUT", "/tratamientos/t1", treatmentJSON("t1", "Cuidado capilar", "missing"), 400)
	assertIDs(t, catalogCall(t, r, "GET", "/profesionales/p1", "", 200), "tratamientosIds", "t1", "t2")
	catalogCall(t, r, "PUT", "/profesionales/p1", `{"nombre":"Uno","tratamientosIds":["missing"]}`, 400)
	catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("t1", "Facial"), 409)
	catalogCall(t, r, "PUT", "/tratamientos/missing", treatmentJSON("missing", "Facial"), 404)
	catalogCall(t, r, "PUT", "/tratamientos/t1", treatmentJSON("other", "Facial"), 400)
	catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("bad", "Unknown"), 404)
	query := catalogCall(t, r, "GET", "/tratamientos?q=piel&precioMax=100&sort=precio&order=desc&page=1&limit=1", "", 200)
	if query["pagination"].(map[string]any)["total"] != float64(2) || len(query["tratamientos"].([]any)) != 1 {
		t.Fatal(query)
	}
	for _, q := range []string{"page=0", "limit=51", "precioMax=NaN", "sort=unknown", "order=invalid"} {
		catalogCall(t, r, "GET", "/tratamientos?"+q, "", 400)
	}
	catalogCall(t, r, "DELETE", "/profesionales/p1", "", 204)
	catalogCall(t, r, "GET", "/profesionales/p1", "", 404)
	assertIDs(t, catalogCall(t, r, "GET", "/tratamientos/t1", "", 200), "profesionalesIds")
	if len(catalogCall(t, r, "GET", "/profesionales?tratamientoId=t1", "", 200)["profesionales"].([]any)) != 0 {
		t.Fatal("inactive professional visible")
	}
	catalogCall(t, r, "POST", "/profesionales", `{"id":"p1","nombre":"Reused"}`, 409)
	catalogCall(t, r, "DELETE", "/tratamientos/t1", "", 204)
	catalogCall(t, r, "GET", "/tratamientos/t1", "", 404)
	catalogCall(t, r, "DELETE", "/tratamientos/t1", "", 404)
	catalogCall(t, r, "DELETE", "/categorias/capilar", "", 204)
	catalogCall(t, r, "GET", "/categorias/capilar", "", 404)
	catalogCall(t, r, "DELETE", "/categorias/missing", "", 404)
	catalogCall(t, r, "PUT", "/categorias/missing", `{"nombre":"Ninguna"}`, 404)
	catalogCall(t, r, "PUT", "/profesionales/missing", `{"nombre":"Ninguno"}`, 404)
	// Tombstones remain in Mongo; reads never return them, restarts never resurrect categories.
	ctx := context.Background()
	var tomb Tratamiento
	if err := s.coleccion.FindOne(ctx, bson.M{"id": "t1"}).Decode(&tomb); err != nil || !tomb.Eliminado {
		t.Fatal("treatment history removed", err)
	}
	if err := s.inicializarCatalogo(ctx); err != nil {
		t.Fatal(err)
	}
	catalogCall(t, r, "GET", "/categorias/capilar", "", 404)
	catalogCall(t, r, "DELETE", "/categorias/corporal", "", 204)
	if err := s.inicializarCatalogo(ctx); err != nil {
		t.Fatal(err)
	}
	catalogCall(t, r, "GET", "/categorias/corporal", "", 404)
}
func TestCatalogTransactionRollback(t *testing.T) {
	s, _ := catalogFixture(t)
	ctx := context.Background()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	if s.mutate(c, func(tx context.Context) error {
		if _, err := s.coleccion.InsertOne(tx, Tratamiento{ID: "rolled-back"}); err != nil {
			return err
		}
		return fail(409, "TEST_ABORT", "Abort controlled transaction")
	}) {
		t.Fatal("must abort")
	}
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	err := s.coleccion.FindOne(ctx, bson.M{"id": "rolled-back"}).Err()
	if !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatal("write survived aborted transaction", err)
	}
}
func TestCatalogConcurrentCategoryDeleteAndCreate(t *testing.T) {
	s, r := catalogFixture(t)
	r2 := crearRouter(s, func(context.Context) error { return nil })
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("race%d", i)
		name := "Race " + id
		catalogCall(t, r, "POST", "/categorias", fmt.Sprintf(`{"id":%q,"nombre":%q}`, id, name), 201)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			codes[0] = catalogRequest(r, "POST", "/tratamientos", treatmentJSON(id, name)).Code
		}()
		go func() { defer wg.Done(); codes[1] = catalogRequest(r2, "DELETE", "/categorias/"+id, "").Code }()
		wg.Wait()
		if !((codes[0] == 201 && codes[1] == 409) || (codes[0] == 404 && codes[1] == 204)) {
			t.Fatalf("nonserializable result: %v", codes)
		}
	}
}
func TestCatalogConcurrentAssociations(t *testing.T) {
	s, r := catalogFixture(t)
	r2 := crearRouter(s, func(context.Context) error { return nil })
	catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("t1", "Facial"), 201)
	catalogCall(t, r, "POST", "/profesionales", `{"id":"p1","nombre":"Uno"}`, 201)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		codes[0] = catalogRequest(r, "PUT", "/tratamientos/t1", treatmentJSON("t1", "Facial", "p1")).Code
	}()
	go func() { defer wg.Done(); codes[1] = catalogRequest(r2, "DELETE", "/profesionales/p1", "").Code }()
	wg.Wait()
	if codes[1] != 204 || (codes[0] != 200 && codes[0] != 400) {
		t.Fatal(codes)
	}
	assertIDs(t, catalogCall(t, r, "GET", "/tratamientos/t1", "", 200), "profesionalesIds")
}
