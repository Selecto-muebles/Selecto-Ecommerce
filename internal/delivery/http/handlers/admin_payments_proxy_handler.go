package handlers

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	paymentsinfra "Selecto-Ecommerce/internal/infrastructure/payments"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/serviceauth"

	"github.com/gin-gonic/gin"
)

func AdminPaymentsProxyHandler(cfg *config.Config, targetPath string) gin.HandlerFunc {
	httpClient := paymentsinfra.NewHTTPClient(10*time.Second, cfg.PaymentsIDTokenAudience)
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
		resp, err := httpClient.Do(req)
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
	serviceauth.AddHeaders(req, secret, body, requestID, correlationID)
}

func internalAdminSignature(secret, timestamp string, body []byte) string {
	return serviceauth.Signature(secret, timestamp, body)
}
