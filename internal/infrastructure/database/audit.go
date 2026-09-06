package database

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditReport struct {
	Tables     int
	Migrations int
	Indexes    int
}

var requiredCommerceColumns = map[string][]string{
	"schema_migrations":        {"version", "checksum", "applied_at"},
	"products":                 {"id", "name", "price", "stock", "active", "sku", "description", "category", "created_at", "updated_at"},
	"users":                    {"id", "email", "password", "role", "first_name", "last_name", "dni", "street_address", "street_number", "postal_code", "province", "locality", "phone_number", "email_verified_at", "session_version"},
	"orders":                   {"id", "user_id", "status", "total", "created_at", "expires_at", "paid_at", "cancelled_at", "payment_status", "payment_id", "active_payment_preference_id", "active_checkout_url", "active_payment_environment", "idempotency_key", "request_hash", "payment_provider", "provider_payment_id"},
	"order_items":              {"id", "order_id", "product_id", "quantity", "price", "selected_options"},
	"payment_webhook_events":   {"id", "event_key", "payment_id", "order_id", "status", "amount_cents", "received_at", "processed_at", "result", "payment_provider", "provider_payment_id"},
	"audit_logs":               {"id", "actor_email", "action", "entity_type", "entity_id", "metadata", "created_at"},
	"product_images":           {"id", "product_id", "mime_type", "alt_text", "sort_order", "content", "size_bytes", "created_at"},
	"product_options":          {"id", "product_id", "name", "values", "sort_order"},
	"user_identities":          {"id", "user_id", "provider", "provider_subject", "provider_email", "created_at"},
	"order_shipping_addresses": {"order_id", "recipient_first_name", "recipient_last_name", "dni", "street_address", "street_number", "postal_code", "province", "locality", "phone_number", "requested_delivery_date", "created_at"},
	"shipments":                {"id", "order_id", "status", "carrier", "tracking_number", "tracking_url", "estimated_delivery_at", "shipped_at", "delivered_at", "customer_note", "created_at", "updated_at"},
	"account_tokens":           {"id", "user_id", "purpose", "token_hash", "expires_at", "consumed_at", "created_at"},
	"email_outbox":             {"id", "event_key", "recipient", "template", "payload", "status", "attempts", "next_attempt_at", "locked_at", "sent_at", "last_error", "created_at", "updated_at"},
	"marketing_subscriptions":  {"id", "email", "status", "source", "consent_at", "unsubscribed_at", "created_at", "updated_at"},
}

var requiredCommerceIndexes = []string{
	"idx_orders_user_id",
	"idx_orders_status_expires_at",
	"idx_order_items_order_id",
	"idx_products_active_created_at",
	"idx_users_dni_unique",
	"idx_email_outbox_due",
	"idx_orders_user_idempotency_key",
	"idx_marketing_subscriptions_email",
	"idx_orders_provider_payment_id",
}

var requiredCommerceConstraints = []string{
	"orders_status_check",
	"orders_payment_status_check",
	"product_options_name_not_blank",
	"product_options_values_array",
}

func AuditSchema(ctx context.Context, pool *pgxpool.Pool) (AuditReport, error) {
	currentSchema, err := databaseSchema(ctx, pool)
	if err != nil {
		return AuditReport{}, err
	}
	if err := auditRoleIsolation(ctx, pool); err != nil {
		return AuditReport{}, err
	}
	for table, columns := range requiredCommerceColumns {
		schema := currentSchema
		if table == "marketing_subscriptions" {
			schema = "commerce"
		}
		if err := auditTableColumns(ctx, pool, schema, table, columns); err != nil {
			return AuditReport{}, err
		}
	}
	if err := auditMigrationChecksums(ctx, pool); err != nil {
		return AuditReport{}, err
	}
	if err := auditNamedIndexes(ctx, pool, currentSchema, requiredCommerceIndexes); err != nil {
		return AuditReport{}, err
	}
	if err := auditValidatedConstraints(ctx, pool, currentSchema, requiredCommerceConstraints); err != nil {
		return AuditReport{}, err
	}
	if err := auditCommerceColumnContracts(ctx, pool, currentSchema); err != nil {
		return AuditReport{}, err
	}
	return AuditReport{Tables: len(requiredCommerceColumns), Migrations: 12, Indexes: len(requiredCommerceIndexes)}, nil
}

func databaseSchema(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var schema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		return "", fmt.Errorf("resolve current database schema: %w", err)
	}
	if schema != "public" && schema != "commerce" {
		return "", fmt.Errorf("unexpected commerce database schema %q", schema)
	}
	return schema, nil
}

func auditRoleIsolation(ctx context.Context, pool *pgxpool.Pool) error {
	var superuser, paymentsUsage bool
	err := pool.QueryRow(ctx, `SELECT role_record.rolsuper,
		EXISTS (
			SELECT 1 FROM pg_catalog.pg_namespace namespace
			WHERE namespace.nspname = 'payments'
			  AND has_schema_privilege(current_user, namespace.oid, 'USAGE')
		)
		FROM pg_catalog.pg_roles role_record
		WHERE role_record.rolname = current_user`).Scan(&superuser, &paymentsUsage)
	if err != nil {
		return fmt.Errorf("audit commerce role: %w", err)
	}
	if superuser {
		return fmt.Errorf("commerce runtime role must not be a superuser")
	}
	if paymentsUsage {
		return fmt.Errorf("commerce runtime role must not have USAGE on payments schema")
	}
	return nil
}

func auditTableColumns(ctx context.Context, pool *pgxpool.Pool, schema, table string, required []string) error {
	rows, err := pool.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema=$1 AND table_name=$2`, schema, table)
	if err != nil {
		return fmt.Errorf("audit table %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return fmt.Errorf("scan columns for %s.%s: %w", schema, table, err)
		}
		found[column] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read columns for %s.%s: %w", schema, table, err)
	}
	missing := []string{}
	for _, column := range required {
		if !found[column] {
			missing = append(missing, column)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("table %s.%s is missing columns: %s", schema, table, strings.Join(missing, ", "))
	}
	return nil
}

func auditMigrationChecksums(ctx context.Context, pool *pgxpool.Pool) error {
	definitions, err := migrationDefinitions()
	if err != nil {
		return err
	}
	for _, migration := range definitions {
		var checksum string
		if err := pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", migration.Version).Scan(&checksum); err != nil {
			return fmt.Errorf("migration %s is not registered: %w", migration.Version, err)
		}
		if checksum != migration.Checksum {
			return fmt.Errorf("migration %s checksum mismatch", migration.Version)
		}
	}
	return nil
}

func auditNamedIndexes(ctx context.Context, pool *pgxpool.Pool, schema string, required []string) error {
	for _, name := range required {
		indexSchema := schema
		if name == "idx_marketing_subscriptions_email" {
			indexSchema = "commerce"
		}
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_indexes WHERE schemaname=$1 AND indexname=$2
		)`, indexSchema, name).Scan(&exists); err != nil {
			return fmt.Errorf("audit index %s: %w", name, err)
		}
		if !exists {
			return fmt.Errorf("required index %s.%s is missing", indexSchema, name)
		}
	}
	return nil
}

func auditValidatedConstraints(ctx context.Context, pool *pgxpool.Pool, schema string, required []string) error {
	for _, name := range required {
		var valid bool
		err := pool.QueryRow(ctx, `SELECT constraint_record.convalidated
			FROM pg_catalog.pg_constraint constraint_record
			JOIN pg_catalog.pg_class relation ON relation.oid=constraint_record.conrelid
			JOIN pg_catalog.pg_namespace namespace ON namespace.oid=relation.relnamespace
			WHERE namespace.nspname=$1 AND constraint_record.conname=$2`, schema, name).Scan(&valid)
		if err != nil {
			return fmt.Errorf("audit constraint %s: %w", name, err)
		}
		if !valid {
			return fmt.Errorf("required constraint %s is not validated", name)
		}
	}
	return nil
}

func auditCommerceColumnContracts(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	var sessionNullable, sessionDefault string
	err := pool.QueryRow(ctx, `SELECT is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='users' AND column_name='session_version'`, schema).Scan(&sessionNullable, &sessionDefault)
	if err != nil || sessionNullable != "NO" || !strings.Contains(sessionDefault, "0") {
		return fmt.Errorf("users.session_version must be NOT NULL with default 0")
	}
	var webhookNullable string
	err = pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='payment_webhook_events' AND column_name='payment_id'`, schema).Scan(&webhookNullable)
	if err != nil || webhookNullable != "YES" {
		return fmt.Errorf("payment_webhook_events.payment_id must be nullable")
	}
	var legacyMarketing bool
	err = pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_catalog.pg_class relation
		JOIN pg_catalog.pg_namespace namespace ON namespace.oid=relation.relnamespace
		WHERE namespace.nspname='public' AND relation.relname='marketing_subscriptions'
		  AND relation.relkind IN ('r', 'p')
	)`).Scan(&legacyMarketing)
	if err != nil {
		return fmt.Errorf("audit legacy marketing table: %w", err)
	}
	if legacyMarketing {
		return fmt.Errorf("legacy public.marketing_subscriptions table still exists")
	}
	return nil
}
