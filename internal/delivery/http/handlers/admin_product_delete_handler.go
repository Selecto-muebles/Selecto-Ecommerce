package handlers

import (
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"

	"github.com/gin-gonic/gin"
)

func AdminDeleteProductHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}

		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)

		var name string
		var active bool
		if err := tx.QueryRow(c, "SELECT name, active FROM products WHERE id=$1 FOR UPDATE", id).Scan(&name, &active); err != nil {
			handleAdminLookupErr(c, err, "product not found")
			return
		}
		if active {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "product must be inactive before permanent deletion", gin.H{"requires_deactivation": true})
			return
		}

		var orderReferences int
		if err := tx.QueryRow(c, "SELECT COUNT(*) FROM order_items WHERE product_id=$1", id).Scan(&orderReferences); err != nil {
			apperrors.Internal(c)
			return
		}
		if orderReferences > 0 {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "product has order history and cannot be permanently deleted", gin.H{"order_references": orderReferences, "requires_archiving": true})
			return
		}

		if _, err := tx.Exec(c, "DELETE FROM products WHERE id=$1", id); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := writeAuditTx(c, tx, adminActor(c), "product_deleted", "product", id, gin.H{"name": name, "order_references": orderReferences}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}

		logger.Info(logging.EventProductCreated, "event", "product_deleted", "product_id", id)
		c.Status(http.StatusNoContent)
	}
}
