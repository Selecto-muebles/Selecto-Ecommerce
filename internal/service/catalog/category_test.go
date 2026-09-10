package catalog

import "testing"

func TestNormalizeCategory(t *testing.T) {
	item, err := NormalizeCategory("  Calistenia  ", " CALISTENIA ")
	if err != nil || item.Name != "Calistenia" || item.Slug != "calistenia" {
		t.Fatalf("unexpected: %+v %v", item, err)
	}
	for _, slug := range []string{"", "../otra", "dos palabras", "-inicio", "fin-", "doble--guion"} {
		if _, err := NormalizeCategory("Categoría", slug); err == nil {
			t.Errorf("accepted %q", slug)
		}
	}
	if _, err := NormalizeCategory(" ", "valido"); err == nil {
		t.Fatal("accepted empty name")
	}
}
