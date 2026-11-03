package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestValidateEndpoint(t *testing.T) {
	for _, tc := range []struct {
		endpoint, environment string
		valid                 bool
	}{
		{"https://collector.example/v1/traces", "production", true},
		{"http://localhost:4318/v1/traces", "development", true},
		{"http://collector.example/v1/traces", "production", false},
		{"https://user:password@collector.example/v1/traces", "production", false},
		{"https://collector.example/v1/traces?token=secret", "production", false},
		{"not-a-url", "development", false},
		{"https://telemetry.googleapis.com/v1/traces", "production", true},
		{"https://telemetry.googleapis.com/v1/metrics", "production", false},
	} {
		err := ValidateEndpoint(tc.endpoint, tc.environment)
		if (err == nil) != tc.valid {
			t.Errorf("ValidateEndpoint(%q, %q) = %v, valid=%v", tc.endpoint, tc.environment, err, tc.valid)
		}
	}
}

func TestTracingDisabledByDefault(t *testing.T) {
	shutdown, err := InitTracing(context.Background(), "", "production", "")
	if err != nil || shutdown == nil {
		t.Fatalf("disabled tracing must not affect startup: shutdown=%v err=%v", shutdown != nil, err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("disabled tracing shutdown: %v", err)
	}
}

func TestGoogleTelemetryRequiresProject(t *testing.T) {
	_, err := InitTracing(context.Background(), "https://telemetry.googleapis.com/v1/traces", "production", "")
	if err == nil {
		t.Fatal("Google telemetry must have an explicit project")
	}
}

func TestTraceDestination(t *testing.T) {
	if TraceDestination("https://telemetry.googleapis.com/v1/traces") != "google_cloud_trace" || TraceDestination("") != "none" {
		t.Fatal("unexpected trace destination")
	}
}

func TestTracingExportsSampledSpanToOTLP(t *testing.T) {
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/traces" || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("unexpected OTLP request: %s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		received <- r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	shutdown, err := InitTracing(context.Background(), server.URL+"/v1/traces", "development", "")
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{"Traceparent": {"00-00000000000000000000000000000001-0000000000000001-01"}}
	parent := propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(header))
	_, span := otel.Tracer("test").Start(parent, "sampled request")
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	default:
		t.Fatal("sampled trace was not exported")
	}
}
