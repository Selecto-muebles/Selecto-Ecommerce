package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const sampledTraceFlushTimeout = 250 * time.Millisecond

// TraceRequests records only bounded route templates, never request URLs,
// bodies, headers, customer identifiers, or payment identifiers.
func TraceRequests() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/health" || c.Request.URL.Path == "/ready" {
			c.Next()
			return
		}
		parent := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := otel.Tracer("selecto-ecommerce/http").Start(parent, c.Request.Method+" HTTP", trace.WithSpanKind(trace.SpanKindServer))
		c.Request = c.Request.WithContext(ctx)
		defer func() {
			span.End()
			if !span.SpanContext().IsSampled() {
				return
			}
			// Request-based Cloud Run instances have no guaranteed CPU after this
			// request. Flush sampled spans while the request is still active.
			if flusher, ok := otel.GetTracerProvider().(interface{ ForceFlush(context.Context) error }); ok {
				flushCtx, cancel := context.WithTimeout(context.Background(), sampledTraceFlushTimeout)
				defer cancel()
				_ = flusher.ForceFlush(flushCtx)
			}
		}()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		span.SetName(c.Request.Method + " " + route)
		span.SetAttributes(
			attribute.String("http.request.method", c.Request.Method),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", status),
		)
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
	}
}
