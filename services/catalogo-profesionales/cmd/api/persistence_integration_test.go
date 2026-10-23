//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// This test requires an existing fixture. It never calls main, creates indexes,
// seeds documents, or cleans up database contents.
func TestPersistenceReadOnly(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	gateway := strings.TrimRight(os.Getenv("GATEWAY_TEST_URL"), "/")
	if uri == "" || gateway == "" {
		t.Fatal("integration requires MONGODB_TEST_URI and GATEWAY_TEST_URL; no data will be created")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal("cannot configure MongoDB test connection")
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := client.Disconnect(closeCtx); err != nil {
			t.Error("cannot close MongoDB test connection")
		}
	})
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal("real MongoDB is unavailable")
	}
	collection := client.Database("catalogo_db").Collection("tratamientos")
	var expected Tratamiento
	if err := collection.FindOne(ctx, bson.D{{Key: "id", Value: "test-001"}}).Decode(&expected); err != nil {
		t.Fatal("existing test-001 fixture cannot be read; obtain authorization before creating data")
	}
	count, err := collection.CountDocuments(ctx, bson.D{})
	if err != nil {
		t.Fatal("cannot count existing treatments")
	}
	assertTreatment := func(t *testing.T, body []byte) {
		t.Helper()
		var actual Tratamiento
		if err := json.Unmarshal(body, &actual); err != nil {
			t.Fatal("response is not a treatment JSON")
		}
		if !reflect.DeepEqual(expected, actual) {
			t.Fatal("response differs from the real MongoDB document")
		}
	}
	t.Run("handler_uses_real_collection", func(t *testing.T) {
		router := crearRouter(&Servidor{coleccion: collection}, func(ctx context.Context) error { return client.Ping(ctx, nil) })
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tratamientos/test-001", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("handler status: %d", response.Code)
		}
		assertTreatment(t, response.Body.Bytes())
	})
	httpClient := &http.Client{Timeout: 10 * time.Second}
	get := func(t *testing.T, path string, result any) {
		t.Helper()
		response, err := httpClient.Get(gateway + path)
		if err != nil {
			t.Fatal("gateway request failed")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("gateway status: %d", response.StatusCode)
		}
		if err := json.NewDecoder(response.Body).Decode(result); err != nil {
			t.Fatal("gateway returned invalid JSON")
		}
	}
	t.Run("gateway_detail_matches_mongodb", func(t *testing.T) {
		var actual json.RawMessage
		get(t, "/api/v1/catalogo/tratamientos/test-001", &actual)
		assertTreatment(t, actual)
	})
	t.Run("gateway_list_reads_real_count", func(t *testing.T) {
		var result struct {
			Tratamientos []Tratamiento `json:"tratamientos"`
			Pagination   struct {
				Total int64 `json:"total"`
			} `json:"pagination"`
		}
		get(t, "/api/v1/catalogo/tratamientos?page=1&limit=10", &result)
		if result.Pagination.Total != count {
			t.Fatal("gateway total differs from MongoDB count")
		}
		if count > 0 && len(result.Tratamientos) == 0 {
			t.Fatal("gateway returned no stored treatments")
		}
	})
}
