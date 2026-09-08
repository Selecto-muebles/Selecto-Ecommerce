package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateOrderAppliesProductQuantityAcrossVariants(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("volume-pricing-%d@selecto.test", time.Now().UnixNano())
	userID := seedVolumePricingUser(t, pool, email)

	var productID int
	if err := pool.QueryRow(ctx, `INSERT INTO products (name,price,stock,active)
		VALUES ('Volume pricing product',19.99,20,TRUE) RETURNING id`).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_options (product_id,name,values)
		VALUES ($1,'Color','["Negro","Blanco"]'::jsonb)`, productID); err != nil {
		t.Fatalf("seed options: %v", err)
	}

	orderID := createVolumePricingOrder(t, pool, email,
		fmt.Sprintf(`{"items":[{"product_id":%q,"quantity":2,"selected_options":{"Color":"Negro"}},{"product_id":%q,"quantity":2,"selected_options":{"Color":"Blanco"}}]}`,
			utils.EncodeID(productID), utils.EncodeID(productID)), http.StatusCreated)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })

	var total float64
	var stock, lines int
	var charged, original, discount float64
	if err := pool.QueryRow(ctx, "SELECT total FROM orders WHERE id=$1", orderID).Scan(&total); err != nil {
		t.Fatalf("read order total: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1", productID).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), MIN(price), MIN(original_unit_price), MIN(discount_percent)
		FROM order_items WHERE order_id=$1`, orderID).Scan(&lines, &charged, &original, &discount); err != nil {
		t.Fatalf("read priced items: %v", err)
	}
	if total != 71.96 || stock != 16 || lines != 2 || charged != 17.99 || original != 19.99 || discount != 10 {
		t.Fatalf("total/stock/lines/price/original/discount = %.2f/%d/%d/%.2f/%.2f/%.2f", total, stock, lines, charged, original, discount)
	}

	requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext.Request = httptest.NewRequest(http.MethodGet, "/orders/"+utils.EncodeID(orderID), nil)
	order, err := fetchOrder(requestContext, &database.DB{Pool: pool}, orderID, email, false)
	if err != nil {
		t.Fatalf("read customer order contract: %v", err)
	}
	if len(order.Items) != 2 {
		t.Fatalf("customer order items = %d, want 2", len(order.Items))
	}
	for _, item := range order.Items {
		if item.Price != 17.99 || item.OriginalPrice != 19.99 || item.DiscountPercent != 10 || item.TotalSavings != 4 {
			t.Fatalf("unexpected customer pricing contract: %#v", item)
		}
	}

	adminOrder, err := adminOrderDetail(ctx, &database.DB{Pool: pool}, orderID)
	if err != nil {
		t.Fatalf("read admin order contract: %v", err)
	}
	adminItems, ok := adminOrder["items"].([]gin.H)
	if !ok || len(adminItems) != 2 {
		t.Fatalf("admin order items = %#v, want two items", adminOrder["items"])
	}
	for _, item := range adminItems {
		if item["price"] != 17.99 || item["original_unit_price"] != 19.99 || item["discount_percent"] != float64(10) || item["total_savings"] != 4.0 {
			t.Fatalf("unexpected admin pricing contract: %#v", item)
		}
	}
}

func TestCreateOrderRollsBackDiscountedReservationWhenOrderInsertFails(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("volume-rollback-%d@selecto.test", time.Now().UnixNano())
	userID := seedVolumePricingUser(t, pool, email)
	var productID int
	if err := pool.QueryRow(ctx, `INSERT INTO products (name,price,stock,active)
		VALUES ('Volume rollback product',9999999999.99,100,TRUE) RETURNING id`).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, 0, productID, userID) })

	createVolumePricingOrder(t, pool, email,
		fmt.Sprintf(`{"items":[{"product_id":%q,"quantity":100}]}`, utils.EncodeID(productID)), http.StatusInternalServerError)

	var stock, orders int
	if err := pool.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1", productID).Scan(&stock); err != nil {
		t.Fatalf("read rolled back stock: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE user_id=$1", userID).Scan(&orders); err != nil {
		t.Fatalf("count rolled back orders: %v", err)
	}
	if stock != 100 || orders != 0 {
		t.Fatalf("stock/orders = %d/%d, want 100/0", stock, orders)
	}
}

func seedVolumePricingUser(t *testing.T, pool *pgxpool.Pool, email string) int {
	t.Helper()
	var userID int
	err := pool.QueryRow(context.Background(), `INSERT INTO users (
		email,password,role,email_verified_at,first_name,last_name,dni,street_address,
		street_number,postal_code,province,locality,phone_number
	) VALUES ($1,'unused','user',NOW(),'Ada','Lovelace','12345678','Calle','123','1000','Buenos Aires','CABA','1112345678') RETURNING id`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return userID
}

func createVolumePricingOrder(t *testing.T, pool *pgxpool.Pool, email, body string, expectedStatus int) int {
	t.Helper()
	handler := CreateOrderHandler(&database.DB{Pool: pool}, &config.Config{OrderPendingTTL: 15 * time.Minute, StorefrontURL: "http://localhost:5173"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("email", email)
	handler(c)
	if recorder.Code != expectedStatus {
		t.Fatalf("create order status=%d body=%s, want %d", recorder.Code, recorder.Body.String(), expectedStatus)
	}
	if expectedStatus >= 400 {
		return 0
	}
	var response struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	orderID, err := utils.DecodeID(response.OrderID)
	if err != nil {
		t.Fatalf("decode order id: %v", err)
	}
	return orderID
}
