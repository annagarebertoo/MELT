package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/codes"
)

// telMiddleware reemplaza a gin.Logger: log JSON por solicitud con request_id y
// trace_id, span servidor y métricas RED por ruta.
func telMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		inicio := time.Now()
		ruta := c.FullPath()
		if ruta == "" {
			ruta = "unmatched"
		}
		ctx, span, id := telIniciarSolicitud(c.Request, ruta)
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", id)
		if tid := telTraceID(span.SpanContext()); tid != "" {
			c.Header("X-Trace-ID", tid)
		}
		defer func() {
			if recuperado := recover(); recuperado != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				span.SetStatus(codes.Error, "panic recuperado")
			}
			telRegistrarSolicitud(ctx, span, c.Request.Method, ruta, c.Writer.Status(), inicio)
			span.End()
		}()
		c.Next()
	}
}

func telMetricasGin(c *gin.Context) { telManejadorMetricas(c.Writer, c.Request) }
