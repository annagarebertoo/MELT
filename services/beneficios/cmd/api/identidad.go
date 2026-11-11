package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var errSesionInvalida = errors.New("sesión inválida")

type identidadCliente struct {
	ID, Email string
	Roles     []string
}

// La identidad procede exclusivamente de Auth después de verificar el Bearer.
// Los X-Auth-User-* del request no participan, incluso en accesos directos.
func (s *Servidor) requiereCliente() gin.HandlerFunc {
	return func(c *gin.Context) {
		partes := strings.Fields(c.GetHeader("Authorization"))
		if len(partes) != 2 || !strings.EqualFold(partes[0], "Bearer") {
			c.AbortWithStatusJSON(401, gin.H{"error": gin.H{"code": "TOKEN_REQUERIDO", "message": "Iniciá sesión para consultar tus puntos."}})
			return
		}
		if s == nil || s.verificarSesion == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"code": "AUTH_NO_DISPONIBLE", "message": "No se pudo verificar la sesión."}})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		identidad, err := s.verificarSesion(ctx, "Bearer "+partes[1])
		if errors.Is(err, errSesionInvalida) {
			c.AbortWithStatusJSON(401, gin.H{"error": gin.H{"code": "TOKEN_INVALIDO", "message": "La sesión no es válida."}})
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"code": "AUTH_NO_DISPONIBLE", "message": "No se pudo verificar la sesión."}})
			return
		}
		for _, rol := range identidad.Roles {
			if strings.TrimSpace(rol) == "cliente" {
				c.Set("authUserID", identidad.ID)
				c.Set("authUserEmail", identidad.Email)
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(403, gin.H{"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "Esta operación es exclusiva para clientes."}})
	}
}

func verificadorSesionAuth(base string) func(context.Context, string) (identidadCliente, error) {
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, bearer string) (identidadCliente, error) {
		if base == "" {
			return identidadCliente{}, errors.New("Auth no configurado")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/verify", nil)
		if err != nil {
			return identidadCliente{}, err
		}
		req.Header.Set("Authorization", bearer)
		resp, err := client.Do(req)
		if err != nil {
			return identidadCliente{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return identidadCliente{}, errSesionInvalida
		}
		if resp.StatusCode != 204 {
			return identidadCliente{}, errors.New("Auth no disponible")
		}
		identidad := identidadCliente{ID: strings.TrimSpace(resp.Header.Get("X-Auth-User-ID")), Email: strings.TrimSpace(resp.Header.Get("X-Auth-User-Email")), Roles: strings.Split(resp.Header.Get("X-Auth-User-Roles"), ",")}
		if identidad.ID == "" || identidad.Email == "" {
			return identidadCliente{}, errors.New("respuesta incompleta de Auth")
		}
		return identidad, nil
	}
}
