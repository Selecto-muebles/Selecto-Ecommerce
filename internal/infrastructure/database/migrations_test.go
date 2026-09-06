package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMigrationDefinitionsAreSortedAndChecksummed(t *testing.T) {
	definitions, err := migrationDefinitions()
	if err != nil {
		t.Fatalf("migrationDefinitions() error = %v", err)
	}
	if len(definitions) != 12 {
		t.Fatalf("migration count = %d, want 12", len(definitions))
	}
	for index, migration := range definitions {
		if len(migration.Checksum) != 64 || migration.Content == "" {
			t.Fatalf("invalid migration metadata: %+v", migration)
		}
		if index > 0 && definitions[index-1].Version >= migration.Version {
			t.Fatalf("migrations are not sorted: %q then %q", definitions[index-1].Version, migration.Version)
		}
	}
}

type schemaTx struct {
	pgx.Tx
	schema string
}

func (tx schemaTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return stringRow(tx.schema)
}

type stringRow string

func (row stringRow) Scan(dest ...any) error {
	*(dest[0].(*string)) = string(row)
	return nil
}

func TestCommerceMigrationCompatibilityAvoidsOwnerPrivileges(t *testing.T) {
	definitions, err := migrationDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range definitions {
		if migration.Version != "010_marketing_subscriptions.sql" {
			continue
		}
		content, err := executableMigrationContent(context.Background(), schemaTx{schema: "commerce"}, migration)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(content, "CREATE SCHEMA") || strings.Contains(content, "public.marketing_subscriptions") {
			t.Fatalf("compatibility SQL requires cross-schema privileges: %s", content)
		}
		if !strings.Contains(content, "CREATE TABLE IF NOT EXISTS commerce.marketing_subscriptions") {
			t.Fatal("compatibility SQL lost the marketing table definition")
		}
		return
	}
	t.Fatal("marketing migration not found")
}
