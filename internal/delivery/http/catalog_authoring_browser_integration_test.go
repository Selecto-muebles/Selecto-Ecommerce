package http

import (
	"context"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Opt-in browser certification against the real router, authentication and SQL.
// Only a local disposable database is accepted. No request is mocked.
func TestCatalogAuthoringRealBrowser(t *testing.T) {
	runner := os.Getenv("CATALOG_BROWSER_RUNNER")
	databaseURL := os.Getenv("CATALOG_BROWSER_DATABASE_URL")
	if runner == "" || databaseURL == "" {
		t.Skip("set CATALOG_BROWSER_RUNNER and CATALOG_BROWSER_DATABASE_URL for browser certification")
	}
	parsed, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ConnConfig.Database != "selecto_catalog_authoring_test" || (parsed.ConnConfig.Host != "/var/run/postgresql" && parsed.ConnConfig.Host != "127.0.0.1" && parsed.ConnConfig.Host != "localhost") {
		t.Fatal("browser certification requires the isolated local selecto_catalog_authoring_test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = database.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	const email = "catalog-browser@selecto.test"
	const password = "isolated-catalog-browser-only"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO users(email,password,role,email_verified_at) VALUES($1,$2,'admin',NOW()) ON CONFLICT(email) DO UPDATE SET password=EXCLUDED.password,role='admin',email_verified_at=NOW()", email, string(hash)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO categories(name,slug,active) VALUES('Calistenia','calistenia',TRUE),('Entrenamiento','entrenamiento',TRUE) ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	origin := os.Getenv("CATALOG_BROWSER_UI_URL")
	if origin == "" {
		origin = "http://127.0.0.1:5186"
	}
	gin.SetMode(gin.ReleaseMode)
	cfg := &config.Config{AppEnv: "development", JWTSecret: "isolated-catalog-browser-secret-32-characters", JWTTTL: time.Hour, CORSAllowedOrigins: []string{origin}, RateLimitPerMinute: 10000, OrderPendingTTL: 15 * time.Minute, PaymentsRequestTimeout: time.Second, PaymentsServiceURL: "http://127.0.0.1:1", StorefrontURL: origin, AdminURL: origin}
	router := SetupRouter(&database.DB{Pool: pool}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	listener, err := net.Listen("tcp", "127.0.0.1:18086")
	if err != nil {
		t.Fatal(err)
	}
	server := &stdhttp.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	command := exec.Command("node", runner)
	command.Env = append(os.Environ(), "CATALOG_BROWSER_API_URL=http://127.0.0.1:18086", "CATALOG_BROWSER_UI_URL="+origin, "CATALOG_BROWSER_EMAIL="+email, "CATALOG_BROWSER_PASSWORD="+password)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}
