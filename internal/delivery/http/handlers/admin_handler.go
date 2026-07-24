package handlers

import (
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"

	"github.com/gin-gonic/gin"
)

func GetMetricsHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var totalRevenue float64
		var totalOrders int

		err := db.Pool.QueryRow(
			c,
			"SELECT COALESCE(SUM(total), 0) FROM orders WHERE status='paid'",
		).Scan(&totalRevenue)

		if err != nil {
			apperrors.Internal(c)
			return
		}

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
		logger.Debug(logging.EventAdminMetricsRequested, "total_orders", totalOrders)
	}
}
