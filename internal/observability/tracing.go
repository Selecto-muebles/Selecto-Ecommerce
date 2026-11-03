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
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const TraceSampleRatio = 0.01

// InitTracing leaves the existing request path untouched until an OTLP endpoint
// is explicitly configured. Export errors are asynchronous and must never fail
// an order, a payment, or the HTTP server.
func InitTracing(ctx context.Context, endpoint, environment, projectID string) (func(context.Context) error, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	if err := ValidateEndpoint(endpoint, environment); err != nil {
		return nil, err
	}
	options := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
	if isGoogleTelemetryEndpoint(endpoint) {
		projectID = strings.TrimSpace(projectID)
		if projectID == "" {
			return nil, fmt.Errorf("OTEL_GCP_PROJECT_ID is required for Google Cloud Telemetry")
		}
		credentials, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, fmt.Errorf("Google Cloud Telemetry credentials: %w", err)
		}
		options = append(options,
			otlptracehttp.WithHTTPClient(oauth2.NewClient(ctx, credentials.TokenSource)),
			otlptracehttp.WithHeaders(map[string]string{"x-goog-user-project": projectID}),
		)
	}
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("initialize OTLP trace exporter: %w", err)
	}
	attributes := []attribute.KeyValue{
		attribute.String("service.name", "selecto-ecommerce"),
		attribute.String("deployment.environment.name", environment),
	}
	if isGoogleTelemetryEndpoint(endpoint) {
		attributes = append(attributes, attribute.String("gcp.project_id", projectID))
	}
	res, err := resource.New(ctx, resource.WithAttributes(attributes...))
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
	if parsed.Hostname() == "telemetry.googleapis.com" && !isGoogleTelemetryEndpoint(endpoint) {
		return fmt.Errorf("Google Cloud Telemetry traces endpoint must be https://telemetry.googleapis.com/v1/traces")
	}
	return nil
}

func isGoogleTelemetryEndpoint(endpoint string) bool {
	return strings.TrimSpace(endpoint) == "https://telemetry.googleapis.com/v1/traces"
}

func TraceDestination(endpoint string) string {
	if isGoogleTelemetryEndpoint(endpoint) {
		return "google_cloud_trace"
	}
	if strings.TrimSpace(endpoint) != "" {
		return "otlp"
	}
	return "none"
}
