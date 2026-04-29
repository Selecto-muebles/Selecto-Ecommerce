package main

import (
	"log"

	"Selecto-Ecommerce/internal/config"
	httpDelivery "Selecto-Ecommerce/internal/delivery/http"
	"Selecto-Ecommerce/internal/infrastructure/database"
)

func main() {
	cfg := config.LoadConfig()

	// DB
	db := database.NewPostgresPool(cfg.DatabaseURL)
	defer db.Pool.Close()

	// Router (ahora recibe db)
	router := httpDelivery.SetupRouter(db)

	log.Printf("🚀 Server running on port %s", cfg.Port)
	router.Run(":" + cfg.Port)
}