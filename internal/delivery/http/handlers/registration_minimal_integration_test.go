package handlers

import (
	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMinimalRegistrationPersistsNoInventedProfileAndQueuesVerification(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	cfg := &config.Config{StorefrontURL: "http://localhost:18080"}
	for index := 0; index < 2; index++ {
		email := fmt.Sprintf("minimal-%d-%d@test.invalid", suffix, index)
		t.Cleanup(func() {
			pool.Exec(ctx, "DELETE FROM email_outbox WHERE recipient=$1", email)
			pool.Exec(ctx, "DELETE FROM users WHERE email=$1", email)
		})
		raw, _ := json.Marshal(map[string]string{"email": email, "password": "local-test-password"})
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/register", bytes.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		RegisterHandler(&database.DB{Pool: pool}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))(c)
		if rec.Code != 200 {
			t.Fatalf("minimal registration %d %s", rec.Code, rec.Body.String())
		}
		var dni, address, role string
		var verified bool
		if err := pool.QueryRow(ctx, "SELECT COALESCE(dni,''),COALESCE(street_address,''),role,email_verified_at IS NOT NULL FROM users WHERE email=$1", email).Scan(&dni, &address, &role, &verified); err != nil {
			t.Fatal(err)
		}
		if dni != "" || address != "" || role != "user" || verified {
			t.Fatal("registration invented profile/privileges/verification")
		}
		var outbox int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM email_outbox WHERE recipient=$1 AND template='verify_email'", email).Scan(&outbox); err != nil || outbox != 1 {
			t.Fatal("verification not queued atomically", err)
		}
	}
}

func TestGoogleRegistrationAcceptsVerifiedIdentityWithoutCommercialFields(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("google-minimal-%d@test.invalid", time.Now().UnixNano())
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM audit_logs WHERE actor_email=$1", email)
		pool.Exec(ctx, "DELETE FROM users WHERE email=$1", email)
	})
	cfg := &config.Config{JWTSecret: "isolated-registration-secret-32-characters", JWTTTL: time.Hour}
	token, err := generateGoogleRegistrationToken(&GoogleIdentity{Email: email, Subject: email, FirstName: "Ana", LastName: "Pérez"}, cfg.JWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"registration_token": token})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/auth/google/register", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	GoogleRegisterHandler(&database.DB{Pool: pool}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))(c)
	if rec.Code != 200 {
		t.Fatalf("google registration status %d %s", rec.Code, rec.Body.String())
	}
	var name, dni string
	var verified bool
	if err := pool.QueryRow(ctx, "SELECT first_name,COALESCE(dni,''),email_verified_at IS NOT NULL FROM users WHERE email=$1", email).Scan(&name, &dni, &verified); err != nil {
		t.Fatal(err)
	}
	if name != "Ana" || dni != "" || !verified {
		t.Fatal("Google identity or minimal profile not preserved")
	}
}
