package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func AdminOperationsListEmailOutboxHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		status := strings.TrimSpace(strings.ToLower(c.Query("status")))
		if status != "" && status != "pending" && status != "processing" && status != "sent" && status != "failed" {
			apperrors.BadRequest(c, "invalid status")
			return
		}
		delivery := strings.TrimSpace(strings.ToLower(c.Query("delivery_status")))
		template := strings.TrimSpace(c.Query("template"))
		recipient := strings.ToLower(strings.TrimSpace(c.Query("recipient")))
		where := "($1='' OR status=$1) AND ($2='' OR delivery_status=$2) AND ($3='' OR template=$3) AND ($4='' OR lower(recipient) LIKE '%' || $4 || '%')"
		var total int
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM email_outbox WHERE "+where, status, delivery, template, recipient).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT id,event_key,recipient,template,status,delivery_status,attempts,
		 next_attempt_at,locked_at,sent_at,delivered_at,last_provider_event_at,provider_message_id,last_error,bounce_reason,created_at,updated_at
		 FROM email_outbox WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6`,
			status, delivery, template, recipient, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id int64
			var eventKey, email, template, itemStatus, deliveryStatus, messageID, lastError, bounceReason string
			var attempts int
			var nextAttemptAt, createdAt, updatedAt time.Time
			var lockedAt, sentAt, deliveredAt, lastProviderEventAt sql.NullTime
			if err := rows.Scan(&id, &eventKey, &email, &template, &itemStatus, &deliveryStatus, &attempts,
				&nextAttemptAt, &lockedAt, &sentAt, &deliveredAt, &lastProviderEventAt,
				&messageID, &lastError, &bounceReason, &createdAt, &updatedAt); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{
				"id": id, "event_key": eventKey, "recipient": email, "template": template,
				"status": itemStatus, "delivery_status": deliveryStatus, "attempts": attempts,
				"next_attempt_at": nextAttemptAt, "locked_at": nullableTime(lockedAt),
				"sent_at": nullableTime(sentAt), "delivered_at": nullableTime(deliveredAt),
				"last_provider_event_at": nullableTime(lastProviderEventAt),
				"provider_message_id":    messageID, "last_error": lastError,
				"bounce_reason": bounceReason, "created_at": createdAt, "updated_at": updatedAt,
			})
		}
		if rows.Err() != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func AdminListBrevoEventsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		var total int
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM brevo_webhook_events").Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT event_key,event_type,event_scope,email,provider_message_id,
		 outbox_event_key,occurred_at,created_at FROM brevo_webhook_events
		 ORDER BY created_at DESC LIMIT $1 OFFSET $2`, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var eventKey, eventType, scope, email, messageID, outboxEventKey string
			var occurredAt sql.NullTime
			var createdAt time.Time
			if err := rows.Scan(&eventKey, &eventType, &scope, &email, &messageID, &outboxEventKey, &occurredAt, &createdAt); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"event_key": eventKey, "event_type": eventType, "event_scope": scope,
				"email": email, "provider_message_id": messageID, "outbox_event_key": outboxEventKey,
				"occurred_at": nullableTime(occurredAt), "created_at": createdAt})
		}
		if rows.Err() != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}
