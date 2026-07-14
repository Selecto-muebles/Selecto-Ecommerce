package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"Selecto-Ecommerce/internal/delivery/http/middleware"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(10))
	router.POST("/body", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	withinLimit := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader("0123456789"))
	withinResponse := httptest.NewRecorder()
	router.ServeHTTP(withinResponse, withinLimit)
	if withinResponse.Code != http.StatusNoContent {
		t.Fatalf("within limit status = %d, want %d", withinResponse.Code, http.StatusNoContent)
	}

	overLimit := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader("01234567890"))
	overResponse := httptest.NewRecorder()
	router.ServeHTTP(overResponse, overLimit)
	if overResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over limit status = %d, want %d", overResponse.Code, http.StatusRequestEntityTooLarge)
	}
}
