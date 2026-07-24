package postgres

import (
	"context"
	"time"
)

func (repository *OrderRepository) ReleaseExpired(ctx context.Context, olderThan time.Duration, batchSize int, writeAudit bool) (int64, error) {
	tx, err := repository.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `SELECT id FROM orders
		WHERE status='pending'
		AND COALESCE(expires_at, created_at + make_interval(secs => $1)) < NOW()
		ORDER BY COALESCE(expires_at, created_at + make_interval(secs => $1)), id
		LIMIT $2 FOR UPDATE SKIP LOCKED`, int(olderThan.Seconds()), batchSize)
	if err != nil {
		return 0, err
	}
	orderIDs := make([]int, 0, batchSize)
	for rows.Next() {
		var orderID int
		if err := rows.Scan(&orderID); err != nil {
			rows.Close()
			return 0, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	for _, orderID := range orderIDs {
		if err := RestoreOrderStock(ctx, tx, orderID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, "UPDATE orders SET status='cancelled', cancelled_at=NOW() WHERE id=$1", orderID); err != nil {
			return 0, err
		}
		if writeAudit {
			if _, err := tx.Exec(ctx, `INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata)
				VALUES ('system', 'order_reservation_expired', 'order', $1, '{}')`, orderID); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(orderIDs)), nil
}
