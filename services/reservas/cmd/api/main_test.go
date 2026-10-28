package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAgendaValida(t *testing.T) {
	cases := []struct { name string; day int; start, end string; want bool }{
		{"rango válido", 1, "09:00", "17:00", true},
		{"día fuera de rango", 7, "09:00", "17:00", false},
		{"fin igual a inicio", 2, "09:00", "09:00", false},
		{"hora inválida", 2, "25:00", "26:00", false},
		{"fin anterior", 2, "18:00", "09:00", false},
	}
	for _, tc := range cases { t.Run(tc.name, func(t *testing.T) {
		if got:=agendaValida(tc.day,tc.start,tc.end); got!=tc.want { t.Fatalf("agendaValida()=%v, se esperaba %v",got,tc.want) }
	}) }
}

func TestSlotNoSuperpuesto(t *testing.T) {
	base:=time.Date(2026,10,28,10,0,0,0,time.UTC)
	cases:=[]struct{name string; start,end time.Time; want bool}{
		{"solapado",base.Add(30*time.Minute),base.Add(90*time.Minute),false},
		{"adyacente posterior",base.Add(time.Hour),base.Add(2*time.Hour),true},
		{"adyacente anterior",base.Add(-time.Hour),base,true},
	}
	for _,tc:=range cases { t.Run(tc.name,func(t *testing.T){
		if got:=slotNoSuperpuesto(tc.start,tc.end,base,base.Add(time.Hour)); got!=tc.want { t.Fatalf("slotNoSuperpuesto()=%v, se esperaba %v",got,tc.want) }
	}) }
}

func TestRequiereIdentidadDeniegaAnonimo(t *testing.T) {
	gin.SetMode(gin.TestMode); router:=gin.New()
	router.GET("/turnos",requiereIdentidad(),requierePermiso("reservas.consultar-mis-turnos"),func(c *gin.Context){c.Status(http.StatusOK)})
	rec:=httptest.NewRecorder(); router.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/turnos",nil))
	if rec.Code!=http.StatusUnauthorized { t.Fatalf("se esperaba 401 y se obtuvo %d",rec.Code) }
}

func TestPermisosPorOperacion(t *testing.T) {
	cases:=[]struct{name,op,role string; want int}{
		{"cliente puede crear","reservas.crear","cliente",http.StatusOK},
		{"profesional no crea por cliente","reservas.crear","profesional",http.StatusForbidden},
		{"recepción puede consultar turnos","reservas.consultar-mis-turnos","recepcion",http.StatusOK},
		{"cliente no gestiona agenda","reservas.gestionar-agenda","cliente",http.StatusForbidden},
		{"operación desconocida denegada","reservas.desconocida","administrador",http.StatusForbidden},
	}
	for _,tc:=range cases { t.Run(tc.name,func(t *testing.T){
		router:=gin.New()
		router.GET("/test",func(c *gin.Context){ c.Set("identidad",identidad{ID:"u1",Email:"u@example.com",Roles:[]string{tc.role}}); c.Next() },requierePermiso(tc.op),func(c *gin.Context){c.Status(http.StatusOK)})
		rec:=httptest.NewRecorder(); router.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/test",nil))
		if rec.Code!=tc.want { t.Fatalf("se esperaba %d y se obtuvo %d: %s",tc.want,rec.Code,rec.Body.String()) }
	}) }
}

func TestTratamientoCompatible(t *testing.T) {
	tratamiento := tratamientoCatalogo{ID: "facial-01", ProfesionalesIDs: []string{"prof-1", "prof-2"}}
	if !tratamientoCompatible(tratamiento, "prof-2") { t.Fatal("se esperaba que prof-2 estuviera habilitado") }
	if tratamientoCompatible(tratamiento, "prof-3") { t.Fatal("un profesional no asociado no debe ser habilitado") }
	if tratamientoCompatible(tratamientoCatalogo{ID: "sin-asignar"}, "prof-1") { t.Fatal("un tratamiento sin profesionales asociados debe fallar de forma segura") }
}

func TestImporteSenaTreintaPorCiento(t *testing.T) {
	cases := []struct{ precio, want float64 }{{10000,3000},{1999.99,600},{0,0}}
	for _,tc:=range cases { if got:=importeSena(tc.precio); got!=tc.want { t.Fatalf("importeSena(%v)=%v; esperado %v",tc.precio,got,tc.want) } }
}

func TestImporteDevolucionSegunAnticipacion(t *testing.T) {
	now:=time.Date(2026,10,28,12,0,0,0,time.UTC)
	cases:=[]struct{name string; inicio time.Time; want float64}{{"más de 24 horas",now.Add(25*time.Hour),3000},{"exactamente 24 horas",now.Add(24*time.Hour),3000},{"menos de 24 horas",now.Add(23*time.Hour+59*time.Minute),0},{"seña cero",now.Add(48*time.Hour),0}}
	for _,tc:=range cases { t.Run(tc.name,func(t *testing.T){if got:=importeDevolucion(3000,tc.inicio,now);tc.name=="seña cero" { got=importeDevolucion(0,tc.inicio,now); if got!=tc.want {t.Fatalf("got %v want %v",got,tc.want)} } else if got!=tc.want {t.Fatalf("got %v want %v",got,tc.want)}}) }
}
