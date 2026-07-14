package handlers

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"Selecto-Ecommerce/internal/config"

	"github.com/gin-gonic/gin"
)

func TestRegisterRejectsPasswordBeyondBcryptLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/register", RegisterHandler(nil, &config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	body := `{"email":"security-test@selecto.test","password":"` + strings.Repeat("x", maxBcryptPasswordBytes+1) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
