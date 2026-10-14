package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func AdminOperationsListProductReviewsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		status := strings.TrimSpace(c.Query("status"))
		if status != "" && !validReviewStatus(status) {
			apperrors.BadRequest(c, "invalid review status")
			return
		}
		var total int
		if err := db.Pool.QueryRow(c, `SELECT COUNT(*) FROM product_reviews WHERE ($1='' OR status=$1)`, status).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		items, err := adminReviewItems(c, db, status, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func adminReviewItems(c *gin.Context, db *database.DB, status string, limit, offset int) ([]gin.H, error) {
	rows, err := db.Pool.Query(c, `SELECT r.id,r.rating,r.title,r.comment,r.status,r.pinned,r.moderation_note,
	 r.created_at,r.updated_at,r.moderated_by,r.moderated_at,p.id,p.name,u.email,o.id
	 FROM product_reviews r JOIN products p ON p.id=r.product_id
	 JOIN users u ON u.id=r.user_id JOIN orders o ON o.id=r.order_id
	 WHERE ($1='' OR r.status=$1) ORDER BY r.pinned DESC,r.created_at DESC,r.id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, rating, productID, orderID int
		var title, comment, itemStatus, note, productName, email string
		var pinned bool
		var moderatedBy sql.NullString
		var moderatedAt sql.NullTime
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &rating, &title, &comment, &itemStatus, &pinned, &note, &createdAt, &updatedAt, &moderatedBy, &moderatedAt, &productID, &productName, &email, &orderID); err != nil {
			return nil, err
		}
		items = append(items, gin.H{
			"id": utils.EncodeID(id), "rating": rating, "title": title, "comment": comment,
			"status": itemStatus, "pinned": pinned, "moderation_note": note, "moderated_by": nullableString(moderatedBy),
			"moderated_at": nullableTime(moderatedAt), "verified_purchase": true,
			"created_at": createdAt, "updated_at": updatedAt,
			"product":        gin.H{"id": utils.EncodeID(productID), "name": productName},
			"customer_email": email, "order_id": utils.EncodeID(orderID),
		})
	}
	return items, rows.Err()
}

func AdminOperationsModerateProductReviewHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		reviewID, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input struct {
			Status *string `json:"status"`
			Note   *string `json:"note"`
			Pinned *bool   `json:"pinned"`
		}
		if c.ShouldBindJSON(&input) != nil || (input.Status == nil && input.Pinned == nil) {
			apperrors.BadRequest(c, "specify a valid review status or pinned value")
			return
		}
		if input.Status != nil && !validReviewStatus(*input.Status) {
			apperrors.BadRequest(c, "status must be pending, published or rejected")
			return
		}
		note := ""
		if input.Note != nil {
			note = strings.TrimSpace(*input.Note)
		}
		if utf8.RuneCountInString(note) > 500 {
			apperrors.BadRequest(c, "moderation note is too long")
			return
		}
		if input.Status != nil && *input.Status == "rejected" && note == "" {
			apperrors.BadRequest(c, "a moderation reason is required to remove a review")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var previousStatus, previousNote string
		var previousPinned bool
		if err := tx.QueryRow(c, `SELECT status,pinned,moderation_note FROM product_reviews WHERE id=$1 FOR UPDATE`, reviewID).Scan(&previousStatus, &previousPinned, &previousNote); err != nil {
			handleAdminLookupErr(c, err, "review not found")
			return
		}
		status := previousStatus
		if input.Status != nil {
			status = *input.Status
		}
		nextNote := previousNote
		if input.Note != nil {
			nextNote = note
		}
		pinned := previousPinned
		if input.Pinned != nil {
			pinned = *input.Pinned
		}
		if input.Pinned != nil && *input.Pinned && status != "published" {
			apperrors.BadRequest(c, "only published reviews can be pinned")
			return
		}
		if status != "published" {
			pinned = false
		}
		if previousStatus == status && previousPinned == pinned && previousNote == nextNote {
			apperrors.BadRequest(c, "the review has no changes to save")
			return
		}
		if _, err := tx.Exec(c, `UPDATE product_reviews SET status=$1,pinned=$2,moderation_note=$3,
		 moderated_by=$4,moderated_at=NOW(),updated_at=NOW() WHERE id=$5`, status, pinned, nextNote, adminActor(c), reviewID); err != nil {
			apperrors.Internal(c)
			return
		}
		if previousStatus != status {
			metadata := gin.H{"from": previousStatus, "to": status, "previous_note": previousNote, "note": nextNote, "pinned": pinned}
			if err := writeAuditTx(c, tx, adminActor(c), "product_review_"+status, "product_review", reviewID, metadata); err != nil {
				apperrors.Internal(c)
				return
			}
		}
		if previousNote != nextNote && previousStatus == status {
			metadata := gin.H{"status": status, "previous_note": previousNote, "note": nextNote}
			if err := writeAuditTx(c, tx, adminActor(c), "product_review_note_updated", "product_review", reviewID, metadata); err != nil {
				apperrors.Internal(c)
				return
			}
		}
		if previousPinned != pinned {
			metadata := gin.H{"status": status, "from_pinned": previousPinned, "to_pinned": pinned}
			action := "product_review_unpinned"
			if pinned {
				action = "product_review_pinned"
			}
			if err := writeAuditTx(c, tx, adminActor(c), action, "product_review", reviewID, metadata); err != nil {
				apperrors.Internal(c)
				return
			}
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": utils.EncodeID(reviewID), "status": status, "pinned": pinned, "moderation_note": nextNote})
	}
}

func validReviewStatus(status string) bool {
	return status == "pending" || status == "published" || status == "rejected"
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
