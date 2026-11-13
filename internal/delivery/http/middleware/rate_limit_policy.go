package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func rateLimitPolicy(c *gin.Context, limit int) (string, int) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		switch c.FullPath() {
		case "/seo/head", "/sitemap.xml":
			return "metadata", limit
		case "/product-images/:id", "/carousel-images/:id":
			// Responsive photos and their thumbnails are distinct HTTP requests.
			return "media", limit * 5
		case "/products", "/categories", "/carousel-slides", "/products/:id/reviews":
			return "catalog", limit
		}
	}
	if c.Request.Method == http.MethodPost {
		switch c.FullPath() {
		case "/payments/webhook", "/communications/brevo/webhook":
			return "webhooks", limit
		}
	}
	return "operations", limit
}
