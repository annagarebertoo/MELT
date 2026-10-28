package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type identidad struct {
	ID     string   `json:"id"`
	Email  string   `json:"email"`
	Roles  []string `json:"roles"`
}

func main() {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"servicio": "reservas", "estado": "ok"})
	})

	// Ruta de integración para verificar que el gateway propaga una identidad
	// verificada. Las futuras operaciones de negocio deben usar requiereIdentidad
	// con los roles que correspondan y aplicar autorización por recurso.
	router.GET("/me", requiereIdentidad(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"usuario": identidadDesdeHeaders(c)})
	})

	if err := router.Run(":8082"); err != nil {
		panic(err)
	}
}

func identidadDesdeHeaders(c *gin.Context) identidad {
	roles := []string{}
	for _, rol := range strings.Split(c.GetHeader("X-Auth-User-Roles"), ",") {
		if rol = strings.TrimSpace(rol); rol != "" {
			roles = append(roles, rol)
		}
	}
	return identidad{
		ID: strings.TrimSpace(c.GetHeader("X-Auth-User-ID")),
		Email: strings.ToLower(strings.TrimSpace(c.GetHeader("X-Auth-User-Email"))),
		Roles: roles,
	}
}

func requiereIdentidad(rolesPermitidos ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		usuario := identidadDesdeHeaders(c)
		if usuario.ID == "" || usuario.Email == "" || len(usuario.Roles) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "IDENTIDAD_REQUERIDA", "message": "Iniciá sesión para continuar."},
			})
			return
		}
		if len(rolesPermitidos) > 0 {
			permitido := false
			for _, actual := range usuario.Roles {
				for _, esperado := range rolesPermitidos {
					if actual == esperado {
						permitido = true
					}
				}
			}
			if !permitido {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "No tenés permisos para realizar esta acción."},
				})
				return
			}
		}
		c.Set("identidad", usuario)
		c.Next()
	}
}
