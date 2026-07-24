package postgres

import (
	"context"
	"errors"
	"fmt"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	orderservice "Selecto-Ecommerce/internal/service/orders"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/validation"

	"github.com/jackc/pgx/v5"
)

type OrderRepository struct {
	db  *database.DB
	cfg *config.Config
}

func NewOrderRepository(db *database.DB, cfg *config.Config) *OrderRepository {
	return &OrderRepository{db: db, cfg: cfg}
}

type reservedOrderItem struct {
	productID       int
	quantity        int
	price           money.Cents
	selectedOptions map[string]string
}

func (repository *OrderRepository) Create(ctx context.Context, command orderservice.CreateCommand) (orderservice.CreateResult, error) {
	tx, err := repository.db.Pool.Begin(ctx)
	if err != nil {
		return orderservice.CreateResult{}, err
	}
	defer tx.Rollback(ctx)

	userID, accountProfile, err := loadCustomerForOrder(ctx, tx, command.Email)
	if err != nil {
		return orderservice.CreateResult{}, err
	}
	if command.IdempotencyKey != "" {
		replayed, found, err := findIdempotentOrder(ctx, tx, userID, command.IdempotencyKey, command.RequestHash)
		if err != nil {
			return orderservice.CreateResult{}, err
		}
		if found {
			if err := tx.Commit(ctx); err != nil {
				return orderservice.CreateResult{}, err
			}
			return replayed, nil
		}
	}

	shippingProfile := command.ShippingProfile
	if !command.ShippingProvided {
		shippingProfile = validation.NormalizeCustomerProfile(accountProfile)
	}
	if err := shippingProfile.Validate(); err != nil {
		return orderservice.CreateResult{}, orderservice.InvalidShippingError{Err: err, PublicMessage: "shipping address must be valid"}
	}

	itemsByProduct := make(map[int][]orderservice.GroupedItem, len(command.PreparedItems.ProductIDs))
	for _, item := range command.PreparedItems.Grouped {
		itemsByProduct[item.ProductID] = append(itemsByProduct[item.ProductID], item)
	}
	total, reserved, reservations, err := reserveProducts(ctx, tx, command.PreparedItems, itemsByProduct)
	if err != nil {
		return orderservice.CreateResult{}, err
	}
	orderID, err := insertOrder(ctx, tx, repository.cfg, userID, command, total)
	if err != nil {
		return orderservice.CreateResult{}, err
	}
	if err := insertShippingAddress(ctx, tx, orderID, shippingProfile, command); err != nil {
		return orderservice.CreateResult{}, err
	}
	if err := insertOrderItems(ctx, tx, orderID, reserved); err != nil {
		return orderservice.CreateResult{}, err
	}
	if err := insertOrderAuditAndEmail(ctx, tx, repository.cfg, orderID, command.Email, total); err != nil {
		return orderservice.CreateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return orderservice.CreateResult{}, err
	}
	return orderservice.CreateResult{
		OrderID: orderID, UserID: userID, Status: string(domain.OrderStatusPending), Total: total, Reservations: reservations,
	}, nil
}

func loadCustomerForOrder(ctx context.Context, tx pgx.Tx, email string) (int, validation.CustomerProfile, error) {
	var userID int
	var profile validation.CustomerProfile
	err := tx.QueryRow(ctx, `SELECT id, COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(dni, ''),
		COALESCE(street_address, ''), COALESCE(street_number, ''), COALESCE(postal_code, ''),
		COALESCE(province, ''), COALESCE(locality, ''), COALESCE(phone_number, '') FROM users WHERE email=$1`, email).Scan(
		&userID, &profile.FirstName, &profile.LastName, &profile.DNI, &profile.StreetAddress, &profile.StreetNumber,
		&profile.PostalCode, &profile.Province, &profile.Locality, &profile.PhoneNumber,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, profile, orderservice.ErrUserNotFound
	}
	return userID, profile, err
}

func findIdempotentOrder(ctx context.Context, tx pgx.Tx, userID int, key, requestHash string) (orderservice.CreateResult, bool, error) {
	lockKey := fmt.Sprintf("create-order:%d:%s", userID, key)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil {
		return orderservice.CreateResult{}, false, err
	}
	var orderID int
	var status, existingHash, totalDecimal string
	err := tx.QueryRow(ctx, "SELECT id, status, total::TEXT, COALESCE(request_hash, '') FROM orders WHERE user_id=$1 AND idempotency_key=$2", userID, key).Scan(&orderID, &status, &totalDecimal, &existingHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return orderservice.CreateResult{}, false, nil
	}
	if err != nil {
		return orderservice.CreateResult{}, false, err
	}
	if existingHash != requestHash {
		return orderservice.CreateResult{}, false, orderservice.ErrIdempotencyConflict
	}
	total, err := money.FromDecimalString(totalDecimal)
	return orderservice.CreateResult{OrderID: orderID, UserID: userID, Status: status, Total: total, Replayed: true}, true, err
}

func reserveProducts(ctx context.Context, tx pgx.Tx, prepared orderservice.PreparedItems, grouped map[int][]orderservice.GroupedItem) (money.Cents, []reservedOrderItem, []orderservice.Reservation, error) {
	var total money.Cents
	items := make([]reservedOrderItem, 0, len(prepared.Grouped))
	reservations := make([]orderservice.Reservation, 0, len(prepared.ProductIDs))
	for _, productID := range prepared.ProductIDs {
		quantity := prepared.QuantityByProduct[productID]
		var priceCents int64
		var stock int
		err := tx.QueryRow(ctx, "SELECT ROUND(price * 100)::BIGINT, stock FROM products WHERE id=$1 AND active=TRUE FOR UPDATE", productID).Scan(&priceCents, &stock)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, nil, orderservice.InvalidProductError{ProductID: productID}
		}
		if err != nil {
			return 0, nil, nil, err
		}
		if stock < quantity {
			return 0, nil, nil, orderservice.InsufficientStockError{ProductID: productID, Available: stock}
		}
		for _, item := range grouped[productID] {
			if err := ValidateSelectedOptions(ctx, tx, productID, item.SelectedOptions); err != nil {
				return 0, nil, nil, orderservice.ProductOptionsError{ProductID: productID, Err: err}
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE products SET stock=stock-$1 WHERE id=$2", quantity, productID); err != nil {
			return 0, nil, nil, err
		}
		price := money.Cents(priceCents)
		total += price * money.Cents(quantity)
		reservations = append(reservations, orderservice.Reservation{ProductID: productID, Quantity: quantity, RemainingStock: stock - quantity})
		for _, item := range grouped[productID] {
			items = append(items, reservedOrderItem{productID: productID, quantity: item.Quantity, price: price, selectedOptions: item.SelectedOptions})
		}
	}
	return total, items, reservations, nil
}
