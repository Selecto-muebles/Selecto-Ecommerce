package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
