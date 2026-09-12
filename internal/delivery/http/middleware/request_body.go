package middleware

import (
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func RequestBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := maxBytes
		if (c.Request.Method == http.MethodPost && c.FullPath() == "/admin/carousel-slides") ||
			(c.Request.Method == http.MethodPatch && c.FullPath() == "/admin/carousel-slides/:id") {
			limit = (2 << 20) + (64 << 10)
		}
		if c.Request.Method == http.MethodPost && strings.HasSuffix(c.FullPath(), "/images") {
			limit = 6 << 20
		}
		if c.Request.ContentLength > limit {
			apperrors.JSON(c, http.StatusRequestEntityTooLarge, apperrors.CodeInvalidInput, "request body too large", nil)
			c.Abort()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
