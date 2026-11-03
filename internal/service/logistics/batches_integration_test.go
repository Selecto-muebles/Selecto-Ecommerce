package logistics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCutoffBatchPersistsCSVAndNeverDuplicatesOrders(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := Store{Pool: pool}
	var userID, productID, firstID, secondID int
	email := fmt.Sprintf("cutoff-%d@selecto.test", time.Now().UnixNano())
	if err = pool.QueryRow(ctx, `INSERT INTO users(email,password,role) VALUES($1,'unused','user') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO products(name,sku,price,stock,active) VALUES('Demo corte','DEMO-CORTE',100,2,true) RETURNING id`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE action='logistics_batch_handed_off' AND actor_email='operator@selecto.test'`)
		_, _ = pool.Exec(ctx, `DELETE FROM logistics_cutoff_batch_orders WHERE order_id IN ($1,$2)`, firstID, secondID)
		_, _ = pool.Exec(ctx, `DELETE FROM logistics_cutoff_batches WHERE cutoff_at IN ($1,$2)`, time.Date(2020, 1, 4, 16, 0, 0, 0, time.UTC), time.Date(2020, 1, 6, 16, 0, 0, 0, time.UTC))
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id IN ($1,$2)`, firstID, secondID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	})
	paid := time.Date(2020, 1, 4, 15, 0, 0, 0, time.UTC)
	for index, target := range []*int{&firstID, &secondID} {
		if err = pool.QueryRow(ctx, `INSERT INTO orders(user_id,status,payment_status,total,paid_at) VALUES($1,'paid','paid',100,$2) RETURNING id`, userID, paid).Scan(target); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,product_name_snapshot,sku_snapshot,quantity,price) VALUES($1,$2,'Demo corte','DEMO-CORTE',1,100)`, *target, productID); err != nil {
			t.Fatal(err)
		}
		dni := "12345678"
		if index == 1 {
			dni = ""
		}
		if _, err = pool.Exec(ctx, `INSERT INTO order_shipping_addresses(order_id,recipient_first_name,recipient_last_name,dni,street_address,street_number,postal_code,province,locality,phone_number) VALUES($1,'Cliente','Ficticio',$2,'Calle','1','1000','Buenos Aires','CABA','1100000000')`, *target, dni); err != nil {
			t.Fatal(err)
		}
	}
	firstCutoff := time.Date(2020, 1, 4, 16, 1, 0, 0, time.UTC)
	created, err := store.PrepareDue(ctx, firstCutoff)
	if err != nil || len(created) != 1 || created[0].OrderCount != 1 || created[0].ExcludedOrderCount != 1 || !created[0].LegacyReviewRequired {
		t.Fatalf("first batch: %+v %v", created, err)
	}
	if again, err := store.PrepareDue(ctx, firstCutoff); err != nil || len(again) != 0 {
		t.Fatalf("duplicate batch: %+v %v", again, err)
	}
	content, hash, err := store.CSV(ctx, created[0].ID)
	if err != nil || !strings.Contains(string(content), utils.EncodeID(firstID)) || strings.Contains(string(content), utils.EncodeID(secondID)) {
		t.Fatalf("unexpected CSV: %v %s", err, content)
	}
	actualHash := sha256.Sum256(content)
	if hash != hex.EncodeToString(actualHash[:]) {
		t.Fatal("CSV checksum changed")
	}
	if _, err = pool.Exec(ctx, `UPDATE order_shipping_addresses SET dni='87654321' WHERE order_id=$1`, secondID); err != nil {
		t.Fatal(err)
	}
	created, err = store.PrepareDue(ctx, time.Date(2020, 1, 6, 16, 1, 0, 0, time.UTC))
	if err != nil || len(created) != 1 || created[0].OrderCount != 1 {
		t.Fatalf("Monday batch: %+v %v", created, err)
	}
	content, _, err = store.CSV(ctx, created[0].ID)
	if err != nil || !strings.Contains(string(content), utils.EncodeID(secondID)) || strings.Contains(string(content), utils.EncodeID(firstID)) {
		t.Fatalf("Monday CSV: %v %s", err, content)
	}
	if err = store.ConfirmHandoff(ctx, created[0].ID, "operator@selecto.test", "email", "logistica@example.test", "acuse demo"); err != nil {
		t.Fatal(err)
	}
	if err = store.ConfirmHandoff(ctx, created[0].ID, "operator@selecto.test", "email", "logistica@example.test", "acuse demo"); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx)
	if err != nil || len(items) < 2 || items[0].State != "handed_off" {
		t.Fatalf("handoff: %+v %v", items, err)
	}
}
