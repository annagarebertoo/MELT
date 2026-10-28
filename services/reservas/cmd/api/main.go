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


// permisosPorOperacion define la autorización a nivel de operación (no solo
// a nivel de ruta). Cuando se implementen los handlers de turnos, deben
// registrar cada operación con requierePermiso usando una de estas claves.
var permisosPorOperacion = map[string]map[string]bool{
	"reservas.consultar-propias": {"cliente": true},
	"reservas.crear":             {"cliente": true},
	"reservas.cancelar-propia":   {"cliente": true},
	"reservas.consultar-agenda":  {"profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.gestionar-agenda":  {"profesional": true, "recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.consultar-todas":  {"recepcion": true, "administrador": true, "superadministrador": true},
	"reservas.gestionar-turnos":  {"recepcion": true, "administrador": true, "superadministrador": true},
}

func requierePermiso(operacion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		usuario, ok := c.Get("identidad")
		if !ok {
			// También valida que los headers estén completos. En producción,
			// el gateway es quien los escribe después de verificar el JWT.
			identidadVerificada := identidadDesdeHeaders(c)
			if identidadVerificada.ID == "" || identidadVerificada.Email == "" || len(identidadVerificada.Roles) == 0 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": gin.H{"code": "IDENTIDAD_REQUERIDA", "message": "Iniciá sesión para continuar."},
				})
				return
			}
			usuario = identidadVerificada
		}
		identidadActual, ok := usuario.(identidad)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "IDENTIDAD_INVALIDA", "message": "No se pudo verificar la identidad."},
			})
			return
		}
		rolesPermitidos, existe := permisosPorOperacion[operacion]
		if !existe {
			// Denegar por defecto: una operación sin política nunca queda abierta.
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"code": "OPERACION_SIN_PERMISO", "message": "La operación no tiene permisos configurados."},
			})
			return
		}
		for _, rol := range identidadActual.Roles {
			if rolesPermitidos[rol] {
				c.Set("identidad", identidadActual)
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": gin.H{"code": "PERMISOS_INSUFICIENTES", "message": "No tenés permisos para realizar esta acción."},
		})
	}
}
