package handlers

import (
	"context"
	"net/http"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/gestionar"

	"github.com/gin-gonic/gin"
)

type gestionarPickupTimeReader interface {
	PickupTimes(context.Context) ([]gestionar.PickupTime, error)
}

func AdminGestionarStatusHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"configured": cfg.GestionarConfigured(),
			"mode":       "read_only",
			"can_send":   false,
		})
	}
}

func AdminGestionarPickupTimesHandler(cfg *config.Config, client gestionarPickupTimeReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.GestionarConfigured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "Faltan las credenciales de prueba de Gestionar."})
			return
		}
		times, err := client.PickupTimes(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": "No se pudo consultar los horarios de Gestionar. Reintentá más tarde."})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": times})
	}
}
