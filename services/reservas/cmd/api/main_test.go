package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestZonaHorariaReservasDefaultArgentina(t *testing.T) {
	t.Setenv("MELT_TIMEZONE", "")
	loc := zonaHorariaReservas()
	_, offset := time.Now().In(loc).Zone()
	if offset != -3*60*60 {
		t.Fatalf("offset de Argentina = %d, se esperaba UTC-3", offset)
	}
}

func TestDisponibilidadExigeTratamientoYProfesionalCompatible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	catalogo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(tratamientoCatalogo{ID: "facial-01", CategoriaID: "facial", DuracionMinutos: 60, ProfesionalesIDs: []string{"prof-1"}})
	}))
	defer catalogo.Close()
	s := &servidorReservas{catalogoURL: catalogo.URL, httpClient: catalogo.Client()}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("identidad", identidad{ID: "cliente-1", Email: "cliente@example.com", Roles: []string{"cliente"}})
		c.Next()
	})
	router.GET("/disponibilidad", s.disponibilidad)

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/disponibilidad?profesionalId=prof-1&fecha=2026-11-09", nil))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("sin tratamiento: status=%d, se esperaba 400", missing.Code)
	}

	query := url.Values{"profesionalId": {"prof-2"}, "tratamientoId": {"facial-01"}, "fecha": {"2026-11-09"}}
	incompatible := httptest.NewRecorder()
	router.ServeHTTP(incompatible, httptest.NewRequest(http.MethodGet, "/disponibilidad?"+query.Encode(), nil))
	if incompatible.Code != http.StatusUnprocessableEntity {
		t.Fatalf("profesional incompatible: status=%d, se esperaba 422", incompatible.Code)
	}
}

func TestResolverPerfilProfesionalUsaClaveInternaYPerfilDeCatalogo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/profesionales/por-cuenta/auth-123" {
			t.Errorf("unexpected resolver path %q", r.URL.Path)
		}
		if r.Header.Get("X-Melt-Internal-Key") != "test-internal" {
			t.Errorf("internal key missing")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"profesionalId": "catalogo-456"})
	}))
	defer server.Close()
	s := &servidorReservas{catalogoURL: server.URL, internalKey: "test-internal", httpClient: server.Client()}
	profileID, status, err := s.resolverPerfilProfesional(context.Background(), "auth-123")
	if err != nil || status != http.StatusOK || profileID != "catalogo-456" {
		t.Fatalf("resolve result=(%q,%d,%v)", profileID, status, err)
	}
}

func TestZonaHorariaReservasPermiteConfiguracion(t *testing.T) {
	t.Setenv("MELT_TIMEZONE", "UTC")
	loc := zonaHorariaReservas()
	if loc.String() != "UTC" {
		t.Fatalf("zona configurada=%q, se esperaba UTC", loc.String())
	}
}

func TestZonaHorariaReservasValorInvalidoUsaArgentina(t *testing.T) {
	t.Setenv("MELT_TIMEZONE", "zona-que-no-existe")
	loc := zonaHorariaReservas()
	_, offset := time.Now().In(loc).Zone()
	if offset != -3*60*60 {
		t.Fatalf("fallback offset=%d, se esperaba UTC-3", offset)
	}
}

func TestAgendaValida(t *testing.T) {
	cases := []struct {
		name       string
		day        int
		start, end string
		want       bool
	}{
		{"rango válido", 1, "09:00", "17:00", true},
		{"día fuera de rango", 7, "09:00", "17:00", false},
		{"fin igual a inicio", 2, "09:00", "09:00", false},
		{"hora inválida", 2, "25:00", "26:00", false},
		{"fin anterior", 2, "18:00", "09:00", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agendaValida(tc.day, tc.start, tc.end); got != tc.want {
				t.Fatalf("agendaValida()=%v, se esperaba %v", got, tc.want)
			}
		})
	}
}

func TestSlotNoSuperpuesto(t *testing.T) {
	base := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		start, end time.Time
		want       bool
	}{
		{"solapado", base.Add(30 * time.Minute), base.Add(90 * time.Minute), false},
		{"adyacente posterior", base.Add(time.Hour), base.Add(2 * time.Hour), true},
		{"adyacente anterior", base.Add(-time.Hour), base, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := slotNoSuperpuesto(tc.start, tc.end, base, base.Add(time.Hour)); got != tc.want {
				t.Fatalf("slotNoSuperpuesto()=%v, se esperaba %v", got, tc.want)
			}
		})
	}
}

func TestPrimerIntervaloDe15Minutos(t *testing.T) {
	cases := []struct{ inicio, esperado int }{{0, 0}, {540, 540}, {550, 555}, {1439, 1440}}
	for _, tc := range cases {
		if got := primerIntervaloDe15Minutos(tc.inicio); got != tc.esperado {
			t.Fatalf("primerIntervaloDe15Minutos(%d)=%d, esperado %d", tc.inicio, got, tc.esperado)
		}
	}
}

func TestRequiereIdentidadDeniegaAnonimo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/turnos", requiereIdentidad(), requierePermiso("reservas.consultar-turnos"), func(c *gin.Context) { c.Status(http.StatusOK) })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/turnos", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("se esperaba 401 y se obtuvo %d", rec.Code)
	}
}

func TestPermisosPorOperacion(t *testing.T) {
	cases := []struct {
		name, op, role string
		want           int
	}{
		{"cliente puede crear", "reservas.crear", "cliente", http.StatusOK},
		{"profesional no crea por cliente", "reservas.crear", "profesional", http.StatusForbidden},
		{"recepción puede consultar turnos", "reservas.consultar-turnos", "recepcion", http.StatusOK},
		{"cliente no gestiona agenda", "reservas.gestionar-agenda", "cliente", http.StatusForbidden},
		{"operación desconocida denegada", "reservas.desconocida", "administrador", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				c.Set("identidad", identidad{ID: "u1", Email: "u@example.com", Roles: []string{tc.role}})
				c.Next()
			}, requierePermiso(tc.op), func(c *gin.Context) { c.Status(http.StatusOK) })
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
			if rec.Code != tc.want {
				t.Fatalf("se esperaba %d y se obtuvo %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTratamientoCompatible(t *testing.T) {
	tratamiento := tratamientoCatalogo{ID: "facial-01", ProfesionalesIDs: []string{"prof-1", "prof-2"}}
	if !tratamientoCompatible(tratamiento, "prof-2") {
		t.Fatal("se esperaba que prof-2 estuviera habilitado")
	}
	if tratamientoCompatible(tratamiento, "prof-3") {
		t.Fatal("un profesional no asociado no debe ser habilitado")
	}
	if tratamientoCompatible(tratamientoCatalogo{ID: "sin-asignar"}, "prof-1") {
		t.Fatal("un tratamiento sin profesionales asociados debe fallar de forma segura")
	}
}

func TestImporteSenaTreintaPorCiento(t *testing.T) {
	cases := []struct{ precio, want float64 }{{10000, 3000}, {1999.99, 600}, {0, 0}}
	for _, tc := range cases {
		if got := importeSena(tc.precio); got != tc.want {
			t.Fatalf("importeSena(%v)=%v; esperado %v", tc.precio, got, tc.want)
		}
	}
}

func TestImporteDevolucionSegunAnticipacion(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		sena   float64
		inicio time.Time
		want   float64
	}{{"más de 24 horas", 3000, now.Add(25 * time.Hour), 3000}, {"exactamente 24 horas", 3000, now.Add(24 * time.Hour), 3000}, {"un nanosegundo menos de 24 horas", 3000, now.Add(24*time.Hour - time.Nanosecond), 0}, {"seña cero", 0, now.Add(48 * time.Hour), 0}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := importeDevolucion(tc.sena, tc.inicio, now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
