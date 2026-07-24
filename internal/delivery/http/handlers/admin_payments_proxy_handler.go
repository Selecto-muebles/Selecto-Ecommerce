package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func AdminPaymentsProxyHandler(cfg *config.Config, targetPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.PaymentsServiceURL == "" {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service is not configured", nil)
			return
		}
		path := targetPath
		for _, param := range c.Params {
			path = strings.ReplaceAll(path, ":"+param.Key, url.PathEscape(param.Value))
		}
		endpoint := cfg.PaymentsServiceURL + path
		if c.Request.URL.RawQuery != "" {
			endpoint += "?" + c.Request.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint, nil)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		addInternalAdminHeaders(req, cfg.InternalWebhookSecret, nil, middleware.RequestID(c), middleware.CorrelationID(c))
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service unreachable", nil)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(resp.StatusCode, contentType, body)
	}
}

func addInternalAdminHeaders(req *http.Request, secret string, body []byte, requestID, correlationID string) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Service-Name", "selecto-ecommerce")
	req.Header.Set("X-Service-Timestamp", timestamp)
	req.Header.Set("X-Service-Signature", "sha256="+internalAdminSignature(secret, timestamp, body))
	req.Header.Set("X-Request-ID", requestID)
	req.Header.Set("X-Correlation-ID", correlationID)
}

func internalAdminSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
