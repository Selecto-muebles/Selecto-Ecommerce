package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type brevoWebhookPayload struct {
	Event        string          `json:"event"`
	Email        string          `json:"email"`
	MessageID    string          `json:"message-id"`
	Timestamp    int64           `json:"ts_event"`
	Reason       string          `json:"reason"`
	Custom       any             `json:"X-Mailin-custom"`
	CampaignID   int64           `json:"camp_id"`
	ContactID    int64           `json:"contact_id"`
	WebhookEvent json.RawMessage `json:"id"`
	Epoch        int64           `json:"ts_epoch"`
}

func BrevoWebhookHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := c.GetHeader("X-Forwarded-Authorization")
		if authorization == "" {
			authorization = c.GetHeader("Authorization")
		}
		if !validBrevoWebhookAuthorization(authorization, cfg.BrevoWebhookToken) {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid webhook credentials", nil)
			return
		}
		var payload brevoWebhookPayload
		if c.ShouldBindJSON(&payload) != nil {
			apperrors.BadRequest(c, "invalid Brevo webhook")
			return
		}
		payload.Event = strings.ToLower(strings.TrimSpace(payload.Event))
		if payload.Event == "hardbounce" {
			payload.Event = "hard_bounce"
		}
		if payload.Event == "softbounce" {
			payload.Event = "soft_bounce"
		}
		if payload.Timestamp == 0 && payload.Epoch > 0 {
			payload.Timestamp = payload.Epoch
			if payload.Timestamp > 100000000000 {
				payload.Timestamp /= 1000
			}
		}
		payload.Email = strings.ToLower(strings.TrimSpace(payload.Email))
		if payload.Event == "" || payload.Timestamp <= 0 || !validEmail(payload.Email) {
			apperrors.BadRequest(c, "event, email and event timestamp are required")
			return
		}
		if err := storeBrevoWebhook(c, db, payload); err != nil {
			apperrors.Internal(c)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func validBrevoWebhookAuthorization(header, secret string) bool {
	if len(secret) < 32 {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func storeBrevoWebhook(c *gin.Context, db *database.DB, payload brevoWebhookPayload) error {
	outboxEventKey := brevoOutboxEventKey(payload.Custom)
	eventKey := brevoEventKey(payload, outboxEventKey)
	scope := "marketing"
	if outboxEventKey != "" || payload.CampaignID == 0 {
		scope = "transactional"
	}
	occurredAt := time.Now().UTC()
	if payload.Timestamp > 0 {
		occurredAt = time.Unix(payload.Timestamp, 0).UTC()
	}
	tx, err := db.Pool.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	var inserted string
	err = tx.QueryRow(c, `INSERT INTO brevo_webhook_events
	 (event_key,event_type,event_scope,email,provider_message_id,outbox_event_key,occurred_at)
	 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(event_key) DO NOTHING RETURNING event_key`,
		eventKey, payload.Event, scope, payload.Email, payload.MessageID, outboxEventKey, occurredAt).Scan(&inserted)
	if err == pgx.ErrNoRows {
		return tx.Commit(c)
	}
	if err != nil {
		return err
	}
	if outboxEventKey != "" && brevoDeliveryStatus(payload.Event) != "" {
		status := brevoDeliveryStatus(payload.Event)
		deliveredAt := any(nil)
		if status == "delivered" {
			deliveredAt = occurredAt
		}
		_, err = tx.Exec(c, `UPDATE email_outbox SET delivery_status=$2,
		 provider_message_id=CASE WHEN $3='' THEN provider_message_id ELSE $3 END,
		 delivered_at=COALESCE($4,delivered_at),last_provider_event_at=$5,
		 bounce_reason=CASE WHEN $6='' THEN bounce_reason ELSE LEFT($6,1000) END,updated_at=NOW()
		 WHERE event_key=$1 AND (last_provider_event_at IS NULL OR last_provider_event_at<=$5)
		 AND (delivery_status NOT IN ('bounced','blocked','spam','invalid') OR $2 IN ('bounced','blocked','spam','invalid'))
		 AND (delivery_status<>'delivered' OR $2 NOT IN ('sent','deferred'))`, outboxEventKey, status, payload.MessageID, deliveredAt, occurredAt, payload.Reason)
		if err != nil {
			return err
		}
	}
	if isBrevoOptOut(payload.Event) && payload.Email != "" {
		err = applyBrevoSuppression(c, tx, payload.Email, payload.Event, occurredAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit(c)
}

func applyBrevoSuppression(c *gin.Context, tx pgx.Tx, email, event string, occurredAt time.Time) error {
	var id, version int64
	err := tx.QueryRow(c, `UPDATE commerce.marketing_subscriptions SET status='unsubscribed',
	 unsubscribed_at=COALESCE(unsubscribed_at,$2),sync_status='suppressed',suppression_reason=$3,
	 sync_version=sync_version+1,last_provider_event_at=$2,sync_error='',updated_at=NOW()
	 WHERE lower(email)=lower($1) RETURNING id,sync_version`, email, occurredAt, event).Scan(&id, &version)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = enqueueMarketingSync(c, tx, id, email, "unsubscribe", version)
	return err
}

func brevoOutboxEventKey(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case map[string]any:
		key, _ := typed["event_key"].(string)
		return strings.TrimSpace(key)
	case string:
		var object map[string]any
		if json.Unmarshal([]byte(typed), &object) == nil {
			key, _ := object["event_key"].(string)
			return strings.TrimSpace(key)
		}
		for _, part := range strings.FieldsFunc(typed, func(r rune) bool { return r == '&' || r == ';' }) {
			key, raw, found := strings.Cut(part, "=")
			if found && strings.TrimSpace(key) == "event_key" {
				return strings.TrimSpace(raw)
			}
		}
	}
	return ""
}

func brevoEventKey(payload brevoWebhookPayload, outboxEventKey string) string {
	raw := fmt.Sprintf("%s|%s|%s|%d|%s|%s|%d|%d", payload.Event, payload.Email,
		payload.MessageID, payload.Timestamp, payload.WebhookEvent, outboxEventKey, payload.CampaignID, payload.ContactID)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func brevoDeliveryStatus(event string) string {
	statuses := map[string]string{
		"request": "sent", "sent": "sent", "delivered": "delivered", "deferred": "deferred",
		"soft_bounce": "deferred", "hard_bounce": "bounced", "blocked": "blocked",
		"spam": "spam", "invalid": "invalid", "error": "error",
	}
	if status := statuses[event]; status != "" {
		return status
	}
	return ""
}

func isBrevoOptOut(event string) bool {
	switch event {
	case "unsubscribe", "unsubscribed", "hard_bounce", "blocked", "spam", "invalid":
		return true
	default:
		return false
	}
}
