package handlers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type googleLinkFixture struct {
	pool  *pgxpool.Pool
	id    int
	email string
}

type googleLinkVerifier struct{ identity GoogleIdentity }

func (v googleLinkVerifier) Verify(context.Context, string, string) (*GoogleIdentity, error) {
	return &v.identity, nil
}

func newGoogleLinkFixture(t *testing.T) googleLinkFixture {
	t.Helper()
	f := googleLinkFixture{pool: integrationPool(t), email: fmt.Sprintf("google-link-%d@selecto.test", time.Now().UnixNano())}
	if err := f.pool.QueryRow(context.Background(), "INSERT INTO users(email,password,role) VALUES($1,'unused-test-hash','user') RETURNING id", f.email).Scan(&f.id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE entity_type='user' AND entity_id=$1", f.id)
		_, _ = f.pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", f.id)
	})
	return f
}

func (f googleLinkFixture) perform(email, role, subject string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth/google/link", strings.NewReader(`{"credential":"verified-by-test-double"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("email", email)
	c.Set("role", role)
	verifier := googleLinkVerifier{GoogleIdentity{Email: f.email, Subject: subject, EmailValid: true}}
	GoogleLinkHandler(&database.DB{Pool: f.pool}, &config.Config{GoogleClientID: "test-client"}, slog.New(slog.NewTextHandler(io.Discard, nil)), verifier)(c)
	return r
}

func (f googleLinkFixture) assertState(t *testing.T, identities, audits int, verified bool) {
	t.Helper()
	var actualIdentities, actualAudits int
	var actualVerified bool
	err := f.pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM user_identities WHERE user_id=$1),
		(SELECT count(*) FROM audit_logs WHERE entity_type='user' AND entity_id=$1 AND action='google_account_linked'),
		email_verified_at IS NOT NULL FROM users WHERE id=$1`, f.id).Scan(&actualIdentities, &actualAudits, &actualVerified)
	if err != nil {
		t.Fatal(err)
	}
	if actualIdentities != identities || actualAudits != audits || actualVerified != verified {
		t.Fatalf("state identities/audits/verified=%d/%d/%t, want %d/%d/%t", actualIdentities, actualAudits, actualVerified, identities, audits, verified)
	}
}

// These fixture-scoped triggers work with the schema-owning CI role; they do
// not require CREATE DATABASE, superuser privileges or changing another row.
func (f googleLinkFixture) injectFailure(t *testing.T, stage string) func() {
	t.Helper()
	name := pgx.Identifier{fmt.Sprintf("fail_google_link_%d", f.id)}.Sanitize()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, "CREATE FUNCTION "+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected Google link failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	table, trigger := "users", fmt.Sprintf("CREATE TRIGGER %s BEFORE UPDATE ON users FOR EACH ROW WHEN (NEW.id=%d) EXECUTE FUNCTION %s()", name, f.id, name)
	if stage == "audit" {
		table = "audit_logs"
		trigger = fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON audit_logs FOR EACH ROW WHEN (NEW.entity_id=%d AND NEW.entity_type='user') EXECUTE FUNCTION %s()", name, f.id, name)
	}
	if stage == "commit" {
		table = "user_identities"
		trigger = fmt.Sprintf("CREATE CONSTRAINT TRIGGER %s AFTER INSERT ON user_identities DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.user_id=%d) EXECUTE FUNCTION %s()", name, f.id, name)
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			if _, err := f.pool.Exec(ctx, "DROP TRIGGER IF EXISTS "+name+" ON "+table); err != nil {
				t.Error(err)
			}
			if _, err := f.pool.Exec(ctx, "DROP FUNCTION "+name+"()"); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(cleanup)
	if _, err := f.pool.Exec(ctx, trigger); err != nil {
		t.Fatal(err)
	}
	return cleanup
}
