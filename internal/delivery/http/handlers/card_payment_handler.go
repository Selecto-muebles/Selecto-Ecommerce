package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

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

type cardPaymentRequest struct {
	OrderID              utils.PublicID `json:"order_id" binding:"required"`
	PreferenceID         string         `json:"preference_id" binding:"required"`
	Token                string         `json:"token" binding:"required"`
	PaymentMethodID      string         `json:"payment_method_id" binding:"required"`
	IssuerID             int            `json:"issuer_id"`
	Installments         int            `json:"installments" binding:"required"`
	IdentificationType   string         `json:"identification_type"`
	IdentificationNumber string         `json:"identification_number"`
}

func CardPaymentHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	repository := postgresrepo.NewCheckoutRepository(db, cfg)
	client := paymentsinfra.NewClient(cfg.PaymentsServiceURL, cfg.PaymentsRequestTimeout, cfg.InternalWebhookSecret, cfg.PaymentsIDTokenAudience)
	return func(c *gin.Context) {
		var input cardPaymentRequest
		if err := c.ShouldBindJSON(&input); err != nil || input.OrderID.Int() <= 0 ||
			len(input.Token) < 8 || len(input.Token) > 512 || len(input.PreferenceID) > 255 ||
			input.Installments < 1 || input.Installments > 36 {
			apperrors.BadRequest(c, "invalid payment input")
			return
		}
		key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if len(key) != 36 {
			apperrors.BadRequest(c, "idempotency key required")
			return
		}
		email := fmt.Sprint(c.MustGet("email"))
		order, err := repository.LoadAvailable(c.Request.Context(), input.OrderID.Int(), email)
		if err != nil {
			if errors.Is(err, checkoutservice.ErrOrderNotFound) {
				apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "order not available", nil)
			} else {
				apperrors.Internal(c)
			}
			return
		}
		if order.Status != "pending" || order.Preference == nil || order.Preference.ID != input.PreferenceID {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "order or payment preference changed", nil)
			return
		}
		result, status, err := client.CreateCardPayment(c.Request.Context(), paymentsinfra.CardPaymentInput{
			OrderID: order.ID, PreferenceID: order.Preference.ID, Token: input.Token,
			PaymentMethodID: input.PaymentMethodID, IssuerID: input.IssuerID,
			Installments: input.Installments, PayerEmail: order.Customer.Email,
			IdentificationType: input.IdentificationType, IdentificationNumber: input.IdentificationNumber,
			IdempotencyKey: key,
		}, middleware.RequestID(c), middleware.CorrelationID(c))
		if err != nil {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payment result uncertain; check your order before retrying", nil)
			return
		}
		if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
			apperrors.BadRequest(c, "payment data rejected")
			return
		}
		if status == http.StatusConflict {
			apperrors.JSON(c, status, apperrors.CodeConflict, "previous payment requires reconciliation", nil)
			return
		}
		if status < 200 || status >= 300 {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payment could not be verified; check your order before retrying", nil)
			return
		}
		c.JSON(status, result)
	}
}
