package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Tratamiento struct {
	Eliminado        bool     `bson:"eliminado,omitempty" json:"-"`
	ID               string   `bson:"id" json:"id"`
	Nombre           string   `bson:"nombre" json:"nombre"`
	Descripcion      string   `bson:"descripcion" json:"descripcion"`
	Categoria        string   `bson:"categoria" json:"categoria"`
	Precio           float64  `bson:"precio" json:"precio"`
	Moneda           string   `bson:"moneda" json:"moneda"`
	DuracionMinutos  int      `bson:"duracionMinutos" json:"duracionMinutos"`
	Imagenes         []string `bson:"imagenes" json:"imagenes"`
	Keywords         []string `bson:"keywords" json:"keywords"`
	ProfesionalesIDs []string `bson:"profesionalesIds" json:"profesionalesIds"`
}

type Profesional struct {
	ID              string   `bson:"id" json:"id"`
	Nombre          string   `bson:"nombre" json:"nombre"`
	Descripcion     string   `bson:"descripcion,omitempty" json:"descripcion,omitempty"`
	TratamientosIDs []string `bson:"tratamientosIds" json:"tratamientosIds"`
	Activo          bool     `bson:"activo" json:"activo"`
}

type Servidor struct {
	categorias         *mongo.Collection
	control            *mongo.Collection
	coleccion          *mongo.Collection
	profesionales      *mongo.Collection
	authVerifyAdminURL string
	search             *searchClient
	cache              *memcacheClient
	lastIndexSuccess   atomic.Int64
	indexFailures      atomic.Uint64
	searchFailures     atomic.Uint64
}

func main() {
	configureLogger()
	gin.SetMode(gin.ReleaseMode)
	shutdownTracing, err := configureTracing(context.Background())
	if err != nil {
		slog.Warn("tracing_configuration_failed")
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if shutdownTracing(ctx) != nil {
				slog.Warn("tracing_shutdown_failed")
			}
		}()
	}
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cliente, err := mongo.Connect(options.Client().ApplyURI(uri).SetMonitor(mongoMonitor()))
	if err != nil {
		slog.Error("mongodb_configuration_failed")
		os.Exit(1)
	}
	defer func() {
		if err := cliente.Disconnect(context.Background()); err != nil {
			slog.Warn("mongodb_disconnect_failed")
		}
	}()

	if err := cliente.Ping(ctx, nil); err != nil {
		slog.Error("mongodb_connection_failed")
		os.Exit(1)
	}

	coleccion := cliente.Database(databaseName("CATALOGO_MONGODB_DATABASE", "catalogo_db")).Collection("tratamientos")
	profesionales := cliente.Database(databaseName("PROFESIONALES_MONGODB_DATABASE", databaseName("CATALOGO_MONGODB_DATABASE", "catalogo_db"))).Collection("profesionales")

	authURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_URL")), "/")
	if authURL == "" {
		authURL = "http://auth:8084"
	}
	servidor := &Servidor{coleccion: coleccion, profesionales: profesionales, authVerifyAdminURL: authURL + "/verify-admin"}
	searchURL := strings.TrimSpace(os.Getenv("OPENSEARCH_URL"))
	if searchURL == "" {
		searchURL = "http://opensearch:9200"
	}
	servidor.search = newSearchClient(searchURL)
	cacheAddress := strings.TrimSpace(os.Getenv("MEMCACHED_ADDRESS"))
	if cacheAddress == "" {
		cacheAddress = "memcached:11211"
	}
	servidor.cache = newMemcacheClient(cacheAddress)
	servidor.categorias = coleccion.Database().Collection("categorias")
	servidor.control = coleccion.Database().Collection("control_catalogo")
	if err := servidor.inicializarCatalogo(ctx); err != nil {
		slog.Error("catalog_initialization_failed")
		os.Exit(1)
	}
	indexerCtx, stopIndexer := context.WithCancel(context.Background())
	defer stopIndexer()
	servidor.runIndexer(indexerCtx)
	router := crearRouter(servidor, func(ctx context.Context) error { return cliente.Ping(ctx, nil) })
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	server := &http.Server{Addr: ":" + port, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if server.Shutdown(ctx) != nil {
			slog.Warn("http_shutdown_failed")
		}
	}()
	slog.Info("http_server_started")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http_server_failed")
		os.Exit(1)
	}
}

func crearRouter(servidor *Servidor, ping func(context.Context) error) *gin.Engine {
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.NoRoute(func(c *gin.Context) { catalogError(c, 404, "RUTA_NO_ENCONTRADA", "No existe la ruta solicitada.") })
	router.NoMethod(func(c *gin.Context) {
		catalogError(c, 405, "METODO_NO_PERMITIDO", "Método no permitido para esta ruta.")
	})
	router.Use(observabilityMiddleware(slog.Default()))
	router.GET("/metrics", servidor.metrics)

	router.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := ping(ctx); err != nil {
			logDatabaseError(c, "ping")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"servicio": "catalogo-profesionales",
				"estado":   "degradado",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"servicio": "catalogo-profesionales",
			"estado":   "ok",
		})
	})

	router.GET("/tratamientos", servidor.listarTratamientos)
	router.GET("/tratamientos/:id", servidor.obtenerTratamiento)
	router.POST("/tratamientos", servidor.requiereAdmin(), servidor.crearTratamiento)
	router.PUT("/tratamientos/:id", servidor.requiereAdmin(), servidor.actualizarTratamiento)
	router.DELETE("/tratamientos/:id", servidor.requiereAdmin(), servidor.eliminarTratamiento)
	router.GET("/profesionales/:id", servidor.obtenerProfesional)
	router.PUT("/profesionales/:id", servidor.requiereAdmin(), servidor.actualizarProfesional)
	router.DELETE("/profesionales/:id", servidor.requiereAdmin(), servidor.eliminarProfesional)
	router.GET("/categorias", servidor.listarCategorias)
	router.GET("/categorias/:id", servidor.obtenerCategoria)
	router.POST("/categorias", servidor.requiereAdmin(), servidor.crearCategoria)
	router.PUT("/categorias/:id", servidor.requiereAdmin(), servidor.actualizarCategoria)
	router.DELETE("/categorias/:id", servidor.requiereAdmin(), servidor.eliminarCategoria)
	router.GET("/profesionales", servidor.listarProfesionales)
	router.POST("/profesionales", servidor.requiereAdmin(), servidor.crearProfesional)
	return router
}

func normalizarTratamiento(c *gin.Context, tratamiento *Tratamiento) bool {
	tratamiento.ID = strings.TrimSpace(tratamiento.ID)
	tratamiento.Nombre = strings.TrimSpace(tratamiento.Nombre)
	tratamiento.Descripcion = strings.TrimSpace(tratamiento.Descripcion)
	tratamiento.Categoria = strings.TrimSpace(tratamiento.Categoria)
	for i := range tratamiento.Keywords {
		tratamiento.Keywords[i] = strings.TrimSpace(tratamiento.Keywords[i])
	}
	for i := range tratamiento.ProfesionalesIDs {
		tratamiento.ProfesionalesIDs[i] = strings.TrimSpace(tratamiento.ProfesionalesIDs[i])
	}
	if tratamiento.ID == "" || tratamiento.Nombre == "" || !textoValido(tratamiento.Categoria, 100) ||
		tratamiento.Descripcion == "" || tratamiento.Moneda != "ARS" || !precioValido(tratamiento.Precio) ||
		tratamiento.DuracionMinutos < 5 || tratamiento.DuracionMinutos > 480 || !idValido(tratamiento.ID) || !textoValido(tratamiento.Nombre, 200) || !textoValido(tratamiento.Descripcion, 5000) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMETER", "message": "ID, nombre, descripción y categoría son obligatorios; moneda debe ser ARS, precio no puede ser negativo y duración debe estar entre 5 y 480 minutos, como requiere Reservas."}})
		return false
	}
	if tratamiento.Keywords == nil {
		tratamiento.Keywords = []string{}
	}
	if tratamiento.Imagenes == nil {
		tratamiento.Imagenes = []string{}
	}
	if tratamiento.ProfesionalesIDs == nil {
		tratamiento.ProfesionalesIDs = []string{}
	}
	if !listasTratamientoValidas(tratamiento) {
		catalogError(c, 400, "DATOS_INVALIDOS", "Imágenes, keywords o identificadores asociados inválidos.")
		return false
	}
	return true
}

func (s *Servidor) actualizarTratamiento(c *gin.Context) { s.guardarTratamiento(c, false) }

func (s *Servidor) requiereAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(authorization, "Bearer ") || s.authVerifyAdminURL == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "IDENTIDAD_REQUERIDA", "message": "Iniciá sesión con una cuenta administrativa."}})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.authVerifyAdminURL, nil)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "AUTENTICACION_NO_DISPONIBLE", "message": "No se pudo verificar la sesión."}})
			return
		}
		req.Header.Set("Authorization", authorization)
		resp, err := (&http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "AUTENTICACION_NO_DISPONIBLE", "message": "No se pudo verificar la sesión."}})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			c.AbortWithStatusJSON(resp.StatusCode, gin.H{"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "Se requiere rol de administrador."}})
			return
		}
		if resp.StatusCode != http.StatusNoContent {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "AUTENTICACION_NO_DISPONIBLE", "message": "No se pudo verificar la sesión."}})
			return
		}
		c.Next()
	}
}

func (s *Servidor) listarProfesionales(c *gin.Context) {
	if s.profesionales == nil {
		catalogError(c, 503, "CATALOGO_NO_DISPONIBLE", "Profesionales no disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	filter := bson.D{{Key: "activo", Value: true}}
	if treatmentID := strings.TrimSpace(c.Query("tratamientoId")); treatmentID != "" {
		if !idValido(treatmentID) {
			catalogError(c, 400, "INVALID_PARAMETER", "tratamientoId inválido.")
			return
		}
		filter = append(filter, bson.E{Key: "tratamientosIds", Value: treatmentID})
	}
	cursor, err := s.profesionales.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "nombre", Value: 1}, {Key: "id", Value: 1}}))
	if err != nil {
		responderErrorDB(c, err)
		return
	}
	defer cursor.Close(ctx)
	items := make([]Profesional, 0)
	if err := cursor.All(ctx, &items); err != nil {
		responderErrorDB(c, err)
		return
	}
	for i := range items {
		normalizarLecturaProfesional(&items[i])
	}
	c.JSON(http.StatusOK, gin.H{"profesionales": items})
}

func (s *Servidor) crearProfesional(c *gin.Context) { s.guardarProfesional(c, true) }

func (s *Servidor) listarTratamientos(c *gin.Context) {
	if s == nil || s.coleccion == nil {
		catalogError(c, 503, "CATALOGO_NO_DISPONIBLE", "El catálogo no está disponible temporalmente.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	page, limit, err := parsePagination(c.Query("page"), c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_PARAMETER",
				"message": err.Error(),
			},
		})
		return
	}

	filtro := bson.D{{Key: "eliminado", Value: bson.M{"$ne": true}}}
	if categoria := strings.TrimSpace(c.Query("categoria")); categoria != "" {
		filtro = append(filtro, bson.E{
			Key:   "categoria",
			Value: bson.D{{Key: "$regex", Value: "^" + escapeRegex(categoria) + "$"}, {Key: "$options", Value: "i"}},
		})
	}

	if busqueda := strings.TrimSpace(c.Query("q")); busqueda != "" {
		patron := bson.Regex{Pattern: escapeRegex(busqueda), Options: "i"}
		filtro = append(filtro, bson.E{
			Key: "$or",
			Value: bson.A{
				bson.D{{Key: "nombre", Value: patron}},
				bson.D{{Key: "keywords", Value: patron}},
			},
		})
	}

	if precioMaxTexto := strings.TrimSpace(c.Query("precioMax")); precioMaxTexto != "" {
		precioMax, parseErr := strconv.ParseFloat(precioMaxTexto, 64)
		if parseErr != nil || !precioValido(precioMax) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code":    "INVALID_PARAMETER",
					"message": "precioMax debe ser un número mayor o igual a cero.",
				},
			})
			return
		}
		filtro = append(filtro, bson.E{Key: "precio", Value: bson.D{{Key: "$lte", Value: precioMax}}})
	}

	sortField := strings.TrimSpace(c.DefaultQuery("sort", "nombre"))
	sortFields := map[string]string{"nombre": "nombre", "precio": "precio"}
	mongoSortField, ok := sortFields[sortField]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_PARAMETER",
				"message": "sort debe ser nombre o precio.",
			},
		})
		return
	}
	order := strings.ToLower(strings.TrimSpace(c.DefaultQuery("order", "asc")))
	if order != "asc" && order != "desc" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_PARAMETER",
				"message": "order debe ser asc o desc.",
			},
		})
		return
	}
	direction := 1
	if order == "desc" {
		direction = -1
	}
	query := catalogListQuery{Q: strings.TrimSpace(c.Query("q")), Category: strings.TrimSpace(c.Query("categoria")), Page: page, Limit: limit, Sort: mongoSortField, Order: order}
	precioMaxTexto := strings.TrimSpace(c.Query("precioMax"))
	precioMax := float64(0)
	if precioMaxTexto != "" {
		precioMax, _ = strconv.ParseFloat(precioMaxTexto, 64)
		query.MaxPrice = strconv.FormatFloat(precioMax, 'g', -1, 64)
	}
	if query.Q != "" && s.search != nil && !s.searchIndexFresh() {
		freshnessCtx, freshnessCancel := context.WithTimeout(c.Request.Context(), time.Second)
		if err := s.refreshIndexFreshness(freshnessCtx); err != nil {
			slog.Warn("catalog_index_freshness_read_failed", "error", err)
		}
		freshnessCancel()
		if !s.searchIndexFresh() {
			catalogError(c, http.StatusServiceUnavailable, "BUSQUEDA_NO_DISPONIBLE", "El índice de búsqueda no está actualizado; reintentá en unos segundos.")
			return
		}
	}
	if s.cache != nil {
		if cached, ok := s.cache.get(query); ok {
			c.Data(http.StatusOK, "application/json; charset=utf-8", cached)
			return
		}
	}
	if query.Q != "" && s.search != nil {
		searchCtx, searchCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer searchCancel()
		result, err := s.search.searchTreatments(searchCtx, query.Q, query.Category, precioMax, precioMaxTexto != "", page, limit, direction, mongoSortField)
		if err != nil {
			s.searchFailures.Add(1)
			slog.Warn("catalog_search_failed", "error", err)
			catalogError(c, 503, "BUSQUEDA_NO_DISPONIBLE", "La búsqueda no está disponible temporalmente.")
			return
		}
		payload := gin.H{"tratamientos": result.Treatments, "pagination": gin.H{"page": page, "limit": limit, "total": result.Total, "totalPages": totalPages(result.Total, limit)}}
		if raw, err := json.Marshal(payload); err == nil {
			if s.cache != nil {
				s.cache.set(query, raw)
			}
			c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
			return
		}
	}

	total, err := s.coleccion.CountDocuments(ctx, filtro)
	if err != nil {
		responderErrorDB(c, err)
		return
	}
	opts := options.Find().
		SetSkip(int64((page - 1) * limit)).
		SetLimit(int64(limit)).
		SetSort(bson.D{{Key: mongoSortField, Value: direction}, {Key: "id", Value: 1}})

	cursor, err := s.coleccion.Find(ctx, filtro, opts)
	if err != nil {
		responderErrorDB(c, err)
		return
	}
	defer cursor.Close(ctx)

	tratamientos := make([]Tratamiento, 0)
	if err := cursor.All(ctx, &tratamientos); err != nil {
		responderErrorDB(c, err)
		return
	}

	for i := range tratamientos {
		normalizarLecturaTratamiento(&tratamientos[i])
	}
	payload := gin.H{
		"tratamientos": tratamientos,
		"pagination": gin.H{
			"page":       page,
			"limit":      limit,
			"total":      total,
			"totalPages": totalPages(total, limit),
		},
	}
	if raw, err := json.Marshal(payload); err == nil {
		if s.cache != nil {
			s.cache.set(query, raw)
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
		return
	}
	c.JSON(http.StatusOK, payload)
}

func (s *Servidor) obtenerTratamiento(c *gin.Context) {
	if s == nil || s.coleccion == nil {
		catalogError(c, 503, "CATALOGO_NO_DISPONIBLE", "El catálogo no está disponible temporalmente.")
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if !idValido(id) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_PARAMETER",
				"message": "El identificador del tratamiento es inválido.",
			},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	var tratamiento Tratamiento
	err := s.coleccion.FindOne(ctx, bson.D{
		{Key: "id", Value: id},
		{Key: "eliminado", Value: bson.M{"$ne": true}},
	}).Decode(&tratamiento)

	if errors.Is(err, mongo.ErrNoDocuments) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    "TRATAMIENTO_NOT_FOUND",
				"message": "No existe un tratamiento con el identificador solicitado.",
			},
		})
		return
	}

	if err != nil {
		responderErrorDB(c, err)
		return
	}

	normalizarLecturaTratamiento(&tratamiento)
	c.JSON(http.StatusOK, tratamiento)
}

func (s *Servidor) crearTratamiento(c *gin.Context) { s.guardarTratamiento(c, true) }

func precioValido(precio float64) bool {
	return precio >= 0 && !math.IsNaN(precio) && !math.IsInf(precio, 0)
}

func parsePagination(pageText, limitText string) (int, int, error) {
	page, err := parsePositiveInt(pageText, 1, 1_000_000)
	if err != nil {
		return 0, 0, errors.New("page debe ser un entero positivo.")
	}
	limit, err := parsePositiveInt(limitText, 10, 50)
	if err != nil {
		return 0, 0, errors.New("limit debe ser un entero entre 1 y 50.")
	}
	return page, limit, nil
}

func parsePositiveInt(text string, fallback, max int) (int, error) {
	if strings.TrimSpace(text) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < 1 || value > max {
		return 0, errors.New("valor fuera de rango")
	}
	return value, nil
}

func totalPages(total int64, limit int) int64 {
	if total == 0 {
		return 0
	}
	return (total + int64(limit) - 1) / int64(limit)
}

func responderErrorDB(c *gin.Context, err error) {
	logDatabaseError(c, "query")
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": gin.H{
			"code":    "CATALOGO_NO_DISPONIBLE",
			"message": "El catálogo no está disponible temporalmente.",
		},
	})
}

func escapeRegex(texto string) string {
	reemplazos := strings.NewReplacer(
		`\`, `\\`,
		`.`, `\.`,
		`+`, `\+`,
		`*`, `\*`,
		`?`, `\?`,
		`^`, `\^`,
		`$`, `\$`,
		`{`, `\{`,
		`}`, `\}`,
		`(`, `\(`,
		`)`, `\)`,
		`|`, `\|`,
		`[`, `\[`,
		`]`, `\]`,
	)
	return reemplazos.Replace(texto)
}
