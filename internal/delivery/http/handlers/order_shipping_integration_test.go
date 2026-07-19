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
)

func TestParseRequestedDeliveryDate(t *testing.T) {
	now := time.Date(2026, time.July, 19, 15, 0, 0, 0, time.UTC)
	if value, err := parseRequestedDeliveryDate("2026-07-20", now); err != nil || value.Format("2006-01-02") != "2026-07-20" {
		t.Fatalf("valid date = %v/%v", value, err)
	}
	if _, err := parseRequestedDeliveryDate("2026-07-18", now); err == nil {
		t.Fatal("past delivery date must be rejected")
	}
	if _, err := parseRequestedDeliveryDate("20/07/2026", now); err == nil {
		t.Fatal("non ISO delivery date must be rejected")
	}
}

func TestCreateOrderUsesPerOrderShippingSnapshot(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	email := fmt.Sprintf("shipping-%d@selecto.test", time.Now().UnixNano())
	var userID, productID int
	if err := pool.QueryRow(ctx, `INSERT INTO users (email,password,role,first_name,last_name,dni,street_address,street_number,postal_code,province,locality,phone_number)
		VALUES ($1,'unused','user','Cuenta','Original','12345678','Calle cuenta','100','1000','Buenos Aires','CABA','1112345678') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO products (name,price,stock,active) VALUES ('Shipping product',100,5,TRUE) RETURNING id").Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	var orderID int
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })

	requestedDate := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	body := fmt.Sprintf(`{"items":[{"product_id":%q,"quantity":1}],"shipping_address":{"first_name":"Otra","last_name":"Persona","dni":"87654321","street_address":"Destino real","street_number":"456","postal_code":"2000","province":"Santa Fe","locality":"Rosario","phone_number":"3412345678","requested_delivery_date":%q}}`, utils.EncodeID(productID), requestedDate)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("email", email)
	CreateOrderHandler(&database.DB{Pool: pool}, &config.Config{OrderPendingTTL: 15 * time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))(c)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create order status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	decoded, err := utils.DecodeID(response.OrderID)
	if err != nil {
		t.Fatalf("decode public order id: %v", err)
	}
	orderID = decoded

	var firstName, street, number, date string
	if err := pool.QueryRow(ctx, `SELECT recipient_first_name,street_address,street_number,requested_delivery_date::text FROM order_shipping_addresses WHERE order_id=$1`, orderID).Scan(&firstName, &street, &number, &date); err != nil {
		t.Fatalf("read order snapshot: %v", err)
	}
	if firstName != "Otra" || street != "Destino real" || number != "456" || date != requestedDate {
		t.Fatalf("snapshot=%s/%s/%s/%s", firstName, street, number, date)
	}
	var accountStreet string
	if err := pool.QueryRow(ctx, "SELECT street_address FROM users WHERE id=$1", userID).Scan(&accountStreet); err != nil {
		t.Fatalf("read account profile: %v", err)
	}
	if accountStreet != "Calle cuenta" {
		t.Fatalf("account profile changed to %q", accountStreet)
	}
}
