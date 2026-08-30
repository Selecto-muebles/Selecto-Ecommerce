package orders

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/utils"
	"Selecto-Ecommerce/internal/shared/validation"
)

type InvalidProductError struct{ ProductID int }

func (err InvalidProductError) Error() string { return "invalid product" }

type InsufficientStockError struct {
	ProductID int
	Available int
}

func (err InsufficientStockError) Error() string { return "insufficient stock" }

type InvalidShippingError struct {
	Err           error
	PublicMessage string
}

func (err InvalidShippingError) Error() string { return err.Err.Error() }

type ProductOptionsError struct {
	ProductID int
	Err       error
}

func (err ProductOptionsError) Error() string { return err.Err.Error() }

var (
	ErrInvalidIdempotencyKey = errors.New("Idempotency-Key must contain between 8 and 128 characters")
	ErrUserNotFound          = errors.New("user not found")
	ErrIdempotencyConflict   = errors.New("Idempotency-Key was already used with a different order")
)

type ShippingInput struct {
	Profile               validation.CustomerProfile
	RequestedDeliveryDate string
}

type CreateInput struct {
	Email          string
	IdempotencyKey string
	RawRequest     []byte
	Items          []Item
	Shipping       *ShippingInput
	Now            time.Time
}

type CreateCommand struct {
	Email                 string
	IdempotencyKey        string
	RequestHash           string
	PreparedItems         PreparedItems
	ShippingProfile       validation.CustomerProfile
	ShippingProvided      bool
	RequestedDeliveryDate *time.Time
}

type Reservation struct {
	ProductID      int
	Quantity       int
	RemainingStock int
}

type CreateResult struct {
	OrderID       int
	UserID        int
	EmailOutboxID int64
	Status        string
	Total         money.Cents
	Replayed      bool
	Reservations  []Reservation
}

type CreatorRepository interface {
	Create(context.Context, CreateCommand) (CreateResult, error)
}

type Creator struct {
	repository CreatorRepository
	logger     *slog.Logger
}

func NewCreator(repository CreatorRepository, logger *slog.Logger) *Creator {
	return &Creator{repository: repository, logger: logger}
}

func (service *Creator) Create(ctx context.Context, input CreateInput) (CreateResult, error) {
	key := strings.TrimSpace(input.IdempotencyKey)
	if key != "" && (len(key) < 8 || len(key) > 128) {
		return CreateResult{}, ErrInvalidIdempotencyKey
	}
	prepared, err := PrepareItems(input.Items)
	if err != nil {
		return CreateResult{}, err
	}

	shipping := validation.CustomerProfile{}
	var requestedDate *time.Time
	if input.Shipping != nil {
		shipping = validation.NormalizeCustomerProfile(input.Shipping.Profile)
		requestedDate, err = ParseRequestedDeliveryDate(input.Shipping.RequestedDeliveryDate, input.Now)
		if err != nil {
			return CreateResult{}, InvalidShippingError{Err: err, PublicMessage: err.Error()}
		}
	}
	digest := sha256.Sum256(input.RawRequest)
	result, err := service.repository.Create(ctx, CreateCommand{
		Email: input.Email, IdempotencyKey: key, RequestHash: hex.EncodeToString(digest[:]),
		PreparedItems: prepared, ShippingProfile: shipping, ShippingProvided: input.Shipping != nil, RequestedDeliveryDate: requestedDate,
	})
	if err != nil {
		return CreateResult{}, err
	}
	for _, reservation := range result.Reservations {
		service.logger.Info(logging.EventStockReserved, "product_id", reservation.ProductID, "public_id", utils.EncodeID(reservation.ProductID), "quantity", reservation.Quantity, "remaining_stock", reservation.RemainingStock)
	}
	if !result.Replayed {
		service.logger.Info(logging.EventOrderCreated, "order_id", result.OrderID, "public_id", utils.EncodeID(result.OrderID), "user_id", result.UserID, "total_cents", int64(result.Total), "status", result.Status)
	}
	return result, nil
}
