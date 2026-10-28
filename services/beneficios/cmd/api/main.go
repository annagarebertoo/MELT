package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
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

type RegistroExternoRequest struct {
	SolicitudID string `json:"solicitudId" binding:"required"`
	Nombre      string `json:"nombre" binding:"required"`
	Email       string `json:"email" binding:"required,email"`
	Codigo      string `json:"codigoReferido" binding:"required"`
	Plan        string `json:"plan" binding:"required"`
	Membresia   string `json:"membresia" binding:"required"`
}

type RegistroExterno struct {
	ID              string    `bson:"id" json:"id"`
	SolicitudID     string    `bson:"solicitudId" json:"solicitudId"`
	Nombre          string    `bson:"nombre" json:"nombre"`
	Email           string    `bson:"email" json:"email"`
	Origen          string    `bson:"origen" json:"origen"`
	Plan            string    `bson:"plan" json:"plan"`
	Membresia       string    `bson:"membresia" json:"membresia"`
	PuntosRegalo     int       `bson:"puntosRegalo" json:"puntosRegalo"`
	SaldoPuntos      int       `bson:"saldoPuntos" json:"saldoPuntos"`
	RegaloAcreditado bool      `bson:"regaloAcreditado" json:"regaloAcreditado"`
	CreadoEn        time.Time `bson:"creadoEn" json:"creadoEn"`
}

type Servidor struct {
	registros          *mongo.Collection
	codigoGimnasio     string
	claveAPI           string
	puntosPorMembresia map[string]int
}

func main() {
	uri := strings.TrimSpace(os.Getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cliente, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal("no se pudo configurar MongoDB")
	}
	defer func() {
		if err := cliente.Disconnect(context.Background()); err != nil {
			log.Printf("no se pudo cerrar MongoDB: %v", err)
		}
	}()
	if err := cliente.Ping(ctx, nil); err != nil {
		log.Fatal("no se pudo conectar con MongoDB")
	}

	coleccion := cliente.Database("beneficios_db").Collection("registros_externos")
	_, err = coleccion.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_registro_externo_email")},
		{Keys: bson.D{{Key: "solicitudId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_registro_externo_solicitud")},
	})
	if err != nil {
		log.Fatal("no se pudieron crear los índices de registros externos")
	}

	puntos, err := cargarPuntos(os.Getenv("PUNTOS_POR_PLAN_MEMBRESIA"))
	if err != nil {
		log.Fatal("PUNTOS_POR_PLAN_MEMBRESIA debe ser un JSON válido con puntos no negativos")
	}
	servidor := &Servidor{
		registros:          coleccion,
		codigoGimnasio:     strings.TrimSpace(os.Getenv("CODIGO_GIMNASIO")),
		claveAPI:           strings.TrimSpace(os.Getenv("CLAVE_API_GIMNASIO")),
		puntosPorMembresia: puntos,
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8083"
	}
	if err := crearRouter(servidor).Run(":" + port); err != nil {
		log.Fatal("no se pudo iniciar el servicio de beneficios")
	}
}

func crearRouter(servidor *Servidor) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		if servidor == nil || servidor.registros == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"servicio": "beneficios", "estado": "degradado"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := servidor.registros.Database().Client().Ping(ctx, nil); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"servicio": "beneficios", "estado": "degradado"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"servicio": "beneficios", "estado": "ok"})
	})

	router.POST("/registro-externo", servidor.registrarExterno)
	return router
}

func (s *Servidor) registrarExterno(c *gin.Context) {
	if s == nil || s.registros == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "BENEFICIOS_NO_DISPONIBLE", "message": "El servicio de beneficios no está disponible."}})
		return
	}
	if s.codigoGimnasio == "" || s.claveAPI == "" || len(s.puntosPorMembresia) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "REGISTRO_EXTERNO_NO_CONFIGURADO", "message": "El registro externo todavía no está configurado."}})
		return
	}

	claveEnviada := c.GetHeader("X-Partner-Key")
	if claveEnviada == "" || subtle.ConstantTimeCompare([]byte(claveEnviada), []byte(s.claveAPI)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "PARTNER_NO_AUTORIZADO", "message": "El consumidor no está autorizado para registrar altas externas."}})
		return
	}

	var req RegistroExternoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMETER", "message": "Completá nombre, email, solicitudId, codigoReferido, plan y membresia con valores válidos."}})
		return
	}
	req.SolicitudID = strings.TrimSpace(req.SolicitudID)
	req.Nombre = strings.TrimSpace(req.Nombre)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Codigo = strings.TrimSpace(req.Codigo)
	req.Plan = normalizarClave(req.Plan)
	req.Membresia = normalizarClave(req.Membresia)

	if req.SolicitudID == "" || req.Nombre == "" || req.Email == "" || req.Plan == "" || req.Membresia == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMETER", "message": "Los campos obligatorios no pueden estar vacíos."}})
		return
	}
	if req.Codigo != s.codigoGimnasio {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "CODIGO_REFERIDO_INVALIDO", "message": "El código de referido no es válido."}})
		return
	}

	clave := req.Plan + "|" + req.Membresia
	puntos, ok := s.puntosPorMembresia[clave]
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "PLAN_MEMBRESIA_NO_DISPONIBLE", "message": "La combinación de plan y membresía no tiene un regalo configurado."}})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	var existente RegistroExterno
	err := s.registros.FindOne(ctx, bson.D{{Key: "solicitudId", Value: req.SolicitudID}}).Decode(&existente)
	if err == nil {
		if !strings.EqualFold(existente.Email, req.Email) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "SOLICITUD_YA_PROCESADA", "message": "La solicitud ya fue procesada con otros datos."}})
			return
		}
		c.JSON(http.StatusOK, existente)
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "BENEFICIOS_NO_DISPONIBLE", "message": "No se pudo consultar el registro."}})
		return
	}

	registro := RegistroExterno{
		ID:               req.SolicitudID,
		SolicitudID:      req.SolicitudID,
		Nombre:           req.Nombre,
		Email:            req.Email,
		Origen:           "gimnasio",
		Plan:             req.Plan,
		Membresia:        req.Membresia,
		PuntosRegalo:     puntos,
		SaldoPuntos:      puntos,
		RegaloAcreditado: true,
		CreadoEn:         time.Now().UTC(),
	}
	if _, err := s.registros.InsertOne(ctx, registro); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// A unique email or request ID prevents a second gift from being credited.
			var porSolicitud RegistroExterno
			if findErr := s.registros.FindOne(ctx, bson.D{{Key: "solicitudId", Value: req.SolicitudID}}).Decode(&porSolicitud); findErr == nil {
				if !strings.EqualFold(porSolicitud.Email, req.Email) {
					c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "SOLICITUD_YA_PROCESADA", "message": "La solicitud ya fue procesada con otros datos."}})
					return
				}
				c.JSON(http.StatusOK, porSolicitud)
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "REGISTRO_YA_EXISTE", "message": "Ya existe un registro con ese email."}})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "BENEFICIOS_NO_DISPONIBLE", "message": "No se pudo guardar el registro."}})
		return
	}
	c.JSON(http.StatusCreated, registro)
}

func cargarPuntos(raw string) (map[string]int, error) {
	puntos := map[string]int{}
	if strings.TrimSpace(raw) == "" {
		return puntos, nil
	}
	if err := json.Unmarshal([]byte(raw), &puntos); err != nil {
		return nil, err
	}
	for clave, valor := range puntos {
		if normalizarClave(clave) == "" || valor < 0 {
			return nil, errors.New("configuración de puntos inválida")
		}
		puntos[normalizarClave(clave)] = valor
		if clave != normalizarClave(clave) {
			delete(puntos, clave)
		}
	}
	return puntos, nil
}

func normalizarClave(valor string) string {
	return strings.ToLower(strings.TrimSpace(valor))
}
