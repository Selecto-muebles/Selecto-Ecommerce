package handlers

import (
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func GetMetricsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		role, _ := c.Get("role")
		if role != "admin" {
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "forbidden", nil)
			return
		}

		var totalRevenue float64
		var totalOrders int

		// total revenue
		err := db.Pool.QueryRow(
			c,
			"SELECT COALESCE(SUM(total), 0) FROM orders WHERE status='paid'",
		).Scan(&totalRevenue)

		if err != nil {
			apperrors.Internal(c)
			return
		}

		// total orders
		err = db.Pool.QueryRow(
			c,
			"SELECT COUNT(*) FROM orders",
		).Scan(&totalOrders)

		if err != nil {
			apperrors.Internal(c)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"total_revenue": totalRevenue,
			"total_orders":  totalOrders,
		})
	}
}
