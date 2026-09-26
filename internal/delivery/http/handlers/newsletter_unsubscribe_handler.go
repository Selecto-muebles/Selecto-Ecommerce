package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const marketingUnsubscribeTTL = 30 * time.Minute

func NewsletterUnsubscribeHandler(db *database.DB, cfg *config.Config, notifiers ...mailinfra.DispatchNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input emailInput
		if c.ShouldBindJSON(&input) != nil {
			apperrors.BadRequest(c, "email is required")
			return
		}
		email := strings.ToLower(strings.TrimSpace(input.Email))
		if !validEmail(email) {
			apperrors.BadRequest(c, "email is invalid")
			return
		}
		outboxID, err := requestMarketingUnsubscribe(c, db, cfg, email)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyAfterCommit(c.Request.Context(), outboxID, notifiers...)
		c.JSON(http.StatusAccepted, gin.H{"message": "if the address is subscribed, confirmation instructions will be sent"})
	}
}

func requestMarketingUnsubscribe(c *gin.Context, db *database.DB, cfg *config.Config, email string) (int64, error) {
	tx, err := db.Pool.Begin(c)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(c)
	var subscriptionID int64
	err = tx.QueryRow(c, `SELECT id FROM commerce.marketing_subscriptions WHERE lower(email)=lower($1) AND status='subscribed' FOR UPDATE`, email).Scan(&subscriptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, tx.Commit(c)
	}
	if err != nil {
		return 0, err
	}
	var recentlyRequested bool
	if err := tx.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM commerce.marketing_unsubscribe_tokens WHERE lower(email)=lower($1) AND consumed_at IS NULL AND created_at>NOW()-INTERVAL '1 minute')", email).Scan(&recentlyRequested); err != nil {
		return 0, err
	}
	if recentlyRequested {
		return 0, tx.Commit(c)
	}
	token, tokenHash, err := newAccountToken()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(c, `UPDATE commerce.marketing_unsubscribe_tokens SET consumed_at=NOW() WHERE lower(email)=lower($1) AND consumed_at IS NULL`, email); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(c, `INSERT INTO commerce.marketing_unsubscribe_tokens(email,token_hash,expires_at)
	 VALUES($1,$2,NOW()+make_interval(secs=>$3))`, email, tokenHash, int(marketingUnsubscribeTTL.Seconds())); err != nil {
		return 0, err
	}
	outboxID, err := mailinfra.EnqueueReturningID(c, tx, fmt.Sprintf("newsletter-unsubscribe:%d:%s", subscriptionID, tokenHash[:16]), email, "newsletter_unsubscribe", gin.H{
		"url": accountURL(cfg.StorefrontURL, "/newsletter/baja", token),
	})
	if err != nil {
		return 0, err
	}
	return outboxID, tx.Commit(c)
}

func NewsletterUnsubscribeConfirmHandler(db *database.DB, notifiers ...mailinfra.MarketingDispatchNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input tokenInput
		if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Token) == "" {
			apperrors.BadRequest(c, "unsubscribe token is required")
			return
		}
		syncID, err := confirmMarketingUnsubscribe(c, db, strings.TrimSpace(input.Token))
		if errors.Is(err, pgx.ErrNoRows) {
			apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, "unsubscribe token is invalid or expired", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyMarketingAfterCommit(c.Request.Context(), syncID, notifiers...)
		c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
	}
}

func confirmMarketingUnsubscribe(c *gin.Context, db *database.DB, token string) (int64, error) {
	tx, err := db.Pool.Begin(c)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(c)
	var tokenID int64
	var email string
	err = tx.QueryRow(c, `SELECT id,email FROM commerce.marketing_unsubscribe_tokens
	 WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at>NOW()`, hashAccountToken(token)).Scan(&tokenID, &email)
	if err != nil {
		return 0, err
	}
	var subscriptionID, version int64
	if err := tx.QueryRow(c, "SELECT id FROM commerce.marketing_subscriptions WHERE lower(email)=lower($1) FOR UPDATE", email).Scan(&subscriptionID); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(c, "SELECT id FROM commerce.marketing_unsubscribe_tokens WHERE id=$1 AND consumed_at IS NULL AND expires_at>NOW() FOR UPDATE", tokenID).Scan(&tokenID); err != nil {
		return 0, err
	}
	err = tx.QueryRow(c, `UPDATE commerce.marketing_subscriptions SET status='unsubscribed',unsubscribed_at=NOW(),
	 sync_status=CASE WHEN suppression_reason<>'' THEN 'suppressed' ELSE 'pending' END,sync_version=sync_version+1,sync_error='',updated_at=NOW()
	 WHERE lower(email)=lower($1) RETURNING id,sync_version`, email).Scan(&subscriptionID, &version)
	var syncID int64
	if err == nil {
		syncID, err = enqueueMarketingSync(c, tx, subscriptionID, email, "unsubscribe", version)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if _, err := tx.Exec(c, `UPDATE commerce.marketing_unsubscribe_tokens SET consumed_at=NOW() WHERE id=$1`, tokenID); err != nil {
		return 0, err
	}
	return syncID, tx.Commit(c)
}

func enqueueMarketingSync(c *gin.Context, tx pgx.Tx, subscriptionID int64, email, operation string, version int64) (int64, error) {
	var id int64
	err := tx.QueryRow(c, `INSERT INTO commerce.marketing_sync_outbox(event_key,subscription_id,email,operation,subscription_version)
	 VALUES($1,$2,$3,$4,$5) ON CONFLICT(event_key) DO UPDATE SET event_key=EXCLUDED.event_key RETURNING id`,
		fmt.Sprintf("marketing:%d:%d:%s", subscriptionID, version, operation), subscriptionID, email, operation, version).Scan(&id)
	return id, err
}
