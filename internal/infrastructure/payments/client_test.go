package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	checkoutservice "Selecto-Ecommerce/internal/service/checkout"
	"Selecto-Ecommerce/internal/shared/money"
)

func TestCreatePreferenceSignsInternalRequest(t *testing.T) {
	const secret = "test-internal-secret-with-at-least-32-characters"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/create-preference" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Customer checkoutservice.Customer `json:"customer"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Customer.Email != "cliente@example.com" || payload.Customer.Name != "Cliente Selecto" || payload.Customer.Identification != "30111222" {
			t.Fatalf("customer payload = %+v", payload.Customer)
		}
		timestamp := r.Header.Get("X-Service-Timestamp")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp))
		mac.Write([]byte("."))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := r.Header.Get("X-Service-Signature"); !hmac.Equal([]byte(got), []byte(want)) {
			t.Fatalf("signature = %q", got)
		}
		if r.Header.Get("X-Service-Name") != "selecto-ecommerce" || r.Header.Get("X-Request-ID") != "req-1" || r.Header.Get("X-Correlation-ID") != "corr-1" {
			t.Fatal("missing service tracing headers")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"preference_id":"pref-1","checkout_url":"https://example.test/checkout","environment":"test"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second, secret, "")
	result, err := client.CreatePreference(t.Context(), 7, money.Cents(125000), checkoutservice.Customer{Email: "cliente@example.com", Name: "Cliente Selecto", Identification: "30111222"}, "req-1", "corr-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "pref-1" || !strings.HasPrefix(result.CheckoutURL, "https://") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCreateCardPaymentSignsTokenizedRequest(t *testing.T) {
	const secret = "test-internal-secret-with-at-least-32-characters"
	const key = "a17600a9-8051-4e4f-9074-1d8826117a99"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/create-card-payment" || r.Header.Get("Idempotency-Key") != key {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		var payload CardPaymentInput
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.OrderID != 17 || payload.PayerEmail != "cliente@example.com" || payload.Token != "tokenizado" || strings.Contains(string(raw), "transaction_amount") {
			t.Fatalf("unexpected payment payload: %s", raw)
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(r.Header.Get("X-Service-Timestamp")))
		mac.Write([]byte("."))
		mac.Write(raw)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(r.Header.Get("X-Service-Signature")), []byte(want)) {
			t.Fatal("invalid internal signature")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"payment_id":77,"status":"pending","verification_pending":true}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, time.Second, secret, "")
	result, status, err := client.CreateCardPayment(t.Context(), CardPaymentInput{OrderID: 17, PreferenceID: "pref", Token: "tokenizado", PaymentMethodID: "visa", Installments: 1, PayerEmail: "cliente@example.com", IdempotencyKey: key}, "req", "corr")
	if err != nil || status != http.StatusAccepted || result.PaymentID != 77 || !result.VerificationPending {
		t.Fatalf("result=%+v status=%d err=%v", result, status, err)
	}
}
