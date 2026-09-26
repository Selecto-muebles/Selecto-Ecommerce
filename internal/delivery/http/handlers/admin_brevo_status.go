package handlers

import (
	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func AdminBrevoStatusHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var pending, failed int
		if err := db.Pool.QueryRow(c, `SELECT COUNT(*) FILTER(WHERE status IN ('pending','processing')),COUNT(*) FILTER(WHERE status='failed') FROM commerce.marketing_sync_outbox`).Scan(&pending, &failed); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"contacts_enabled": cfg.BrevoContactsEnabled, "webhook_configured": len(cfg.BrevoWebhookToken) >= 32, "sync_pending": pending, "sync_failed": failed})
	}
}
func AdminRetryMarketingSyncHandler(db *database.DB, cfg *config.Config, notifiers ...mailinfra.MarketingDispatchNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.BrevoContactsEnabled {
			apperrors.JSON(c, http.StatusServiceUnavailable, apperrors.CodeInternal, "Brevo contact sync is not configured", nil)
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			apperrors.BadRequest(c, "invalid subscription id")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var version int64
		var email, status string
		if err = tx.QueryRow(c, `SELECT email,status,sync_version FROM commerce.marketing_subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&email, &status, &version); err != nil {
			handleAdminLookupErr(c, err, "subscription not found")
			return
		}
		operation := "unsubscribe"
		if status == "subscribed" {
			operation = "subscribe"
		}
		syncID, err := enqueueMarketingSync(c, tx, id, email, operation, version)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		var state string
		if err = tx.QueryRow(c, "SELECT status FROM commerce.marketing_sync_outbox WHERE id=$1 FOR UPDATE", syncID).Scan(&state); err != nil {
			apperrors.Internal(c)
			return
		}
		if state == "processing" {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "sync is already processing", nil)
			return
		}
		if _, err = tx.Exec(c, `UPDATE commerce.marketing_sync_outbox SET status='pending',attempts=0,last_error='',next_attempt_at=NOW(),locked_at=NULL,completed_at=NULL,updated_at=NOW() WHERE id=$1`, syncID); err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err = tx.Exec(c, `UPDATE commerce.marketing_subscriptions SET sync_status=CASE WHEN suppression_reason<>'' THEN 'suppressed' ELSE 'pending' END,sync_error='',updated_at=NOW() WHERE id=$1`, id); err != nil {
			apperrors.Internal(c)
			return
		}
		if err = writeAuditTx(c, tx, adminActor(c), "marketing_sync_retried", "marketing_subscription", int(id), gin.H{"operation": operation, "version": version}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err = tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyMarketingAfterCommit(c.Request.Context(), syncID, notifiers...)
		c.JSON(http.StatusAccepted, gin.H{"status": "pending"})
	}
}
