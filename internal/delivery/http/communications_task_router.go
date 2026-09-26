package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/config"
	brevoinfra "Selecto-Ecommerce/internal/infrastructure/brevo"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

type marketingTaskRequest struct {
	SyncID int64 `json:"sync_id" binding:"required,min=1"`
}

type marketingSyncProcessor interface {
	ProcessOne(context.Context, int64) error
}

func SetupCommunicationsTaskRouter(
	db *database.DB,
	cfg *config.Config,
	logger *slog.Logger,
	emailWorker emailOutboxProcessor,
	marketingWorker marketingSyncProcessor,
) *gin.Engine {
	router := SetupEmailTaskRouter(db, cfg, logger, emailWorker)
	router.POST("/internal/tasks/marketing-sync", func(c *gin.Context) {
		if c.GetHeader("X-CloudTasks-TaskName") == "" || c.GetHeader("X-CloudTasks-QueueName") == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "cloud task headers required"})
			return
		}
		if !cfg.BrevoContactsEnabled || marketingWorker == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "marketing sync is disabled"})
			return
		}
		var request marketingTaskRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task payload"})
			return
		}
		err := marketingWorker.ProcessOne(c.Request.Context(), request.SyncID)
		if errors.Is(err, brevoinfra.ErrSyncNotReady) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "marketing sync not ready"})
			return
		}
		if err != nil {
			logger.Error("marketing_task_processing_failed", "sync_id", request.SyncID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "marketing sync failed"})
			return
		}
		c.Status(http.StatusNoContent)
	})
	return router
}
