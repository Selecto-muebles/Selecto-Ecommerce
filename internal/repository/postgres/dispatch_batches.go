package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	shipping "Selecto-Ecommerce/internal/service/shipping"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dispatchBatchLock int64 = 731402027
const maxDispatchPrepareOrders = 10000

type DispatchBatchStore struct{ Pool *pgxpool.Pool }

// PrepareDueBatches is idempotent across concurrent job/admin calls. It never
// contacts a carrier or changes any order or shipment. Later runs catch orders
// that were not yet paid when the previous invocation began.
func (store DispatchBatchStore) PrepareDueBatches(ctx context.Context, now time.Time, cutoffHour int) (int, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", dispatchBatchLock); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT o.id, COALESCE(o.paid_at,o.created_at), u.email, COALESCE(s.status,'preparing')
		FROM orders o JOIN users u ON u.id=o.user_id
		LEFT JOIN shipments s ON s.order_id=o.id
		LEFT JOIN gestionar_dispatches gd ON gd.order_id=o.id
		LEFT JOIN dispatch_cutoff_batch_orders bo ON bo.order_id=o.id
		WHERE o.status='paid' AND bo.order_id IS NULL
			AND (s.id IS NULL OR s.status IN ('preparing','ready_for_dispatch'))
			AND (gd.order_id IS NULL OR gd.package_state='rejected')
		ORDER BY COALESCE(o.paid_at,o.created_at),o.id LIMIT $1`, maxDispatchPrepareOrders)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		orderID        int
		cutoff         time.Time
		email          string
		shipmentStatus string
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var orderID int
		var paidAt time.Time
		var email, shipmentStatus string
		if err := rows.Scan(&orderID, &paidAt, &email, &shipmentStatus); err != nil {
			rows.Close()
			return 0, err
		}
		cutoff, err := shipping.DispatchCutoff(paidAt, cutoffHour)
		if err != nil {
			rows.Close()
			return 0, err
		}
		if !cutoff.After(now) {
			candidates = append(candidates, candidate{orderID: orderID, cutoff: cutoff, email: email, shipmentStatus: shipmentStatus})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	batchIDs := map[time.Time]int64{}
	assigned := 0
	for _, entry := range candidates {
		cutoff := entry.cutoff
		for !cutoff.After(now) {
			var sealed bool
			err := tx.QueryRow(ctx, `SELECT sealed_at IS NOT NULL FROM dispatch_cutoff_batches WHERE cutoff_at=$1`, cutoff).Scan(&sealed)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return 0, err
			}
			if !sealed {
				break
			}
			cutoff, err = shipping.DispatchCutoff(cutoff, cutoffHour)
			if err != nil {
				return 0, err
			}
		}
		if cutoff.After(now) {
			continue // A sealed cutoff's late payment waits for the next due slot.
		}
		batchID, ok := batchIDs[cutoff]
		if !ok {
			if err := tx.QueryRow(ctx, `INSERT INTO dispatch_cutoff_batches(cutoff_at)
				VALUES($1) ON CONFLICT(cutoff_at) DO UPDATE SET cutoff_at=EXCLUDED.cutoff_at
				RETURNING id`, cutoff).Scan(&batchID); err != nil {
				return 0, fmt.Errorf("create dispatch batch: %w", err)
			}
			batchIDs[cutoff] = batchID
		}
		result, err := tx.Exec(ctx, `INSERT INTO dispatch_cutoff_batch_orders(batch_id,order_id,email_snapshot,shipment_status_snapshot)
			VALUES($1,$2,$3,$4) ON CONFLICT(order_id) DO NOTHING`, batchID, entry.orderID, entry.email, entry.shipmentStatus)
		if err != nil {
			return 0, fmt.Errorf("assign dispatch order: %w", err)
		}
		assigned += int(result.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return assigned, nil
}

// SealBatch freezes membership before the CSV is read. It shares the advisory
// lock with PrepareDueBatches, so an in-flight preparation cannot append rows.
func (store DispatchBatchStore) SealBatch(ctx context.Context, id int64) (bool, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", dispatchBatchLock); err != nil {
		return false, err
	}
	var exists int64
	if err := tx.QueryRow(ctx, `UPDATE dispatch_cutoff_batches SET sealed_at=COALESCE(sealed_at,NOW()) WHERE id=$1 RETURNING id`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, tx.Commit(ctx)
}

type DispatchBatch struct {
	ID         int64      `json:"id"`
	CutoffAt   time.Time  `json:"cutoff_at"`
	OrderCount int        `json:"order_count"`
	SealedAt   *time.Time `json:"sealed_at"`
}

func (store DispatchBatchStore) ListBatches(ctx context.Context, limit int) ([]DispatchBatch, error) {
	rows, err := store.Pool.Query(ctx, `SELECT b.id,b.cutoff_at,COUNT(bo.order_id)::int,b.sealed_at
		FROM dispatch_cutoff_batches b JOIN dispatch_cutoff_batch_orders bo ON bo.batch_id=b.id
		GROUP BY b.id ORDER BY b.cutoff_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batches := []DispatchBatch{}
	for rows.Next() {
		var batch DispatchBatch
		if err := rows.Scan(&batch.ID, &batch.CutoffAt, &batch.OrderCount, &batch.SealedAt); err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}
