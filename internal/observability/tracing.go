package observability

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const TraceSampleRatio = 0.01

// InitTracing leaves the existing request path untouched until an OTLP endpoint
// is explicitly configured. Export errors are asynchronous and must never fail
// an order, a payment, or the HTTP server.
func InitTracing(ctx context.Context, endpoint, environment string) (func(context.Context) error, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	if err := ValidateEndpoint(endpoint, environment); err != nil {
		return nil, err
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("initialize OTLP trace exporter: %w", err)
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", "selecto-ecommerce"),
		attribute.String("deployment.environment.name", environment),
	))
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return nil, fmt.Errorf("initialize trace resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(TraceSampleRatio)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return provider.Shutdown, nil
}

func ValidateEndpoint(endpoint, environment string) error {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must be an HTTP(S) URL without credentials or query")
	}
	if parsed.Scheme != "https" && (environment == "production" || parsed.Scheme != "http") {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must use HTTPS in production")
	}
	return nil
}
