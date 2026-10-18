package handlers

import (
	"crypto/hmac"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"Selecto-Ecommerce/internal/config"

	"github.com/gin-gonic/gin"
)

func TestMarketplaceOAuthCallbackForwardsOnlySignedCodeAndState(t *testing.T) {
	secret := "ecommerce-payments-internal-secret"
	state := strings.Repeat("a", 43)
	called := false
	payments := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		called = true
		if request.Method != http.MethodPost || request.URL.Path != marketplaceCallbackPath || request.URL.RawQuery != "" {
			t.Errorf("unexpected internal callback request %s %s", request.Method, request.URL)
		}
		body, _ := io.ReadAll(request.Body)
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil || payload["code"] != "secretcode" || payload["state"] != state {
			t.Errorf("invalid forwarded OAuth fields: %v", err)
		}
		want := "sha256=" + internalAdminSignature(secret, request.Header.Get("X-Service-Timestamp"), body)
		if !hmac.Equal([]byte(want), []byte(request.Header.Get("X-Service-Signature"))) {
			t.Error("OAuth callback was not signed with the internal service secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"seller_id":123,"active":false}`))
	}))
	defer payments.Close()
	config := &config.Config{PaymentsServiceURL: payments.URL, InternalWebhookSecret: secret}
	router := gin.New()
	router.GET("/payments/mercadopago/callback", MarketplaceOAuthCallback(config))
	router.GET(marketplaceResultPath, MarketplaceOAuthResult)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/payments/mercadopago/callback?code=secretcode&state="+state, nil))
	if !called || response.Code != http.StatusSeeOther || response.Header().Get("Location") != marketplaceResultPath+"?estado=ok" ||
		strings.Contains(response.Body.String(), "secretcode") || strings.Contains(response.Body.String(), "123") ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe OAuth callback result: status=%d body=%q", response.Code, response.Body.String())
	}
	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, response.Header().Get("Location"), nil))
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "Cuenta vinculada") {
		t.Fatalf("clean OAuth result: status=%d body=%q", result.Code, result.Body.String())
	}
}

func TestMarketplaceOAuthCallbackRejectsInvalidStateWithoutForwarding(t *testing.T) {
	called := false
	payments := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	defer payments.Close()
	router := gin.New()
	router.GET("/payments/mercadopago/callback", MarketplaceOAuthCallback(&config.Config{PaymentsServiceURL: payments.URL}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/payments/mercadopago/callback?code=secretcode&state=bad", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != marketplaceResultPath+"?estado=error" || called {
		t.Fatalf("invalid callback reached Payments: status=%d called=%t", response.Code, called)
	}
}
