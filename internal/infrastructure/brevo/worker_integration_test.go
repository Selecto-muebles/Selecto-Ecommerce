package brevo

import (
	"Selecto-Ecommerce/internal/infrastructure/database"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

type recordingContacts struct {
	mu      sync.Mutex
	calls   []bool
	fail    bool
	entered chan struct{}
	release chan struct{}
}

func (f *recordingContacts) SetSubscription(ctx context.Context, _ string, subscribed bool) error {
	f.mu.Lock()
	f.calls = append(f.calls, subscribed)
	f.mu.Unlock()
	if f.entered != nil {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.fail {
		return errors.New("provider temporarily unavailable")
	}
	return nil
}
func workerFixture(t *testing.T) (*Worker, *pgxpool.Pool, *recordingContacts, int64, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "commerce"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	email := fmt.Sprintf("worker-%d@selecto.test", time.Now().UnixNano())
	var id int64
	if err := pool.QueryRow(context.Background(), "INSERT INTO marketing_subscriptions(email,status,consent_at) VALUES($1,'subscribed',NOW()) RETURNING id", email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM marketing_subscriptions WHERE id=$1", id) })
	client := &recordingContacts{}
	worker := NewWorker(&database.DB{Pool: pool}, client, slog.New(slog.NewTextHandler(io.Discard, nil)), 10)
	return worker, pool, client, id, email
}
func queueTestSync(t *testing.T, pool *pgxpool.Pool, id int64, email, operation string, version int64) int64 {
	t.Helper()
	var syncID int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO marketing_sync_outbox(event_key,subscription_id,email,operation,subscription_version) VALUES($1,$2,$3,$4,$5) RETURNING id`, fmt.Sprintf("worker:%d:%d:%s", id, version, operation), id, email, operation, version).Scan(&syncID); err != nil {
		t.Fatal(err)
	}
	return syncID
}
func TestSpecificTaskDoesNotClaimAnotherTaskAndSkipsObsoleteSubscribe(t *testing.T) {
	worker, pool, client, id, email := workerFixture(t)
	ctx := context.Background()
	old := queueTestSync(t, pool, id, email, "subscribe", 1)
	if _, err := pool.Exec(ctx, "UPDATE marketing_subscriptions SET status='unsubscribed',sync_version=2 WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	latest := queueTestSync(t, pool, id, email, "unsubscribe", 2)
	if err := worker.ProcessOne(ctx, latest); err != nil {
		t.Fatal(err)
	}
	if err := worker.ProcessOne(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := worker.ProcessOne(ctx, latest); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 1 || client.calls[0] {
		t.Fatalf("provider calls %v", client.calls)
	}
	var status string
	if err := pool.QueryRow(ctx, "SELECT status FROM marketing_sync_outbox WHERE id=$1", old).Scan(&status); err != nil || status != "obsolete" {
		t.Fatalf("old task %s %v", status, err)
	}
}
func TestSyncFailureRetainsSuppressionAndUsesBoundedRetries(t *testing.T) {
	worker, pool, client, id, email := workerFixture(t)
	ctx := context.Background()
	client.fail = true
	if _, err := pool.Exec(ctx, "UPDATE marketing_subscriptions SET status='unsubscribed',sync_status='suppressed',suppression_reason='spam' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	syncID := queueTestSync(t, pool, id, email, "unsubscribe", 1)
	if _, err := pool.Exec(ctx, "UPDATE marketing_sync_outbox SET attempts=7 WHERE id=$1", syncID); err != nil {
		t.Fatal(err)
	}
	if err := worker.ProcessOne(ctx, syncID); err == nil {
		t.Fatal("provider failure ignored")
	}
	var state, syncState string
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT status,attempts FROM marketing_sync_outbox WHERE id=$1", syncID).Scan(&state, &attempts); err != nil || state != "failed" || attempts != 8 {
		t.Fatalf("retry state %s %d %v", state, attempts, err)
	}
	if err := pool.QueryRow(ctx, "SELECT sync_status FROM marketing_subscriptions WHERE id=$1", id).Scan(&syncState); err != nil || syncState != "suppressed" {
		t.Fatalf("suppression lost %s %v", syncState, err)
	}
}
func TestProviderWriteSerializesWithConcurrentOptOut(t *testing.T) {
	worker, pool, client, id, email := workerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client.entered = make(chan struct{})
	client.release = make(chan struct{})
	task := queueTestSync(t, pool, id, email, "subscribe", 1)
	processed := make(chan error, 1)
	go func() { processed <- worker.ProcessOne(ctx, task) }()
	select {
	case <-client.entered:
	case <-ctx.Done():
		t.Fatal("worker did not reach provider")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		close(client.release)
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
		close(client.release)
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, "UPDATE marketing_subscriptions SET status='unsubscribed',sync_version=2 WHERE id=$1", id)
	if err == nil {
		close(client.release)
		t.Fatal("optout committed while subscribe still in flight")
	}
	_ = tx.Rollback(ctx)
	close(client.release)
	if err := <-processed; err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE marketing_subscriptions SET status='unsubscribed',sync_version=2 WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	client.entered = nil
	optout := queueTestSync(t, pool, id, email, "unsubscribe", 2)
	if err := worker.ProcessOne(ctx, optout); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 2 || !client.calls[0] || client.calls[1] {
		t.Fatalf("incorrect ordering %v", client.calls)
	}
}
