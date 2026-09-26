package brevo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestContactClientUsesListMembershipWithoutBlockingOrderEmails(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var payloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "test-key" {
			t.Error("API key missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		methods = append(methods, r.Method)
		payloads = append(payloads, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewClient(ClientConfig{APIKey: "test-key", ListID: 12, BaseURL: server.URL})
	for _, subscribed := range []bool{true, false} {
		if err := client.SetSubscription(context.Background(), "member@selecto.test", subscribed); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 2 || methods[0] != "POST" || methods[1] != "PUT" {
		t.Fatalf("methods %v", methods)
	}
	if payloads[0]["updateEnabled"] != true || payloads[1]["unlinkListIds"] == nil {
		t.Fatalf("payloads %v", payloads)
	}
	for _, body := range payloads {
		if _, exists := body["emailBlacklisted"]; exists {
			t.Fatal("newsletter optout blocks transactional mail")
		}
	}
}
func TestClientNeverForwardsAPIKeyToRedirectTarget(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true; w.WriteHeader(204) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer origin.Close()
	err := NewClient(ClientConfig{APIKey: "secret", ListID: 1, BaseURL: origin.URL}).SetSubscription(context.Background(), "email@selecto.test", true)
	if err == nil || redirected {
		t.Fatal("redirect followed or reported success")
	}
}
