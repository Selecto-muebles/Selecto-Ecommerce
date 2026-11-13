package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const testEdgeSecret = "isolated-load-balancer-token-32-characters"

func rateLimitRouter(limiter gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(RateLimitClient(testEdgeSecret), limiter)
	for _, path := range []string{"/products", "/categories", "/carousel-slides", "/product-images/:id", "/carousel-images/:id", "/products/:id/reviews", "/seo/head", "/sitemap.xml"} {
		r.GET(path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}
	for _, path := range []string{"/login", "/register", "/checkout", "/orders", "/payments/webhook", "/communications/brevo/webhook"} {
		r.POST(path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}
	return r
}

func limitedRequest(r http.Handler, method, path, client, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "169.254.169.126:1234"
	req.Header.Set("X-Selecto-Edge-Token", token)
	req.Header.Set("X-Selecto-Client-IP", client)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}

func TestRateLimitIsolatesBotsPhotosCatalogAndOperations(t *testing.T) {
	r := rateLimitRouter(RateLimit(2))
	request := func(method, path string, want int) {
		t.Helper()
		res := limitedRequest(r, method, path, "192.0.2.1", testEdgeSecret)
		if res.Code != want {
			t.Fatalf("%s %s = %d, want %d", method, path, res.Code, want)
		}
	}
	for i := range 10 {
		want := http.StatusTooManyRequests
		if i < 2 {
			want = http.StatusOK
		}
		request("GET", "/seo/head?path=/", want)
	}
	for i := range 10 {
		request("GET", fmt.Sprintf("/product-images/photo-%d?w=240", i), 200)
	}
	request("GET", "/carousel-images/1", 429)
	request("GET", "/categories", 200)
	request("GET", "/products", 200)
	request("GET", "/products/demo/reviews", 429)
	request("POST", "/orders", 200)
	request("POST", "/checkout", 200)
	request("POST", "/login", 429)
	request("POST", "/payments/webhook", 200)
	request("POST", "/communications/brevo/webhook", 200)
	request("POST", "/payments/webhook", 429)
}

func TestRateLimitConcurrentVisitorsBehindOneProxy(t *testing.T) {
	r := rateLimitRouter(RateLimit(120))
	var wg sync.WaitGroup
	for visitor := range 30 {
		wg.Go(func() {
			for step := range 30 {
				path := []string{"/products", "/categories", "/carousel-slides", "/products/demo/reviews", "/product-images/demo?w=240", "/product-images/demo?w=1280"}[step%6]
				res := limitedRequest(r, "GET", path, fmt.Sprintf("192.0.2.%d", visitor+1), testEdgeSecret)
				if res.Code != http.StatusOK {
					t.Errorf("visitor %d navigation failed: %s = %d", visitor, path, res.Code)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestRateLimitRetryAfterRoundsUpAndRecovers(t *testing.T) {
	now := time.Unix(1700000000, 0)
	r := rateLimitRouter(rateLimitWithClock(1, func() time.Time { return now }))
	if got := limitedRequest(r, "GET", "/products", "192.0.2.1", testEdgeSecret).Code; got != 200 {
		t.Fatal(got)
	}
	now = now.Add(1500 * time.Millisecond)
	res := limitedRequest(r, "GET", "/categories", "192.0.2.1", testEdgeSecret)
	if res.Code != 429 || res.Header().Get("Retry-After") != "59" || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid retry response: %d, %v", res.Code, res.Header())
	}
	now = now.Add(59 * time.Second)
	if got := limitedRequest(r, "GET", "/products", "192.0.2.1", testEdgeSecret).Code; got != 200 {
		t.Fatalf("quota did not recover after Retry-After: %d", got)
	}
}
