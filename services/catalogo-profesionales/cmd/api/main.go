package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Tratamiento struct {
	ID              string   `bson:"id" json:"id"`
	Nombre          string   `bson:"nombre" json:"nombre"`
	Descripcion     string   `bson:"descripcion" json:"descripcion"`
	Categoria       string   `bson:"categoria" json:"categoria"`
	Precio          float64  `bson:"precio" json:"precio"`
	Moneda          string   `bson:"moneda" json:"moneda"`
	DuracionMinutos int      `bson:"duracionMinutos" json:"duracionMinutos"`
	Imagenes        []string `bson:"imagenes" json:"imagenes"`
	Keywords        []string `bson:"keywords" json:"keywords"`
}

type Servidor struct {
	coleccion *mongo.Collection
}

func main() {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cliente, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("No se pudo configurar MongoDB: %v", err)
	}
	defer func() {
		if err := cliente.Disconnect(context.Background()); err != nil {
			log.Printf("Error al desconectar MongoDB: %v", err)
		}
	}()

	if err := cliente.Ping(ctx, nil); err != nil {
		log.Fatalf("No se pudo conectar con MongoDB: %v", err)
	}

	coleccion := cliente.Database("catalogo_db").Collection("tratamientos")
	_, err = coleccion.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uq_tratamientos_id"),
	})
	if err != nil {
		log.Fatalf("No se pudo crear el índice único de tratamientos: %v", err)
	}

	servidor := &Servidor{coleccion: coleccion}
	router := crearRouter(servidor, func(ctx context.Context) error { return cliente.Ping(ctx, nil) })
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("No se pudo iniciar el servidor: %v", err)
	}
}

func crearRouter(servidor *Servidor, ping func(context.Context) error) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := ping(ctx); err != nil {
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
	router.POST("/tratamientos", servidor.crearTratamiento)
	return router
}

func (s *Servidor) listarTratamientos(c *gin.Context) {
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

	filtro := bson.D{}
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
		if parseErr != nil || precioMax < 0 {
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

	c.JSON(http.StatusOK, gin.H{
		"tratamientos": tratamientos,
		"pagination": gin.H{
			"page":       page,
			"limit":      limit,
			"total":      total,
			"totalPages": totalPages(total, limit),
		},
	})
}

func (s *Servidor) obtenerTratamiento(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_PARAMETER",
				"message": "El identificador del tratamiento es obligatorio.",
			},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	var tratamiento Tratamiento
	err := s.coleccion.FindOne(ctx, bson.D{
		{Key: "id", Value: id},
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
		log.Printf("Error al consultar tratamiento: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "INTERNAL_ERROR",
				"message": "Se produjo un error interno.",
			},
		})
		return
	}

	c.JSON(http.StatusOK, tratamiento)
}

func (s *Servidor) crearTratamiento(c *gin.Context) {
	var tratamiento Tratamiento
	if err := c.ShouldBindJSON(&tratamiento); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "El cuerpo debe ser un JSON válido."})
		return
	}

	tratamiento.ID = strings.TrimSpace(tratamiento.ID)
	tratamiento.Nombre = strings.TrimSpace(tratamiento.Nombre)
	tratamiento.Descripcion = strings.TrimSpace(tratamiento.Descripcion)
	tratamiento.Categoria = strings.TrimSpace(tratamiento.Categoria)
	for i := range tratamiento.Keywords {
		tratamiento.Keywords[i] = strings.TrimSpace(tratamiento.Keywords[i])
	}

	if tratamiento.ID == "" ||
		tratamiento.Nombre == "" ||
		tratamiento.Categoria == "" ||
		tratamiento.Descripcion == "" ||
		tratamiento.Moneda != "ARS" ||
		tratamiento.Precio < 0 ||
		tratamiento.DuracionMinutos <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_PARAMETER",
				"message": "ID, nombre, categoría, descripción y moneda ARS son obligatorios; el precio no puede ser negativo y la duración debe ser positiva.",
			},
		})
		return
	}
	if tratamiento.Keywords == nil {
		tratamiento.Keywords = []string{}
	}
	if tratamiento.Imagenes == nil {
		tratamiento.Imagenes = []string{}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if _, err := s.coleccion.InsertOne(ctx, tratamiento); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "Ya existe un tratamiento con ese ID."})
			return
		}
		responderErrorDB(c, err)
		return
	}
	c.JSON(http.StatusCreated, tratamiento)
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
	log.Printf("Error de base de datos: %v", err)
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
