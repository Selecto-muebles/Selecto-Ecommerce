package brevo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/jackc/pgx/v5"
)

var ErrSyncNotReady = errors.New("marketing sync is not ready for processing")

type Worker struct {
	db        *database.DB
	client    ContactClient
	logger    *slog.Logger
	batchSize int
}

type syncItem struct {
	ID                  int64
	SubscriptionID      int64
	Email               string
	Operation           string
	SubscriptionVersion int64
	Attempts            int
}

func NewWorker(db *database.DB, client ContactClient, logger *slog.Logger, batchSize int) *Worker {
	return &Worker{db: db, client: client, logger: logger, batchSize: batchSize}
}

func (worker *Worker) ProcessBatch(ctx context.Context) (int, error) {
	items, err := worker.claimBatch(ctx, worker.batchSize)
	if err != nil {
		return 0, err
	}
	processed := 0
	var firstError error
	for _, item := range items {
		if err := worker.processClaimed(ctx, item); err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		processed++
	}
	return processed, firstError
}

func (worker *Worker) ProcessOne(ctx context.Context, id int64) error {
	item, claimed, err := worker.claimOne(ctx, id)
	if err != nil {
		return err
	}
	if !claimed {
		return worker.resolveUnclaimed(ctx, id)
	}
	return worker.processClaimed(ctx, item)
}

func (worker *Worker) processClaimed(ctx context.Context, item syncItem) error {
	tx, err := worker.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentVersion int64
	err = tx.QueryRow(ctx, `SELECT sync_version FROM commerce.marketing_subscriptions WHERE id=$1 FOR UPDATE`, item.SubscriptionID).Scan(&currentVersion)
	if err != nil {
		_ = tx.Rollback(ctx)
		worker.fail(ctx, item, err)
		return err
	}
	if currentVersion != item.SubscriptionVersion {
		if _, err = tx.Exec(ctx, `UPDATE commerce.marketing_sync_outbox SET status='obsolete',completed_at=NOW(),locked_at=NULL,updated_at=NOW() WHERE id=$1`, item.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	subscribed := item.Operation == "subscribe"
	if err := worker.client.SetSubscription(ctx, item.Email, subscribed); err != nil {
		_ = tx.Rollback(ctx)
		worker.fail(ctx, item, err)
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE commerce.marketing_sync_outbox SET status='succeeded',completed_at=NOW(),locked_at=NULL,last_error='',updated_at=NOW() WHERE id=$1`, item.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE commerce.marketing_subscriptions SET sync_status=CASE WHEN suppression_reason<>'' THEN 'suppressed' ELSE 'synced' END,sync_error='',synced_at=NOW(),updated_at=NOW()
	 WHERE id=$1 AND sync_version=$2`, item.SubscriptionID, item.SubscriptionVersion); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	worker.logger.Info("marketing_contact_synced", "subscription_id", item.SubscriptionID, "operation", item.Operation)
	return nil
}

func (worker *Worker) claimBatch(ctx context.Context, limit int) ([]syncItem, error) {
	rows, err := worker.db.Pool.Query(ctx, claimSyncSQL+` LIMIT $1 FOR UPDATE SKIP LOCKED)
	 UPDATE commerce.marketing_sync_outbox o SET status='processing',locked_at=NOW(),attempts=o.attempts+1,updated_at=NOW()
	 FROM candidate c WHERE o.id=c.id RETURNING o.id,o.subscription_id,o.email,o.operation,o.subscription_version,o.attempts`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []syncItem{}
	for rows.Next() {
		var item syncItem
		if err := rows.Scan(&item.ID, &item.SubscriptionID, &item.Email, &item.Operation, &item.SubscriptionVersion, &item.Attempts); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const claimSyncSQL = `WITH candidate AS (SELECT id FROM commerce.marketing_sync_outbox
 WHERE (status='pending' AND next_attempt_at<=NOW()) OR (status='processing' AND locked_at<NOW()-INTERVAL '5 minutes')
 ORDER BY id`

func (worker *Worker) claimOne(ctx context.Context, id int64) (syncItem, bool, error) {
	var item syncItem
	err := worker.db.Pool.QueryRow(ctx, `WITH candidate AS (SELECT id FROM commerce.marketing_sync_outbox
	 WHERE id=$1 AND ((status='pending' AND next_attempt_at<=NOW()) OR (status='processing' AND locked_at<NOW()-INTERVAL '5 minutes')) FOR UPDATE SKIP LOCKED)
	 UPDATE commerce.marketing_sync_outbox o SET status='processing',locked_at=NOW(),attempts=o.attempts+1,updated_at=NOW()
	 FROM candidate c WHERE o.id=c.id AND o.id=$1 RETURNING o.id,o.subscription_id,o.email,o.operation,o.subscription_version,o.attempts`, id).Scan(
		&item.ID, &item.SubscriptionID, &item.Email, &item.Operation, &item.SubscriptionVersion, &item.Attempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return syncItem{}, false, nil
	}
	return item, err == nil, err
}

func (worker *Worker) resolveUnclaimed(ctx context.Context, id int64) error {
	var status string
	err := worker.db.Pool.QueryRow(ctx, `SELECT status FROM commerce.marketing_sync_outbox WHERE id=$1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || status == "succeeded" || status == "failed" || status == "obsolete" {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrSyncNotReady
}

func (worker *Worker) fail(ctx context.Context, item syncItem, syncErr error) {
	status := "pending"
	if item.Attempts >= 8 {
		status = "failed"
	}
	_, err := worker.db.Pool.Exec(ctx, `UPDATE commerce.marketing_sync_outbox SET status=$2,
	 next_attempt_at=NOW()+make_interval(secs=>LEAST(1800,(POWER(2,attempts)::INTEGER*5))),
	 locked_at=NULL,last_error=LEFT($3,1000),updated_at=NOW() WHERE id=$1`, item.ID, status, syncErr.Error())
	if err == nil {
		_, err = worker.db.Pool.Exec(ctx, `UPDATE commerce.marketing_subscriptions SET sync_status=CASE WHEN suppression_reason<>'' THEN 'suppressed' ELSE $2 END,sync_attempts=sync_attempts+1,
	 sync_error=LEFT($3,1000),updated_at=NOW() WHERE id=$1 AND sync_version=$4`, item.SubscriptionID, status, syncErr.Error(), item.SubscriptionVersion)
	}
	if err != nil {
		worker.logger.Error("marketing_sync_retry_failed", "sync_id", item.ID, "error", err)
	}
	worker.logger.Warn("marketing_contact_sync_failed", "sync_id", item.ID, "error", fmt.Sprint(syncErr))
}
