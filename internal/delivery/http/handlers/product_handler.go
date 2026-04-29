package handlers

import (
	"net/http"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

type CreateProductInput struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

// 🟢 GET /products
func GetProductsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Pool.Query(c, "SELECT id, name, price, stock FROM products")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var products []map[string]interface{}

		for rows.Next() {
			var id int
			var name string
			var price float64
			var stock int

			err := rows.Scan(&id, &name, &price, &stock)
			if err != nil {
				continue
			}

			products = append(products, gin.H{
				"id":    id,
				"name":  name,
				"price": price,
				"stock": stock,
			})
		}

		c.JSON(http.StatusOK, products)
	}
}

// 🔐 POST /products (admin)
func CreateProductHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		role, _ := c.Get("role")
		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		var input CreateProductInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
			return
		}

		_, err := db.Pool.Exec(
			c,
			"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3)",
			input.Name,
			input.Price,
			input.Stock,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "product created"})
	}
}