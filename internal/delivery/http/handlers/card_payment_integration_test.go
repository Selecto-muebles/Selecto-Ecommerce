package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func TestCardPaymentRequiresOwnedPendingOrderAndUsesStoredEmail(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 5, 1)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })
	var email string
	if err := pool.QueryRow(ctx, "SELECT email FROM users WHERE id=$1", userID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET active_payment_preference_id='pref-test',
		active_checkout_url='https://www.mercadopago.com.ar/checkout/v1/redirect',
		active_payment_environment='test',expires_at=NOW()+INTERVAL '15 minutes' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input["payer_email"] != email || input["preference_id"] != "pref-test" || input["token"] != "test-card-token" || input["transaction_amount"] != nil {
			t.Errorf("unsafe payment request: %+v", input)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"payment_id":42,"status":"pending","verification_pending":true}`))
	}))
	defer server.Close()
	handler := CardPaymentHandler(&database.DB{Pool: pool}, &config.Config{
		OrderPendingTTL: 15 * time.Minute, PaymentsServiceURL: server.URL,
		PaymentsRequestTimeout: 5 * time.Second, InternalWebhookSecret: "test-internal-secret-with-at-least-32-characters",
	})
	call := func(owner string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		body, _ := json.Marshal(gin.H{"order_id": utils.EncodeID(orderID), "preference_id": "pref-test", "token": "test-card-token", "payment_method_id": "visa", "installments": 1})
		c.Request = httptest.NewRequest(http.MethodPost, "/checkout/card-payment", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Idempotency-Key", "a17600a9-8051-4e4f-9074-1d8826117a99")
		c.Set("email", owner)
		handler(c)
		return recorder
	}
	if result := call(email); result.Code != http.StatusAccepted {
		t.Fatalf("owner status=%d body=%s", result.Code, result.Body.String())
	}
	if result := call("other@example.com"); result.Code != http.StatusNotFound {
		t.Fatalf("foreign order status=%d body=%s", result.Code, result.Body.String())
	}
	if calls != 1 {
		t.Fatalf("provider calls=%d, want 1", calls)
	}
}
