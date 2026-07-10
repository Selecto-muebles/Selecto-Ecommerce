package handlers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

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

	if err := restoreOrderStock(ctx, tx, orderID); err != nil {
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
