package http

import (
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/handlers"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func SetupRouter(db *database.DB, cfg *config.Config, logger *slog.Logger) *gin.Engine {
	r := gin.Default()
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(middleware.RequestContext(logger))
	r.Use(middleware.RateLimit(cfg.RateLimitPerMinute))
	r.Use(cors.New(corsConfig(cfg)))

	// -------------------
	// Health
	// -------------------
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// -------------------
	// Public routes
	// -------------------
	r.POST("/register", handlers.RegisterHandler(db, cfg))
	r.POST("/login", handlers.LoginHandler(db, cfg))
	r.GET("/products", handlers.GetProductsHandler(db)) // catálogo público
	r.POST("/payments/webhook", middleware.InternalWebhookAuth(cfg.InternalWebhookSecret, 5*time.Minute), handlers.PaymentWebhookHandler(db, logger))

	// -------------------
	// Protected routes
	// -------------------
	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware(cfg.JWTSecret))

	// 👤 User
	authorized.GET("/me", func(c *gin.Context) {
		email, _ := c.Get("email")

		c.JSON(http.StatusOK, gin.H{
			"email": email,
		})
	})

	// 🛍️ Products (admin)
	authorized.POST("/products", handlers.CreateProductHandler(db))
	authorized.POST("/orders", handlers.CreateOrderHandler(db, cfg, logger))
	authorized.GET("/orders/:id", handlers.GetOrderHandler(db))
	authorized.GET("/my-orders", handlers.GetMyOrdersHandler(db))
	authorized.GET("/admin/metrics", handlers.GetMetricsHandler(db))
	authorized.POST("/checkout", handlers.CheckoutHandler(db, cfg, logger))

	// 🔐 Admin test
	authorized.GET("/admin/test", func(c *gin.Context) {
		role, _ := c.Get("role")

		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

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
				logger.Error("expired_order_release_failed", "error", err)
				continue
			}
			if released > 0 {
				logger.Info("expired_order_release_completed", "orders_released", released)
			}
		}
	}()
}
