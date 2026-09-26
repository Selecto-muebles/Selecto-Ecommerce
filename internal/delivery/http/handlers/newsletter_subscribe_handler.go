package handlers

import (
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func NewsletterSubscribeHandler(db *database.DB, notifiers ...mailinfra.MarketingDispatchNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input newsletterInput
		if c.ShouldBindJSON(&input) != nil || !input.Consent {
			apperrors.BadRequest(c, "email and consent are required")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))
		input.Source = strings.TrimSpace(input.Source)
		if !validEmail(input.Email) || len(input.Source) > 80 {
			apperrors.BadRequest(c, "email or source is invalid")
			return
		}
		if input.Source == "" {
			input.Source = "storefront"
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var subscriptionID, version int64
		var syncStatus string
		err = tx.QueryRow(c, `INSERT INTO commerce.marketing_subscriptions
		 (email,status,source,consent_at,unsubscribed_at,sync_status,sync_version,sync_error,updated_at)
		 VALUES($1,'subscribed',$2,NOW(),NULL,'pending',1,'',NOW())
		 ON CONFLICT (lower(email)) DO UPDATE SET source=EXCLUDED.source,
		 status=CASE WHEN marketing_subscriptions.suppression_reason<>'' THEN marketing_subscriptions.status ELSE 'subscribed' END,
		 consent_at=CASE WHEN marketing_subscriptions.suppression_reason<>'' THEN marketing_subscriptions.consent_at ELSE NOW() END,
		 unsubscribed_at=CASE WHEN marketing_subscriptions.suppression_reason<>'' THEN marketing_subscriptions.unsubscribed_at ELSE NULL END,
		 sync_status=CASE WHEN marketing_subscriptions.suppression_reason<>'' THEN 'suppressed' ELSE 'pending' END,
		 sync_version=CASE WHEN marketing_subscriptions.suppression_reason<>'' THEN marketing_subscriptions.sync_version ELSE marketing_subscriptions.sync_version+1 END,
		 sync_error=CASE WHEN marketing_subscriptions.suppression_reason<>'' THEN marketing_subscriptions.sync_error ELSE '' END,
		 updated_at=NOW() RETURNING id,sync_version,sync_status`, input.Email, input.Source).Scan(&subscriptionID, &version, &syncStatus)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		var syncID int64
		if syncStatus != "suppressed" {
			syncID, err = enqueueMarketingSync(c, tx, subscriptionID, input.Email, "subscribe", version)
			if err != nil {
				apperrors.Internal(c)
				return
			}
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyMarketingAfterCommit(c.Request.Context(), syncID, notifiers...)
		if syncStatus == "suppressed" {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "this address cannot receive marketing messages; contact support", nil)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "subscribed"})
	}
}
