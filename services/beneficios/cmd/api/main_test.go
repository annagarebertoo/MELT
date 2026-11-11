package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type memoria struct {
	mu    sync.Mutex
	saldo map[string]int64
	ops   map[string]Movimiento
}

func nuevaMemoria() *memoria {
	return &memoria{saldo: map[string]int64{}, ops: map[string]Movimiento{}}
}
func (m *memoria) acreditar(_ context.Context, r AcreditarRequest) (Movimiento, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.ops[r.SolicitudID]; ok {
		return resolverRepetida(prev, r)
	}
	if m.saldo[r.UsuarioID] > maxPuntos-r.Puntos {
		return Movimiento{}, false, errLimite
	}
	m.saldo[r.UsuarioID] += r.Puntos
	v := Movimiento{SolicitudID: r.SolicitudID, UsuarioID: r.UsuarioID, Puntos: r.Puntos, SaldoPuntos: m.saldo[r.UsuarioID], Tipo: "acreditacion_externa"}
	m.ops[r.SolicitudID] = v
	return v, false, nil
}
func (m *memoria) consultar(_ context.Context, id string) (int64, []Movimiento, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := []Movimiento{}
	for _, o := range m.ops {
		if o.UsuarioID == id {
			v = append(v, o)
		}
	}
	return m.saldo[id], v, nil
}
func request(r http.Handler, body, key string) *httptest.ResponseRecorder {
	q := httptest.NewRequest("POST", "/acreditar-puntos", strings.NewReader(body))
	q.Header.Set("X-Partner-Key", key)
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}
func servidorPrueba(a almacenPuntos) *Servidor {
	return &Servidor{almacen: a, claveAPI: "test", verificarSesion: func(_ context.Context, bearer string) (identidadCliente, error) {
		if bearer != "Bearer cliente-test" {
			return identidadCliente{}, errSesionInvalida
		}
		return identidadCliente{ID: "u", Email: "u@test.com", Roles: []string{"cliente"}}, nil
	}, verificarUsuario: func(_ context.Context, id string) error {
		if id == "missing" {
			return errUsuario
		}
		return nil
	}}
}
func TestValidacionYSeguridad(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := crearRouter(servidorPrueba(nuevaMemoria()))
	for _, tc := range []struct {
		body, key string
		status    int
	}{
		{`{"solicitudId":"op","usuarioId":"u","puntos":5}`, "", 401},
		{`{"solicitudId":"op","usuarioId":"u","puntos":5}`, "wrong", 401},
		{`{}`, "test", 400}, {`null`, "test", 400}, {`{"usuarioId":"u","puntos":5}`, "test", 400},
		{`{"solicitudId":"op","puntos":5}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"u"}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"u","puntos":0}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"u","puntos":-1}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"u","puntos":1.5}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"u","puntos":"5"}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"u","puntos":9007199254740992}`, "test", 400},
		{`{"solicitudId":" ","usuarioId":"u","puntos":5}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"missing","puntos":5}`, "test", 404},
		{`{"solicitudId":"op","usuarioId":"u","puntos":5,"plan":"premium"}`, "test", 400},
		{`{"solicitudId":"op","usuarioId":"u","puntos":5} {}`, "test", 400},
	} {
		t.Run(tc.body+tc.key, func(t *testing.T) {
			w := request(r, tc.body, tc.key)
			if w.Code != tc.status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestAcumulacionIdempotenciaYConsulta(t *testing.T) {
	a := nuevaMemoria()
	a.saldo["u"] = 50
	r := crearRouter(servidorPrueba(a))
	body := `{"solicitudId":"op","usuarioId":"u","puntos":200}`
	first := request(r, body, "test")
	if first.Code != 201 {
		t.Fatal(first.Body.String())
	}
	repeat := request(r, body, "test")
	if repeat.Code != 200 || repeat.Body.String() != first.Body.String() {
		t.Fatal("reintento alteró resultado")
	}
	for _, b := range []string{`{"solicitudId":"op","usuarioId":"u","puntos":201}`, `{"solicitudId":"op","usuarioId":"otro","puntos":200}`} {
		if w := request(r, b, "test"); w.Code != 409 {
			t.Fatal(w.Code)
		}
	}
	q := httptest.NewRequest("GET", "/mis-puntos", nil)
	q.Header.Set("Authorization", "Bearer cliente-test")
	q.Header.Set("X-Auth-User-ID", "otra-cuenta")
	q.Header.Set("X-Auth-User-Email", "u@test.com")
	q.Header.Set("X-Auth-User-Roles", "cliente")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	var v struct {
		Saldo       int64        `json:"saldoPuntos"`
		Movimientos []Movimiento `json:"movimientos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || v.Saldo != 250 || len(v.Movimientos) != 1 {
		t.Fatal(w.Body.String())
	}
}
func TestConcurrentesHTTP(t *testing.T) {
	a := nuevaMemoria()
	r := crearRouter(servidorPrueba(a))
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"solicitudId":"op-%d","usuarioId":"u","puntos":2}`, i%10)
			w := request(r, body, "test")
			if w.Code != 200 && w.Code != 201 {
				t.Errorf("%d %s", w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
	if a.saldo["u"] != 20 || len(a.ops) != 10 {
		t.Fatal(a.saldo, len(a.ops))
	}
}
func TestAuthNoDisponible(t *testing.T) {
	s := servidorPrueba(nuevaMemoria())
	s.verificarUsuario = func(context.Context, string) error { return errors.New("offline") }
	if w := request(crearRouter(s), `{"solicitudId":"op","usuarioId":"u","puntos":5}`, "test"); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestVerificadorAuth(t *testing.T) {
	for _, status := range []int{204, 404, 401, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Melt-Internal-Key") != "internal" || r.URL.Path != "/internal/usuarios/u" {
					t.Error("contrato interno incorrecto")
				}
				w.WriteHeader(status)
			}))
			defer srv.Close()
			err := verificadorAuth(srv.URL, "internal")(context.Background(), "u")
			if (status == 204) != (err == nil) {
				t.Fatal(err)
			}
			if status == 404 && !errors.Is(err, errUsuario) {
				t.Fatal(err)
			}
		})
	}
}
func TestRutasYPermisos(t *testing.T) {
	r := crearRouter(nil)
	for _, path := range []string{"/registro-externo", "/internal/registro-gym"} {
		if w := requestPath(r, "POST", path); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	if w := requestPath(r, "GET", "/health"); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if w := requestPath(r, "GET", "/mis-puntos"); w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func requestPath(r http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func (m *memoria) buscarSolicitud(_ context.Context, id string) (Movimiento, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.ops[id]
	if !ok {
		return Movimiento{}, mongo.ErrNoDocuments
	}
	return v, nil
}
func TestRepeticionSinAuthYConflictoUsuarioInexistente(t *testing.T) {
	s := servidorPrueba(nuevaMemoria())
	r := crearRouter(s)
	body := `{"solicitudId":"original","usuarioId":"u","puntos":5}`
	first := request(r, body, "test")
	if first.Code != 201 {
		t.Fatal(first.Body.String())
	}
	s.verificarUsuario = func(context.Context, string) error { return errors.New("Auth caído") }
	replay := request(r, body, "test")
	if replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatal(replay.Code, replay.Body.String())
	}
	if w := request(r, `{"solicitudId":"original","usuarioId":"missing","puntos":5}`, "test"); w.Code != 409 {
		t.Fatal(w.Code)
	}
}
