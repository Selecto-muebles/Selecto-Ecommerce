package postgres

import (
	"context"
	"fmt"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/domain"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	orderservice "Selecto-Ecommerce/internal/service/orders"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/utils"
	"Selecto-Ecommerce/internal/shared/validation"

	"github.com/jackc/pgx/v5"
)

func insertOrder(ctx context.Context, tx pgx.Tx, cfg *config.Config, userID int, command orderservice.CreateCommand, total money.Cents) (int, error) {
	var orderID int
	err := tx.QueryRow(ctx, `INSERT INTO orders (user_id, status, total, expires_at, idempotency_key, request_hash)
		VALUES ($1, $2, $3, NOW() + make_interval(secs => $4), NULLIF($5, ''), NULLIF($6, '')) RETURNING id`,
		userID, domain.OrderStatusPending, total.DecimalString(), int(cfg.OrderPendingTTL.Seconds()), command.IdempotencyKey, command.RequestHash,
	).Scan(&orderID)
	return orderID, err
}

func insertShippingAddress(ctx context.Context, tx pgx.Tx, orderID int, profile validation.CustomerProfile, command orderservice.CreateCommand) error {
	_, err := tx.Exec(ctx, `INSERT INTO order_shipping_addresses (
		order_id, recipient_first_name, recipient_last_name, dni, street_address, street_number,
		postal_code, province, locality, phone_number, requested_delivery_date
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		orderID, profile.FirstName, profile.LastName, profile.DNI, profile.StreetAddress, profile.StreetNumber,
		profile.PostalCode, profile.Province, profile.Locality, profile.PhoneNumber, command.RequestedDeliveryDate,
	)
	return err
}

func insertOrderItems(ctx context.Context, tx pgx.Tx, orderID int, items []reservedOrderItem) error {
	for _, item := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO order_items (order_id, product_id, quantity, price, selected_options)
			VALUES ($1, $2, $3, $4, $5)`, orderID, item.productID, item.quantity, item.price.DecimalString(), item.selectedOptions); err != nil {
			return err
		}
	}
	return nil
}

func insertOrderAuditAndEmail(ctx context.Context, tx pgx.Tx, cfg *config.Config, orderID int, email string, total money.Cents) error {
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata)
		VALUES ($1, 'order_created', 'order', $2, $3)`, email, orderID, fmt.Sprintf(`{"total_cents":%d}`, total)); err != nil {
		return err
	}
	publicID := utils.EncodeID(orderID)
	return mailinfra.Enqueue(ctx, tx, fmt.Sprintf("order-created:%d", orderID), email, "order_created", map[string]any{
		"order_id": publicID,
		"total":    fmt.Sprintf("$ %.2f", total.Float64()),
		"url":      cfg.StorefrontURL + "/cuenta/ordenes/" + publicID,
	})
}
