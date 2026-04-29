package http

import (
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/delivery/http/handlers"

	"github.com/gin-gonic/gin"
)

func SetupRouter(db *database.DB) *gin.Engine {
	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.POST("/register", handlers.RegisterHandler(db))

	return r
}