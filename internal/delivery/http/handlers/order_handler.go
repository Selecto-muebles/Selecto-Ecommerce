package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/collection"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/utils"
	"Selecto-Ecommerce/internal/shared/validation"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const (
	maxItemsPerOrder         = 50
	maxQuantityPerItem       = 100
	maxTotalQuantityPerOrder = 100
)

type OrderItemInput struct {
	ProductID       utils.PublicID    `json:"product_id" binding:"required"`
	Quantity        int               `json:"quantity" binding:"required"`
	SelectedOptions map[string]string `json:"selected_options,omitempty"`
}

type CreateOrderInput struct {
	Items           []OrderItemInput          `json:"items" binding:"required"`
	ShippingAddress *CreateOrderShippingInput `json:"shipping_address,omitempty"`
}

type CreateOrderShippingInput struct {
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name"`
	DNI                   string `json:"dni"`
	StreetAddress         string `json:"street_address"`
	StreetNumber          string `json:"street_number"`
	PostalCode            string `json:"postal_code"`
	Province              string `json:"province"`
	Locality              string `json:"locality"`
	PhoneNumber           string `json:"phone_number"`
	RequestedDeliveryDate string `json:"requested_delivery_date"`
}

type OrderItemResponse struct {
	ID              string            `json:"id"`
	ProductID       string            `json:"product_id"`
	Name            string            `json:"name"`
	Quantity        int               `json:"quantity"`
	Price           float64           `json:"price"`
	Subtotal        float64           `json:"subtotal"`
	SelectedOptions map[string]string `json:"selected_options"`
}

type OrderResponse struct {
	ID              string                   `json:"id"`
	UserID          int                      `json:"user_id"`
	Status          string                   `json:"status"`
	Total           float64                  `json:"total"`
	CreatedAt       time.Time                `json:"created_at"`
	Items           []OrderItemResponse      `json:"items"`
	ShippingAddress *ShippingAddressResponse `json:"shipping_address,omitempty"`
	Shipment        *ShipmentResponse        `json:"shipment,omitempty"`
}

func invalidOrderItem(item OrderItemInput) bool {
	return item.ProductID.Int() <= 0 || item.Quantity <= 0
}

func exceedsItemQuantityLimit(item OrderItemInput) bool {
	return item.Quantity > maxQuantityPerItem
}

func (input CreateOrderShippingInput) normalizedProfile() validation.CustomerProfile {
	return validation.NormalizeCustomerProfile(validation.CustomerProfile{
		FirstName: input.FirstName, LastName: input.LastName, DNI: input.DNI,
		StreetAddress: input.StreetAddress, StreetNumber: input.StreetNumber,
		PostalCode: input.PostalCode, Province: input.Province, Locality: input.Locality,
		PhoneNumber: input.PhoneNumber,
	})
}

func parseRequestedDeliveryDate(value string, now time.Time) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	requested, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, errors.New("requested_delivery_date must use YYYY-MM-DD")
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if requested.Before(today) {
		return nil, errors.New("requested_delivery_date cannot be in the past")
	}
	return &requested, nil
}

type groupedOrderItem struct {
	ProductID       int
	Quantity        int
	SelectedOptions map[string]string
}

func groupOrderItems(items []OrderItemInput) ([]groupedOrderItem, map[int]int, error) {
	grouped := make([]groupedOrderItem, 0, len(items))
	groupIndex := make(map[string]int, len(items))
	quantityByProduct := make(map[int]int, len(items))

	for _, item := range items {
		productID := item.ProductID.Int()
		selectedOptions := item.SelectedOptions
		if selectedOptions == nil {
			selectedOptions = map[string]string{}
		}
		raw, err := json.Marshal(selectedOptions)
		if err != nil {
			return nil, nil, err
		}
		key := fmt.Sprintf("%d:%s", productID, raw)
		quantityByProduct[productID] += item.Quantity

		if index, exists := groupIndex[key]; exists {
			grouped[index].Quantity += item.Quantity
			continue
		}
		groupIndex[key] = len(grouped)
		grouped = append(grouped, groupedOrderItem{
			ProductID:       productID,
			Quantity:        item.Quantity,
			SelectedOptions: selectedOptions,
		})
	}

	return grouped, quantityByProduct, nil
}

func CreateOrderHandler(db *database.DB, cfg *config.Config, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		emailValue, _ := c.Get("email")
		email := fmt.Sprint(emailValue)

		var input CreateOrderInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}

		if len(input.Items) == 0 {
			apperrors.BadRequest(c, "order must contain at least one item")
			return
		}
		if len(input.Items) > maxItemsPerOrder {
			apperrors.BadRequest(c, "too many items in order")
			return
		}

		if _, found := collection.Find(input.Items, invalidOrderItem); found {
			apperrors.BadRequest(c, "product_id and quantity must be positive")
			return
		}
		if _, found := collection.Find(input.Items, exceedsItemQuantityLimit); found {
			apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, "quantity exceeds per-item limit", gin.H{"max_quantity": maxQuantityPerItem})
			return
		}
		totalQuantity := collection.Reduce(input.Items, 0, func(total int, item OrderItemInput) int {
			return total + item.Quantity
		})
		if totalQuantity > maxTotalQuantityPerOrder {
			apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, "order quantity limit exceeded", gin.H{"max_total_quantity": maxTotalQuantityPerOrder})
			return
		}

		groupedItems, quantityByProduct, err := groupOrderItems(input.Items)
		if err != nil {
			apperrors.BadRequest(c, "invalid selected options")
			return
		}
		productIDs := collection.SortedIntKeys(quantityByProduct)
		itemsByProduct := make(map[int][]groupedOrderItem, len(productIDs))
		for _, item := range groupedItems {
			itemsByProduct[item.ProductID] = append(itemsByProduct[item.ProductID], item)
		}

		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)

		var userID int
		var accountProfile validation.CustomerProfile
		err = tx.QueryRow(c, `SELECT id, COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(dni, ''),
			COALESCE(street_address, ''), COALESCE(street_number, ''), COALESCE(postal_code, ''),
			COALESCE(province, ''), COALESCE(locality, ''), COALESCE(phone_number, '')
			FROM users WHERE email=$1`, email).Scan(
			&userID, &accountProfile.FirstName, &accountProfile.LastName, &accountProfile.DNI,
			&accountProfile.StreetAddress, &accountProfile.StreetNumber, &accountProfile.PostalCode,
			&accountProfile.Province, &accountProfile.Locality, &accountProfile.PhoneNumber,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "user not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}

		shippingProfile := validation.NormalizeCustomerProfile(accountProfile)
		var requestedDeliveryDate *time.Time
		if input.ShippingAddress != nil {
			shippingProfile = input.ShippingAddress.normalizedProfile()
			requestedDeliveryDate, err = parseRequestedDeliveryDate(input.ShippingAddress.RequestedDeliveryDate, time.Now())
			if err != nil {
				apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, err.Error(), nil)
				return
			}
		}
		if err := shippingProfile.Validate(); err != nil {
			apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, "shipping address must be valid", nil)
			return
		}

		var total money.Cents
		type reservedItem struct {
			ProductID       int
			Quantity        int
			Price           money.Cents
			SelectedOptions map[string]string
		}
		reservedItems := make([]reservedItem, 0, len(groupedItems))

		for _, productID := range productIDs {
			quantity := quantityByProduct[productID]
			var priceCents int64
			var stock int

			err := tx.QueryRow(
				c,
				"SELECT ROUND(price * 100)::BIGINT, stock FROM products WHERE id=$1 AND active = TRUE FOR UPDATE",
				productID,
			).Scan(&priceCents, &stock)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, "invalid product", gin.H{"product_id": utils.EncodeID(productID)})
					return
				}
				apperrors.Internal(c)
				return
			}

			if stock < quantity {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeInsufficientStock, "insufficient stock", gin.H{"product_id": utils.EncodeID(productID), "available": stock})
				return
			}
			for _, item := range itemsByProduct[productID] {
				if err := validateSelectedOptions(c, tx, productID, item.SelectedOptions); err != nil {
					apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, err.Error(), gin.H{"product_id": utils.EncodeID(productID)})
					return
				}
			}

			_, err = tx.Exec(
				c,
				"UPDATE products SET stock = stock - $1 WHERE id=$2",
				quantity,
				productID,
			)
			if err != nil {
				apperrors.Internal(c)
				return
			}
			logger.Info(logging.EventStockReserved, "product_id", productID, "public_id", utils.EncodeID(productID), "quantity", quantity, "remaining_stock", stock-quantity)

			itemPrice := money.Cents(priceCents)
			total += itemPrice * money.Cents(quantity)
			for _, item := range itemsByProduct[productID] {
				reservedItems = append(reservedItems, reservedItem{
					ProductID:       productID,
					Quantity:        item.Quantity,
					Price:           itemPrice,
					SelectedOptions: item.SelectedOptions,
				})
			}
		}

		var orderID int
		err = tx.QueryRow(
			c,
			"INSERT INTO orders (user_id, status, total, expires_at) VALUES ($1, $2, $3, NOW() + make_interval(secs => $4)) RETURNING id",
			userID,
			domain.OrderStatusPending,
			total.DecimalString(),
			int(cfg.OrderPendingTTL.Seconds()),
		).Scan(&orderID)
		if err != nil {
			apperrors.Internal(c)
			return
		}

		if _, err := tx.Exec(c, `
			INSERT INTO order_shipping_addresses (
				order_id,
				recipient_first_name,
				recipient_last_name,
				dni,
				street_address,
				street_number,
				postal_code,
				province,
				locality,
				phone_number,
				requested_delivery_date
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			orderID, shippingProfile.FirstName, shippingProfile.LastName, shippingProfile.DNI,
			shippingProfile.StreetAddress, shippingProfile.StreetNumber, shippingProfile.PostalCode,
			shippingProfile.Province, shippingProfile.Locality, shippingProfile.PhoneNumber,
			requestedDeliveryDate); err != nil {
			apperrors.Internal(c)
			return
		}

		for _, item := range reservedItems {
			_, err := tx.Exec(
				c,
				"INSERT INTO order_items (order_id, product_id, quantity, price, selected_options) VALUES ($1, $2, $3, $4, $5)",
				orderID,
				item.ProductID,
				item.Quantity,
				item.Price.DecimalString(),
				item.SelectedOptions,
			)
			if err != nil {
				apperrors.Internal(c)
				return
			}
		}

		if _, err := tx.Exec(
			c,
			"INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)",
			email,
			"order_created",
			"order",
			orderID,
			fmt.Sprintf(`{"total_cents":%d}`, total),
		); err != nil {
			apperrors.Internal(c)
			return
		}

		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}

		logger.Info(logging.EventOrderCreated, "order_id", orderID, "public_id", utils.EncodeID(orderID), "user_id", userID, "total_cents", int64(total), "status", domain.OrderStatusPending)

		c.JSON(http.StatusCreated, gin.H{
			"order_id": utils.EncodeID(orderID),
			"status":   string(domain.OrderStatusPending),
			"total":    total.Float64(),
		})
	}
}

func GetOrderHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID, err := utils.DecodeID(c.Param("id"))
		if err != nil || orderID <= 0 {
			apperrors.BadRequest(c, "invalid order id")
			return
		}

		emailValue, _ := c.Get("email")
		roleValue, _ := c.Get("role")
		email := fmt.Sprint(emailValue)
		role := fmt.Sprint(roleValue)

		order, err := fetchOrder(c, db, orderID, email, role == "admin")
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "order not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}

		c.JSON(http.StatusOK, order)
	}
}

func GetMyOrdersHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		emailValue, _ := c.Get("email")
		email := fmt.Sprint(emailValue)

		rows, err := db.Pool.Query(
			c,
			`SELECT o.id, o.user_id, o.status, o.total, o.created_at
			 FROM orders o
			 JOIN users u ON u.id = o.user_id
			 WHERE u.email = $1
			 ORDER BY o.created_at DESC`,
			email,
		)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()

		orders := make([]OrderResponse, 0)
		for rows.Next() {
			var orderID int
			var order OrderResponse
			if err := rows.Scan(&orderID, &order.UserID, &order.Status, &order.Total, &order.CreatedAt); err != nil {
				apperrors.Internal(c)
				return
			}
			order.ID = utils.EncodeID(orderID)
			address, shipment, err := loadOrderShipping(c, db.Pool, orderID)
			if err != nil {
				apperrors.Internal(c)
				return
			}
			order.ShippingAddress = address
			order.Shipment = shipment
			orders = append(orders, order)
		}

		c.JSON(http.StatusOK, orders)
	}
}

func fetchOrder(c *gin.Context, db *database.DB, orderID int, email string, allowAnyUser bool) (*OrderResponse, error) {
	query := `SELECT o.id, o.user_id, o.status, o.total, o.created_at
		FROM orders o
		JOIN users u ON u.id = o.user_id
		WHERE o.id=$1`
	args := []interface{}{orderID}
	if !allowAnyUser {
		query += " AND u.email=$2"
		args = append(args, email)
	}

	var dbOrderID int
	var order OrderResponse
	err := db.Pool.QueryRow(c, query, args...).Scan(&dbOrderID, &order.UserID, &order.Status, &order.Total, &order.CreatedAt)
	if err != nil {
		return nil, err
	}
	order.ID = utils.EncodeID(dbOrderID)
	order.ShippingAddress, order.Shipment, err = loadOrderShipping(c, db.Pool, orderID)
	if err != nil {
		return nil, err
	}

	rows, err := db.Pool.Query(
		c,
		`SELECT oi.id, oi.product_id, p.name, oi.quantity, oi.price, oi.selected_options
		 FROM order_items oi
		 JOIN products p ON p.id = oi.product_id
		 WHERE oi.order_id=$1
		 ORDER BY oi.id`,
		orderID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	order.Items = make([]OrderItemResponse, 0)
	for rows.Next() {
		var itemID int
		var productID int
		var item OrderItemResponse
		var selectedOptions []byte
		if err := rows.Scan(&itemID, &productID, &item.Name, &item.Quantity, &item.Price, &selectedOptions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(selectedOptions, &item.SelectedOptions); err != nil {
			return nil, err
		}
		item.ID = utils.EncodeID(itemID)
		item.ProductID = utils.EncodeID(productID)
		item.Subtotal = item.Price * float64(item.Quantity)
		order.Items = append(order.Items, item)
	}

	return &order, rows.Err()
}

func ReleaseExpiredPendingOrders(ctx context.Context, db *database.DB, olderThan time.Duration) (int64, error) {
	return releaseExpiredPendingOrders(ctx, db, olderThan, 100, false)
}

func ReleaseExpiredPendingOrdersWithAudit(ctx context.Context, db *database.DB, olderThan time.Duration, batchSize int) (int64, error) {
	return releaseExpiredPendingOrders(ctx, db, olderThan, batchSize, true)
}

func releaseExpiredPendingOrders(ctx context.Context, db *database.DB, olderThan time.Duration, batchSize int, writeAudit bool) (int64, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(
		ctx,
		`SELECT id
		 FROM orders
		 WHERE status='pending'
		   AND COALESCE(expires_at, created_at + make_interval(secs => $1)) < NOW()
		 ORDER BY COALESCE(expires_at, created_at + make_interval(secs => $1)), id
		 LIMIT $2
		 FOR UPDATE SKIP LOCKED`,
		int(olderThan.Seconds()),
		batchSize,
	)
	if err != nil {
		return 0, err
	}

	orderIDs := make([]int, 0)
	for rows.Next() {
		var orderID int
		if err := rows.Scan(&orderID); err != nil {
			rows.Close()
			return 0, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	for _, orderID := range orderIDs {
		if err := restoreOrderStock(ctx, tx, orderID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, "UPDATE orders SET status='cancelled', cancelled_at=NOW() WHERE id=$1", orderID); err != nil {
			return 0, err
		}
		if writeAudit {
			if _, err := tx.Exec(
				ctx,
				"INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)",
				"system",
				"order_reservation_expired",
				"order",
				orderID,
				"{}",
			); err != nil {
				return 0, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	return int64(len(orderIDs)), nil
}
