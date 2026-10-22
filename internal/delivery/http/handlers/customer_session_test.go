package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

func TestRefreshCookieIsHttpOnlyAndHostBound(t *testing.T) {
	cfg := &config.Config{AppEnv: "production"}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	setRefreshCookie(c, cfg, "test-token")
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-selecto_refresh" || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != int(customerRefreshTTL.Seconds()) {
		t.Fatalf("unsafe refresh cookie: %+v", cookie)
	}
	if refreshTokenHash("one") == refreshTokenHash("two") {
		t.Fatal("distinct tokens have the same hash")
	}
}

func TestCustomerSessionOriginIsRestricted(t *testing.T) {
	cfg := &config.Config{AppEnv: "production", StorefrontURL: "https://selectosport.com"}
	for _, test := range []struct {
		origin  string
		allowed bool
	}{
		{"https://selectosport.com", true},
		{"https://evil.example", false},
		{"", false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/auth/session/refresh", nil)
		c.Request.Header.Set("Origin", test.origin)
		if got := validCustomerSessionOrigin(c, cfg); got != test.allowed {
			t.Fatalf("origin %q allowed = %t, want %t", test.origin, got, test.allowed)
		}
	}
}

func TestCustomerRefreshRotatesAndLogoutRevokes(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("refresh-%d@selecto.test", time.Now().UnixNano())
	var userID int
	if err := pool.QueryRow(ctx, "INSERT INTO users (email,password,role,email_verified_at) VALUES ($1,'unused','user',NOW()) RETURNING id", email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID) })
	cfg := &config.Config{AppEnv: "production", StorefrontURL: "https://selectosport.com", JWTSecret: "test-session-secret-at-least-32-characters", JWTTTL: time.Hour}
	db := &database.DB{Pool: pool}

	request := func(path string, cookie *http.Cookie, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		c.Request.Header.Set("Origin", cfg.StorefrontURL)
		if cookie != nil {
			c.Request.AddCookie(cookie)
		}
		handler(c)
		return recorder
	}

	login := request("/login", nil, func(c *gin.Context) {
		if err := issueCustomerRefreshSession(c, db, cfg, userID, 0); err != nil {
			t.Fatal(err)
		}
	})
	first := login.Result().Cookies()[0]
	refresh := request("/auth/session/refresh", first, RefreshCustomerSessionHandler(db, cfg))
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh = %d: %s", refresh.Code, refresh.Body.String())
	}
	second := refresh.Result().Cookies()[0]
	if first.Value == second.Value {
		t.Fatal("refresh did not rotate the cookie")
	}
	if replay := request("/auth/session/refresh", first, RefreshCustomerSessionHandler(db, cfg)); replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay = %d, want 401", replay.Code)
	}
	if logout := request("/auth/session/logout", second, LogoutCustomerSessionHandler(db, cfg)); logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", logout.Code)
	}
	if replay := request("/auth/session/refresh", second, RefreshCustomerSessionHandler(db, cfg)); replay.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session = %d, want 401", replay.Code)
	}
	newLogin := request("/login", nil, func(c *gin.Context) {
		if err := issueCustomerRefreshSession(c, db, cfg, userID, 0); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := pool.Exec(ctx, "UPDATE users SET session_version=1 WHERE id=$1", userID); err != nil {
		t.Fatal(err)
	}
	if replay := request("/auth/session/refresh", newLogin.Result().Cookies()[0], RefreshCustomerSessionHandler(db, cfg)); replay.Code != http.StatusUnauthorized {
		t.Fatalf("session after revocation = %d, want 401", replay.Code)
	}
}
