package http

import (
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/handlers"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/logging"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func SetupRouter(db *database.DB, cfg *config.Config, logger *slog.Logger) *gin.Engine {
	r := gin.Default()
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(middleware.RequestContext(logger))
	r.Use(middleware.RateLimit(cfg.RateLimitPerMinute))
	r.Use(cors.New(corsConfig(cfg)))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/register", handlers.RegisterHandler(db, cfg, logger))
	r.POST("/login", handlers.LoginHandler(db, cfg, logger))
	r.GET("/products", handlers.GetProductsHandler(db, logger))
	r.POST("/payments/webhook", middleware.InternalWebhookAuth(cfg.InternalWebhookSecret, 5*time.Minute), handlers.PaymentWebhookHandler(db, logger))

	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware(cfg.JWTSecret))

	authorized.GET("/me", func(c *gin.Context) {
		email, _ := c.Get("email")

		c.JSON(http.StatusOK, gin.H{
			"email": email,
		})
	})

	authorized.POST("/orders", handlers.CreateOrderHandler(db, cfg, logger))
	authorized.GET("/orders/:id", handlers.GetOrderHandler(db))
	authorized.GET("/my-orders", handlers.GetMyOrdersHandler(db))
	authorized.POST("/checkout", handlers.CheckoutHandler(db, cfg, logger))

	admin := authorized.Group("/")
	admin.Use(middleware.RequireAdmin(db, logger))

	admin.POST("/products", handlers.CreateProductHandler(db, logger))
	admin.GET("/admin/metrics", handlers.GetMetricsHandler(db, logger))
	admin.GET("/admin/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "welcome admin",
		})
	})

	return r
}

func corsConfig(cfg *config.Config) cors.Config {
	corsCfg := cors.Config{
		AllowOrigins:  cfg.CORSAllowedOrigins,
		AllowMethods:  []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID", "X-Correlation-ID", "X-Service-Name", "X-Service-Timestamp", "X-Service-Signature", "Idempotency-Key", "X-Selecto-Signature", "X-Selecto-Timestamp"},
		ExposeHeaders: []string{"X-Request-ID", "X-Correlation-ID"},
	}

	if cfg.AppEnv != "production" {
		corsCfg.AllowOrigins = nil
		corsCfg.AllowAllOrigins = true
	}

	return corsCfg
}

func StartExpiredOrderWorker(db *database.DB, cfg *config.Config, logger *slog.Logger) {
	go func() {
		ticker := time.NewTicker(cfg.ReleaseWorkerInterval)
		defer ticker.Stop()
		for range ticker.C {
			released, err := handlers.ReleaseExpiredPendingOrdersWithAudit(db, cfg.OrderPendingTTL)
			if err != nil {
				logger.Error(logging.EventExpiredOrderReleaseFailed, "error", err)
				continue
			}
			if released > 0 {
				logger.Info(logging.EventExpiredOrderReleaseCompleted, "orders_released", released)
			}
		}
	}()
}
