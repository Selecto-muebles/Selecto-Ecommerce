package http

import (
	"Selecto-Ecommerce/internal/delivery/http/handlers"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

func SetupRouter(db *database.DB) *gin.Engine {
	r := gin.Default()

	// Health
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Public routes
	r.POST("/register", handlers.RegisterHandler(db))
	r.POST("/login", handlers.LoginHandler(db))

	// Protected routes
	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware())

	authorized.GET("/me", func(c *gin.Context) {
		email, _ := c.Get("email")

		c.JSON(200, gin.H{
			"email": email,
		})
	})

	return r
}