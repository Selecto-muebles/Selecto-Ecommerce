package http

import (
	"net/http"

	"Selecto-Ecommerce/internal/delivery/http/handlers"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

func SetupRouter(db *database.DB) *gin.Engine {
	r := gin.Default()

	// -------------------
	// Health
	// -------------------
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// -------------------
	// Public routes
	// -------------------
	r.POST("/register", handlers.RegisterHandler(db))
	r.POST("/login", handlers.LoginHandler(db))
	r.GET("/products", handlers.GetProductsHandler(db)) // catálogo público
	r.POST("/payments/webhook", handlers.PaymentWebhookHandler(db))

	// -------------------
	// Protected routes
	// -------------------
	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware())

	// 👤 User
	authorized.GET("/me", func(c *gin.Context) {
		email, _ := c.Get("email")

		c.JSON(http.StatusOK, gin.H{
			"email": email,
		})
	})

	// 🛍️ Products (admin)
	authorized.POST("/products", handlers.CreateProductHandler(db))
	authorized.POST("/orders", handlers.CreateOrderHandler(db))
	authorized.GET("/orders/:id", handlers.GetOrderHandler(db))
	authorized.GET("/my-orders", handlers.GetMyOrdersHandler(db))
	authorized.GET("/admin/metrics", handlers.GetMetricsHandler(db))
	authorized.POST("/checkout", handlers.CheckoutHandler(db))

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
