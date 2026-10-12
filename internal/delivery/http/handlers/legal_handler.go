package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type publishedLegalDocument struct {
	Version     string    `json:"version"`
	Title       string    `json:"title"`
	Body        string    `json:"body_markdown"`
	SHA256      string    `json:"sha256"`
	EffectiveAt time.Time `json:"effective_at"`
}

func PublicTermsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var document publishedLegalDocument
		err := db.Pool.QueryRow(c, `SELECT version,title,body_markdown,body_sha256,effective_at
			FROM legal_documents WHERE slug='terms' AND published ORDER BY effective_at DESC LIMIT 1`).Scan(
			&document.Version, &document.Title, &document.Body, &document.SHA256, &document.EffectiveAt)
		if errors.Is(err, pgx.ErrNoRows) {
			apperrors.JSON(c, http.StatusServiceUnavailable, apperrors.CodeServiceUnavailable, "legal terms are temporarily unavailable", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.Header("Cache-Control", "public, max-age=300, stale-while-revalidate=3600")
		c.JSON(http.StatusOK, document)
	}
}

type withdrawalInput struct {
	OrderReference string `json:"order_reference" binding:"required"`
	Email          string `json:"email" binding:"required"`
	Reason         string `json:"reason"`
}

type withdrawalResult struct {
	ID         int64  `json:"id"`
	PublicCode string `json:"public_code"`
	Status     string `json:"status"`
	Replayed   bool   `json:"replayed,omitempty"`
}

func PublicWithdrawalHandler(db *database.DB, cfg *config.Config, notifiers ...mailinfra.DispatchNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input withdrawalInput
		if c.ShouldBindJSON(&input) != nil {
			apperrors.BadRequest(c, "order reference and email are required")
			return
		}
		input.OrderReference = strings.TrimSpace(input.OrderReference)
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))
		input.Reason = strings.TrimSpace(input.Reason)
		key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if input.OrderReference == "" || len(input.OrderReference) > 80 || !validEmail(input.Email) || len(input.Reason) > 2000 || len(key) < 8 || len(key) > 128 {
			apperrors.BadRequest(c, "order reference, valid email, optional reason, and Idempotency-Key are required")
			return
		}
		requestMaterial, err := json.Marshal(input)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		requestDigest := sha256.Sum256(requestMaterial)
		requestHash := hex.EncodeToString(requestDigest[:])
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		if _, err = tx.Exec(c, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "withdrawal:"+key); err != nil {
			apperrors.Internal(c)
			return
		}
		var result withdrawalResult
		var existingHash string
		err = tx.QueryRow(c, `SELECT id,public_code,status,request_sha256 FROM consumer_withdrawal_requests WHERE idempotency_key=$1`, key).Scan(&result.ID, &result.PublicCode, &result.Status, &existingHash)
		if err == nil {
			if existingHash != requestHash {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "Idempotency-Key was already used for a different request", nil)
				return
			}
			if err := tx.Commit(c); err != nil {
				apperrors.Internal(c)
				return
			}
			result.Replayed = true
			c.JSON(http.StatusOK, result)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			apperrors.Internal(c)
			return
		}
		code, err := newWithdrawalCode()
		if err != nil {
			apperrors.Internal(c)
			return
		}
		var orderID any
		if decoded, decodeErr := utils.DecodeID(input.OrderReference); decodeErr == nil && decoded > 0 {
			var matched int
			matchErr := tx.QueryRow(c, `SELECT o.id FROM orders o JOIN users u ON u.id=o.user_id WHERE o.id=$1 AND lower(u.email)=lower($2)`, decoded, input.Email).Scan(&matched)
			if matchErr == nil {
				orderID = matched
			} else if !errors.Is(matchErr, pgx.ErrNoRows) {
				apperrors.Internal(c)
				return
			}
		}
		err = tx.QueryRow(c, `INSERT INTO consumer_withdrawal_requests(public_code,idempotency_key,request_sha256,order_reference,order_id,customer_email,reason)
			VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,status`, code, key, requestHash, input.OrderReference, orderID, input.Email, input.Reason).Scan(&result.ID, &result.Status)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		result.PublicCode = code
		outboxID, err := mailinfra.EnqueueReturningID(c, tx, "withdrawal-ack:"+code, input.Email, "withdrawal_ack", gin.H{
			"public_code":     code,
			"order_reference": input.OrderReference,
			"url":             strings.TrimRight(cfg.StorefrontURL, "/") + "/boton-de-arrepentimiento",
		})
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, `INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,metadata) VALUES($1,'consumer_withdrawal_received','consumer_withdrawal',$2,$3)`, input.Email, result.ID, fmt.Sprintf(`{"public_code":%q}`, code)); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyAfterCommit(c.Request.Context(), outboxID, notifiers...)
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusAccepted, result)
	}
}

func newWithdrawalCode() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "SEL-" + time.Now().UTC().Format("20060102") + "-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes), nil
}
