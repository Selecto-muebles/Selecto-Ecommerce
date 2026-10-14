package gestionar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickupTimesUsesServerSideTokenAndReturnsOnlySlots(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != pickupTimesPath || r.Header.Get("secret-token-key") != "sandbox-test-token-123" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"Success","success":true,"data":[{"id":7,"time":"14:00 a 18:00","secret_token":"must-not-leak"}]}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "sandbox-test-token-123", server.Client())
	slots, err := client.PickupTimes(context.Background())
	if err != nil || len(slots) != 1 || slots[0].ID != 7 || slots[0].Time != "14:00 a 18:00" {
		t.Fatalf("pickup times = %+v, err = %v", slots, err)
	}
}

func TestPickupTimesRejectsUnsuccessfulAndOversizedResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"http error", http.StatusUnauthorized, `{"status":"Error","message":"secret-token-key invalid"}`},
		{"business error", http.StatusOK, `{"status":"Error","success":false,"data":[]}`},
		{"invalid slot", http.StatusOK, `{"status":"Success","success":true,"data":[{"id":0,"time":""}]}`},
		{"oversized", http.StatusOK, strings.Repeat("x", maxResponseBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, err := NewClient(server.URL, "sandbox-test-token-123", server.Client()).PickupTimes(context.Background())
			if err == nil || strings.Contains(err.Error(), "secret-token-key invalid") {
				t.Fatalf("expected sanitized error, got %v", err)
			}
		})
	}
}

func TestPickupTimesDoesNotFollowRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://example.invalid/steal")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	_, err := NewClient(server.URL, "sandbox-test-token-123", server.Client()).PickupTimes(context.Background())
	if err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}
