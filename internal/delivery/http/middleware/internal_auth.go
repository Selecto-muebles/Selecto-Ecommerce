package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

const (
	serviceNameHeader        = "X-Service-Name"
	serviceSignatureHeader   = "X-Service-Signature"
	serviceTimestampHeader   = "X-Service-Timestamp"
	legacySignatureHeader    = "X-Selecto-Signature"
	legacyTimestampHeader    = "X-Selecto-Timestamp"
	idempotencyKeyHeader     = "Idempotency-Key"
	internalSignatureContext = "internal_signature"
	internalTimestampContext = "internal_timestamp"
	internalServiceContext   = "internal_service_name"
	idempotencyKeyContext    = "idempotency_key"
)

func InternalWebhookAuth(secret string, maxSkew time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if secret == "" {
			c.Next()
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			apperrors.BadRequest(c, "could not read request body")
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		headers := normalizedInternalAuthHeaders(c)
		timestamp := headers.timestamp
		signature := headers.signature
		if timestamp == "" || signature == "" {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeInvalidSignature, "missing internal signature", nil)
			c.Abort()
			return
		}

		unixTimestamp, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeInvalidSignature, "invalid internal signature timestamp", nil)
			c.Abort()
			return
		}
		eventTime := time.Unix(unixTimestamp, 0)
		if time.Since(eventTime) > maxSkew || time.Until(eventTime) > maxSkew {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeExpiredSignature, "internal signature timestamp expired", nil)
			c.Abort()
			return
		}

		expected := internalSignature(secret, timestamp, body)
		if !hmac.Equal([]byte(normalizeSignature(signature)), []byte(expected)) {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeInvalidSignature, "invalid internal signature", nil)
			c.Abort()
			return
		}

		c.Set(internalSignatureContext, normalizeSignature(signature))
		c.Set(internalTimestampContext, timestamp)
		c.Set(internalServiceContext, headers.serviceName)
		c.Set(idempotencyKeyContext, headers.idempotencyKey)
		c.Next()
	}
}

type internalAuthHeaders struct {
	timestamp      string
	signature      string
	serviceName    string
	idempotencyKey string
}

func normalizedInternalAuthHeaders(c *gin.Context) internalAuthHeaders {
	timestamp := c.GetHeader(serviceTimestampHeader)
	signature := c.GetHeader(serviceSignatureHeader)
	if timestamp == "" || signature == "" {
		timestamp = c.GetHeader(legacyTimestampHeader)
		signature = c.GetHeader(legacySignatureHeader)
	}

	return internalAuthHeaders{
		timestamp:      timestamp,
		signature:      signature,
		serviceName:    c.GetHeader(serviceNameHeader),
		idempotencyKey: c.GetHeader(idempotencyKeyHeader),
	}
}

func internalSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeSignature(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "sha256=")
	return value
}
