package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const ghostKitchenDefaultBaseURL = "https://57-156-56-219.sslip.io/public/v1"

// ghostKitchenClient keeps the partner credential on the server and limits each
// upstream request to the timeout defined by the provider contract.
type ghostKitchenClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type ghostKitchenProblem struct {
	Type          string `json:"type,omitempty"`
	Title         string `json:"title,omitempty"`
	Status        int    `json:"status,omitempty"`
	Code          string `json:"code,omitempty"`
	Detail        string `json:"detail,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
}

type ghostKitchenResponse struct {
	status        int
	body          []byte
	correlationID string
}

func newGhostKitchenClient(apiKey string, transport http.RoundTripper, baseURL string) *ghostKitchenClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = ghostKitchenDefaultBaseURL
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &ghostKitchenClient{baseURL: strings.TrimRight(baseURL, "/"), apiKey: strings.TrimSpace(apiKey), http: &http.Client{Transport: transport, Timeout: 3 * time.Second}}
}

func ghostKitchenFromEnv() *ghostKitchenClient {
	return newGhostKitchenClient(os.Getenv("GHOSTKITCHEN_API_KEY"), nil, os.Getenv("GHOSTKITCHEN_BASE_URL"))
}

func (g *ghostKitchenClient) request(ctx context.Context, method, path string, query url.Values, body []byte, correlationID string) (ghostKitchenResponse, error) {
	if g == nil || g.apiKey == "" {
		return ghostKitchenResponse{}, errors.New("integración GhostKitchen no configurada")
	}
	endpoint := g.baseURL + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return ghostKitchenResponse{}, err
		}
		req.Header.Set("X-API-Key", g.apiKey)
		req.Header.Set("Accept", "application/json")
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		if correlationID != "" {
			req.Header.Set("X-Correlation-ID", correlationID)
		}
		resp, err := g.http.Do(req)
		if err != nil {
			return ghostKitchenResponse{}, err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return ghostKitchenResponse{}, readErr
		}
		response := ghostKitchenResponse{status: resp.StatusCode, body: data, correlationID: resp.Header.Get("X-Correlation-ID")}
		var problem ghostKitchenProblem
		_ = json.Unmarshal(data, &problem)
		if problem.CorrelationID != "" {
			response.correlationID = problem.CorrelationID
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
			return response, nil
		}
		if attempt == 3 {
			return response, nil
		}
		delay := retryAfter(resp.Header.Get("Retry-After"), attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ghostKitchenResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	return ghostKitchenResponse{}, errors.New("se agotaron los reintentos de GhostKitchen")
}

func retryAfter(value string, attempt int) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(attempt*2) * time.Second
}

func (s *Servidor) agregarRutasGhostKitchen(r *gin.Engine) {
	r.GET("/ghostkitchen/sedes", s.requiereCliente(), s.listarSedesGhostKitchen)
	r.GET("/ghostkitchen/sedes/cercana", s.requiereCliente(), s.sedeCercanaGhostKitchen)
	r.GET("/ghostkitchen/sedes/:sedeId/platos", s.requiereCliente(), s.listarPlatosGhostKitchen)
	r.GET("/ghostkitchen/platos/:platoId", s.requiereCliente(), s.detallePlatoGhostKitchen)
	r.POST("/ghostkitchen/proximidad", s.requiereCliente(), s.compararProximidadGhostKitchen)
}

func (s *Servidor) proxyGhostKitchen(c *gin.Context, method, path string, query url.Values, body []byte) {
	client := ghostKitchenFromEnv()
	if client.apiKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "GHOSTKITCHEN_NO_CONFIGURADO", "message": "La integración de cocinas cercanas todavía no está configurada."}})
		return
	}
	correlationID := c.GetHeader("X-Request-ID")
	if correlationID == "" {
		correlationID = c.GetHeader("X-Correlation-ID")
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	response, err := client.request(ctx, method, path, query, body, correlationID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "GHOSTKITCHEN_NO_DISPONIBLE", "message": "No pudimos consultar las cocinas cercanas."}})
		return
	}
	if response.correlationID != "" {
		c.Header("X-Correlation-ID", response.correlationID)
	}
	c.Data(response.status, "application/json; charset=utf-8", response.body)
}

func (s *Servidor) listarSedesGhostKitchen(c *gin.Context) {
	s.proxyGhostKitchen(c, http.MethodGet, "sedes", nil, nil)
}

func (s *Servidor) sedeCercanaGhostKitchen(c *gin.Context) {
	query := url.Values{}
	query.Set("lat", c.Query("lat"))
	query.Set("lng", c.Query("lng"))
	s.proxyGhostKitchen(c, http.MethodGet, "sedes/cercana", query, nil)
}

var ghostKitchenPlatoParams = []string{"q", "categoria", "etiqueta", "restauranteId", "precioMax", "soloDisponibles", "orden", "page", "pageSize"}

func (s *Servidor) listarPlatosGhostKitchen(c *gin.Context) {
	query := url.Values{}
	for _, key := range ghostKitchenPlatoParams {
		if values, ok := c.Request.URL.Query()[key]; ok {
			for _, value := range values {
				query.Add(key, value)
			}
		}
	}
	s.proxyGhostKitchen(c, http.MethodGet, "sedes/"+url.PathEscape(c.Param("sedeId"))+"/platos", query, nil)
}

func (s *Servidor) detallePlatoGhostKitchen(c *gin.Context) {
	s.proxyGhostKitchen(c, http.MethodGet, "platos/"+url.PathEscape(c.Param("platoId")), nil, nil)
}

func (s *Servidor) compararProximidadGhostKitchen(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"code": "SOLICITUD_DEMASIADO_GRANDE", "message": "La solicitud supera el tamaño permitido."}})
		return
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "PARAMETRO_INVALIDO", "message": "El cuerpo debe ser un JSON válido."}})
		return
	}
	s.proxyGhostKitchen(c, http.MethodPost, "proximidad", nil, body)
}
