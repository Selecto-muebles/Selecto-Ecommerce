package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"

	"github.com/gin-gonic/gin"
)

type emailTaskRequest struct {
	OutboxID int64 `json:"outbox_id" binding:"required,min=1"`
}

type emailOutboxProcessor interface {
	ProcessOne(context.Context, int64) error
}

// SetupEmailTaskRouter returns a minimal Gin engine that handles a single
// Cloud Tasks HTTP target for processing individual email outbox entries.
// It is meant to run as a separate serverless process from the main API.
func SetupEmailTaskRouter(
	db *database.DB,
	cfg *config.Config,
	logger *slog.Logger,
	worker emailOutboxProcessor,
) *gin.Engine {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	_ = router.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	router.Use(middleware.RequestContext(logger))
	router.Use(middleware.SecurityHeaders(cfg.AppEnv))
	router.Use(middleware.RequestBodyLimit(4 << 10))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.Pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	router.POST("/internal/tasks/email-outbox", func(c *gin.Context) {
		if c.GetHeader("X-CloudTasks-TaskName") == "" || c.GetHeader("X-CloudTasks-QueueName") == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "cloud task headers required"})
			return
		}
		var request emailTaskRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task payload"})
			return
		}
		err := worker.ProcessOne(c.Request.Context(), request.OutboxID)
		if errors.Is(err, mailinfra.ErrEmailNotReady) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "email not ready"})
			return
		}
		if err != nil {
			logger.Error("email_task_processing_failed", "email_id", request.OutboxID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "email processing failed"})
			return
		}
		c.Status(http.StatusNoContent)
	})

	return router
}