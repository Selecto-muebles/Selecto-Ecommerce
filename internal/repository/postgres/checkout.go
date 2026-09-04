package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	checkoutservice "Selecto-Ecommerce/internal/service/checkout"

	"github.com/jackc/pgx/v5"
)

type CheckoutRepository struct {
	db  *database.DB
	cfg *config.Config
}

func NewCheckoutRepository(db *database.DB, cfg *config.Config) *CheckoutRepository {
	return &CheckoutRepository{db: db, cfg: cfg}
}

func (repository *CheckoutRepository) LoadAvailable(ctx context.Context, orderID int, email string) (checkoutservice.Order, error) {
	var order checkoutservice.Order
	var preferenceID, checkoutURL, environment sql.NullString
	err := repository.db.Pool.QueryRow(ctx, `SELECT o.status, ROUND(o.total * 100)::BIGINT,
		o.active_payment_preference_id, o.active_checkout_url, o.active_payment_environment,
		u.email, TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), COALESCE(u.dni, '')
		FROM orders o JOIN users u ON u.id=o.user_id
		WHERE o.id=$1 AND u.email=$2
		AND COALESCE(o.expires_at, o.created_at + make_interval(secs => $3)) > NOW()`,
		orderID, email, int(repository.cfg.OrderPendingTTL.Seconds()),
	).Scan(&order.Status, &order.Total, &preferenceID, &checkoutURL, &environment,
		&order.Customer.Email, &order.Customer.Name, &order.Customer.Identification)
	if errors.Is(err, pgx.ErrNoRows) {
		return order, checkoutservice.ErrOrderNotFound
	}
	if err != nil {
		return order, err
	}
	order.ID = orderID
	order.Preference = nullablePreference(preferenceID, checkoutURL, environment)
	return order, nil
}

func (repository *CheckoutRepository) SavePreference(ctx context.Context, orderID int, preference checkoutservice.Preference) (bool, error) {
	command, err := repository.db.Pool.Exec(ctx, `UPDATE orders SET
		active_payment_preference_id=$1, active_checkout_url=$2, active_payment_environment=$3,
		expires_at=NOW() + make_interval(secs => $6)
		WHERE id=$4 AND status=$5 AND active_payment_preference_id IS NULL
		AND COALESCE(expires_at, created_at + make_interval(secs => $6)) > NOW()`,
		preference.ID, preference.CheckoutURL, preference.Environment, orderID, domain.OrderStatusPending,
		int(repository.cfg.OrderPendingTTL.Seconds()),
	)
	return command.RowsAffected() == 1, err
}

func (repository *CheckoutRepository) FindPendingPreference(ctx context.Context, orderID int) (*checkoutservice.Preference, error) {
	var preferenceID, checkoutURL, environment sql.NullString
	err := repository.db.Pool.QueryRow(ctx, `SELECT active_payment_preference_id, active_checkout_url, active_payment_environment
		FROM orders WHERE id=$1 AND status=$2`, orderID, domain.OrderStatusPending,
	).Scan(&preferenceID, &checkoutURL, &environment)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nullablePreference(preferenceID, checkoutURL, environment), nil
}

func (repository *CheckoutRepository) WriteAudit(ctx context.Context, orderID int, email, preferenceID string) error {
	_, err := repository.db.Pool.Exec(ctx, `INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata)
		VALUES ($1, 'checkout_preference_created', 'order', $2, $3)`, email, orderID, fmt.Sprintf(`{"preference_id":%q}`, preferenceID))
	return err
}

func nullablePreference(id, checkoutURL, environment sql.NullString) *checkoutservice.Preference {
	if !id.Valid || !checkoutURL.Valid || id.String == "" || checkoutURL.String == "" {
		return nil
	}
	return &checkoutservice.Preference{ID: id.String, CheckoutURL: checkoutURL.String, Environment: environment.String}
}
