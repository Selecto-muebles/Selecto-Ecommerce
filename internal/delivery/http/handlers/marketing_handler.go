package handlers

import (
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type newsletterInput struct {
	Email   string `json:"email"`
	Consent bool   `json:"consent"`
	Source  string `json:"source"`
}

func NewsletterSubscribeHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input newsletterInput
		if err := c.ShouldBindJSON(&input); err != nil || !input.Consent {
			apperrors.BadRequest(c, "email and consent are required")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))
		if !validEmail(input.Email) {
			apperrors.BadRequest(c, "email is invalid")
			return
		}
		input.Source = strings.TrimSpace(input.Source)
		if input.Source == "" {
			input.Source = "storefront"
		}
		if len(input.Source) > 80 {
			apperrors.BadRequest(c, "source is too long")
			return
		}
		_, err := db.Pool.Exec(c, `
			INSERT INTO marketing_subscriptions (email, status, source, consent_at, unsubscribed_at, updated_at)
			VALUES ($1, 'subscribed', $2, NOW(), NULL, NOW())
			ON CONFLICT (lower(email)) DO UPDATE SET
				status='subscribed', source=EXCLUDED.source, consent_at=NOW(),
				unsubscribed_at=NULL, updated_at=NOW()`, input.Email, input.Source)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "subscribed"})
	}
}

func NewsletterUnsubscribeHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Email string `json:"email"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "email is required")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))
		if !validEmail(input.Email) {
			apperrors.BadRequest(c, "email is invalid")
			return
		}
		_, err := db.Pool.Exec(c, `UPDATE marketing_subscriptions SET status='unsubscribed', unsubscribed_at=NOW(), updated_at=NOW() WHERE lower(email)=lower($1)`, input.Email)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
	}
}

func AdminListMarketingSubscriptionsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		status := strings.TrimSpace(strings.ToLower(c.Query("status")))
		if status != "" && status != "subscribed" && status != "unsubscribed" {
			apperrors.BadRequest(c, "invalid status")
			return
		}
		var total int
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM marketing_subscriptions WHERE ($1='' OR status=$1)", status).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT id, email, status, source, consent_at, unsubscribed_at, created_at, updated_at FROM marketing_subscriptions WHERE ($1='' OR status=$1) ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, status, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id int64
			var email, subscriptionStatus, source string
			var consentAt, unsubscribedAt, createdAt, updatedAt *time.Time
			if err := rows.Scan(&id, &email, &subscriptionStatus, &source, &consentAt, &unsubscribedAt, &createdAt, &updatedAt); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"id": id, "email": email, "status": subscriptionStatus, "source": source, "consent_at": consentAt, "unsubscribed_at": unsubscribedAt, "created_at": createdAt, "updated_at": updatedAt})
		}
		if err := rows.Err(); err != nil && err != pgx.ErrNoRows {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func AdminListEmailOutboxHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		status := strings.TrimSpace(strings.ToLower(c.Query("status")))
		if status != "" && status != "pending" && status != "processing" && status != "sent" && status != "failed" {
			apperrors.BadRequest(c, "invalid status")
			return
		}
		template := strings.TrimSpace(strings.ToLower(c.Query("template")))
		recipient := strings.TrimSpace(strings.ToLower(c.Query("recipient")))
		var total int
		where := "($1='' OR status=$1) AND ($2='' OR template=$2) AND ($3='' OR lower(recipient) LIKE '%' || $3 || '%')"
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM email_outbox WHERE "+where, status, template, recipient).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT id, event_key, recipient, template, status, attempts, next_attempt_at, locked_at, sent_at, last_error, created_at, updated_at FROM email_outbox WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $4 OFFSET $5`, status, template, recipient, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id int64
			var eventKey, email, emailTemplate, emailStatus, lastError string
			var attempts int
			var nextAttemptAt, createdAt, updatedAt time.Time
			var lockedAt, sentAt *time.Time
			if err := rows.Scan(&id, &eventKey, &email, &emailTemplate, &emailStatus, &attempts, &nextAttemptAt, &lockedAt, &sentAt, &lastError, &createdAt, &updatedAt); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"id": id, "event_key": eventKey, "recipient": email, "template": emailTemplate, "status": emailStatus, "attempts": attempts, "next_attempt_at": nextAttemptAt, "locked_at": lockedAt, "sent_at": sentAt, "last_error": lastError, "created_at": createdAt, "updated_at": updatedAt})
		}
		if err := rows.Err(); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}
