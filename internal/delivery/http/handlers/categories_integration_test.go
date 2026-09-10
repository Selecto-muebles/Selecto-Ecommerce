package handlers

import (
	"Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/service/catalog"
	migrationfiles "Selecto-Ecommerce/migrations"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCategoryMigrationPreservesLegacyProductsAndRepeats(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	schema := fmt.Sprintf("category_migration_%d", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema+"; CREATE TABLE products(id SERIAL PRIMARY KEY, category TEXT NOT NULL DEFAULT '')"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO products(category) VALUES ('Calistenia'),(' calistenia '),(''),('Barras')"); err != nil {
		t.Fatal(err)
	}
	content, err := migrationfiles.Files.ReadFile("014_catalog_categories.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			t.Fatal(err)
		}
	}
	var products, categories, linked int
	if err := tx.QueryRow(ctx, "SELECT (SELECT COUNT(*) FROM products),(SELECT COUNT(*) FROM categories),(SELECT COUNT(*) FROM products WHERE category_id IS NOT NULL)").Scan(&products, &categories, &linked); err != nil {
		t.Fatal(err)
	}
	if products != 4 || categories != 2 || linked != 3 {
		t.Fatalf("backfill counts: %d %d %d", products, categories, linked)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM categories"); err == nil {
		t.Fatal("referenced category deletion accepted")
	}
}

func TestCategoryLegacyWriteAndRollback(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	name := fmt.Sprintf("Categoria-%d", time.Now().UnixNano())
	var id, other int64
	if err := tx.QueryRow(ctx, `INSERT INTO products(name,price,stock,category) VALUES ('Test',100,2,$1) RETURNING category_id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO products(name,price,stock,category) VALUES ('Test',100,2,LOWER($1)) RETURNING category_id`, name).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if id != other {
		t.Fatal("case-insensitive categories duplicated")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM categories WHERE id=$1", id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback category: %d %v", count, err)
	}
}

func TestCategoryCreateAuditIsAtomicAndDuplicateRejected(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	name := fmt.Sprintf("categoria-%d", time.Now().UnixNano())
	item := catalog.Category{Name: name, Slug: name, Active: true}
	// A NULL actor causes NOT NULL failure after the category insert in the transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "INSERT INTO categories(name,slug) VALUES ($1,$1)", name); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs(actor_email,action,entity_type,entity_id) VALUES (NULL,'category_created','category',1)`); err == nil {
		t.Fatal("expected audit failure")
	}
	_ = tx.Rollback(ctx)
	created, err := postgres.CreateCategory(ctx, pool, item, "categories@test.invalid")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='category' AND entity_id=$1", created.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM categories WHERE id=$1", created.ID)
	})
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='category' AND entity_id=$1", created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit: %d %v", count, err)
	}
	if _, err := postgres.CreateCategory(ctx, pool, item, "categories@test.invalid"); err == nil {
		t.Fatal("duplicate accepted")
	}
}
