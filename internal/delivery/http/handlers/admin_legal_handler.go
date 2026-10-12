package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"github.com/gin-gonic/gin"
)

func AdminLegalTermsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var document publishedLegalDocument
		err := db.Pool.QueryRow(c, `SELECT version,title,body_markdown,body_sha256,effective_at FROM legal_documents WHERE slug='terms' AND published ORDER BY effective_at DESC LIMIT 1`).Scan(&document.Version, &document.Title, &document.Body, &document.SHA256, &document.EffectiveAt)
		if err != nil {
			handleAdminLookupErr(c, err, "published legal document not found")
			return
		}
		c.JSON(http.StatusOK, document)
	}
}

type withdrawalAdminItem struct {
	ID             int64     `json:"id"`
	PublicCode     string    `json:"public_code"`
	OrderReference string    `json:"order_reference"`
	CustomerEmail  string    `json:"customer_email"`
	Reason         string    `json:"reason"`
	Status         string    `json:"status"`
	AdminNote      string    `json:"admin_note"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func AdminWithdrawalListHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		status := strings.TrimSpace(c.Query("status"))
		if status != "" && !validWithdrawalStatus(status) {
			apperrors.BadRequest(c, "invalid status")
			return
		}
		var total int
		if err := db.Pool.QueryRow(c, `SELECT count(*) FROM consumer_withdrawal_requests WHERE ($1='' OR status=$1)`, status).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT id,public_code,order_reference,customer_email,reason,status,admin_note,created_at,updated_at FROM consumer_withdrawal_requests WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, status, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := make([]withdrawalAdminItem, 0)
		for rows.Next() {
			var item withdrawalAdminItem
			if err := rows.Scan(&item.ID, &item.PublicCode, &item.OrderReference, &item.CustomerEmail, &item.Reason, &item.Status, &item.AdminNote, &item.CreatedAt, &item.UpdatedAt); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, item)
		}
		if rows.Err() != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

type withdrawalUpdateInput struct {
	Status    string `json:"status" binding:"required"`
	AdminNote string `json:"admin_note"`
}

func AdminWithdrawalUpdateHandler(db *database.DB, notifiers ...mailinfra.DispatchNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id < 1 {
			apperrors.BadRequest(c, "invalid request id")
			return
		}
		var input withdrawalUpdateInput
		if c.ShouldBindJSON(&input) != nil || !validWithdrawalStatus(input.Status) || len(input.AdminNote) > 2000 {
			apperrors.BadRequest(c, "invalid request status or note")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var previous string
		if err := tx.QueryRow(c, "SELECT status FROM consumer_withdrawal_requests WHERE id=$1 FOR UPDATE", id).Scan(&previous); err != nil {
			handleAdminLookupErr(c, err, "withdrawal request not found")
			return
		}
		if !allowedWithdrawalTransition(previous, input.Status) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "invalid withdrawal status transition", nil)
			return
		}
		var item withdrawalAdminItem
		err = tx.QueryRow(c, `UPDATE consumer_withdrawal_requests SET status=$2,admin_note=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 RETURNING id,public_code,order_reference,customer_email,reason,status,admin_note,created_at,updated_at`, id, input.Status, strings.TrimSpace(input.AdminNote), adminActor(c)).Scan(&item.ID, &item.PublicCode, &item.OrderReference, &item.CustomerEmail, &item.Reason, &item.Status, &item.AdminNote, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		var outboxID int64
		if previous != input.Status {
			outboxID, err = mailinfra.EnqueueReturningID(c, tx, "withdrawal-status:"+item.PublicCode+":"+input.Status, item.CustomerEmail, "withdrawal_update", gin.H{
				"public_code":  item.PublicCode,
				"status_label": withdrawalStatusLabel(input.Status),
			})
			if err != nil {
				apperrors.Internal(c)
				return
			}
		}
		if err := writeAuditTx(c, tx, adminActor(c), "consumer_withdrawal_updated", "consumer_withdrawal", int(id), gin.H{"from": previous, "to": input.Status}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyAfterCommit(c.Request.Context(), outboxID, notifiers...)
		c.JSON(http.StatusOK, item)
	}
}

func withdrawalStatusLabel(status string) string {
	switch status {
	case "received":
		return "recibida"
	case "in_review":
		return "en revisión"
	case "refund_in_progress":
		return "con reintegro en curso"
	case "resolved":
		return "resuelta"
	default:
		return "actualizada"
	}
}

func validWithdrawalStatus(value string) bool {
	switch value {
	case "received", "in_review", "refund_in_progress", "resolved":
		return true
	}
	return false
}

func allowedWithdrawalTransition(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case "received":
		return to == "in_review" || to == "refund_in_progress" || to == "resolved"
	case "in_review":
		return to == "refund_in_progress" || to == "resolved"
	case "refund_in_progress":
		return to == "resolved"
	}
	return false
}
