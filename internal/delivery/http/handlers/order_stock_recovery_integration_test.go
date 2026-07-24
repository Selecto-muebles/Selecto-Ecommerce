package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaymentWebhookContractIsIdempotent(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 3, 2)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })

	handler := PaymentWebhookHandler(&database.DB{Pool: pool}, &config.Config{StorefrontURL: "http://localhost:5173"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := fmt.Sprintf(`{"payment_id":900001,"order_id":%q,"amount":200,"status":"paid"}`, utils.EncodeID(orderID))
	perform := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		handler(c)
		return recorder
	}

	if recorder := perform(); recorder.Code != http.StatusOK {
		t.Fatalf("first webhook status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := perform(); recorder.Code != http.StatusOK {
		t.Fatalf("duplicate webhook status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var status, paymentStatus string
	var paymentID int64
	if err := pool.QueryRow(ctx, "SELECT status, payment_status, payment_id FROM orders WHERE id=$1", orderID).Scan(&status, &paymentStatus, &paymentID); err != nil {
		t.Fatalf("read paid order: %v", err)
	}
	if status != "paid" || paymentStatus != "paid" || paymentID != 900001 {
		t.Fatalf("order state = %s/%s/%d, want paid/paid/900001", status, paymentStatus, paymentID)
	}
	var events, audits int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM payment_webhook_events WHERE order_id=$1", orderID).Scan(&events); err != nil {
		t.Fatalf("count webhook events: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='order' AND entity_id=$1 AND action='payment_webhook_applied'", orderID).Scan(&audits); err != nil {
		t.Fatalf("count payment audits: %v", err)
	}
	if events != 1 || audits != 1 {
		t.Fatalf("events/audits = %d/%d, want 1/1", events, audits)
	}
}

func TestExpiredOrderWithActivePreferenceReleasesStockOnceAcrossWorkers(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 5, 2)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })
	if _, err := pool.Exec(ctx, "UPDATE orders SET expires_at=TIMESTAMPTZ '2000-01-01 00:00:00+00', active_payment_preference_id='expired-pref' WHERE id=$1", orderID); err != nil {
		t.Fatalf("expire order: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan int64, 2)
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			released, err := ReleaseExpiredPendingOrdersWithAudit(ctx, &database.DB{Pool: pool}, 15*time.Minute, 10)
			results <- released
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	var total int64
	for released := range results {
		total += released
	}
	for err := range errors {
		if err != nil {
			t.Fatalf("release worker error: %v", err)
		}
	}
	if total < 1 {
		t.Fatalf("released orders = %d, want at least the target order", total)
	}

	var status string
	var stock, audits int
	if err := pool.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1", orderID).Scan(&status); err != nil {
		t.Fatalf("read order status: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1", productID).Scan(&stock); err != nil {
		t.Fatalf("read product stock: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='order' AND entity_id=$1 AND action='order_reservation_expired'", orderID).Scan(&audits); err != nil {
		t.Fatalf("count expiration audits: %v", err)
	}
	if status != "cancelled" || stock != 7 || audits != 1 {
		t.Fatalf("status/stock/audits = %s/%d/%d, want cancelled/7/1", status, stock, audits)
	}
}

func TestCheckoutCannotReactivateOrderThatExpiresDuringPreferenceCreation(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 5, 1)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })
	var email string
	if err := pool.QueryRow(ctx, "SELECT email FROM users WHERE id=$1", userID).Scan(&email); err != nil {
		t.Fatalf("read user email: %v", err)
	}

	payments := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, err := pool.Exec(request.Context(), "UPDATE orders SET expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1", orderID); err != nil {
			t.Errorf("expire order during checkout: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte(`{"preference_id":"late-pref","checkout_url":"https://checkout.example/late","environment":"test"}`))
	}))
	t.Cleanup(payments.Close)

	handler := CheckoutHandler(&database.DB{Pool: pool}, &config.Config{
		PaymentsServiceURL:     payments.URL,
		PaymentsRequestTimeout: 2 * time.Second,
		OrderPendingTTL:        15 * time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/checkout", bytes.NewBufferString(fmt.Sprintf(`{"order_id":%q}`, utils.EncodeID(orderID))))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("email", email)
	handler(c)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("checkout status = %d body=%s, want conflict", recorder.Code, recorder.Body.String())
	}
	var preferenceID sql.NullString
	if err := pool.QueryRow(ctx, "SELECT active_payment_preference_id FROM orders WHERE id=$1", orderID).Scan(&preferenceID); err != nil {
		t.Fatalf("read order preference: %v", err)
	}
	if preferenceID.Valid {
		t.Fatalf("active preference = %q, want null", preferenceID.String)
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run transactional integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedPendingOrder(t *testing.T, pool *pgxpool.Pool, stock, quantity int) (int, int, int) {
	t.Helper()
	ctx := context.Background()
	var userID, productID, orderID int
	email := fmt.Sprintf("rc2-%d@selecto.test", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, "INSERT INTO users (email,password,role) VALUES ($1,'unused','user') RETURNING id", email).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO products (name,price,stock,active) VALUES ('RC2 product',100,$1,TRUE) RETURNING id", stock).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO orders (user_id,status,payment_status,total,expires_at) VALUES ($1,'pending','pending',$2,NOW()+INTERVAL '15 minutes') RETURNING id", userID, quantity*100).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO order_items (order_id,product_id,quantity,price) VALUES ($1,$2,$3,100)", orderID, productID, quantity); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	return userID, productID, orderID
}

func assertOrderCancellationState(t *testing.T, pool *pgxpool.Pool, orderID, productID int, wantStatus string, wantStock, wantAudits int) {
	t.Helper()
	ctx := context.Background()
	var status string
	var stock, audits int
	if err := pool.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1", orderID).Scan(&status); err != nil {
		t.Fatalf("read order status: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1", productID).Scan(&stock); err != nil {
		t.Fatalf("read product stock: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='order' AND entity_id=$1 AND action='order_cancelled'", orderID).Scan(&audits); err != nil {
		t.Fatalf("count cancellation audits: %v", err)
	}
	if status != wantStatus || stock != wantStock || audits != wantAudits {
		t.Fatalf("status/stock/audits = %s/%d/%d, want %s/%d/%d", status, stock, audits, wantStatus, wantStock, wantAudits)
	}
}

func cleanupOrderFixture(ctx context.Context, pool *pgxpool.Pool, orderID, productID, userID int) {
	_, _ = pool.Exec(ctx, "DELETE FROM email_outbox WHERE event_key LIKE $1", fmt.Sprintf("%%:%d:%%", orderID))
	_, _ = pool.Exec(ctx, "DELETE FROM email_outbox WHERE event_key=$1", fmt.Sprintf("order-created:%d", orderID))
	_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='order' AND entity_id=$1", orderID)
	_, _ = pool.Exec(ctx, "DELETE FROM payment_webhook_events WHERE order_id=$1", orderID)
	_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", orderID)
	_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
}
