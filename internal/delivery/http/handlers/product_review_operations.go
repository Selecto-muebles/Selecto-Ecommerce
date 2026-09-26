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
	rows, err := db.Pool.Query(c, `SELECT r.id,r.rating,r.title,r.comment,r.status,r.moderation_note,
	 r.created_at,r.updated_at,r.moderated_by,r.moderated_at,p.id,p.name,u.email,o.id
	 FROM product_reviews r JOIN products p ON p.id=r.product_id
	 JOIN users u ON u.id=r.user_id JOIN orders o ON o.id=r.order_id
	 WHERE ($1='' OR r.status=$1) ORDER BY r.created_at DESC,r.id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, rating, productID, orderID int
		var title, comment, itemStatus, note, productName, email string
		var moderatedBy sql.NullString
		var moderatedAt sql.NullTime
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &rating, &title, &comment, &itemStatus, &note, &createdAt, &updatedAt, &moderatedBy, &moderatedAt, &productID, &productName, &email, &orderID); err != nil {
			return nil, err
		}
		items = append(items, gin.H{
			"id": utils.EncodeID(id), "rating": rating, "title": title, "comment": comment,
			"status": itemStatus, "moderation_note": note, "moderated_by": nullableString(moderatedBy),
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
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if c.ShouldBindJSON(&input) != nil || !validReviewStatus(input.Status) {
			apperrors.BadRequest(c, "status must be pending, published or rejected")
			return
		}
		input.Note = strings.TrimSpace(input.Note)
		if utf8.RuneCountInString(input.Note) > 500 {
			apperrors.BadRequest(c, "moderation note is too long")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var previousStatus, previousNote string
		if err := tx.QueryRow(c, `SELECT status,moderation_note FROM product_reviews WHERE id=$1 FOR UPDATE`, reviewID).Scan(&previousStatus, &previousNote); err != nil {
			handleAdminLookupErr(c, err, "review not found")
			return
		}
		_, err = tx.Exec(c, `UPDATE product_reviews SET status=$1,moderation_note=$2,
		 moderated_by=$3,moderated_at=NOW(),updated_at=NOW() WHERE id=$4`, input.Status, input.Note, adminActor(c), reviewID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		metadata := gin.H{"from": previousStatus, "to": input.Status, "previous_note": previousNote, "note": input.Note}
		if err := writeAuditTx(c, tx, adminActor(c), "product_review_"+input.Status, "product_review", reviewID, metadata); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": utils.EncodeID(reviewID), "status": input.Status, "moderation_note": input.Note})
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
