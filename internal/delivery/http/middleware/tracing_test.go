package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceRequestsPreservesHTTPResponse(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(t.Context())
	})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(TraceRequests())
	router.GET("/orders/:id", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	request := httptest.NewRequest(http.MethodGet, "/orders/order-123?token=not-for-telemetry", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"ok\":true}" {
		t.Fatalf("tracing changed HTTP behavior: %d %s", response.Code, response.Body.String())
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "GET /orders/:id" {
		t.Fatalf("expected one span with a route template, got %v", spans)
	}
	for _, field := range spans[0].Attributes() {
		if strings.Contains(field.Value.Emit(), "order-123") || strings.Contains(field.Value.Emit(), "not-for-telemetry") {
			t.Fatalf("trace leaked URL data in %s", field.Key)
		}
	}
}
