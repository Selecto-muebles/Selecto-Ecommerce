package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func AdminObservabilityHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		dbStatus := "ready"
		if err := db.Pool.Ping(c); err != nil {
			dbStatus = "unavailable"
		}
		var pendingExpired int
		_ = db.Pool.QueryRow(c, "SELECT COUNT(*) FROM orders WHERE status='pending' AND active_payment_preference_id IS NULL AND COALESCE(expires_at, created_at + make_interval(secs => $1)) < NOW()", int(cfg.OrderPendingTTL.Seconds())).Scan(&pendingExpired)
		webhooks, _ := adminRecentPaymentWebhookEvents(c, db)
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": dbStatus, "pending_expired_orders": pendingExpired, "recent_payment_webhook_events": webhooks})
	}
}

func adminRecentPaymentWebhookEvents(ctx context.Context, db *database.DB) ([]gin.H, error) {
	rows, err := db.Pool.Query(ctx, "SELECT event_key, COALESCE(payment_id, 0), COALESCE(payment_provider, CASE WHEN payment_id IS NOT NULL THEN 'mercadopago' ELSE '' END), COALESCE(provider_payment_id, payment_id::TEXT, ''), order_id, status, received_at, processed_at, COALESCE(result, '') FROM payment_webhook_events ORDER BY received_at DESC LIMIT 20")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var key, paymentProvider, providerPaymentID, status, result string
		var paymentID, orderID int
		var receivedAt time.Time
		var processedAt sql.NullTime
		if err := rows.Scan(&key, &paymentID, &paymentProvider, &providerPaymentID, &orderID, &status, &receivedAt, &processedAt, &result); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"event_key": key, "payment_id": paymentID, "payment_provider": paymentProvider, "provider_payment_id": providerPaymentID, "order_id": utils.EncodeID(orderID), "status": status, "received_at": receivedAt, "processed_at": nullableTime(processedAt), "result": result})
	}
	return items, rows.Err()
}
