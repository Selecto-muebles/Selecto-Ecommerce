package middleware_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/delivery/http/middleware"

	"github.com/gin-gonic/gin"
)

const (
	serviceNameHeader      = "X-Service-Name"
	serviceSignatureHeader = "X-Service-Signature"
	serviceTimestampHeader = "X-Service-Timestamp"
	legacySignatureHeader  = "X-Selecto-Signature"
	legacyTimestampHeader  = "X-Selecto-Timestamp"
	idempotencyKeyHeader   = "Idempotency-Key"
)

func TestInternalWebhookAuthAcceptsServiceHeaders(t *testing.T) {
	resp := performInternalWebhookAuthRequest(t, authRequest{
		setHeaders: func(req *http.Request, secret string, timestamp string, body []byte) {
			req.Header.Set(serviceNameHeader, "selecto-payments")
			req.Header.Set(serviceTimestampHeader, timestamp)
			req.Header.Set(serviceSignatureHeader, "sha256="+internalSignature(secret, timestamp, body))
			req.Header.Set(idempotencyKeyHeader, "payment:1")
		},
	})

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

func TestInternalWebhookAuthAcceptsLegacySelectoHeaders(t *testing.T) {
	resp := performInternalWebhookAuthRequest(t, authRequest{
		setHeaders: func(req *http.Request, secret string, timestamp string, body []byte) {
			req.Header.Set(legacyTimestampHeader, timestamp)
			req.Header.Set(legacySignatureHeader, internalSignature(secret, timestamp, body))
		},
	})

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

func TestInternalWebhookAuthPrioritizesServiceHeaders(t *testing.T) {
	resp := performInternalWebhookAuthRequest(t, authRequest{
		setHeaders: func(req *http.Request, secret string, timestamp string, body []byte) {
			req.Header.Set(serviceTimestampHeader, timestamp)
			req.Header.Set(serviceSignatureHeader, "sha256="+internalSignature(secret, timestamp, body))
			req.Header.Set(legacyTimestampHeader, timestamp)
			req.Header.Set(legacySignatureHeader, "bad")
		},
	})

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

func TestInternalWebhookAuthRejectsInvalidSignature(t *testing.T) {
	resp := performInternalWebhookAuthRequest(t, authRequest{
		setHeaders: func(req *http.Request, _ string, timestamp string, _ []byte) {
			req.Header.Set(serviceTimestampHeader, timestamp)
			req.Header.Set(serviceSignatureHeader, "bad")
		},
	})

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

type authRequest struct {
	setHeaders func(req *http.Request, secret string, timestamp string, body []byte)
}

func performInternalWebhookAuthRequest(t *testing.T, request authRequest) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	secret := "internal-secret"
	body := []byte(`{"payment_id":1}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	router := gin.New()
	router.POST("/payments/webhook", middleware.InternalWebhookAuth(secret, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewReader(body))
	request.setHeaders(req, secret, timestamp, body)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)
	return resp
}

func internalSignature(secret string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
