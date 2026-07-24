package checkout

import (
	"context"
	"errors"
	"log/slog"

	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/utils"
)

var (
	ErrOrderNotFound        = errors.New("order not found or expired")
	ErrGatewayNotConfigured = errors.New("payments service is not configured")
)

type OrderNotPendingError struct{ Status string }

func (err OrderNotPendingError) Error() string { return "order is not pending" }

type GatewayErrorKind string

const (
	GatewayUnreachable GatewayErrorKind = "unreachable"
	GatewayInvalid     GatewayErrorKind = "invalid_response"
	GatewayRejected    GatewayErrorKind = "rejected"
	GatewayIncomplete  GatewayErrorKind = "incomplete_response"
)

type GatewayError struct {
	Kind    GatewayErrorKind
	Payload map[string]any
}

func (err GatewayError) Error() string { return string(err.Kind) }

type Preference struct {
	ID          string
	CheckoutURL string
	Environment string
	Payload     map[string]any
}

type Order struct {
	ID         int
	Status     string
	Total      money.Cents
	Preference *Preference
}

type Repository interface {
	LoadAvailable(context.Context, int, string) (Order, error)
	SavePreference(context.Context, int, Preference) (bool, error)
	FindPendingPreference(context.Context, int) (*Preference, error)
	WriteAudit(context.Context, int, string, string) error
}

type Gateway interface {
	CreatePreference(context.Context, int, money.Cents, string, string) (Preference, error)
}

type Input struct {
	OrderID       int
	Email         string
	RequestID     string
	CorrelationID string
}

type Result struct {
	Preference Preference
	Reused     bool
}

type Service struct {
	repository Repository
	gateway    Gateway
	logger     *slog.Logger
}

func NewService(repository Repository, gateway Gateway, logger *slog.Logger) *Service {
	return &Service{repository: repository, gateway: gateway, logger: logger}
}

func (service *Service) Start(ctx context.Context, input Input) (Result, error) {
	order, err := service.repository.LoadAvailable(ctx, input.OrderID, input.Email)
	if err != nil {
		return Result{}, err
	}
	if domain.OrderStatus(order.Status) != domain.OrderStatusPending {
		service.logger.Info(logging.EventCheckoutRejected, "order_id", order.ID, "public_id", utils.EncodeID(order.ID), "status", order.Status)
		return Result{}, OrderNotPendingError{Status: order.Status}
	}
	if order.Preference != nil {
		service.logReused(order.ID, order.Preference.ID)
		return Result{Preference: *order.Preference, Reused: true}, nil
	}

	service.logger.Info(logging.EventCheckoutStarted, "order_id", order.ID, "public_id", utils.EncodeID(order.ID), "amount_cents", int64(order.Total))
	preference, err := service.gateway.CreatePreference(ctx, order.ID, order.Total, input.RequestID, input.CorrelationID)
	if err != nil {
		return Result{}, err
	}
	saved, err := service.repository.SavePreference(ctx, order.ID, preference)
	if err != nil {
		return Result{}, err
	}
	if !saved {
		existing, err := service.repository.FindPendingPreference(ctx, order.ID)
		if err != nil {
			return Result{}, err
		}
		if existing == nil {
			return Result{}, OrderNotPendingError{}
		}
		service.logReused(order.ID, existing.ID)
		return Result{Preference: *existing, Reused: true}, nil
	}
	if err := service.repository.WriteAudit(ctx, order.ID, input.Email, preference.ID); err != nil {
		service.logger.Error(logging.EventCheckoutAuditFailed, "order_id", order.ID, "preference_id", preference.ID, "error", err)
	}
	service.logger.Info(logging.EventCheckoutPreferenceCreated, "order_id", order.ID, "public_id", utils.EncodeID(order.ID), "preference_id", preference.ID, "environment", preference.Environment)
	return Result{Preference: preference}, nil
}

func (service *Service) logReused(orderID int, preferenceID string) {
	service.logger.Info(logging.EventCheckoutPreferenceReused, "order_id", orderID, "public_id", utils.EncodeID(orderID), "preference_id", preferenceID)
}
