package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOpenSearchClientSpanIsCorrelatedAndOmitsQuery(t *testing.T) {
	exporter := testTraceProvider(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	ctx, parent := otel.Tracer("test").Start(context.Background(), "HTTP catalogo")
	client := newSearchClient(server.URL)
	if _, err := client.request(ctx, http.MethodGet, "/_search?query=private-value", nil); err != nil {
		t.Fatalf("OpenSearch request failed: %v", err)
	}
	parent.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected request and OpenSearch spans, got %d", len(spans))
	}
	var requestSpan, searchSpan *tracetest.SpanStub
	for index := range spans {
		span := &spans[index]
		switch span.Name {
		case "HTTP catalogo":
			requestSpan = span
		case "OpenSearch GET":
			searchSpan = span
		}
	}
	if requestSpan == nil || searchSpan == nil || searchSpan.Parent.SpanID() != requestSpan.SpanContext.SpanID() {
		t.Fatalf("OpenSearch span is not a child of the request span: %+v", spans)
	}
	for _, attr := range searchSpan.Attributes {
		if attr.Key == attribute.Key("url.full") || attr.Key == attribute.Key("url.query") {
			t.Fatalf("OpenSearch span exported a raw URL attribute: %s", attr.Key)
		}
		if attr.Value.Type() == attribute.STRING && attr.Value.AsString() == "private-value" {
			t.Fatal("OpenSearch span exported query data")
		}
	}
}

func TestMemcachedSpanIsCorrelatedAndHasBoundedAttributes(t *testing.T) {
	exporter := testTraceProvider(t)
	ctx, parent := otel.Tracer("test").Start(context.Background(), "HTTP catalogo")
	_, span := startMemcachedSpan(ctx, "get")
	span.SetAttributes(attribute.Bool("cache.hit", false))
	span.End()
	parent.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected request and Memcached spans, got %d", len(spans))
	}
	var requestSpan, cacheSpan *tracetest.SpanStub
	for index := range spans {
		span := &spans[index]
		switch span.Name {
		case "HTTP catalogo":
			requestSpan = span
		case "Memcached GET":
			cacheSpan = span
		}
	}
	if requestSpan == nil || cacheSpan == nil || cacheSpan.Parent.SpanID() != requestSpan.SpanContext.SpanID() {
		t.Fatalf("Memcached span is not a child of the request span: %+v", spans)
	}
}
