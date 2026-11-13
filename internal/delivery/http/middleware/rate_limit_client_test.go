package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRateLimitRejectsForgedAndMalformedClientIdentity(t *testing.T) {
	for _, token := range []string{"", "forged", testEdgeSecret} {
		t.Run(token, func(t *testing.T) {
			r := rateLimitRouter(RateLimit(1))
			for i, client := range []string{"192.0.2.1", "192.0.2.2"} {
				// With the correct token, malformed addresses must still use the
				// peer quota. IP lists and IPv6 zone IDs are never accepted.
				if token == testEdgeSecret {
					client = []string{"192.0.2.1, 192.0.2.2", "fe80::1%eth0"}[i]
				}
				want := 200
				if i > 0 {
					want = 429
				}
				if got := limitedRequest(r, "GET", "/products", client, token).Code; got != want {
					t.Fatalf("unverified identity changed quota: %d, want %d", got, want)
				}
			}
		})
	}
}

func TestRateLimitNormalizesIPv6AndIgnoresForwardedHeaders(t *testing.T) {
	r := rateLimitRouter(RateLimit(1))
	if got := limitedRequest(r, "GET", "/products", "::ffff:192.0.2.1", testEdgeSecret).Code; got != 200 {
		t.Fatal(got)
	}
	if got := limitedRequest(r, "GET", "/products", "192.0.2.1", testEdgeSecret).Code; got != 429 {
		t.Fatalf("mapped IPv4 escaped quota: %d", got)
	}
	if got := limitedRequest(r, "GET", "/products", "2001:db8::1", testEdgeSecret).Code; got != 200 {
		t.Fatal(got)
	}
	if got := limitedRequest(r, "GET", "/products", "2001:db8:0:0:0:0:0:1", testEdgeSecret).Code; got != 429 {
		t.Fatalf("alternate IPv6 spelling escaped quota: %d", got)
	}
	for i := range 2 {
		req := httptest.NewRequest("GET", "/products", nil)
		req.RemoteAddr = "169.254.169.126:1234"
		req.Header.Set("X-Forwarded-For", []string{"192.0.2.3", "192.0.2.4"}[i])
		req.Header.Set("X-Real-IP", req.Header.Get("X-Forwarded-For"))
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if (i == 0 && res.Code != 200) || (i == 1 && res.Code != 429) {
			t.Fatalf("untrusted proxy headers changed quota: %d", res.Code)
		}
	}
}

func TestRateLimitClientDisabledByDefaultAndStripsPrivateHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimitClient(""))
	r.GET("/", func(c *gin.Context) {
		if _, ok := c.Get(rateLimitClientKey); ok || c.GetHeader("X-Selecto-Edge-Token") != "" || c.GetHeader("X-Selecto-Client-IP") != "" {
			t.Error("disabled identity accepted or leaked private headers")
		}
		c.Status(http.StatusOK)
	})
	limitedRequest(r, "GET", "/", "192.0.2.1", testEdgeSecret)
}
