//go:build catalogointegration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestAuditDurationMatchesReservas(t *testing.T) {
	_, r := catalogFixture(t)
	for _, minutes := range []int{1, 4, 5, 480, 481, 1440} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			body := strings.Replace(treatmentJSON(fmt.Sprintf("duration-%d", minutes), "Facial"), `"duracionMinutos":30`, fmt.Sprintf(`"duracionMinutos":%d`, minutes), 1)
			status := 201
			if minutes < 5 || minutes > 480 {
				status = 400
			}
			catalogCall(t, r, "POST", "/tratamientos", body, status)
		})
	}
}
func TestAuditLegacyIDsAndArrays(t *testing.T) {
	s, r := catalogFixture(t)
	ctx := context.Background()
	// The old API allowed non-ASCII IDs and dots. Missing optional arrays are valid legacy storage.
	_, err := s.coleccion.InsertOne(ctx, bson.M{"id": "trat.v1-á", "nombre": "Anterior", "descripcion": "Anterior", "categoria": "Facial", "precio": 100, "moneda": "ARS", "duracionMinutos": 30})
	if err != nil {
		t.Fatal(err)
	}
	item := catalogCall(t, r, "GET", "/tratamientos/trat.v1-á", "", 200)
	for _, field := range []string{"imagenes", "keywords", "profesionalesIds"} {
		assertIDs(t, item, field)
	}
	list := catalogCall(t, r, "GET", "/tratamientos", "", 200)["tratamientos"].([]any)
	for _, field := range []string{"imagenes", "keywords", "profesionalesIds"} {
		assertIDs(t, list[0].(map[string]any), field)
	}
	catalogCall(t, r, "PUT", "/tratamientos/trat.v1-á", treatmentJSON("trat.v1-á", "Facial"), 200)
	_, err = s.profesionales.InsertOne(ctx, bson.M{"id": "prof.v1-á", "nombre": "Anterior", "activo": true})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, catalogCall(t, r, "GET", "/profesionales/prof.v1-á", "", 200), "tratamientosIds")
	people := catalogCall(t, r, "GET", "/profesionales", "", 200)["profesionales"].([]any)
	assertIDs(t, people[0].(map[string]any), "tratamientosIds")
	catalogCall(t, r, "PUT", "/tratamientos/trat.v1-á", treatmentJSON("trat.v1-á", "Facial", "prof.v1-á"), 200)
	catalogCall(t, r, "DELETE", "/tratamientos/trat.v1-á", "", 204)
}
func TestAuditReciprocalAssociationLimits(t *testing.T) {
	for _, direction := range []string{"professional-to-treatment", "treatment-to-professional"} {
		t.Run(direction, func(t *testing.T) {
			s, r := catalogFixture(t)
			ctx := context.Background()
			ids := []string{}
			for i := 0; i < 200; i++ {
				ids = append(ids, fmt.Sprintf("existing-%03d", i))
			}
			if direction == "professional-to-treatment" {
				catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("full", "Facial"), 201)
				_, err := s.coleccion.UpdateOne(ctx, bson.M{"id": "full"}, bson.M{"$set": bson.M{"profesionalesIds": ids}})
				if err != nil {
					t.Fatal(err)
				}
				catalogCall(t, r, "POST", "/profesionales", `{"id":"new","nombre":"Nuevo","tratamientosIds":["full"]}`, 409)
				catalogCall(t, r, "GET", "/profesionales/new", "", 404)
				assertIDs(t, catalogCall(t, r, "GET", "/tratamientos/full", "", 200), "profesionalesIds", ids...)
			} else {
				catalogCall(t, r, "POST", "/profesionales", `{"id":"full","nombre":"Completo"}`, 201)
				_, err := s.profesionales.UpdateOne(ctx, bson.M{"id": "full"}, bson.M{"$set": bson.M{"tratamientosIds": ids}})
				if err != nil {
					t.Fatal(err)
				}
				catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("new", "Facial", "full"), 409)
				catalogCall(t, r, "GET", "/tratamientos/new", "", 404)
				assertIDs(t, catalogCall(t, r, "GET", "/profesionales/full", "", 200), "tratamientosIds", ids...)
			}
		})
	}
}
func TestAuditDeactivateReactivateAndCategoryFilter(t *testing.T) {
	_, r := catalogFixture(t)
	catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("t1", "Facial"), 201)
	catalogCall(t, r, "POST", "/profesionales", `{"id":"p1","nombre":"Uno","tratamientosIds":["t1"]}`, 201)
	catalogCall(t, r, "PUT", "/profesionales/p1", `{"nombre":"Uno","activo":false}`, 200)
	assertIDs(t, catalogCall(t, r, "GET", "/tratamientos/t1", "", 200), "profesionalesIds")
	catalogCall(t, r, "GET", "/profesionales/p1", "", 404)
	catalogCall(t, r, "PUT", "/profesionales/p1", `{"nombre":"Uno","activo":true,"tratamientosIds":["t1"]}`, 200)
	assertIDs(t, catalogCall(t, r, "GET", "/tratamientos/t1", "", 200), "profesionalesIds", "p1")
	people := catalogCall(t, r, "GET", "/profesionales?tratamientoId=t1", "", 200)["profesionales"].([]any)
	if len(people) != 1 {
		t.Fatal(people)
	}
	for _, name := range []string{"facial", "Facial"} {
		data := catalogCall(t, r, "GET", "/tratamientos?categoria="+name, "", 200)
		if len(data["tratamientos"].([]any)) != 1 {
			t.Fatal(data)
		}
	}
	catalogCall(t, r, "DELETE", "/tratamientos/t1", "", 204)
	assertIDs(t, catalogCall(t, r, "GET", "/profesionales/p1", "", 200), "tratamientosIds")
	data := catalogCall(t, r, "GET", "/tratamientos", "", 200)
	if len(data["tratamientos"].([]any)) != 0 || data["pagination"].(map[string]any)["total"] != float64(0) {
		t.Fatal(data)
	}
}
func TestAuditContractFixtureExport(t *testing.T) {
	s, r := catalogFixture(t)
	p := catalogCall(t, r, "POST", "/profesionales", `{"id":"p1","nombre":"Profesional"}`, 201)
	tr := catalogCall(t, r, "POST", "/tratamientos", treatmentJSON("t1", "Facial", "p1"), 201)
	cat := catalogCall(t, r, "GET", "/categorias/facial", "", 200)
	// Include pre-existing documents without arrays, not only our new write format.
	_, err := s.coleccion.InsertOne(context.Background(), bson.M{"id": "legacy", "nombre": "Legacy", "descripcion": "Anterior", "categoria": "Facial", "precio": 1, "moneda": "ARS", "duracionMinutos": 30})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	add := func(contract, schema string, response any) {
		rows = append(rows, map[string]any{"contract": contract, "schema": schema, "response": response})
	}
	add("catalogo-admin-v1.openapi.json", "Profesional", p)
	add("catalogo-admin-v1.openapi.json", "Tratamiento", tr)
	add("catalogo-admin-v1.openapi.json", "Categoria", cat)
	add("tratamientos-v1.openapi.yaml", "Tratamiento", catalogCall(t, r, "GET", "/tratamientos/legacy", "", 200))
	add("tratamientos-v1.openapi.yaml", "TratamientosResponse", catalogCall(t, r, "GET", "/tratamientos", "", 200))
	add("tratamientos-v1.openapi.yaml", "Profesional", catalogCall(t, r, "GET", "/profesionales/p1", "", 200))
	add("catalogo-admin-v1.openapi.json", "Error", catalogCall(t, r, "DELETE", "/categorias/facial", "", 409))
	if file := os.Getenv("CATALOGO_CONTRACT_RESULTS"); file != "" {
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
