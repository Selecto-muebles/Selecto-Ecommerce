package handlers

import (
	"net/http"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

type OrderItemInput struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}

type CreateOrderInput struct {
	Items []OrderItemInput `json:"items"`
}

func CreateOrderHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		email, _ := c.Get("email")

		var input CreateOrderInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
			return
		}

		var total float64 = 0

		// calcular total
		for _, item := range input.Items {
			var price float64

			err := db.Pool.QueryRow(
				c,
				"SELECT price FROM products WHERE id=$1",
				item.ProductID,
			).Scan(&price)

			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product"})
				return
			}

			total += price * float64(item.Quantity)
		}

		// crear orden
		var orderID int

		err := db.Pool.QueryRow(
			c,
			"INSERT INTO orders (user_email, total) VALUES ($1, $2) RETURNING id",
			email,
			total,
		).Scan(&orderID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// insertar items
		for _, item := range input.Items {
			_, err := db.Pool.Exec(
				c,
				"INSERT INTO order_items (order_id, product_id, quantity) VALUES ($1, $2, $3)",
				orderID,
				item.ProductID,
				item.Quantity,
			)

			if err != nil {
				continue
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "order created",
			"order_id": orderID,
			"total": total,
		})
	}
}