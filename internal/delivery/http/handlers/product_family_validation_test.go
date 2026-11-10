package handlers

import (
	"strings"
	"testing"
)

func TestFamilyValidationCountsCharactersInsteadOfUTF8Bytes(t *testing.T) {
	if !validFamilyName(strings.Repeat("á", 120)) || validFamilyName(strings.Repeat("á", 121)) {
		t.Fatal("publication names must use the same character limits as PostgreSQL and the form")
	}
	axis := strings.Repeat("é", 40)
	axes, err := normalizeFamilyAxes([]string{axis})
	if err != nil {
		t.Fatalf("accented axis rejected: %v", err)
	}
	if _, err = validateVariantValues(axes, map[string]string{axis: strings.Repeat("ñ", 80)}); err != nil {
		t.Fatalf("accented value rejected: %v", err)
	}
	if _, err = validateVariantValues(axes, map[string]string{axis: strings.Repeat("ñ", 81)}); err == nil {
		t.Fatal("oversized value accepted")
	}
}
