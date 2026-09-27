package handlers

import (
	"context"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/repository/postgres"
)

func TestCheckoutUsesConfirmedShippingWhenRegisteredProfileIsEmpty(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	userID, productID, orderID := seedPendingOrder(t, pool, 5, 1)
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })
	var email string
	if err := pool.QueryRow(ctx, `UPDATE users SET first_name='',last_name='',dni='' WHERE id=$1 RETURNING email`, userID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_shipping_addresses
  (order_id,recipient_first_name,recipient_last_name,dni,street_address,street_number,postal_code,province,locality,phone_number)
  VALUES ($1,'Ana','Pérez','87654321','Destino','100','1000','Buenos Aires','CABA','1112345678')
  ON CONFLICT (order_id) DO UPDATE SET recipient_first_name='Ana',recipient_last_name='Pérez',dni='87654321'`, orderID); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewCheckoutRepository(&database.DB{Pool: pool}, &config.Config{OrderPendingTTL: 15 * time.Minute})
	order, err := repo.LoadAvailable(ctx, orderID, email)
	if err != nil {
		t.Fatal(err)
	}
	if order.Customer.Email != email || order.Customer.Name != "Ana Pérez" || order.Customer.Identification != "87654321" {
		t.Fatalf("checkout lost confirmed identity: %+v", order.Customer)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET first_name='Cuenta',last_name='Original',dni='12345678' WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	order, err = repo.LoadAvailable(ctx, orderID, email)
	if err != nil {
		t.Fatal(err)
	}
	if order.Customer.Name != "Cuenta Original" || order.Customer.Identification != "12345678" {
		t.Fatalf("existing buyer identity changed: %+v", order.Customer)
	}
}
