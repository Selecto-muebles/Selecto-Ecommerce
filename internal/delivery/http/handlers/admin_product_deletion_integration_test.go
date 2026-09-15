package handlers

import (
	"context"
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

func TestAdminDeleteProductRemovesUnreferencedActiveProduct(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	var productID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO products (name, sku, price, stock, active)
		VALUES ('Delete active guard', $1, 100, 1, TRUE) RETURNING id`,
		fmt.Sprintf("delete-active-%d", time.Now().UnixNano())).Scan(&productID); err != nil {
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
		t.Fatalf("delete active status = %d body=%s, want 204", recorder.Code, recorder.Body.String())
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM products WHERE id=$1", productID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("product remains after delete: %d %v", remaining, err)
	}
	var wasActive bool
	if err := pool.QueryRow(ctx, `SELECT (metadata->>'was_active')::boolean FROM audit_logs WHERE entity_type='product' AND entity_id=$1 AND action='product_deleted'`, productID).Scan(&wasActive); err != nil || !wasActive {
		t.Fatalf("audit active state: %v %v", wasActive, err)
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

func TestAdminDeleteProductArchivesReferencedProductAndPreservesOrderHistory(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 3, 1)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })
	for iteration, active := range []bool{true, false} {
		if _, err := pool.Exec(ctx, "UPDATE products SET active=$1 WHERE id=$2", active, productID); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/products/"+utils.EncodeID(productID), nil)
		c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(productID)}}
		AdminDeleteProductHandler(&database.DB{Pool: pool}, slog.Default())(c)
		c.Writer.WriteHeaderNow()
		if recorder.Code != http.StatusNoContent || recorder.Header().Get("X-Deletion-Mode") != "archived" {
			t.Fatalf("referenced delete: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var actual bool
		var archivedAt *time.Time
		if err := pool.QueryRow(ctx, "SELECT active,archived_at FROM products WHERE id=$1", productID).Scan(&actual, &archivedAt); err != nil || actual || archivedAt == nil {
			t.Fatalf("product was not archived: active=%v archived_at=%v err=%v", actual, archivedAt, err)
		}
		var references, audits int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM order_items WHERE product_id=$1", productID).Scan(&references); err != nil || references != 1 {
			t.Fatalf("order history changed: %d %v", references, err)
		}
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='product' AND entity_id=$1 AND action='product_archived'", productID).Scan(&audits); err != nil || audits != iteration+1 {
			t.Fatalf("archive audit count: %d %v", audits, err)
		}
	}
}
