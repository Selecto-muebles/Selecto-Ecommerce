package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	shipping "Selecto-Ecommerce/internal/service/shipping"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrGestionarConflict = errors.New("gestionar operation conflicts with existing state")
var ErrGestionarNotFound = errors.New("gestionar order was not found")

type GestionarPackage struct {
	OrderID           int
	SubmissionKey     string
	State             string
	ExternalPackageID string
}

type GestionarLedger struct{ Pool *pgxpool.Pool }

// ReservePackage serializes on the order row. It never sends a request to the
// provider and returns the same record for repeat callers.
func (ledger GestionarLedger) ReservePackage(ctx context.Context, orderID int) (GestionarPackage, []shipping.GestionarBlocker, error) {
	tx, err := ledger.Pool.Begin(ctx)
	if err != nil {
		return GestionarPackage{}, nil, err
	}
	defer tx.Rollback(ctx)
	var orderStatus string
	err = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", orderID).Scan(&orderStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return GestionarPackage{}, nil, ErrGestionarNotFound
	}
	if err != nil {
		return GestionarPackage{}, nil, err
	}
	var existing GestionarPackage
	err = tx.QueryRow(ctx, `SELECT order_id, submission_key, package_state,
		COALESCE(external_package_id,'') FROM gestionar_dispatches WHERE order_id=$1`, orderID).
		Scan(&existing.OrderID, &existing.SubmissionKey, &existing.State, &existing.ExternalPackageID)
	if err == nil {
		return existing, nil, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return GestionarPackage{}, nil, err
	}
	ready := shipping.GestionarReadiness{OrderStatus: orderStatus}
	var firstName, lastName string
	err = tx.QueryRow(ctx, `SELECT COALESCE(s.status,''), a.order_id IS NOT NULL,
		COALESCE(a.recipient_first_name,''), COALESCE(a.recipient_last_name,''),
		COALESCE(a.street_address,''), COALESCE(a.street_number,''),
		COALESCE(a.locality,''), COALESCE(a.province,''), COALESCE(a.postal_code,''),
		COALESCE(a.phone_number,''), u.email,
		(SELECT COUNT(*) FROM order_items WHERE order_id=o.id),
		(SELECT COUNT(*) FROM order_items WHERE order_id=o.id AND BTRIM(sku_snapshot)='')
		FROM orders o JOIN users u ON u.id=o.user_id
		LEFT JOIN shipments s ON s.order_id=o.id
		LEFT JOIN order_shipping_addresses a ON a.order_id=o.id WHERE o.id=$1`, orderID).
		Scan(&ready.ShipmentStatus, &ready.HasAddress, &firstName, &lastName,
			&ready.Street, &ready.StreetNumber, &ready.Locality, &ready.Province,
			&ready.PostalCode, &ready.Phone, &ready.Email, &ready.ItemCount, &ready.MissingSKUs)
	if err != nil {
		return GestionarPackage{}, nil, err
	}
	ready.RecipientName = strings.TrimSpace(firstName + " " + lastName)
	if blockers := ready.Blockers(); len(blockers) != 0 {
		return GestionarPackage{}, blockers, nil
	}
	key := fmt.Sprintf("selecto:order:%d:package:v1", orderID)
	err = tx.QueryRow(ctx, `INSERT INTO gestionar_dispatches(order_id,submission_key)
		VALUES($1,$2) RETURNING order_id,submission_key,package_state`, orderID, key).
		Scan(&existing.OrderID, &existing.SubmissionKey, &existing.State)
	if err != nil {
		return GestionarPackage{}, nil, err
	}
	return existing, nil, tx.Commit(ctx)
}

// ClaimPackageAttempt permits exactly one sender. After a timeout, callers
// must reconcile the uncertain state instead of resending.
func (ledger GestionarLedger) ClaimPackageAttempt(ctx context.Context, orderID int) (bool, error) {
	var id int
	err := ledger.Pool.QueryRow(ctx, `UPDATE gestionar_dispatches d SET package_state='in_flight',
		attempted_at=NOW(),updated_at=NOW()
		FROM orders o JOIN shipments s ON s.order_id=o.id
		WHERE d.order_id=$1 AND d.order_id=o.id AND d.package_state='reserved'
		AND o.status='paid' AND s.status IN ('preparing','ready_for_dispatch')
		AND EXISTS(SELECT 1 FROM order_shipping_addresses a WHERE a.order_id=o.id)
		AND EXISTS(SELECT 1 FROM order_items i WHERE i.order_id=o.id)
		AND NOT EXISTS(SELECT 1 FROM order_items i WHERE i.order_id=o.id AND BTRIM(i.sku_snapshot)='')
		RETURNING d.order_id`, orderID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (ledger GestionarLedger) ConfirmPackage(ctx context.Context, orderID int, externalID string) error {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" || len(externalID) > 255 {
		return ErrGestionarConflict
	}
	var id int
	err := ledger.Pool.QueryRow(ctx, `UPDATE gestionar_dispatches SET package_state='confirmed',
		external_package_id=$2,confirmed_at=NOW(),last_error_code='',updated_at=NOW()
		WHERE order_id=$1 AND package_state IN ('in_flight','uncertain') RETURNING order_id`, orderID, externalID).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var current string
	if ledger.Pool.QueryRow(ctx, `SELECT external_package_id FROM gestionar_dispatches
		WHERE order_id=$1 AND package_state='confirmed'`, orderID).Scan(&current) == nil && current == externalID {
		return nil
	}
	return ErrGestionarConflict
}

func (ledger GestionarLedger) MarkPackageUncertain(ctx context.Context, orderID int, code string) error {
	if !validGestionarErrorCode(code) {
		return ErrGestionarConflict
	}
	command, err := ledger.Pool.Exec(ctx, `UPDATE gestionar_dispatches SET package_state='uncertain',
		last_error_code=$2,updated_at=NOW() WHERE order_id=$1 AND package_state='in_flight'`, orderID, code)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrGestionarConflict
	}
	return nil
}

// RejectPackage is for definitive provider rejections only. A timeout is
// uncertain, not rejected, because the provider may already have created it.
func (ledger GestionarLedger) RejectPackage(ctx context.Context, orderID int, code string) error {
	if !validGestionarErrorCode(code) {
		return ErrGestionarConflict
	}
	command, err := ledger.Pool.Exec(ctx, `UPDATE gestionar_dispatches SET package_state='rejected',
		last_error_code=$2,updated_at=NOW() WHERE order_id=$1 AND package_state='in_flight'`, orderID, code)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrGestionarConflict
	}
	return nil
}

func validGestionarErrorCode(code string) bool {
	if len(code) == 0 || len(code) > 80 {
		return false
	}
	for _, char := range code {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func (ledger GestionarLedger) Package(ctx context.Context, orderID int) (GestionarPackage, error) {
	var record GestionarPackage
	err := ledger.Pool.QueryRow(ctx, `SELECT order_id,submission_key,package_state,
		COALESCE(external_package_id,'') FROM gestionar_dispatches WHERE order_id=$1`, orderID).
		Scan(&record.OrderID, &record.SubmissionKey, &record.State, &record.ExternalPackageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GestionarPackage{}, ErrGestionarNotFound
	}
	return record, err
}
