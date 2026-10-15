package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGestionarLedgerSerializesPackagesAndPickups(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run Gestionar ledger integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	orderID, cleanup := seedGestionarLedgerOrder(t, pool)
	defer cleanup()
	ledger := GestionarLedger{Pool: pool}
	const workers = 12
	var wait sync.WaitGroup
	results := make(chan GestionarPackage, workers)
	errorsCh := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			record, blockers, err := ledger.ReservePackage(ctx, orderID)
			if err != nil || len(blockers) > 0 {
				errorsCh <- fmt.Errorf("reserve package: %v, blockers=%v", err, blockers)
				return
			}
			results <- record
		}()
	}
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	var key string
	for record := range results {
		if key == "" {
			key = record.SubmissionKey
		}
		if record.OrderID != orderID || record.State != "reserved" || record.SubmissionKey != key {
			t.Fatalf("inconsistent reservation: %+v", record)
		}
	}
	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM gestionar_dispatches WHERE order_id=$1`, orderID).Scan(&rowCount); err != nil || rowCount != 1 {
		t.Fatalf("package reservation rows=%d err=%v", rowCount, err)
	}
	var claims int
	var claimMu sync.Mutex
	claimErrors := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			claimed, err := ledger.ClaimPackageAttempt(ctx, orderID)
			if err != nil {
				claimErrors <- err
			}
			if claimed {
				claimMu.Lock()
				claims++
				claimMu.Unlock()
			}
		}()
	}
	wait.Wait()
	close(claimErrors)
	for err := range claimErrors {
		t.Fatal(err)
	}
	if claims != 1 {
		t.Fatalf("package send claims=%d, want 1", claims)
	}
	if err := ledger.MarkPackageUncertain(ctx, orderID, "timeout"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := ledger.ClaimPackageAttempt(ctx, orderID); err != nil || claimed {
		t.Fatalf("uncertain package retried: claimed=%v err=%v", claimed, err)
	}
	if err := ledger.ConfirmPackage(ctx, orderID, "package-test-1"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.ConfirmPackage(ctx, orderID, "package-test-1"); err != nil {
		t.Fatalf("same package confirmation was not idempotent: %v", err)
	}
	if err := ledger.ConfirmPackage(ctx, orderID, "different-package"); !errors.Is(err, ErrGestionarConflict) {
		t.Fatalf("conflicting package accepted: %v", err)
	}
	pickups := make(chan GestionarPickupBatch, workers)
	pickupErrors := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			pickup, err := ledger.ReservePickupBatch(ctx, []int{orderID}, 7)
			if err != nil {
				pickupErrors <- err
				return
			}
			pickups <- pickup
		}()
	}
	wait.Wait()
	close(pickups)
	close(pickupErrors)
	for err := range pickupErrors {
		t.Fatalf("concurrent pickup reservation: %v", err)
	}
	var batch GestionarPickupBatch
	for pickup := range pickups {
		if batch.ID == 0 {
			batch = pickup
		}
		if pickup.ID != batch.ID || pickup.State != "reserved" {
			t.Fatalf("inconsistent pickup reservation: %+v, first=%+v", pickup, batch)
		}
	}
	if batch.ID == 0 {
		t.Fatal("no pickup batch reserved")
	}
	again, err := ledger.ReservePickupBatch(ctx, []int{orderID}, 7)
	if err != nil || again.ID != batch.ID {
		t.Fatalf("duplicate pickup: %+v err=%v", again, err)
	}
	if _, err := ledger.ReservePickupBatch(ctx, []int{orderID}, 8); !errors.Is(err, ErrGestionarConflict) {
		t.Fatalf("overlapping pickup accepted: %v", err)
	}
	if claimed, err := ledger.ClaimPickupAttempt(ctx, batch.ID); err != nil || !claimed {
		t.Fatalf("claim pickup: %v %v", claimed, err)
	}
	if err := ledger.MarkPickupUncertain(ctx, batch.ID, "timeout"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := ledger.ClaimPickupAttempt(ctx, batch.ID); err != nil || claimed {
		t.Fatalf("uncertain pickup retried: %v %v", claimed, err)
	}
	if err := ledger.ConfirmPickup(ctx, batch.ID, "pickup-test-1"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.ConfirmPickup(ctx, batch.ID, "pickup-test-1"); err != nil {
		t.Fatalf("same pickup confirmation was not idempotent: %v", err)
	}
	if err := ledger.RecordProviderStatus(ctx, orderID, "event-test-1", "PROVIDER_STATUS", nil); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RecordProviderStatus(ctx, orderID, "event-test-1", "PROVIDER_STATUS", nil); err != nil {
		t.Fatalf("duplicate status was not idempotent: %v", err)
	}
	if err := ledger.RecordProviderStatus(ctx, orderID, "event-test-1", "DIFFERENT", nil); !errors.Is(err, ErrGestionarConflict) {
		t.Fatalf("status key reused with other data: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM gestionar_status_events WHERE order_id=$1`, orderID).Scan(&rowCount); err != nil || rowCount != 1 {
		t.Fatalf("status rows=%d err=%v", rowCount, err)
	}
}

func TestGestionarLedgerBlocksIncompleteOrder(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run Gestionar ledger integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	orderID, cleanup := seedGestionarLedgerOrder(t, pool)
	defer cleanup()
	ledger := GestionarLedger{Pool: pool}
	if _, blockers, err := ledger.ReservePackage(ctx, orderID); err != nil || len(blockers) != 0 {
		t.Fatalf("reserve complete order: blockers=%v err=%v", blockers, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM order_shipping_addresses WHERE order_id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := ledger.ClaimPackageAttempt(ctx, orderID); err != nil || claimed {
		t.Fatalf("order missing address can be sent: %v %v", claimed, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM gestionar_dispatches WHERE order_id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	record, blockers, err := ledger.ReservePackage(ctx, orderID)
	if err != nil || record.OrderID != 0 || len(blockers) == 0 {
		t.Fatalf("incomplete order reserved: %+v blockers=%v err=%v", record, blockers, err)
	}
	if claimed, err := ledger.ClaimPackageAttempt(ctx, orderID); err != nil || claimed {
		t.Fatalf("incomplete order can be sent: %v %v", claimed, err)
	}
}

func TestGestionarLedgerDefinitiveRejectionCannotBeRetried(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run Gestionar ledger integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	orderID, cleanup := seedGestionarLedgerOrder(t, pool)
	defer cleanup()
	ledger := GestionarLedger{Pool: pool}
	if _, blockers, err := ledger.ReservePackage(ctx, orderID); err != nil || len(blockers) > 0 {
		t.Fatalf("reserve package: blockers=%v err=%v", blockers, err)
	}
	if claimed, err := ledger.ClaimPackageAttempt(ctx, orderID); err != nil || !claimed {
		t.Fatalf("claim package: %v %v", claimed, err)
	}
	if err := ledger.RejectPackage(ctx, orderID, "provider_422"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := ledger.ClaimPackageAttempt(ctx, orderID); err != nil || claimed {
		t.Fatalf("rejected package retried: %v %v", claimed, err)
	}
	if err := ledger.ConfirmPackage(ctx, orderID, "unexpected-package"); !errors.Is(err, ErrGestionarConflict) {
		t.Fatalf("rejected package confirmed: %v", err)
	}
	if err := ledger.RejectPackage(ctx, orderID, "secret: raw provider message"); !errors.Is(err, ErrGestionarConflict) {
		t.Fatalf("unsafe raw provider error accepted: %v", err)
	}
}

func seedGestionarLedgerOrder(t *testing.T, pool *pgxpool.Pool) (int, func()) {
	t.Helper()
	ctx := context.Background()
	var userID, productID, orderID int
	email := fmt.Sprintf("gestion-ledger-%d@selecto.test", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password,role)
		VALUES($1,'unused','user') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,sku,price,stock,active)
		VALUES('Test package','TEST-LEDGER',100,1,TRUE) RETURNING id`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(user_id,status,total,paid_at)
		VALUES($1,'paid',100,NOW()) RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_items
		(order_id,product_id,product_name_snapshot,sku_snapshot,quantity,price)
		VALUES($1,$2,'Test package','TEST-LEDGER',1,100)`, orderID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_shipping_addresses
		(order_id,recipient_first_name,recipient_last_name,dni,street_address,street_number,
		postal_code,province,locality,phone_number)
		VALUES($1,'Test','Receiver','12345678','Street','10','1000','BA','Town','111111')`, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO shipments(order_id,status) VALUES($1,'preparing')`, orderID); err != nil {
		t.Fatal(err)
	}
	return orderID, func() {
		_, _ = pool.Exec(ctx, `DELETE FROM gestionar_status_events WHERE order_id=$1`, orderID)
		_, _ = pool.Exec(ctx, `WITH removed AS (
			DELETE FROM gestionar_pickup_orders WHERE order_id=$1 RETURNING batch_id
		) DELETE FROM gestionar_pickup_batches WHERE id IN (SELECT batch_id FROM removed)`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM gestionar_dispatches WHERE order_id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	}
}
