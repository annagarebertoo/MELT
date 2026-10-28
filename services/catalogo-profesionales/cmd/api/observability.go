package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const serviceName = "catalogo-profesionales"

type requestIDKey struct{}

// The gateway uses the same bounded allowlist. Never log arbitrary header values.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func requestID(value string) string {
	if validRequestID.MatchString(value) {
		return value
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		// crypto/rand failure is not recoverable on supported Go platforms.
		panic("request ID generation failed")
	}
	return hex.EncodeToString(bytes[:])
}

func configureLogger() *slog.Logger {
	logger := newJSONLogger(os.Stdout)
	slog.SetDefault(logger)
	return logger
}

func newJSONLogger(output io.Writer) *slog.Logger {
	instance, _ := os.Hostname()
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				attr.Key = "timestamp"
			}
			return attr
		},
	})).With(
		"service", serviceName, "instance", instance,
	)
}

func configureTracing(ctx context.Context) (func(context.Context) error, error) {
	// Without an endpoint, local unit tests and standalone startup need no collector.
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
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
	instance, _ := os.Hostname()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", serviceName),
			attribute.String("service.instance.id", instance),
		)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Second)),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		// Export errors may contain endpoints or credentials. Keep a safe event only.
		slog.Warn("telemetry_export_failed")
	}))
	return provider.Shutdown, nil
}

func mongoMonitor() *event.CommandMonitor {
	monitor := otelmongo.NewMonitor(otelmongo.WithCommandAttributeDisabled(true))
	failed := monitor.Failed
	monitor.Failed = func(ctx context.Context, original *event.CommandFailedEvent) {
		copy := *original
		// Do not export a server error that could include query values or credentials.
		copy.Failure = errors.New("MongoDB command failed")
		failed(ctx, &copy)
	}
	return monitor
}

func observabilityMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		id := requestID(c.GetHeader("X-Request-ID"))
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		ctx := propagation.TraceContext{}.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx = context.WithValue(ctx, requestIDKey{}, id)
		ctx, span := otel.Tracer("melt/catalogo/http").Start(ctx, c.Request.Method+" "+route,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", c.Request.Method),
				attribute.String("http.route", route),
				attribute.String("request.id", id),
			),
		)
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", id)
		spanContext := span.SpanContext()
		if spanContext.IsValid() {
			c.Header("X-Trace-ID", spanContext.TraceID().String())
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				// Preserve Gin Recovery's HTTP 500 behavior without dumping headers or payloads.
				c.AbortWithStatus(http.StatusInternalServerError)
				span.SetStatus(codes.Error, "request panic recovered")
			}
			status := c.Writer.Status()
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
				span.SetStatus(codes.Error, "HTTP server error")
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			logger.Log(ctx, level, "http_request",
				"method", c.Request.Method, "route", route,
				"status", status, "duration_ms", float64(time.Since(started).Microseconds())/1000,
				"request_id", id, "trace_id", traceID(spanContext), "span_id", spanID(spanContext),
			)
			span.End()
		}()
		c.Next()
	}
}

func traceID(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

func spanID(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}

func logDatabaseError(c *gin.Context, operation string) {
	ctx := c.Request.Context()
	span := trace.SpanFromContext(ctx)
	span.SetStatus(codes.Error, "MongoDB operation failed")
	slog.ErrorContext(ctx, "database_operation_failed", "operation", operation,
		"request_id", ctx.Value(requestIDKey{}), "trace_id", traceID(span.SpanContext()),
	)
}
