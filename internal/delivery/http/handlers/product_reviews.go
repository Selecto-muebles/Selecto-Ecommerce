package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type productReviewSummary struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}

func productReviewSummaries(c *gin.Context, db *database.DB, productIDs []int) (map[int]productReviewSummary, error) {
	result := make(map[int]productReviewSummary, len(productIDs))
	if len(productIDs) == 0 {
		return result, nil
	}
	rows, err := db.Pool.Query(c, `SELECT product_id,ROUND(AVG(rating)::numeric,1),COUNT(*)
		FROM product_reviews WHERE status='published' AND product_id=ANY($1) GROUP BY product_id`, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var item productReviewSummary
		if err := rows.Scan(&id, &item.Average, &item.Count); err != nil {
			return nil, err
		}
		result[id] = item
	}
	return result, rows.Err()
}

func ListProductReviewsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		productID, err := utils.DecodeID(c.Param("id"))
		if err != nil || productID <= 0 {
			apperrors.BadRequest(c, "invalid product id")
			return
		}
		page := adminPagination(c)
		var total int
		var average float64
		if err := db.Pool.QueryRow(c, `SELECT COUNT(*),COALESCE(ROUND(AVG(rating)::numeric,1),0)
			FROM product_reviews WHERE product_id=$1 AND status='published'`, productID).Scan(&total, &average); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT r.id,r.rating,r.title,r.comment,r.created_at,
			COALESCE(NULLIF(BTRIM(u.first_name),''),'Cliente'),LEFT(COALESCE(u.last_name,''),1)
			FROM product_reviews r JOIN users u ON u.id=r.user_id
			WHERE r.product_id=$1 AND r.status='published'
			ORDER BY r.created_at DESC,r.id DESC LIMIT $2 OFFSET $3`, productID, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id, rating int
			var title, comment, firstName, lastInitial string
			var createdAt time.Time
			if err := rows.Scan(&id, &rating, &title, &comment, &createdAt, &firstName, &lastInitial); err != nil {
				apperrors.Internal(c)
				return
			}
			reviewer := firstName
			if lastInitial != "" {
				reviewer += " " + lastInitial + "."
			}
			items = append(items, gin.H{"id": utils.EncodeID(id), "rating": rating, "title": title, "comment": comment, "reviewer": reviewer, "verified_purchase": true, "created_at": createdAt})
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total, "summary": productReviewSummary{Average: average, Count: total}})
	}
}

func SubmitProductReviewHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		productID, err := utils.DecodeID(c.Param("id"))
		if err != nil || productID <= 0 {
			apperrors.BadRequest(c, "invalid product id")
			return
		}
		var input struct {
			Rating  int    `json:"rating"`
			Title   string `json:"title"`
			Comment string `json:"comment"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid review")
			return
		}
		input.Title, input.Comment = strings.TrimSpace(input.Title), strings.TrimSpace(input.Comment)
		if input.Rating < 1 || input.Rating > 5 || utf8.RuneCountInString(input.Title) > 120 || utf8.RuneCountInString(input.Comment) < 10 || utf8.RuneCountInString(input.Comment) > 2000 {
			apperrors.BadRequest(c, "rating, title or comment is invalid")
			return
		}
		emailValue, _ := c.Get("email")
		email := fmt.Sprint(emailValue)
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var userID, orderID int
		err = tx.QueryRow(c, `SELECT u.id,o.id FROM users u JOIN orders o ON o.user_id=u.id
			JOIN order_items oi ON oi.order_id=o.id JOIN products p ON p.id=oi.product_id
			WHERE u.email=$1 AND oi.product_id=$2 AND o.status='paid' AND p.archived_at IS NULL
			ORDER BY COALESCE(o.paid_at,o.created_at) DESC LIMIT 1`, email, productID).Scan(&userID, &orderID)
		if errors.Is(err, pgx.ErrNoRows) {
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "a paid purchase is required to review this product", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		var reviewID int
		err = tx.QueryRow(c, `INSERT INTO product_reviews(product_id,user_id,order_id,rating,title,comment)
			VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(user_id,product_id) DO UPDATE SET
			order_id=EXCLUDED.order_id,rating=EXCLUDED.rating,title=EXCLUDED.title,comment=EXCLUDED.comment,
			status='pending',moderated_by=NULL,moderated_at=NULL,updated_at=NOW() RETURNING id`, productID, userID, orderID, input.Rating, input.Title, input.Comment).Scan(&reviewID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if err := writeAuditTx(c, tx, email, "product_review_submitted", "product_review", reviewID, gin.H{"product_id": productID, "order_id": orderID}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": utils.EncodeID(reviewID), "status": "pending"})
	}
}

func AdminListProductReviewsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		status := strings.TrimSpace(c.Query("status"))
		if status != "" && status != "pending" && status != "published" && status != "rejected" {
			apperrors.BadRequest(c, "invalid review status")
			return
		}
		var total int
		if err := db.Pool.QueryRow(c, `SELECT COUNT(*) FROM product_reviews WHERE ($1='' OR status=$1)`, status).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT r.id,r.rating,r.title,r.comment,r.status,r.created_at,r.updated_at,
			p.id,p.name,u.email,o.id FROM product_reviews r JOIN products p ON p.id=r.product_id
			JOIN users u ON u.id=r.user_id JOIN orders o ON o.id=r.order_id
			WHERE ($1='' OR r.status=$1) ORDER BY r.created_at DESC,r.id DESC LIMIT $2 OFFSET $3`, status, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id, rating, productID, orderID int
			var title, comment, itemStatus, productName, email string
			var createdAt, updatedAt time.Time
			if err := rows.Scan(&id, &rating, &title, &comment, &itemStatus, &createdAt, &updatedAt, &productID, &productName, &email, &orderID); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"id": utils.EncodeID(id), "rating": rating, "title": title, "comment": comment, "status": itemStatus, "verified_purchase": true, "created_at": createdAt, "updated_at": updatedAt, "product": gin.H{"id": utils.EncodeID(productID), "name": productName}, "customer_email": email, "order_id": utils.EncodeID(orderID)})
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func AdminModerateProductReviewHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		reviewID, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input struct {
			Status string `json:"status"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || (input.Status != "published" && input.Status != "rejected") {
			apperrors.BadRequest(c, "status must be published or rejected")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		tag, err := tx.Exec(c, `UPDATE product_reviews SET status=$1,moderated_by=$2,moderated_at=NOW(),updated_at=NOW() WHERE id=$3`, input.Status, adminActor(c), reviewID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if tag.RowsAffected() == 0 {
			apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "review not found", nil)
			return
		}
		if err := writeAuditTx(c, tx, adminActor(c), "product_review_"+input.Status, "product_review", reviewID, gin.H{"status": input.Status}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": utils.EncodeID(reviewID), "status": input.Status})
	}
}
