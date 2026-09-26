package database

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

func auditCommunicationOperations(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	definitions := []struct {
		schema, table string
		columns       []string
	}{
		{schema, "categories", []string{"updated_at"}},
		{schema, "product_reviews", []string{"moderation_note"}},
		{schema, "shipments", []string{"status_changed_at"}},
		{schema, "email_outbox", []string{"delivery_status", "provider_message_id", "delivered_at", "last_provider_event_at", "bounce_reason"}},
		{"commerce", "marketing_subscriptions", []string{"sync_status", "sync_version", "sync_attempts", "sync_error", "synced_at", "last_provider_event_at", "suppression_reason"}},
	}
	for _, item := range definitions {
		if err := auditTableColumns(ctx, pool, item.schema, item.table, item.columns); err != nil {
			return err
		}
	}
	if err := auditValidatedConstraints(ctx, pool, schema, []string{"email_outbox_delivery_status_check"}); err != nil {
		return err
	}
	if err := auditValidatedConstraints(ctx, pool, "commerce", []string{"marketing_subscriptions_sync_status_check"}); err != nil {
		return err
	}
	var allowsOptOut bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname=$1 AND c.conname='email_outbox_template_check' AND pg_get_constraintdef(c.oid) LIKE '%newsletter_unsubscribe%')`, schema).Scan(&allowsOptOut); err != nil {
		return err
	}
	if !allowsOptOut {
		return fmt.Errorf("email outbox must support verified newsletter opt-out")
	}
	return nil
}
