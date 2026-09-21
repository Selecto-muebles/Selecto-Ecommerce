package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func TestAdminPasswordRecoveryUsesAdminSurfaceAndRevokesSessions(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("admin-password-reset-%d@selecto.test", time.Now().UnixNano())
	var userID int
	if err := pool.QueryRow(ctx, "INSERT INTO users (email,password,role,email_verified_at) VALUES ($1,'old-hash','admin',NOW()) RETURNING id", email).Scan(&userID); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_outbox WHERE recipient=$1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM account_tokens WHERE user_id=$1", userID)
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='user' AND entity_id=$1", userID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
	})

	cfg := &config.Config{AdminURL: "https://admin.selecto.test"}
	forgot := httptest.NewRecorder()
	forgotContext, _ := gin.CreateTestContext(forgot)
	forgotContext.Request = httptest.NewRequest(http.MethodPost, "/admin/auth/password/forgot", bytes.NewBufferString(fmt.Sprintf(`{"email":%q}`, email)))
	forgotContext.Request.Header.Set("Content-Type", "application/json")
	AdminForgotPasswordHandler(&database.DB{Pool: pool}, cfg)(forgotContext)
	if forgot.Code != http.StatusAccepted {
		t.Fatalf("forgot status = %d body=%s", forgot.Code, forgot.Body.String())
	}

	var payload []byte
	if err := pool.QueryRow(ctx, "SELECT payload FROM email_outbox WHERE recipient=$1 AND template='password_reset'", email).Scan(&payload); err != nil {
		t.Fatalf("read reset outbox: %v", err)
	}
	var message map[string]string
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode reset payload: %v", err)
	}
	resetURL, err := url.Parse(message["url"])
	if err != nil || resetURL.Scheme != "https" || resetURL.Host != "admin.selecto.test" || resetURL.Path != "/restablecer-contrasena" {
		t.Fatalf("unexpected admin reset URL %q: %v", message["url"], err)
	}
	token := resetURL.Query().Get("token")

	customerReset := httptest.NewRecorder()
	customerContext, _ := gin.CreateTestContext(customerReset)
	customerContext.Request = resetRequest("/auth/password/reset", token, "NewPassword123")
	ResetPasswordHandler(&database.DB{Pool: pool})(customerContext)
	if customerReset.Code != http.StatusBadRequest {
		t.Fatalf("customer reset accepted admin token: %d body=%s", customerReset.Code, customerReset.Body.String())
	}

	adminReset := httptest.NewRecorder()
	adminContext, _ := gin.CreateTestContext(adminReset)
	adminContext.Request = resetRequest("/admin/auth/password/reset", token, "NewPassword123")
	AdminResetPasswordHandler(&database.DB{Pool: pool})(adminContext)
	if adminReset.Code != http.StatusOK {
		t.Fatalf("admin reset status = %d body=%s", adminReset.Code, adminReset.Body.String())
	}

	var password string
	var sessionVersion int64
	if err := pool.QueryRow(ctx, "SELECT password, session_version FROM users WHERE id=$1", userID).Scan(&password, &sessionVersion); err != nil {
		t.Fatalf("read updated admin: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(password), []byte("NewPassword123")) != nil || sessionVersion != 1 {
		t.Fatalf("admin password/session version was not updated: %d", sessionVersion)
	}
	var audits int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE action='admin_password_reset' AND entity_type='user' AND entity_id=$1", userID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("admin reset audits = %d, err=%v", audits, err)
	}
}

func resetRequest(path, token, password string) *http.Request {
	body := fmt.Sprintf(`{"token":%q,"password":%q}`, token, password)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
