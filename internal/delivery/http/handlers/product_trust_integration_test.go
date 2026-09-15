package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaidBuyerReviewRequiresModerationAndBecomesPublic(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("review-%d@selecto.test", suffix)
	var userID, productID, orderID int
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password,role,first_name,last_name) VALUES($1,'hash','user','Ana','Pérez') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,sku,price,stock,active,specifications) VALUES($1,$2,100,2,TRUE,'{"Material":"Acero"}') RETURNING id`, fmt.Sprintf("review-product-%d", suffix), fmt.Sprintf("REV-%d", suffix)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(user_id,status,total,payment_status,paid_at) VALUES($1,'paid',100,'paid',NOW()) RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,quantity,price) VALUES($1,$2,1,100)`, orderID, productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM product_reviews WHERE product_id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", orderID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
	})

	productPublicID := utils.EncodeID(productID)
	submit := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(submit)
	c.Set("email", email)
	c.Request = httptest.NewRequest(http.MethodPost, "/products/"+productPublicID+"/reviews", bytes.NewBufferString(`{"rating":5,"title":"Excelente","comment":"Muy firme y bien terminado."}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: productPublicID}}
	SubmitProductReviewHandler(&database.DB{Pool: pool})(c)
	if submit.Code != http.StatusCreated {
		t.Fatalf("submit status=%d body=%s", submit.Code, submit.Body.String())
	}
	var submitted struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(submit.Body.Bytes(), &submitted); err != nil {
		t.Fatal(err)
	}

	before := listPublishedReviews(t, pool, productPublicID)
	if before != 0 {
		t.Fatalf("pending review visible publicly: total=%d", before)
	}
	moderate := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(moderate)
	c.Set("email", "admin@selecto.test")
	c.Request = httptest.NewRequest(http.MethodPatch, "/admin/product-reviews/"+submitted.ID, bytes.NewBufferString(`{"status":"published"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: submitted.ID}}
	AdminModerateProductReviewHandler(&database.DB{Pool: pool})(c)
	if moderate.Code != http.StatusOK {
		t.Fatalf("moderate status=%d body=%s", moderate.Code, moderate.Body.String())
	}
	if total := listPublishedReviews(t, pool, productPublicID); total != 1 {
		t.Fatalf("published review total=%d, want 1", total)
	}
}

func TestDeleteHistoricalProductArchivesItImmediately(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var userID, productID, orderID int
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password,role) VALUES($1,'hash','user') RETURNING id`, fmt.Sprintf("archive-%d@selecto.test", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,price,stock,active) VALUES($1,100,1,TRUE) RETURNING id`, fmt.Sprintf("archive-product-%d", suffix)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(user_id,status,total) VALUES($1,'paid',100) RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,quantity,price) VALUES($1,$2,1,100)`, orderID, productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", orderID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("email", "admin@selecto.test")
	c.Request = httptest.NewRequest(http.MethodDelete, "/admin/products/"+utils.EncodeID(productID), nil)
	c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(productID)}}
	AdminDeleteProductHandler(&database.DB{Pool: pool}, slog.Default())(c)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("X-Deletion-Mode") != "archived" {
		t.Fatalf("delete status/mode=%d/%q body=%s", recorder.Code, recorder.Header().Get("X-Deletion-Mode"), recorder.Body.String())
	}
	var active bool
	var archivedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT active,archived_at FROM products WHERE id=$1", productID).Scan(&active, &archivedAt); err != nil || active || archivedAt == nil {
		t.Fatalf("archived product state active=%t archived_at=%v err=%v", active, archivedAt, err)
	}
}

func TestCommercialMetricsUsePaidOrdersAndArgentinaPeriods(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var userID, productID, orderID int
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password,role) VALUES($1,'hash','user') RETURNING id`, fmt.Sprintf("metrics-%d@selecto.test", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,price,stock,active) VALUES($1,250,4,TRUE) RETURNING id`, fmt.Sprintf("metrics-product-%d", suffix)).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(user_id,status,total,payment_status,paid_at) VALUES($1,'paid',500,'paid',NOW()) RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,quantity,price) VALUES($1,$2,2,250)`, orderID, productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM orders WHERE id=$1", orderID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
	})
	metrics, err := adminCommercialMetrics(ctx, &database.DB{Pool: pool}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if metrics["timezone"] != "America/Argentina/Buenos_Aires" || metrics["units_sold_month"].(int) < 2 {
		t.Fatalf("unexpected commercial metrics: %+v", metrics)
	}
	if len(metrics["top_products"].([]gin.H)) == 0 {
		t.Fatal("paid product missing from top products")
	}
}

func listPublishedReviews(t *testing.T, pool *pgxpool.Pool, productID string) int {
	t.Helper()
	decoded, _ := utils.DecodeID(productID)
	var total int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM product_reviews WHERE product_id=$1 AND status='published'`, decoded).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}
