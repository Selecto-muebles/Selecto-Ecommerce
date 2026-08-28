package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	orderservice "Selecto-Ecommerce/internal/service/orders"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/collection"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func CreateOrderHandler(db *database.DB, cfg *config.Config, logger *slog.Logger, notifiers ...mailinfra.DispatchNotifier) gin.HandlerFunc {
	creator := orderservice.NewCreator(postgresrepo.NewOrderRepository(db, cfg), logger)
	return func(c *gin.Context) {
		var input CreateOrderInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		rawRequest, err := json.Marshal(input)
		if err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		result, err := creator.Create(c, orderservice.CreateInput{
			Email: fmt.Sprint(c.MustGet("email")), IdempotencyKey: c.GetHeader("Idempotency-Key"), RawRequest: rawRequest,
			Items: collection.Map(input.Items, func(item OrderItemInput) orderservice.Item {
				return orderservice.Item{ProductID: item.ProductID.Int(), Quantity: item.Quantity, SelectedOptions: item.SelectedOptions}
			}),
			Shipping: serviceShippingInput(input.ShippingAddress),
			Now:      time.Now(),
		})
		if err != nil {
			handleCreateOrderError(c, err)
			return
		}
		mailinfra.NotifyAfterCommit(c.Request.Context(), result.EmailOutboxID, notifiers...)
		status := http.StatusCreated
		if result.Replayed {
			status = http.StatusOK
		}
		payload := gin.H{"order_id": utils.EncodeID(result.OrderID), "status": result.Status, "total": result.Total.Float64()}
		if result.Replayed {
			payload["replayed"] = true
		}
		c.JSON(status, payload)
	}
}

func serviceShippingInput(input *CreateOrderShippingInput) *orderservice.ShippingInput {
	if input == nil {
		return nil
	}
	return &orderservice.ShippingInput{Profile: input.normalizedProfile(), RequestedDeliveryDate: input.RequestedDeliveryDate}
}

func handleCreateOrderError(c *gin.Context, err error) {
	var invalidProduct orderservice.InvalidProductError
	var insufficient orderservice.InsufficientStockError
	var invalidOptions orderservice.ProductOptionsError
	var invalidShipping orderservice.InvalidShippingError
	switch {
	case errors.Is(err, orderservice.ErrInvalidIdempotencyKey):
		apperrors.BadRequest(c, err.Error())
	case errors.Is(err, orderservice.ErrEmptyOrder), errors.Is(err, orderservice.ErrTooManyItems), errors.Is(err, orderservice.ErrInvalidItem):
		apperrors.BadRequest(c, err.Error())
	case errors.Is(err, orderservice.ErrItemQuantityLimit):
		apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, err.Error(), gin.H{"max_quantity": orderservice.MaxQuantityPerItem})
	case errors.Is(err, orderservice.ErrOrderQuantityLimit):
		apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, err.Error(), gin.H{"max_total_quantity": orderservice.MaxTotalQuantityPerOrder})
	case errors.Is(err, orderservice.ErrUserNotFound):
		apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, err.Error(), nil)
	case errors.Is(err, orderservice.ErrIdempotencyConflict):
		apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, err.Error(), nil)
	case errors.As(err, &invalidProduct):
		apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, err.Error(), gin.H{"product_id": utils.EncodeID(invalidProduct.ProductID)})
	case errors.As(err, &insufficient):
		apperrors.JSON(c, http.StatusConflict, apperrors.CodeInsufficientStock, err.Error(), gin.H{"product_id": utils.EncodeID(insufficient.ProductID), "available": insufficient.Available})
	case errors.As(err, &invalidOptions):
		apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, err.Error(), gin.H{"product_id": utils.EncodeID(invalidOptions.ProductID)})
	case errors.As(err, &invalidShipping):
		apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, invalidShipping.PublicMessage, nil)
	default:
		apperrors.Internal(c)
	}
}
