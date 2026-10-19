package postgres

import (
	"database/sql"
	"testing"
)

func TestNullablePreferencePreservesMarketplaceMode(t *testing.T) {
	preference := nullablePreference(
		sql.NullString{String: "pref-seller", Valid: true},
		sql.NullString{String: "https://www.mercadopago.com.ar/checkout", Valid: true},
		sql.NullString{String: "production", Valid: true},
		true,
	)
	if preference == nil || !preference.Marketplace || preference.ID != "pref-seller" {
		t.Fatalf("marketplace preference was not recovered: %+v", preference)
	}
}
