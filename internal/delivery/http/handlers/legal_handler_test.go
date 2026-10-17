package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"github.com/gin-gonic/gin"
)

func TestWithdrawalStatusTransitionsAreForwardOnly(t *testing.T) {
	for _, test := range []struct {
		from, to string
		want     bool
	}{
		{"received", "in_review", true}, {"received", "resolved", true},
		{"in_review", "refund_in_progress", true}, {"refund_in_progress", "resolved", true},
		{"resolved", "in_review", false}, {"refund_in_progress", "received", false},
	} {
		if got := allowedWithdrawalTransition(test.from, test.to); got != test.want {
			t.Errorf("allowedWithdrawalTransition(%q,%q)=%v want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestPublicWithdrawalIsUnauthenticatedIdempotentAndAuditable(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	db := &database.DB{Pool: pool}
	termsRecorder := httptest.NewRecorder()
	termsContext, _ := gin.CreateTestContext(termsRecorder)
	termsContext.Request = httptest.NewRequest(http.MethodGet, "/legal/terms", nil)
	PublicTermsHandler(db)(termsContext)
	if termsRecorder.Code != http.StatusOK || !bytes.Contains(termsRecorder.Body.Bytes(), []byte(`"version":"1.0"`)) {
		t.Fatalf("public legal document status=%d body=%s", termsRecorder.Code, termsRecorder.Body.String())
	}
	key := fmt.Sprintf("withdrawal-test-%d", time.Now().UnixNano())
	email := fmt.Sprintf("withdrawal-%d@selecto.test", time.Now().UnixNano())
	var requestID int64
	var outboxID int64
	t.Cleanup(func() {
		if requestID > 0 {
			_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='consumer_withdrawal' AND entity_id=$1", requestID)
			_, _ = pool.Exec(ctx, "DELETE FROM consumer_withdrawal_requests WHERE id=$1", requestID)
		}
		_, _ = pool.Exec(ctx, "DELETE FROM email_outbox WHERE recipient=$1 AND template IN ('withdrawal_ack','withdrawal_update')", email)
	})
	handler := PublicWithdrawalHandler(db, &config.Config{StorefrontURL: "https://selectosport.com"})
	perform := func(reference, reason string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/consumer/withdrawals", bytes.NewBufferString(fmt.Sprintf(`{"order_reference":%q,"email":%q,"reason":%q}`, reference, email, reason)))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Idempotency-Key", key)
		handler(c)
		return recorder
	}
	first := perform("ORD-UNKNOWN", "Me arrepiento")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first request status=%d body=%s", first.Code, first.Body.String())
	}
	var receipt withdrawalResult
	if err := json.Unmarshal(first.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.PublicCode == "" || receipt.Status != "received" {
		t.Fatalf("invalid receipt: %+v", receipt)
	}
	requestID = receipt.ID
	if err := pool.QueryRow(ctx, "SELECT id FROM email_outbox WHERE event_key=$1", "withdrawal-ack:"+receipt.PublicCode).Scan(&outboxID); err != nil {
		t.Fatalf("acknowledgment not in outbox: %v", err)
	}
	second := perform("ORD-UNKNOWN", "Me arrepiento")
	if second.Code != http.StatusOK || !bytes.Contains(second.Body.Bytes(), []byte(receipt.PublicCode)) {
		t.Fatalf("idempotent replay status=%d body=%s", second.Code, second.Body.String())
	}
	conflict := perform("ORD-UNKNOWN", "cambio de datos")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM consumer_withdrawal_requests WHERE idempotency_key=$1", key).Scan(&count); err != nil || count != 1 {
		t.Fatalf("idempotent request count=%d err=%v", count, err)
	}

	adminRecorder := httptest.NewRecorder()
	adminContext, _ := gin.CreateTestContext(adminRecorder)
	adminContext.Params = gin.Params{{Key: "id", Value: fmt.Sprint(receipt.ID)}}
	adminContext.Request = httptest.NewRequest(http.MethodPatch, "/admin/consumer-requests/"+fmt.Sprint(receipt.ID), bytes.NewBufferString(`{"status":"in_review","admin_note":"Contactar"}`))
	adminContext.Request.Header.Set("Content-Type", "application/json")
	adminContext.Set("email", "admin@selecto.test")
	AdminWithdrawalUpdateHandler(db)(adminContext)
	if adminRecorder.Code != http.StatusOK {
		t.Fatalf("admin update status=%d body=%s", adminRecorder.Code, adminRecorder.Body.String())
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_outbox WHERE recipient=$1 AND template='withdrawal_update'", email).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin transition notification count=%d err=%v", count, err)
	}
	var status string
	if err := pool.QueryRow(ctx, "SELECT status FROM consumer_withdrawal_requests WHERE id=$1", receipt.ID).Scan(&status); err != nil || status != "in_review" {
		t.Fatalf("stored status=%q err=%v", status, err)
	}
}
