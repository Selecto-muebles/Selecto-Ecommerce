package main

import (
	"log"
	"selecto-backend/internal/config"
	"selecto-backend/internal/delivery/http"
	"selecto-backend/internal/infrastructure/database"
)

func main() {
	cfg := config.LoadConfig()

	// DB
	db := database.NewPostgresPool(cfg.DatabaseURL)
	defer db.Close()

	// Router
	router := http.SetupRouter()

	log.Printf("🚀 Server running on port %s", cfg.Port)
	router.Run(":" + cfg.Port)
}