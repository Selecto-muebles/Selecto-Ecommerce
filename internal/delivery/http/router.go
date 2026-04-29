package http

import (
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
		c.JSON(200, gin.H{"status": "ok"})
	})

	// -------------------
	// Public routes
	// -------------------
	r.POST("/register", handlers.RegisterHandler(db))
	r.POST("/login", handlers.LoginHandler(db))
	r.GET("/products", handlers.GetProductsHandler(db)) // catálogo público

	// -------------------
	// Protected routes
	// -------------------
	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware())

	// 👤 User
	authorized.GET("/me", func(c *gin.Context) {
		email, _ := c.Get("email")

		c.JSON(200, gin.H{
			"email": email,
		})
	})

	// 🛍️ Products (admin)
	authorized.POST("/products", handlers.CreateProductHandler(db))

	// 🔐 Admin test
	authorized.GET("/admin/test", func(c *gin.Context) {
		role, _ := c.Get("role")

		if role != "admin" {
			c.JSON(403, gin.H{"error": "forbidden"})
			return
		}

		c.JSON(200, gin.H{
			"message": "welcome admin",
		})
	})

	return r
}