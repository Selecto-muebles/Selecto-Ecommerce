package handlers

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
)

// The parent and first variant share the caller's transaction. Existing API
// clients can still attach a variant by family_id or create an individual SKU.
func assignProductPublication(ctx context.Context, tx pgx.Tx, input adminProductInput, actor string) (int, []byte, error) {
	familyID := 0
	if input.FamilyID != nil {
		familyID = *input.FamilyID
	}
	if input.Publication != nil {
		name := strings.TrimSpace(input.Publication.Name)
		axes, err := normalizeFamilyAxes(input.Publication.Axes)
		if familyID != 0 || err != nil || !validFamilyName(name) {
			return 0, nil, errInvalidFamily
		}
		if _, err := validateVariantValues(axes, input.VariantValues); err != nil {
			return 0, nil, err
		}
		raw, _ := json.Marshal(axes)
		if err := tx.QueryRow(ctx, "INSERT INTO product_families(name,axes) VALUES($1,$2) RETURNING id", name, raw).Scan(&familyID); err != nil {
			return 0, nil, err
		}
		if err := writeAuditTx(ctx, tx, actor, "product_family_created", "product_family", familyID, map[string]any{"name": name, "axes": axes}); err != nil {
			return 0, nil, err
		}
	}
	raw, err := familyAssignment(ctx, tx, familyID, input.VariantValues)
	return familyID, raw, err
}
