package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/gestionar"

	"github.com/gin-gonic/gin"
)

type pickupReaderStub struct {
	called bool
	err    error
}

func (stub *pickupReaderStub) PickupTimes(context.Context) ([]gestionar.PickupTime, error) {
	stub.called = true
	return []gestionar.PickupTime{{ID: 7, Time: "14:00 a 18:00"}}, stub.err
}

func TestGestionarHandlerNeverCallsProviderWithoutConfiguration(t *testing.T) {
	stub := &pickupReaderStub{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/pickup-times", nil)
	AdminGestionarPickupTimesHandler(&config.Config{}, stub)(c)
	if recorder.Code != http.StatusServiceUnavailable || stub.called {
		t.Fatalf("status = %d, provider called = %t", recorder.Code, stub.called)
	}
}

func TestGestionarHandlerReturnsSanitizedPickupTimes(t *testing.T) {
	cfg := &config.Config{GestionarAPIBaseURL: "https://sandbox.example.test", GestionarSecretToken: "sandbox-test-token-123"}
	stub := &pickupReaderStub{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/pickup-times", nil)
	AdminGestionarPickupTimesHandler(cfg, stub)(c)
	if recorder.Code != http.StatusOK || !stub.called || !strings.Contains(recorder.Body.String(), "14:00 a 18:00") || strings.Contains(recorder.Body.String(), cfg.GestionarSecretToken) {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stub.err = errors.New("private provider detail")
	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/pickup-times", nil)
	AdminGestionarPickupTimesHandler(cfg, stub)(c)
	if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "private provider detail") {
		t.Fatalf("provider error leaked: %s", recorder.Body.String())
	}
}

func TestGestionarPickupTimesFromTLSProviderToAdmin(t *testing.T) {
	const token = "sandbox-test-token-123"
	var requests atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/external-client/pickup-times" || r.Header.Get("secret-token-key") != token {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"Success","success":true,"data":[{"id":7,"time":"14:00 a 18:00","secret_token":"provider-private"}]}`))
	}))
	defer provider.Close()

	cfg := &config.Config{GestionarAPIBaseURL: provider.URL, GestionarSecretToken: token}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/pickup-times", nil)
	AdminGestionarPickupTimesHandler(cfg, gestionar.NewClient(provider.URL, token, provider.Client()))(c)
	if recorder.Code != http.StatusOK || requests.Load() != 1 || !strings.Contains(recorder.Body.String(), `"time":"14:00 a 18:00"`) ||
		strings.Contains(recorder.Body.String(), token) || strings.Contains(recorder.Body.String(), "provider-private") {
		t.Fatalf("unexpected admin response: status=%d requests=%d body=%s", recorder.Code, requests.Load(), recorder.Body.String())
	}
}
