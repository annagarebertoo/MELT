package main

// Telemetría común de MELT: logs JSON, métricas en formato Prometheus y trazas
// OpenTelemetry. El mismo archivo existe en cada servicio (los módulos Go son
// independientes); solo cambia telServicio.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const telServicio = "reservas"

type telRequestIDKey struct{}

// Misma lista acotada que usa el Gateway: nunca se registra un encabezado arbitrario.
var telRequestIDValido = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

var telBucketsDuracion = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

var telMetricas = telNuevoRegistro()

func telRequestID(valor string) string {
	if telRequestIDValido.MatchString(valor) {
		return valor
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("no se pudo generar el request ID")
	}
	return hex.EncodeToString(b[:])
}

// telRequestIDDe devuelve el request ID asociado al contexto, si existe.
func telRequestIDDe(ctx context.Context) string {
	if id, ok := ctx.Value(telRequestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// telConfigurarLogger instala un logger JSON como predeterminado. slog.SetDefault
// también redirige los log.Printf existentes al mismo formato estructurado.
func telConfigurarLogger(salida io.Writer) *slog.Logger {
	instancia, _ := os.Hostname()
	logger := slog.New(slog.NewJSONHandler(salida, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				attr.Key = "timestamp"
			}
			return attr
		},
	})).With("service", telServicio, "instance", instancia)
	slog.SetDefault(logger)
	return logger
}

// telConfigurarTrazas exporta spans por OTLP/HTTP cuando hay endpoint. Sin endpoint
// igual propaga traceparent para no cortar trazas que vienen del Gateway.
func telConfigurarTrazas(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithTimeout(2*time.Second),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		return nil, err
	}
	instancia, _ := os.Hostname()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", telServicio),
			attribute.String("service.instance.id", instancia),
		)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Second)),
	)
	otel.SetTracerProvider(provider)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		// El error puede incluir endpoints o credenciales: solo se registra el evento.
		slog.Warn("telemetry_export_failed")
	}))
	return provider.Shutdown, nil
}

// telInstrumentarClientesHTTP envuelve el transporte por defecto para que cada
// llamada saliente cree un span cliente, propague traceparent y X-Request-ID y
// quede medida en melt_http_client_requests_total.
func telInstrumentarClientesHTTP() {
	if _, ya := http.DefaultTransport.(telTransporte); ya {
		return
	}
	http.DefaultTransport = telTransporte{base: http.DefaultTransport}
}

type telTransporte struct{ base http.RoundTripper }

func (t telTransporte) RoundTrip(req *http.Request) (*http.Response, error) {
	inicio := time.Now()
	destino := req.URL.Hostname()
	ctx, span := otel.Tracer("melt/"+telServicio+"/http-client").Start(req.Context(), "HTTP "+req.Method+" "+destino,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.request.method", req.Method),
			attribute.String("server.address", destino),
		),
	)
	defer span.End()
	saliente := req.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(saliente.Header))
	if id := telRequestIDDe(ctx); id != "" && saliente.Header.Get("X-Request-ID") == "" {
		saliente.Header.Set("X-Request-ID", id)
	}
	resp, err := t.base.RoundTrip(saliente)
	estado := "error"
	if err != nil {
		span.SetStatus(codes.Error, "HTTP client error")
	} else {
		estado = strconv.Itoa(resp.StatusCode)
		span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
		if resp.StatusCode >= 500 {
			span.SetStatus(codes.Error, "HTTP upstream error")
		}
	}
	telMetricas.contar("melt_http_client_requests_total", "Llamadas HTTP salientes por destino y resultado.", 1, "destino", destino, "status", estado)
	telMetricas.observar("melt_http_client_request_duration_seconds", "Duración de llamadas HTTP salientes.", time.Since(inicio).Seconds(), "destino", destino)
	return resp, err
}

// telIniciarSpan crea un span interno (por ejemplo, para el consumo de un evento).
func telIniciarSpan(ctx context.Context, nombre string, atributos ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer("melt/"+telServicio).Start(ctx, nombre, trace.WithAttributes(atributos...))
}

// telInyectarCabeceras escribe traceparent en un mapa (por ejemplo, encabezados AMQP).
func telInyectarCabeceras(ctx context.Context, destino map[string]any) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	for k, v := range carrier {
		destino[k] = v
	}
}

// telExtraerCabeceras recupera el contexto de traza desde un mapa de encabezados.
func telExtraerCabeceras(ctx context.Context, origen map[string]any) context.Context {
	carrier := propagation.MapCarrier{}
	for k, v := range origen {
		if s, ok := v.(string); ok {
			carrier[k] = s
		}
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func telTraceID(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

func telSpanID(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}

// telRegistrarSolicitud registra métricas, log JSON y cierre del span de una solicitud HTTP entrante.
func telRegistrarSolicitud(ctx context.Context, span trace.Span, metodo, ruta string, estado int, inicio time.Time) {
	duracion := time.Since(inicio)
	if ruta != "/metrics" {
		telMetricas.registrarHTTP(metodo, ruta, estado, duracion)
	}
	span.SetAttributes(attribute.Int("http.response.status_code", estado))
	nivel := slog.LevelInfo
	if estado >= 500 {
		nivel = slog.LevelError
		span.SetStatus(codes.Error, "HTTP server error")
	} else if estado >= 400 {
		nivel = slog.LevelWarn
	}
	if ruta == "/health" || ruta == "/metrics" {
		nivel = slog.LevelDebug
	}
	sc := span.SpanContext()
	slog.Log(ctx, nivel, "http_request",
		"method", metodo, "route", ruta, "status", estado,
		"duration_ms", float64(duracion.Microseconds())/1000,
		"request_id", telRequestIDDe(ctx), "trace_id", telTraceID(sc), "span_id", telSpanID(sc),
	)
}

// telIniciarSolicitud extrae traceparent, asigna request ID y abre el span servidor.
func telIniciarSolicitud(req *http.Request, ruta string) (context.Context, trace.Span, string) {
	id := telRequestID(req.Header.Get("X-Request-ID"))
	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))
	ctx = context.WithValue(ctx, telRequestIDKey{}, id)
	ctx, span := otel.Tracer("melt/"+telServicio+"/http").Start(ctx, req.Method+" "+ruta,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.request.method", req.Method),
			attribute.String("http.route", ruta),
			attribute.String("request.id", id),
		),
	)
	return ctx, span, id
}

// ---- Registro de métricas en formato de exposición de Prometheus ----

type telHTTPClave struct{ metodo, ruta, estado string }

type telHistograma struct {
	cantidad uint64
	suma     float64
	buckets  []uint64
}

type telSerie struct {
	nombre    string
	etiquetas string
}

type telRegistro struct {
	mu         sync.Mutex
	instancia  string
	http       map[telHTTPClave]*telHistograma
	contadores map[telSerie]float64
	histos     map[telSerie]*telHistograma
	ayudas     map[string]string
	tipos      map[string]string
}

func telNuevoRegistro() *telRegistro {
	instancia, err := os.Hostname()
	if err != nil || strings.TrimSpace(instancia) == "" {
		instancia = "unknown"
	}
	return &telRegistro{
		instancia: instancia, http: map[telHTTPClave]*telHistograma{},
		contadores: map[telSerie]float64{}, histos: map[telSerie]*telHistograma{},
		ayudas: map[string]string{}, tipos: map[string]string{},
	}
}

func telMetodo(m string) string {
	switch m {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return m
	}
	return "OTHER"
}

func telObservarHistograma(h *telHistograma, valor float64) {
	if valor < 0 {
		valor = 0
	}
	h.cantidad++
	h.suma += valor
	for i, limite := range telBucketsDuracion {
		if valor <= limite {
			h.buckets[i]++
		}
	}
}

func (r *telRegistro) registrarHTTP(metodo, ruta string, estado int, duracion time.Duration) {
	if ruta == "" {
		ruta = "unmatched"
	}
	clave := telHTTPClave{metodo: telMetodo(metodo), ruta: ruta, estado: strconv.Itoa(estado)}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.http[clave]
	if h == nil {
		h = &telHistograma{buckets: make([]uint64, len(telBucketsDuracion))}
		r.http[clave] = h
	}
	telObservarHistograma(h, duracion.Seconds())
}

// telEtiquetas arma pares clave="valor" en orden estable; los valores se escapan con %q.
func telEtiquetas(kv ...string) string {
	partes := make([]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		partes = append(partes, fmt.Sprintf("%s=%q", kv[i], kv[i+1]))
	}
	return strings.Join(partes, ",")
}

// contar suma valor a un contador de negocio o técnico.
func (r *telRegistro) contar(nombre, ayuda string, valor float64, kv ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ayudas[nombre], r.tipos[nombre] = ayuda, "counter"
	r.contadores[telSerie{nombre: nombre, etiquetas: telEtiquetas(kv...)}] += valor
}

// observar registra un valor en un histograma con los buckets de duración.
func (r *telRegistro) observar(nombre, ayuda string, valor float64, kv ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ayudas[nombre], r.tipos[nombre] = ayuda, "histogram"
	serie := telSerie{nombre: nombre, etiquetas: telEtiquetas(kv...)}
	h := r.histos[serie]
	if h == nil {
		h = &telHistograma{buckets: make([]uint64, len(telBucketsDuracion))}
		r.histos[serie] = h
	}
	telObservarHistograma(h, valor)
}

func telUnir(base, extra string) string {
	if base == "" {
		return extra
	}
	return base + "," + extra
}

func (r *telRegistro) escribir(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	comunes := telEtiquetas("service", telServicio, "instance", r.instancia)
	fmt.Fprintln(w, "# HELP melt_service_info Identidad de la instancia del servicio.")
	fmt.Fprintln(w, "# TYPE melt_service_info gauge")
	fmt.Fprintf(w, "melt_service_info{%s} 1\n", comunes)

	fmt.Fprintln(w, "# HELP melt_http_requests_total Solicitudes HTTP atendidas.")
	fmt.Fprintln(w, "# TYPE melt_http_requests_total counter")
	fmt.Fprintln(w, "# HELP melt_http_request_duration_seconds Duración de solicitudes HTTP atendidas.")
	fmt.Fprintln(w, "# TYPE melt_http_request_duration_seconds histogram")
	claves := make([]telHTTPClave, 0, len(r.http))
	for k := range r.http {
		claves = append(claves, k)
	}
	sort.Slice(claves, func(i, j int) bool {
		a, b := claves[i], claves[j]
		return a.ruta+a.metodo+a.estado < b.ruta+b.metodo+b.estado
	})
	for _, k := range claves {
		h := r.http[k]
		et := telUnir(comunes, telEtiquetas("method", k.metodo, "route", k.ruta, "status", k.estado))
		fmt.Fprintf(w, "melt_http_requests_total{%s} %d\n", et, h.cantidad)
		telEscribirHistograma(w, "melt_http_request_duration_seconds", et, h)
	}

	nombres := make([]string, 0, len(r.ayudas))
	for n := range r.ayudas {
		nombres = append(nombres, n)
	}
	sort.Strings(nombres)
	for _, n := range nombres {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", n, r.ayudas[n], n, r.tipos[n])
		series := []telSerie{}
		for s := range r.contadores {
			if s.nombre == n {
				series = append(series, s)
			}
		}
		for s := range r.histos {
			if s.nombre == n {
				series = append(series, s)
			}
		}
		sort.Slice(series, func(i, j int) bool { return series[i].etiquetas < series[j].etiquetas })
		for _, s := range series {
			et := telUnir(comunes, s.etiquetas)
			if h, ok := r.histos[s]; ok {
				telEscribirHistograma(w, n, et, h)
				continue
			}
			fmt.Fprintf(w, "%s{%s} %s\n", n, et, strconv.FormatFloat(r.contadores[s], 'g', -1, 64))
		}
	}
}

func telEscribirHistograma(w io.Writer, nombre, et string, h *telHistograma) {
	for i, limite := range telBucketsDuracion {
		fmt.Fprintf(w, "%s_bucket{%s,le=%q} %d\n", nombre, et, strconv.FormatFloat(limite, 'g', -1, 64), h.buckets[i])
	}
	fmt.Fprintf(w, "%s_bucket{%s,le=\"+Inf\"} %d\n", nombre, et, h.cantidad)
	fmt.Fprintf(w, "%s_sum{%s} %g\n", nombre, et, h.suma)
	fmt.Fprintf(w, "%s_count{%s} %d\n", nombre, et, h.cantidad)
}

// telManejadorMetricas expone /metrics. El Gateway bloquea esta ruta desde Internet.
func telManejadorMetricas(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	telMetricas.escribir(w)
}
