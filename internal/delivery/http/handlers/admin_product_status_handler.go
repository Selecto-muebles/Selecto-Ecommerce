package handlers

import (
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"

	"github.com/gin-gonic/gin"
)

func AdminUpdateProductStatusHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input struct {
			Active *bool `json:"active"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || input.Active == nil {
			apperrors.BadRequest(c, "active is required")
			return
		}
		var before, archived bool
		if err := db.Pool.QueryRow(c, "SELECT active,archived_at IS NOT NULL FROM products WHERE id=$1", id).Scan(&before, &archived); err != nil {
			handleAdminLookupErr(c, err, "product not found")
			return
		}
		if archived && *input.Active {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "archived product cannot be activated", nil)
			return
		}
		if _, err := db.Pool.Exec(c, "UPDATE products SET active=$1, updated_at=NOW() WHERE id=$2", *input.Active, id); err != nil {
			apperrors.Internal(c)
			return
		}
		action := "product_deactivated"
		if *input.Active {
			action = "product_activated"
		}
		_ = writeAudit(c, db, adminActor(c), action, "product", id, gin.H{"before_active": before, "after_active": *input.Active})
		logger.Info(logging.EventProductCreated, "event", action, "product_id", id)
		item, _ := adminProduct(c, db, id)
		c.JSON(http.StatusOK, item)
	}
}
