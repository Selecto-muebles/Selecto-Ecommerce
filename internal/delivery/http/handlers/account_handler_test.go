package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

func TestValidEmail(t *testing.T) {
	for _, value := range []string{"customer@example.com", "name.surname+orders@example.com"} {
		if !validEmail(value) {
			t.Fatalf("valid email %q was rejected", value)
		}
	}
	for _, value := range []string{"invalid", "Name <customer@example.com>", "customer@", " customer@example.com "} {
		if validEmail(value) {
			t.Fatalf("invalid email %q was accepted", value)
		}
	}
}

func TestAccountTokensAreRandomAndHashed(t *testing.T) {
	first, firstHash, err := newAccountToken()
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := newAccountToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == firstHash || first == second || firstHash == secondHash {
		t.Fatal("account tokens are not independently hashed random values")
	}
	if hashAccountToken(first) != firstHash {
		t.Fatal("token hash is not deterministic")
	}
}

func TestResetPasswordConsumesTokenAndRevokesSessions(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("password-reset-%d@selecto.test", time.Now().UnixNano())
	var userID int
	if err := pool.QueryRow(ctx, "INSERT INTO users (email,password,role,email_verified_at) VALUES ($1,'old-hash','user',NOW()) RETURNING id", email).Scan(&userID); err != nil {
		t.Fatalf("seed reset user: %v", err)
	}
	rawToken := "single-use-reset-token"
	if _, err := pool.Exec(ctx, "INSERT INTO account_tokens (user_id,purpose,token_hash,expires_at) VALUES ($1,'password_reset',$2,NOW()+INTERVAL '30 minutes')", userID, hashAccountToken(rawToken)); err != nil {
		t.Fatalf("seed reset token: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM account_tokens WHERE user_id=$1", userID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/password/reset", bytes.NewBufferString(`{"token":"single-use-reset-token","password":"NewPassword123"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	ResetPasswordHandler(&database.DB{Pool: pool})(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reset status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var sessionVersion int64
	var consumed bool
	if err := pool.QueryRow(ctx, "SELECT session_version FROM users WHERE id=$1", userID).Scan(&sessionVersion); err != nil {
		t.Fatalf("read session version: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM account_tokens WHERE user_id=$1 AND purpose='password_reset'", userID).Scan(&consumed); err != nil {
		t.Fatalf("read reset token: %v", err)
	}
	if sessionVersion != 1 || !consumed {
		t.Fatalf("session version/consumed = %d/%t, want 1/true", sessionVersion, consumed)
	}
	replay := httptest.NewRecorder()
	replayContext, _ := gin.CreateTestContext(replay)
	replayContext.Request = httptest.NewRequest(http.MethodPost, "/auth/password/reset", bytes.NewBufferString(`{"token":"single-use-reset-token","password":"AnotherPassword123"}`))
	replayContext.Request.Header.Set("Content-Type", "application/json")
	ResetPasswordHandler(&database.DB{Pool: pool})(replayContext)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replayed reset status = %d body=%s", replay.Code, replay.Body.String())
	}
}

func TestLocalRegistrationRequiresEmailVerificationBeforeLogin(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("verify-registration-%d@selecto.test", time.Now().UnixNano())
	dni := fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_outbox WHERE recipient=$1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email=$1", email)
	})
	cfg := &config.Config{StorefrontURL: "http://localhost:5173", JWTSecret: "test-secret-with-at-least-32-characters", JWTTTL: time.Hour}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	registerBody := fmt.Sprintf(`{"email":%q,"password":"Password123","first_name":"Ada","last_name":"Lovelace","dni":%q,"street_address":"Calle","street_number":"123","postal_code":"1000","province":"Buenos Aires","locality":"CABA","phone_number":"1112345678"}`, email, dni)
	register := httptest.NewRecorder()
	registerContext, _ := gin.CreateTestContext(register)
	registerContext.Request = httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(registerBody))
	registerContext.Request.Header.Set("Content-Type", "application/json")
	RegisterHandler(&database.DB{Pool: pool}, cfg, logger)(registerContext)
	if register.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}

	loginRequest := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(fmt.Sprintf(`{"email":%q,"password":"Password123"}`, email)))
		c.Request.Header.Set("Content-Type", "application/json")
		LoginHandler(&database.DB{Pool: pool}, cfg, logger)(c)
		return recorder
	}
	if login := loginRequest(); login.Code != http.StatusForbidden {
		t.Fatalf("unverified login status = %d body=%s", login.Code, login.Body.String())
	}

	var payload []byte
	if err := pool.QueryRow(ctx, "SELECT payload FROM email_outbox WHERE recipient=$1 AND template='verify_email'", email).Scan(&payload); err != nil {
		t.Fatalf("read verification outbox: %v", err)
	}
	var message map[string]string
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode verification payload: %v", err)
	}
	verificationURL, err := url.Parse(message["url"])
	if err != nil {
		t.Fatalf("parse verification URL: %v", err)
	}
	verify := httptest.NewRecorder()
	verifyContext, _ := gin.CreateTestContext(verify)
	verifyContext.Request = httptest.NewRequest(http.MethodPost, "/auth/email/verify", bytes.NewBufferString(fmt.Sprintf(`{"token":%q}`, verificationURL.Query().Get("token"))))
	verifyContext.Request.Header.Set("Content-Type", "application/json")
	VerifyEmailHandler(&database.DB{Pool: pool})(verifyContext)
	if verify.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", verify.Code, verify.Body.String())
	}
	if login := loginRequest(); login.Code != http.StatusOK {
		t.Fatalf("verified login status = %d body=%s", login.Code, login.Body.String())
	}
}
