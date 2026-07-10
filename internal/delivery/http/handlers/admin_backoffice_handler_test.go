package handlers

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func TestAdminSortsAreWhitelisted(t *testing.T) {
	if got := productSort("drop table products"); got != "created_at DESC, id DESC" {
		t.Fatalf("productSort unsafe value = %q", got)
	}
	if got := orderSort("drop table orders"); got != "o.created_at DESC, o.id DESC" {
		t.Fatalf("orderSort unsafe value = %q", got)
	}
	if got := customerSort("drop table users"); got != "u.id DESC" {
		t.Fatalf("customerSort unsafe value = %q", got)
	}
}

func TestAdminStockAdjustmentRequiresReasonBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/admin/products/:id/stock-adjustments", AdminAdjustProductStockHandler(nil, slog.Default()))

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/products/"+utils.EncodeID(1)+"/stock-adjustments", bytes.NewBufferString(`{"delta":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAdminPaymentsProxyRequiresConfiguredPaymentsService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/admin/payments", AdminPaymentsProxyHandler(&config.Config{}, "/internal/admin/payments"))

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/payments", nil)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}
