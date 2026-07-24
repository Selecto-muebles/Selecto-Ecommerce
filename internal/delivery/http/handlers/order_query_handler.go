package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

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
