package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRateLimitKeepsHealthAndReadinessAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RateLimit(1))
	router.GET("/products", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/ready", func(c *gin.Context) { c.Status(http.StatusOK) })

	assertStatus := func(path string, want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.RemoteAddr = "192.0.2.10:1234"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, want)
		}
	}

	assertStatus("/products", http.StatusOK)
	assertStatus("/products", http.StatusTooManyRequests)
	assertStatus("/health", http.StatusOK)
	assertStatus("/ready", http.StatusOK)
}
