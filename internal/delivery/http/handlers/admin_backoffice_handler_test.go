package handlers

import (
	"bytes"
	"crypto/hmac"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"Selecto-Ecommerce/internal/config"
	adminservice "Selecto-Ecommerce/internal/service/admin"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func TestAdminSortsAreWhitelisted(t *testing.T) {
	if got := adminservice.ProductSort("drop table products"); got != "created_at DESC, id DESC" {
		t.Fatalf("productSort unsafe value = %q", got)
	}
	if got := adminservice.OrderSort("drop table orders"); got != "o.created_at DESC, o.id DESC" {
		t.Fatalf("orderSort unsafe value = %q", got)
	}
	if got := adminservice.CustomerSort("drop table users"); got != "u.id DESC" {
		t.Fatalf("customerSort unsafe value = %q", got)
	}
}

func TestAdminPaymentsProxyPreservesContractAndSignsRequest(t *testing.T) {
	secret := "rc2-internal-secret"
	payments := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/admin/payments" || r.URL.Query().Get("status") != "approved" || r.URL.Query().Get("page") != "2" {
			t.Errorf("proxied URL = %s, want payments filters preserved", r.URL.String())
		}
		timestamp := r.Header.Get("X-Service-Timestamp")
		signature := r.Header.Get("X-Service-Signature")
		want := "sha256=" + internalAdminSignature(secret, timestamp, nil)
		if timestamp == "" || !hmac.Equal([]byte(signature), []byte(want)) {
			t.Errorf("invalid internal signature timestamp=%q signature=%q", timestamp, signature)
		}
		if r.Header.Get("X-Service-Name") != "selecto-ecommerce" {
			t.Errorf("X-Service-Name = %q", r.Header.Get("X-Service-Name"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[{"payment_id":700001}],"page":2,"page_size":20,"total":1}`))
	}))
	defer payments.Close()

	router := gin.New()
	router.GET("/admin/payments", AdminPaymentsProxyHandler(&config.Config{PaymentsServiceURL: payments.URL, InternalWebhookSecret: secret}, "/internal/admin/payments"))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/payments?status=approved&page=2", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("proxy response status/content-type = %d/%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	if got := recorder.Body.String(); got != `{"items":[{"payment_id":700001}],"page":2,"page_size":20,"total":1}` {
		t.Fatalf("proxy body = %s", got)
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
