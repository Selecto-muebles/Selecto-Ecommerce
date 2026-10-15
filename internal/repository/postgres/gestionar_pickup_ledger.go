package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type GestionarPickupBatch struct {
	ID           int64
	RequestKey   string
	PickupTimeID int
	State        string
	ExternalID   string
}

// ReservePickupBatch groups confirmed packages into one daily pickup.
// It is a local reservation; no provider request is sent here.
func (ledger GestionarLedger) ReservePickupBatch(ctx context.Context, orderIDs []int, pickupTimeID int) (GestionarPickupBatch, error) {
	if pickupTimeID <= 0 || len(orderIDs) == 0 || len(orderIDs) > 100 {
		return GestionarPickupBatch{}, ErrGestionarConflict
	}
	ids := slices.Clone(orderIDs)
	slices.Sort(ids)
	for i, id := range ids {
		if id <= 0 || (i > 0 && id == ids[i-1]) {
			return GestionarPickupBatch{}, ErrGestionarConflict
		}
	}
	keyMaterial := fmt.Sprintf("%d:%v", pickupTimeID, ids)
	digest := sha256.Sum256([]byte(keyMaterial))
	key := "selecto:pickup:v1:" + hex.EncodeToString(digest[:])
	tx, err := ledger.Pool.Begin(ctx)
	if err != nil {
		return GestionarPickupBatch{}, err
	}
	defer tx.Rollback(ctx)
	var batch GestionarPickupBatch
	err = tx.QueryRow(ctx, `SELECT id,request_key,pickup_time_id,state,
		COALESCE(external_pickup_id,'') FROM gestionar_pickup_batches WHERE request_key=$1`, key).
		Scan(&batch.ID, &batch.RequestKey, &batch.PickupTimeID, &batch.State, &batch.ExternalID)
	if err == nil {
		return batch, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return GestionarPickupBatch{}, err
	}
	// Lock in canonical order so overlapping batches cannot race or deadlock.
	for _, id := range ids {
		var state string
		err := tx.QueryRow(ctx, `SELECT package_state FROM gestionar_dispatches
			WHERE order_id=$1 FOR UPDATE`, id).Scan(&state)
		if err != nil || state != "confirmed" {
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return GestionarPickupBatch{}, err
			}
			return GestionarPickupBatch{}, ErrGestionarConflict
		}
		var assigned bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gestionar_pickup_orders WHERE order_id=$1)`, id).Scan(&assigned); err != nil {
			return GestionarPickupBatch{}, err
		}
		if assigned {
			if tx.QueryRow(ctx, `SELECT id,request_key,pickup_time_id,state,
				COALESCE(external_pickup_id,'') FROM gestionar_pickup_batches WHERE request_key=$1`, key).
				Scan(&batch.ID, &batch.RequestKey, &batch.PickupTimeID, &batch.State, &batch.ExternalID) == nil {
				return batch, tx.Commit(ctx)
			}
			return GestionarPickupBatch{}, ErrGestionarConflict
		}
	}
	err = tx.QueryRow(ctx, `INSERT INTO gestionar_pickup_batches(request_key,pickup_time_id)
		VALUES($1,$2) ON CONFLICT(request_key) DO NOTHING
		RETURNING id,request_key,pickup_time_id,state`, key, pickupTimeID).
		Scan(&batch.ID, &batch.RequestKey, &batch.PickupTimeID, &batch.State)
	if errors.Is(err, pgx.ErrNoRows) {
		// Same batch won the race in another transaction.
		err = tx.QueryRow(ctx, `SELECT id,request_key,pickup_time_id,state,
			COALESCE(external_pickup_id,'') FROM gestionar_pickup_batches WHERE request_key=$1`, key).
			Scan(&batch.ID, &batch.RequestKey, &batch.PickupTimeID, &batch.State, &batch.ExternalID)
		if err != nil {
			return GestionarPickupBatch{}, err
		}
		return batch, tx.Commit(ctx)
	}
	if err != nil {
		return GestionarPickupBatch{}, err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO gestionar_pickup_orders(batch_id,order_id) VALUES($1,$2)`, batch.ID, id); err != nil {
			return GestionarPickupBatch{}, err
		}
	}
	return batch, tx.Commit(ctx)
}

func (ledger GestionarLedger) ClaimPickupAttempt(ctx context.Context, batchID int64) (bool, error) {
	var id int64
	err := ledger.Pool.QueryRow(ctx, `UPDATE gestionar_pickup_batches b SET state='in_flight',
		attempted_at=NOW(),updated_at=NOW() WHERE b.id=$1 AND b.state='reserved'
		AND EXISTS(SELECT 1 FROM gestionar_pickup_orders WHERE batch_id=b.id)
		AND NOT EXISTS(
			SELECT 1 FROM gestionar_pickup_orders po
			JOIN gestionar_dispatches d ON d.order_id=po.order_id
			JOIN orders o ON o.id=po.order_id
			LEFT JOIN shipments s ON s.order_id=o.id
			WHERE po.batch_id=b.id AND (d.package_state<>'confirmed' OR o.status<>'paid'
				OR s.status IS NULL OR s.status NOT IN ('preparing','ready_for_dispatch'))
		) RETURNING b.id`, batchID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (ledger GestionarLedger) MarkPickupUncertain(ctx context.Context, batchID int64, code string) error {
	if !validGestionarErrorCode(code) {
		return ErrGestionarConflict
	}
	command, err := ledger.Pool.Exec(ctx, `UPDATE gestionar_pickup_batches SET state='uncertain',
		last_error_code=$2,updated_at=NOW() WHERE id=$1 AND state='in_flight'`, batchID, code)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrGestionarConflict
	}
	return nil
}

func (ledger GestionarLedger) RejectPickup(ctx context.Context, batchID int64, code string) error {
	if !validGestionarErrorCode(code) {
		return ErrGestionarConflict
	}
	command, err := ledger.Pool.Exec(ctx, `UPDATE gestionar_pickup_batches SET state='rejected',
		last_error_code=$2,updated_at=NOW() WHERE id=$1 AND state='in_flight'`, batchID, code)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrGestionarConflict
	}
	return nil
}

func (ledger GestionarLedger) ConfirmPickup(ctx context.Context, batchID int64, externalID string) error {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" || len(externalID) > 255 {
		return ErrGestionarConflict
	}
	var id int64
	err := ledger.Pool.QueryRow(ctx, `UPDATE gestionar_pickup_batches SET state='confirmed',
		external_pickup_id=$2,confirmed_at=NOW(),last_error_code='',updated_at=NOW()
		WHERE id=$1 AND state IN ('in_flight','uncertain') RETURNING id`, batchID, externalID).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var current string
	if ledger.Pool.QueryRow(ctx, `SELECT external_pickup_id FROM gestionar_pickup_batches
		WHERE id=$1 AND state='confirmed'`, batchID).Scan(&current) == nil && current == externalID {
		return nil
	}
	return ErrGestionarConflict
}

// RecordProviderStatus stores a deduplicated raw status. Mapping it onto a
// customer-visible shipment awaits Gestionar's authenticated status contract.
func (ledger GestionarLedger) RecordProviderStatus(ctx context.Context, orderID int, eventKey, status string, occurredAt *time.Time) error {
	eventKey, status = strings.TrimSpace(eventKey), strings.TrimSpace(status)
	if eventKey == "" || len(eventKey) > 255 || status == "" || len(status) > 120 {
		return ErrGestionarConflict
	}
	var id int64
	err := ledger.Pool.QueryRow(ctx, `INSERT INTO gestionar_status_events
		(order_id,event_key,external_status,occurred_at)
		SELECT order_id,$2,$3,$4 FROM gestionar_dispatches
		WHERE order_id=$1 AND package_state='confirmed'
		ON CONFLICT(event_key) DO NOTHING RETURNING id`, orderID, eventKey, status, occurredAt).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var existingOrder int
	var existingStatus string
	if ledger.Pool.QueryRow(ctx, `SELECT order_id,external_status FROM gestionar_status_events
		WHERE event_key=$1`, eventKey).Scan(&existingOrder, &existingStatus) == nil &&
		existingOrder == orderID && existingStatus == status {
		return nil
	}
	return ErrGestionarConflict
}
