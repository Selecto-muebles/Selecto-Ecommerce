package serviceidentity

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestAuthenticatedTransportAddsAndCachesIdentityToken(t *testing.T) {
	fetches := 0
	token := testToken(time.Now().Add(time.Hour))
	transport := &authenticatedTransport{
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if got := request.Header.Get("Authorization"); got != "Bearer "+token {
				t.Fatalf("unexpected authorization header %q", got)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
		}),
		audience: "https://service.run.app",
		fetchToken: func(context.Context, string) (string, time.Time, error) {
			fetches++
			return token, time.Now().Add(time.Hour), nil
		},
	}
	for range 2 {
		request, err := http.NewRequest(http.MethodGet, "https://service.run.app/ready", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transport.RoundTrip(request); err != nil {
			t.Fatal(err)
		}
	}
	if fetches != 1 {
		t.Fatalf("expected one token fetch, got %d", fetches)
	}
}

func TestTokenExpirationRejectsMalformedToken(t *testing.T) {
	if _, err := tokenExpiration("invalid"); err == nil {
		t.Fatal("expected malformed token to be rejected")
	}
}

func testToken(expiresAt time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, expiresAt.Unix())))
	return "header." + payload + ".signature"
}
