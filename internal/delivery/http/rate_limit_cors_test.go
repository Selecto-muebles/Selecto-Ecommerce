package http

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

func TestRateLimitedResponsesAndPreflightsPreserveCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := SetupRouter(&database.DB{}, &config.Config{RateLimitPerMinute: 1, CORSAllowedOrigins: []string{"https://admin.example"}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "192.0.2.1:1234"
		req.Header.Set("Origin", "https://admin.example")
		if method == "OPTIONS" {
			req.Header.Set("Access-Control-Request-Method", "POST")
			req.Header.Set("Access-Control-Request-Headers", "content-type,authorization")
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		return res
	}
	for range 5 {
		if got := request("OPTIONS", "/register").Code; got != 204 {
			t.Fatalf("preflight used operation quota: %d", got)
		}
	}
	if got := request("GET", "/unknown").Code; got != 404 {
		t.Fatalf("preflight depleted operation quota: %d", got)
	}
	res := request("POST", "/register")
	if res.Code != 429 || res.Header().Get("Access-Control-Allow-Origin") != "https://admin.example" {
		t.Fatalf("rate limit became a CORS network error: %d, %v", res.Code, res.Header())
	}
	if !strings.Contains(res.Header().Get("Access-Control-Expose-Headers"), "Retry-After") {
		t.Fatal("browser cannot read Retry-After")
	}
	if got := request("OPTIONS", "/register").Code; got != 204 {
		t.Fatalf("exhausted quota blocked CORS: %d", got)
	}
}
