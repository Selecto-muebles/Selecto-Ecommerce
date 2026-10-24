package handlers

import (
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/service/shipping"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func AdminDispatchBatchesHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := (postgres.DispatchBatchStore{Pool: db.Pool}).ListBatches(c, 60)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "cutoff_hour": cfg.DispatchCutoffHour,
			"timezone": shipping.DispatchTimezone, "whatsapp_recipient": cfg.GestionarWhatsAppRecipient})
	}
}

func AdminPrepareDispatchBatchesHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		count, err := (postgres.DispatchBatchStore{Pool: db.Pool}).PrepareDueBatches(c, time.Now(), cfg.DispatchCutoffHour)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"orders_assigned": count})
	}
}
