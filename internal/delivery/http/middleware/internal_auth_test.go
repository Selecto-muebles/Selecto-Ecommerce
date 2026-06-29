package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestInternalWebhookAuthAcceptsServiceHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "internal-secret"
	body := []byte(`{"payment_id":1}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	router := gin.New()
	router.POST("/payments/webhook", InternalWebhookAuth(secret, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewReader(body))
	req.Header.Set(serviceNameHeader, "selecto-payments")
	req.Header.Set(serviceTimestampHeader, timestamp)
	req.Header.Set(serviceSignatureHeader, "sha256="+internalSignature(secret, timestamp, body))
	req.Header.Set(idempotencyKeyHeader, "payment:1")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

func TestInternalWebhookAuthAcceptsLegacySelectoHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "internal-secret"
	body := []byte(`{"payment_id":1}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	router := gin.New()
	router.POST("/payments/webhook", InternalWebhookAuth(secret, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewReader(body))
	req.Header.Set(legacyTimestampHeader, timestamp)
	req.Header.Set(legacySignatureHeader, internalSignature(secret, timestamp, body))
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

func TestInternalWebhookAuthPrioritizesServiceHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "internal-secret"
	body := []byte(`{"payment_id":1}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	router := gin.New()
	router.POST("/payments/webhook", InternalWebhookAuth(secret, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewReader(body))
	req.Header.Set(serviceTimestampHeader, timestamp)
	req.Header.Set(serviceSignatureHeader, "sha256="+internalSignature(secret, timestamp, body))
	req.Header.Set(legacyTimestampHeader, timestamp)
	req.Header.Set(legacySignatureHeader, "bad")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

func TestInternalWebhookAuthRejectsInvalidSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/payments/webhook", InternalWebhookAuth("internal-secret", time.Minute), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(serviceTimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set(serviceSignatureHeader, "bad")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}
