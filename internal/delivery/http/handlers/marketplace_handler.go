package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	paymentsinfra "Selecto-Ecommerce/internal/infrastructure/payments"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

const marketplaceCallbackPath = "/internal/marketplace/callback"
const marketplaceResultPath = "/payments/mercadopago/resultado"

func AdminMarketplaceProxyHandler(cfg *config.Config, targetPath string) gin.HandlerFunc {
	client := paymentsinfra.NewHTTPClient(12*time.Second, cfg.PaymentsIDTokenAudience)
	return func(c *gin.Context) {
		if cfg.PaymentsServiceURL == "" {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service is not configured", nil)
			return
		}
		var body []byte
		if c.Request.Method == http.MethodPost && targetPath == "/internal/admin/marketplace/activate" {
			var err error
			body, err = io.ReadAll(io.LimitReader(c.Request.Body, 1024))
			if err != nil || len(body) == 0 || len(body) >= 1024 || !json.Valid(body) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid seller id"})
				return
			}
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method,
			strings.TrimRight(cfg.PaymentsServiceURL, "/")+targetPath, bytes.NewReader(body))
		if err != nil {
			apperrors.Internal(c)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		addInternalAdminHeaders(req, cfg.InternalWebhookSecret, body, middleware.RequestID(c), middleware.CorrelationID(c))
		response, err := client.Do(req)
		if err != nil {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service unreachable", nil)
			return
		}
		defer response.Body.Close()
		result, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		c.Header("Cache-Control", "no-store")
		c.Data(response.StatusCode, "application/json", result)
	}
}

// MarketplaceOAuthCallback accepts only the one-time code and state from MP.
// The code never appears in an admin URL, browser response, or application log.
func MarketplaceOAuthCallback(cfg *config.Config) gin.HandlerFunc {
	client := paymentsinfra.NewHTTPClient(12*time.Second, cfg.PaymentsIDTokenAudience)
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		code, state := c.Query("code"), c.Query("state")
		if cfg.PaymentsServiceURL == "" || len(code) < 8 || len(code) > 2048 || len(state) != 43 || c.Query("error") != "" {
			c.Redirect(http.StatusSeeOther, marketplaceResultPath+"?estado=error")
			return
		}
		payload, _ := json.Marshal(gin.H{"code": code, "state": state})
		ctx, cancel := context.WithTimeout(c.Request.Context(), 13*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimRight(cfg.PaymentsServiceURL, "/")+marketplaceCallbackPath, bytes.NewReader(payload))
		if err != nil {
			c.Redirect(http.StatusSeeOther, marketplaceResultPath+"?estado=error")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		addInternalAdminHeaders(req, cfg.InternalWebhookSecret, payload, middleware.RequestID(c), middleware.CorrelationID(c))
		response, err := client.Do(req)
		if err != nil {
			c.Redirect(http.StatusSeeOther, marketplaceResultPath+"?estado=error")
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			c.Redirect(http.StatusSeeOther, marketplaceResultPath+"?estado=error")
			return
		}
		c.Redirect(http.StatusSeeOther, marketplaceResultPath+"?estado=ok")
	}
}

func MarketplaceOAuthResult(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if c.Query("estado") != "ok" {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte("No se pudo vincular la cuenta. Volvé a intentarlo desde Selecto Admin."))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("Cuenta vinculada. Avisale al administrador de Selecto para que confirme la cuenta antes de habilitar los cobros."))
}
