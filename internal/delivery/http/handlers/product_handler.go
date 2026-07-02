package handlers

import (
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

type CreateProductInput struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

type ProductResponse struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

func GetProductsHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		products, err := fetchActiveProducts(c, db)
		if err != nil {
			apperrors.Internal(c)
			return
		}

		logger.Info(logging.EventProductCatalogListed, "products_count", len(products))
		c.JSON(http.StatusOK, products)
	}
}

func fetchActiveProducts(c *gin.Context, db *database.DB) ([]ProductResponse, error) {
	rows, err := db.Pool.Query(c, "SELECT id, name, price, stock FROM products WHERE active = TRUE ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := make([]ProductResponse, 0)
	for rows.Next() {
		var id int
		var product ProductResponse
		if err := rows.Scan(&id, &product.Name, &product.Price, &product.Stock); err != nil {
			return nil, err
		}
		product.ID = utils.EncodeID(id)
		products = append(products, product)
	}

	return products, rows.Err()
}

func CreateProductHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input CreateProductInput

		if err := c.ShouldBindJSON(&input); err != nil {
			logger.Warn(logging.EventProductCreationRejected, "reason", "invalid_payload")
			apperrors.BadRequest(c, "invalid input")
			return
		}

		if input.Name == "" || input.Price < 0 || input.Stock < 0 {
			logger.Warn(logging.EventProductCreationRejected, "reason", "invalid_product_data")
			apperrors.BadRequest(c, "name, price and stock must be valid")
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
			apperrors.Internal(c)
			return
		}

		logger.Info(logging.EventProductCreated, "product_id", productID, "public_id", utils.EncodeID(productID), "stock", input.Stock)

		c.JSON(http.StatusOK, gin.H{
			"message":    "product created",
			"product_id": utils.EncodeID(productID),
		})
	}
}
