package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func GetAdminMeHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		email := adminActor(c)
		var role string
		if err := db.Pool.QueryRow(c, "SELECT role FROM users WHERE email=$1", email).Scan(&role); err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid token", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"email": email, "role": role})
	}
}

func GetAdminDashboardHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now()
		period, valid := dashboardPeriod(c.Query("period"), now)
		if !valid {
			apperrors.BadRequest(c, "period must be 7d, 30d or 90d")
			return
		}
		metrics, err := adminCommercialMetrics(c, db, now)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		comparable, err := adminComparableMetrics(c, db, period)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		for key, value := range comparable {
			metrics[key] = value
		}
		latestOrders, err := adminLatestOrders(c, db, 10)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		metrics["latest_orders"] = latestOrders
		logger.Debug(logging.EventAdminMetricsRequested, "orders_paid", metrics["orders_paid"], "period", period.Key)
		c.JSON(http.StatusOK, metrics)
	}
}

func adminLatestOrders(ctx context.Context, db *database.DB, limit int) ([]gin.H, error) {
	rows, err := db.Pool.Query(ctx, `SELECT o.id, o.status, COALESCE(o.payment_status, ''), o.total, o.created_at, u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, '') FROM orders o JOIN users u ON u.id=o.user_id ORDER BY o.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id int
		var status, paymentStatus string
		var total float64
		var createdAt time.Time
		var email, firstName, lastName string
		if err := rows.Scan(&id, &status, &paymentStatus, &total, &createdAt, &email, &firstName, &lastName); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": utils.EncodeID(id), "status": status, "payment_status": paymentStatus, "total": total, "created_at": createdAt, "customer": gin.H{"email": email, "first_name": firstName, "last_name": lastName}})
	}
	return items, rows.Err()
}
