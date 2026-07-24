package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRestoreOrderStockInsideTransaction(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run transactional stock tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	var userID int
	testEmail := fmt.Sprintf("cancel-stock-%d@selecto.test", time.Now().UnixNano())
	if err := tx.QueryRow(ctx, `INSERT INTO users (email, password, role)
		VALUES ($1, 'not-used', 'user')
		RETURNING id`, testEmail).Scan(&userID); err != nil {
		t.Fatalf("create test user: %v", err)
	}

	var productID int
	if err := tx.QueryRow(ctx, `INSERT INTO products (name, price, stock, active)
		VALUES ('Cancellation stock test', 100, 5, TRUE)
		RETURNING id`).Scan(&productID); err != nil {
		t.Fatalf("create test product: %v", err)
	}

	var orderID int
	if err := tx.QueryRow(ctx, `INSERT INTO orders (user_id, status, payment_status, total)
		VALUES ($1, 'pending', 'pending', 200)
		RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatalf("create test order: %v", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO order_items (order_id, product_id, quantity, price)
		VALUES ($1, $2, 2, 100)`, orderID, productID); err != nil {
		t.Fatalf("create test order item: %v", err)
	}

	if err := postgresrepo.RestoreOrderStock(ctx, tx, orderID); err != nil {
		t.Fatalf("restore order stock: %v", err)
	}

	var stock int
	if err := tx.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1", productID).Scan(&stock); err != nil {
		t.Fatalf("read restored stock: %v", err)
	}
	if stock != 7 {
		t.Fatalf("expected restored stock 7, got %d", stock)
	}
}

func TestAdminCancelOrderTransaction(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 5, 2)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })

	handler := AdminCancelOrderHandler(&database.DB{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	perform := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/orders/"+utils.EncodeID(orderID)+"/cancel", nil)
		c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(orderID)}}
		c.Set("email", "rc2-admin@selecto.test")
		handler(c)
		return recorder
	}

	if recorder := perform(); recorder.Code != http.StatusOK {
		t.Fatalf("first cancellation status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertOrderCancellationState(t, pool, orderID, productID, "cancelled", 7, 1)
	if recorder := perform(); recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate cancellation status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertOrderCancellationState(t, pool, orderID, productID, "cancelled", 7, 1)
}

func TestCustomerCancelOrderIsOwnedAndIdempotent(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 5, 2)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })
	var customerEmail string
	if err := pool.QueryRow(ctx, "SELECT email FROM users WHERE id=$1", userID).Scan(&customerEmail); err != nil {
		t.Fatalf("read customer email: %v", err)
	}

	handler := CancelOrderHandler(&database.DB{Pool: pool}, &config.Config{StorefrontURL: "http://localhost:5173"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	perform := func(email string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/orders/"+utils.EncodeID(orderID)+"/cancel", nil)
		c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(orderID)}}
		c.Set("email", email)
		handler(c)
		return recorder
	}

	if recorder := perform("other@selecto.test"); recorder.Code != http.StatusNotFound {
		t.Fatalf("foreign cancellation status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := perform(customerEmail); recorder.Code != http.StatusOK {
		t.Fatalf("first cancellation status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := perform(customerEmail); recorder.Code != http.StatusOK {
		t.Fatalf("replayed cancellation status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var status string
	var stock, audits, emails int
	if err := pool.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1", orderID).Scan(&status); err != nil {
		t.Fatalf("read cancelled order: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1", productID).Scan(&stock); err != nil {
		t.Fatalf("read restored stock: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='order' AND entity_id=$1 AND action='order_cancelled_by_customer'", orderID).Scan(&audits); err != nil {
		t.Fatalf("count customer cancellation audits: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM email_outbox WHERE event_key=$1", fmt.Sprintf("payment-status:%d:0:cancelled", orderID)).Scan(&emails); err != nil {
		t.Fatalf("count cancellation emails: %v", err)
	}
	if status != "cancelled" || stock != 7 || audits != 1 || emails != 1 {
		t.Fatalf("status/stock/audits/emails = %s/%d/%d/%d, want cancelled/7/1/1", status, stock, audits, emails)
	}
}

func TestCreateOrderIdempotencyKeyReservesStockOnce(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("idempotent-order-%d@selecto.test", time.Now().UnixNano())
	var userID, productID int
	if err := pool.QueryRow(ctx, `INSERT INTO users (email,password,role,email_verified_at,first_name,last_name,dni,street_address,street_number,postal_code,province,locality,phone_number)
		VALUES ($1,'unused','user',NOW(),'Ada','Lovelace','12345678','Calle','123','1000','Buenos Aires','CABA','1112345678') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("seed idempotency user: %v", err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO products (name,price,stock,active) VALUES ('Idempotent product',100,5,TRUE) RETURNING id").Scan(&productID); err != nil {
		t.Fatalf("seed idempotency product: %v", err)
	}
	var orderID int
	t.Cleanup(func() {
		if orderID > 0 {
			cleanupOrderFixture(ctx, pool, orderID, productID, userID)
			return
		}
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
	})

	handler := CreateOrderHandler(&database.DB{Pool: pool}, &config.Config{OrderPendingTTL: 15 * time.Minute, StorefrontURL: "http://localhost:5173"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := fmt.Sprintf(`{"items":[{"product_id":%q,"quantity":2}]}`, utils.EncodeID(productID))
	perform := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Idempotency-Key", "checkout-attempt-integration")
		c.Set("email", email)
		handler(c)
		return recorder
	}

	first := perform()
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d body=%s", first.Code, first.Body.String())
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM orders WHERE user_id=$1 AND idempotency_key='checkout-attempt-integration'", userID).Scan(&orderID); err != nil {
		t.Fatalf("read created order: %v", err)
	}
	second := perform()
	if second.Code != http.StatusOK {
		t.Fatalf("replayed create status = %d body=%s", second.Code, second.Body.String())
	}
	var stock, orders, audits int
	if err := pool.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1", productID).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE user_id=$1", userID).Scan(&orders); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='order' AND entity_id=$1 AND action='order_created'", orderID).Scan(&audits); err != nil {
		t.Fatalf("count creation audits: %v", err)
	}
	if stock != 3 || orders != 1 || audits != 1 {
		t.Fatalf("stock/orders/audits = %d/%d/%d, want 3/1/1", stock, orders, audits)
	}
}
