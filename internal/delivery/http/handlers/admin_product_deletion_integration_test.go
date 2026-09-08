package handlers

import (
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
)

func TestAdminDeleteProductRequiresInactiveProduct(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	var productID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO products (name, sku, price, stock, active)
		VALUES ('Delete active guard', $1, 100, 1, TRUE) RETURNING id`,
		fmt.Sprintf("delete-active-%d", time.Now().UnixNano())).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID) })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/admin/products/"+utils.EncodeID(productID), nil)
	c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(productID)}}
	c.Set("email", "delete-certification@selecto.test")
	AdminDeleteProductHandler(&database.DB{Pool: pool}, slog.Default())(c)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("delete active status = %d body=%s, want 409", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if payload["error_code"] != "conflict" {
		t.Fatalf("error code = %v, want conflict", payload["error_code"])
	}
}

func TestAdminDeleteProductRemovesUnreferencedInactiveProductAndAudits(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	var productID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO products (name, sku, price, stock, active)
		VALUES ('Delete inactive test', $1, 100, 1, FALSE) RETURNING id`,
		fmt.Sprintf("delete-inactive-%d", time.Now().UnixNano())).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product' AND entity_id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/admin/products/"+utils.EncodeID(productID), nil)
	c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(productID)}}
	c.Set("email", "delete-certification@selecto.test")
	AdminDeleteProductHandler(&database.DB{Pool: pool}, slog.Default())(c)
	c.Writer.WriteHeaderNow()

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete inactive status = %d body=%s, want 204", recorder.Code, recorder.Body.String())
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM products WHERE id=$1", productID).Scan(&remaining); err != nil {
		t.Fatalf("count deleted product: %v", err)
	}
	if remaining != 0 {
		t.Fatal("deleted product remains persisted")
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE entity_type='product' AND entity_id=$1 AND action='product_deleted'`, productID).Scan(&auditCount); err != nil {
		t.Fatalf("count deletion audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("deletion audit count = %d, want 1", auditCount)
	}
}
