package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
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

func CreateOrderHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		emailValue, _ := c.Get("email")
		email := fmt.Sprint(emailValue)

		var input CreateOrderInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
			return
		}

		if len(input.Items) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order must contain at least one item"})
			return
		}
		if len(input.Items) > maxItemsPerOrder {
			c.JSON(http.StatusBadRequest, gin.H{"error": "too many items in order"})
			return
		}

		itemsByProduct := make(map[int]int)
		productIDs := make([]int, 0, len(input.Items))
		totalQuantity := 0
		for _, item := range input.Items {
			productID := item.ProductID.Int()
			if productID <= 0 || item.Quantity <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "product_id and quantity must be positive"})
				return
			}
			if item.Quantity > maxQuantityPerItem {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":        "quantity exceeds per-item limit",
					"max_quantity": maxQuantityPerItem,
				})
				return
			}
			totalQuantity += item.Quantity
			if totalQuantity > maxTotalQuantityPerOrder {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":              "order quantity limit exceeded",
					"max_total_quantity": maxTotalQuantityPerOrder,
				})
				return
			}
			if _, exists := itemsByProduct[productID]; !exists {
				productIDs = append(productIDs, productID)
			}
			itemsByProduct[productID] += item.Quantity
		}
		sort.Ints(productIDs)

		tx, err := db.Pool.Begin(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start transaction"})
			return
		}
		defer tx.Rollback(c)

		var userID int
		err = tx.QueryRow(c, "SELECT id FROM users WHERE email=$1", email).Scan(&userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var total float64
		type reservedItem struct {
			ProductID int
			Quantity  int
			Price     float64
		}
		reservedItems := make([]reservedItem, 0, len(itemsByProduct))

		for _, productID := range productIDs {
			quantity := itemsByProduct[productID]
			var price float64
			var stock int

			err := tx.QueryRow(
				c,
				"SELECT price, stock FROM products WHERE id=$1 FOR UPDATE",
				productID,
			).Scan(&price, &stock)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product", "product_id": utils.EncodeID(productID)})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			if stock < quantity {
				c.JSON(http.StatusConflict, gin.H{
					"error":      "insufficient stock",
					"product_id": utils.EncodeID(productID),
					"available":  stock,
				})
				return
			}

			_, err = tx.Exec(
				c,
				"UPDATE products SET stock = stock - $1 WHERE id=$2",
				quantity,
				productID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			log.Printf("stock reserved: product_id=%d public_id=%s quantity=%d remaining=%d", productID, utils.EncodeID(productID), quantity, stock-quantity)

			total += price * float64(quantity)
			reservedItems = append(reservedItems, reservedItem{
				ProductID: productID,
				Quantity:  quantity,
				Price:     price,
			})
		}

		var orderID int
		err = tx.QueryRow(
			c,
			"INSERT INTO orders (user_id, status, total) VALUES ($1, 'pending', $2) RETURNING id",
			userID,
			total,
		).Scan(&orderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		for _, item := range reservedItems {
			_, err := tx.Exec(
				c,
				"INSERT INTO order_items (order_id, product_id, quantity, price) VALUES ($1, $2, $3, $4)",
				orderID,
				item.ProductID,
				item.Quantity,
				item.Price,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		if err := tx.Commit(c); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit order"})
			return
		}

		log.Printf("order created: order_id=%d public_id=%s user_id=%d total=%.2f status=pending", orderID, utils.EncodeID(orderID), userID, total)

		c.JSON(http.StatusCreated, gin.H{
			"order_id": utils.EncodeID(orderID),
			"status":   "pending",
			"total":    total,
		})
	}
}

func GetOrderHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID, err := utils.DecodeID(c.Param("id"))
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
			return
		}

		emailValue, _ := c.Get("email")
		roleValue, _ := c.Get("role")
		email := fmt.Sprint(emailValue)
		role := fmt.Sprint(roleValue)

		order, err := fetchOrder(c, db, orderID, email, role == "admin")
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		orders := make([]OrderResponse, 0)
		for rows.Next() {
			var orderID int
			var order OrderResponse
			if err := rows.Scan(&orderID, &order.UserID, &order.Status, &order.Total, &order.CreatedAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(
		ctx,
		`SELECT id
		 FROM orders
		 WHERE status='pending' AND created_at < NOW() - make_interval(secs => $1)
		 FOR UPDATE`,
		int(olderThan.Seconds()),
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
		if _, err := tx.Exec(ctx, "UPDATE orders SET status='cancelled' WHERE id=$1", orderID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	return int64(len(orderIDs)), nil
}
