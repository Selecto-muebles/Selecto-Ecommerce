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
		now := time.Now().UTC()
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		var salesToday, salesMonth float64
		var ordersPending, ordersPaid, ordersCancelled, productsActive, productsWithoutStock int
		err := db.Pool.QueryRow(c, `
			WITH sales AS (
				SELECT
					COALESCE(SUM(total) FILTER (WHERE COALESCE(paid_at, created_at) >= $1), 0) AS today,
					COALESCE(SUM(total), 0) AS month
				FROM orders
				WHERE status='paid'
				  AND COALESCE(paid_at, created_at) >= $2
				  AND COALESCE(paid_at, created_at) < $3
			), order_counts AS (
				SELECT
				COUNT(*) FILTER (WHERE status='pending'),
				COUNT(*) FILTER (WHERE status='paid'),
					COUNT(*) FILTER (WHERE status='cancelled')
				FROM orders
			), product_counts AS (
				SELECT
					COUNT(*) FILTER (WHERE active),
					COUNT(*) FILTER (WHERE active AND stock=0)
				FROM products
			)
			SELECT sales.*, order_counts.*, product_counts.*
			FROM sales, order_counts, product_counts`, dayStart, monthStart, now).Scan(
			&salesToday,
			&salesMonth,
			&ordersPending,
			&ordersPaid,
			&ordersCancelled,
			&productsActive,
			&productsWithoutStock,
		)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		latestOrders, err := adminLatestOrders(c, db, 10)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		logger.Debug(logging.EventAdminMetricsRequested, "orders_paid", ordersPaid)
		c.JSON(http.StatusOK, gin.H{"sales_today": salesToday, "sales_month": salesMonth, "orders_pending": ordersPending, "orders_paid": ordersPaid, "orders_cancelled": ordersCancelled, "products_active": productsActive, "products_without_stock": productsWithoutStock, "latest_orders": latestOrders})
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
