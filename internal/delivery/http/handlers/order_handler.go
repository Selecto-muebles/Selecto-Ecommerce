package handlers

import (
	"context"
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

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const (
	maxItemsPerOrder         = 50
	maxQuantityPerItem       = 100
	maxTotalQuantityPerOrder = 100
)

type OrderItemInput struct {
	ProductID utils.PublicID `json:"product_id" binding:"required"`
	Quantity  int            `json:"quantity" binding:"required"`
}

type CreateOrderInput struct {
	Items []OrderItemInput `json:"items" binding:"required"`
}

type OrderItemResponse struct {
	ID        string  `json:"id"`
	ProductID string  `json:"product_id"`
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
	Subtotal  float64 `json:"subtotal"`
}

type OrderResponse struct {
	ID        string              `json:"id"`
	UserID    int                 `json:"user_id"`
	Status    string              `json:"status"`
	Total     float64             `json:"total"`
	CreatedAt time.Time           `json:"created_at"`
	Items     []OrderItemResponse `json:"items"`
}

func invalidOrderItem(item OrderItemInput) bool {
	return item.ProductID.Int() <= 0 || item.Quantity <= 0
}

func exceedsItemQuantityLimit(item OrderItemInput) bool {
	return item.Quantity > maxQuantityPerItem
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

		itemsByProduct := collection.GroupSumByInt(input.Items, func(item OrderItemInput) int {
			return item.ProductID.Int()
		}, func(item OrderItemInput) int {
			return item.Quantity
		})
		productIDs := collection.SortedIntKeys(itemsByProduct)

		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)

		var userID int
		err = tx.QueryRow(c, "SELECT id FROM users WHERE email=$1", email).Scan(&userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "user not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}

		var total money.Cents
		type reservedItem struct {
			ProductID int
			Quantity  int
			Price     money.Cents
		}
		reservedItems := make([]reservedItem, 0, len(itemsByProduct))

		for _, productID := range productIDs {
			quantity := itemsByProduct[productID]
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
			reservedItems = append(reservedItems, reservedItem{
				ProductID: productID,
				Quantity:  quantity,
				Price:     itemPrice,
			})
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

		for _, item := range reservedItems {
			_, err := tx.Exec(
				c,
				"INSERT INTO order_items (order_id, product_id, quantity, price) VALUES ($1, $2, $3, $4)",
				orderID,
				item.ProductID,
				item.Quantity,
				item.Price.DecimalString(),
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

	rows, err := db.Pool.Query(
		c,
		`SELECT oi.id, oi.product_id, p.name, oi.quantity, oi.price
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
		if err := rows.Scan(&itemID, &productID, &item.Name, &item.Quantity, &item.Price); err != nil {
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
