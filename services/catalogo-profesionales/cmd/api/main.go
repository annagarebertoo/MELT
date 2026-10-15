package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Tratamiento struct {
	ID          string   `bson:"id" json:"id"`
	Nombre      string   `bson:"nombre" json:"nombre"`
	Descripcion string   `bson:"descripcion" json:"descripcion"`
	Categoria   string   `bson:"categoria" json:"categoria"`
	Precio      float64  `bson:"precio" json:"precio"`
	DuracionMin int      `bson:"duracionMin" json:"duracionMin"`
	Keywords    []string `bson:"keywords" json:"keywords"`
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

	log.Println("Conectado a MongoDB")

	servidor := &Servidor{
		coleccion: cliente.Database("catalogo_db").Collection("tratamientos"),
	}

	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := cliente.Ping(ctx, nil); err != nil {
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

	router.POST("/tratamientos", servidor.crearTratamiento)

	if err := router.Run(":8081"); err != nil {
		log.Fatalf("No se pudo iniciar el servidor: %v", err)
	}
}

func (s *Servidor) listarTratamientos(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	filtro := bson.D{}

	if categoria := strings.TrimSpace(c.Query("categoria")); categoria != "" {
		filtro = append(filtro, bson.E{
			Key: "categoria",
			Value: bson.D{{
				Key:   "$regex",
				Value: "^" + escapeRegex(categoria) + "$",
			}},
		})
	}

	if busqueda := strings.TrimSpace(c.Query("q")); busqueda != "" {
		patron := bson.Regex{
			Pattern: escapeRegex(busqueda),
			Options: "i",
		}

		filtro = append(filtro, bson.E{
			Key: "$or",
			Value: bson.A{
				bson.D{{Key: "nombre", Value: bson.D{{Key: "$regex", Value: patron}}}},
				bson.D{{Key: "descripcion", Value: bson.D{{Key: "$regex", Value: patron}}}},
				bson.D{{Key: "keywords", Value: bson.D{{Key: "$regex", Value: patron}}}},
			},
		})
	}

	cursor, err := s.coleccion.Find(ctx, filtro)
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
		"total":        len(tratamientos),
		"tratamientos": tratamientos,
	})
}

func (s *Servidor) crearTratamiento(c *gin.Context) {
	var tratamiento Tratamiento

	if err := c.ShouldBindJSON(&tratamiento); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "El cuerpo debe ser un JSON válido.",
		})
		return
	}

	tratamiento.ID = strings.TrimSpace(tratamiento.ID)
	tratamiento.Nombre = strings.TrimSpace(tratamiento.Nombre)
	tratamiento.Categoria = strings.TrimSpace(tratamiento.Categoria)

	if tratamiento.ID == "" ||
		tratamiento.Nombre == "" ||
		tratamiento.Categoria == "" ||
		tratamiento.Precio < 0 ||
		tratamiento.DuracionMin <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID, nombre y categoría son obligatorios; precio no puede ser negativo y duración debe ser positiva.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := s.coleccion.InsertOne(ctx, tratamiento)
	if err != nil {
		var writeErr mongo.WriteException
		if errors.As(err, &writeErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "No se pudo guardar el tratamiento; verificá que su ID no esté repetido.",
			})
			return
		}

		responderErrorDB(c, err)
		return
	}

	c.JSON(http.StatusCreated, tratamiento)
}

func responderErrorDB(c *gin.Context, err error) {
	log.Printf("Error de base de datos: %v", err)

	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "El catálogo no está disponible temporalmente.",
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
