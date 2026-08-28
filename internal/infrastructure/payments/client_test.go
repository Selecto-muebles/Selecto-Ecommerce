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
	result, err := client.CreatePreference(t.Context(), 7, money.Cents(125000), "req-1", "corr-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "pref-1" || !strings.HasPrefix(result.CheckoutURL, "https://") {
		t.Fatalf("unexpected result: %+v", result)
	}
}
