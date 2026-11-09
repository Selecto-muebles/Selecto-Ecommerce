package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	orderservice "Selecto-Ecommerce/internal/service/orders"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFamilyVariantIsPreservedInOrderAndDispatchSnapshot(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var familyID, productID int
	if err := tx.QueryRow(ctx, `INSERT INTO product_families(name,axes) VALUES('Order snapshot family ' || nextval('products_id_seq'), '["Largo","Color"]') RETURNING id`).Scan(&familyID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO products(name,price,stock,active,family_id,variant_values)
		VALUES('Paralelas',56000,5,true,$1,'{"Largo":"40 cm","Color":"Negro"}') RETURNING id`, familyID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	prepared, err := orderservice.PrepareItems([]orderservice.Item{{ProductID: productID, Quantity: 2}})
	if err != nil {
		t.Fatal(err)
	}
	_, reserved, _, err := reserveProducts(ctx, tx, prepared, map[int][]orderservice.GroupedItem{productID: prepared.Grouped})
	if err != nil {
		t.Fatal(err)
	}
	if len(reserved) != 1 || reserved[0].selectedOptions["Color"] != "Negro" || reserved[0].selectedOptions["Largo"] != "40 cm" {
		t.Fatalf("variant not saved in order options: %+v", reserved)
	}
	if !strings.Contains(reserved[0].productName, "Color: Negro") || !strings.Contains(reserved[0].productName, "Largo: 40 cm") {
		t.Fatalf("variant not saved in logistics product name: %q", reserved[0].productName)
	}
}
