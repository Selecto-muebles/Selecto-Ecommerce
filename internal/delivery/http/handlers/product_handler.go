package handlers

import (
	"log"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

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
				"id":    utils.EncodeID(id),
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

		if input.Name == "" || input.Price < 0 || input.Stock < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name, price and stock must be valid"})
			return
		}

		var productID int
		err := db.Pool.QueryRow(
			c,
			"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING id",
			input.Name,
			input.Price,
			input.Stock,
		).Scan(&productID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		log.Printf("product created: product_id=%d public_id=%s", productID, utils.EncodeID(productID))

		c.JSON(http.StatusOK, gin.H{
			"message":    "product created",
			"product_id": utils.EncodeID(productID),
		})
	}
}
