package handlers

import (
	"net/http"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

func GetMetricsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		role, _ := c.Get("role")
		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		var totalRevenue float64
		var totalOrders int

		// total revenue
		err := db.Pool.QueryRow(
			c,
			"SELECT COALESCE(SUM(total), 0) FROM orders",
		).Scan(&totalRevenue)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// total orders
		err = db.Pool.QueryRow(
			c,
			"SELECT COUNT(*) FROM orders",
		).Scan(&totalOrders)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"total_revenue": totalRevenue,
			"total_orders":  totalOrders,
		})
	}
}