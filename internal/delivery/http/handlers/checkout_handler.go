package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/infrastructure/database"
	paymentsinfra "Selecto-Ecommerce/internal/infrastructure/payments"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	checkoutservice "Selecto-Ecommerce/internal/service/checkout"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

type CheckoutRequest struct {
	OrderID utils.PublicID `json:"order_id"`
}

func CheckoutHandler(db *database.DB, cfg *config.Config, logger *slog.Logger) gin.HandlerFunc {
	service := checkoutservice.NewService(
		postgresrepo.NewCheckoutRepository(db, cfg),
		paymentsinfra.NewClient(cfg.PaymentsServiceURL, cfg.PaymentsRequestTimeout),
		logger,
	)
	return func(c *gin.Context) {
		var input CheckoutRequest
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		if input.OrderID.Int() <= 0 {
			apperrors.BadRequest(c, "invalid order_id")
			return
		}
		result, err := service.Start(c, checkoutservice.Input{
			OrderID: input.OrderID.Int(), Email: fmt.Sprint(c.MustGet("email")),
			RequestID: middleware.RequestID(c), CorrelationID: middleware.CorrelationID(c),
		})
		if err != nil {
			handleCheckoutError(c, err)
			return
		}
		payload := result.Preference.Payload
		if payload == nil {
			payload = map[string]any{"preference_id": result.Preference.ID, "checkout_url": result.Preference.CheckoutURL}
			if result.Preference.Environment != "" {
				payload["environment"] = result.Preference.Environment
			}
		}
		payload["order_id"] = utils.EncodeID(input.OrderID.Int())
		c.JSON(http.StatusOK, payload)
	}
}

func handleCheckoutError(c *gin.Context, err error) {
	var notPending checkoutservice.OrderNotPendingError
	var gatewayError checkoutservice.GatewayError
	switch {
	case errors.Is(err, checkoutservice.ErrOrderNotFound):
		apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, err.Error(), nil)
	case errors.As(err, &notPending):
		details := gin.H(nil)
		if notPending.Status != "" {
			details = gin.H{"status": notPending.Status}
		}
		apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, notPending.Error(), details)
	case errors.Is(err, checkoutservice.ErrGatewayNotConfigured):
		apperrors.Internal(c)
	case errors.As(err, &gatewayError):
		handleGatewayError(c, gatewayError)
	default:
		apperrors.Internal(c)
	}
}

func handleGatewayError(c *gin.Context, err checkoutservice.GatewayError) {
	switch err.Kind {
	case checkoutservice.GatewayUnreachable:
		apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service unreachable", nil)
	case checkoutservice.GatewayInvalid:
		apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "invalid payments service response", nil)
	case checkoutservice.GatewayRejected:
		apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service rejected checkout", gin.H{"payments_response": err.Payload})
	default:
		apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service response missing preference", nil)
	}
}
