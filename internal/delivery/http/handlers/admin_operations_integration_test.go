package handlers

import (
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/service/catalog"
	"Selecto-Ecommerce/internal/shared/utils"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCategoryOperationsPreserveProductIdentityAndRollbackMissingReorder(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	category, err := postgres.CreateCategory(ctx, pool, catalog.Category{Name: fmt.Sprintf("Original %d", suffix), Slug: fmt.Sprintf("original-%d", suffix), Active: true}, "admin@selecto.test")
	if err != nil {
		t.Fatal(err)
	}
	var product int
	if err := pool.QueryRow(ctx, "INSERT INTO products(name,price,stock,category,category_id) VALUES('Category linked test',100,2,$1,$2) RETURNING id", category.Name, category.ID).Scan(&product); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", product)
		_, _ = pool.Exec(ctx, "DELETE FROM categories WHERE id=$1", category.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='category' AND entity_id=$1", category.ID)
	})
	category.Name = fmt.Sprintf("Renamed %d", suffix)
	category.Active = false
	category.SortOrder = 25
	changed, err := postgres.UpdateCategory(ctx, pool, category, "admin@selecto.test")
	if err != nil {
		t.Fatal(err)
	}
	if changed.ProductCount != 1 || changed.ActiveProductCount != 1 || changed.Active {
		t.Fatalf("counts/state: %+v", changed)
	}
	var name string
	var linked int64
	if err := pool.QueryRow(ctx, "SELECT category,category_id FROM products WHERE id=$1", product).Scan(&name, &linked); err != nil || name != category.Name || linked != category.ID {
		t.Fatalf("identity changed: %q %d %v", name, linked, err)
	}
	visible, err := postgres.ListCategories(ctx, pool, true, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range visible {
		if item.ID == category.ID {
			t.Fatal("inactive category public")
		}
	}
	if err := postgres.ReorderCategories(ctx, pool, []int64{category.ID, 9223372036854775807}, "admin@selecto.test"); err == nil {
		t.Fatal("missing category accepted")
	}
	var order int
	if err := pool.QueryRow(ctx, "SELECT sort_order FROM categories WHERE id=$1", category.ID).Scan(&order); err != nil || order != 25 {
		t.Fatalf("partial reorder: %d %v", order, err)
	}
}
func TestReviewCanBeRemoderatedAndAuditPreservesReason(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	user, product, order := seedPendingOrder(t, pool, 2, 1)
	var review int
	if err := pool.QueryRow(ctx, "INSERT INTO product_reviews(product_id,user_id,order_id,rating,title,comment) VALUES($1,$2,$3,5,'Review','Verified text') RETURNING id", product, user, order).Scan(&review); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM product_reviews WHERE id=$1", review)
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product_review' AND entity_id=$1", review)
		cleanupOrderFixture(ctx, pool, order, product, user)
	})
	for _, status := range []string{"published", "rejected", "pending", "published"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("email", "moderator@selecto.test")
		c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(review)}}
		c.Request = httptest.NewRequest(http.MethodPatch, "/admin/product-reviews/"+utils.EncodeID(review), strings.NewReader(fmt.Sprintf(`{"status":%q,"note":"Manual correction"}`, status)))
		c.Request.Header.Set("Content-Type", "application/json")
		AdminOperationsModerateProductReviewHandler(&database.DB{Pool: pool})(c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("moderate %s: %d %s", status, recorder.Code, recorder.Body)
		}
	}
	var note string
	var audits int
	if err := pool.QueryRow(ctx, "SELECT moderation_note FROM product_reviews WHERE id=$1", review).Scan(&note); err != nil || note != "Manual correction" {
		t.Fatalf("note: %s %v", note, err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='product_review' AND entity_id=$1", review).Scan(&audits); err != nil || audits != 4 {
		t.Fatalf("audit: %d %v", audits, err)
	}
}
func TestComparablePeriodsAndShipmentAgeIgnoreNoteEdits(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	user, product, order := seedPendingOrder(t, pool, 2, 1)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, order, product, user) })
	now := time.Date(2032, 3, 15, 12, 0, 0, 0, time.UTC)
	period, ok := dashboardPeriod("7d", now)
	if !ok || period.CurrentEnd.Sub(period.CurrentStart) != period.PreviousEnd.Sub(period.PreviousStart) {
		t.Fatal("periods not comparable")
	}
	if _, err := pool.Exec(ctx, "UPDATE orders SET status='paid',total=100,paid_at=$2 WHERE id=$1", order, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO shipments(order_id,status,status_changed_at,updated_at) VALUES($1,'ready_for_dispatch',$2,$3)", order, now.Add(-48*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	metrics, err := adminComparableMetrics(ctx, &database.DB{Pool: pool}, period)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["sales_period"].(float64) != 100 || metrics["orders_period"].(int) != 1 || metrics["sales_period_change_percent"] != nil {
		t.Fatalf("comparison: %+v", metrics)
	}
	if metrics["shipments_ready_over_24h"].(int) < 1 || metrics["oldest_ready_for_dispatch_hours"].(float64) < 48 {
		t.Fatalf("age lost after edit: %+v", metrics)
	}
	if _, valid := dashboardPeriod("forever", now); valid {
		t.Fatal("invalid period accepted")
	}
}
