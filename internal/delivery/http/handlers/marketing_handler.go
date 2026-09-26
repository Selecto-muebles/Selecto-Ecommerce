package handlers

import (
	"Selecto-Ecommerce/internal/infrastructure/database"
	"github.com/gin-gonic/gin"
)

type newsletterInput struct {
	Email   string `json:"email"`
	Consent bool   `json:"consent"`
	Source  string `json:"source"`
}

func AdminListMarketingSubscriptionsHandler(db *database.DB) gin.HandlerFunc {
	return AdminOperationsListMarketingSubscriptionsHandler(db)
}

func AdminListEmailOutboxHandler(db *database.DB) gin.HandlerFunc {
	return AdminOperationsListEmailOutboxHandler(db)
}
