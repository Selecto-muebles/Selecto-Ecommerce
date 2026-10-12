package handlers

import (
	"strings"
	"testing"

	migrationfiles "Selecto-Ecommerce/migrations"
)

func TestApprovedLegalTextSplitsAndReassemblesExactly(t *testing.T) {
	body, err := migrationfiles.Files.ReadFile("selecto-terms-v1.md")
	if err != nil {
		t.Fatal(err)
	}
	sections := splitLegalSections(string(body))
	if got := composeLegalSections(sections); got != string(body) {
		t.Fatal("legal editor changed the approved source while parsing")
	}
	articles := 0
	for _, section := range sections {
		if section.Editable {
			articles++
		}
	}
	if articles != 46 {
		t.Fatalf("editable article count = %d, want 46", articles)
	}
	for index := range sections {
		if strings.Contains(sections[index].Heading, "Artículo 7.") {
			sections[index].Body = "\n\nMedio de pago actualizado.\n\n"
			break
		}
	}
	updated := composeLegalSections(sections)
	if !strings.Contains(updated, "Medio de pago actualizado.") || !strings.Contains(updated, "Artículo 8. Facturación") {
		t.Fatal("editing one article lost another article")
	}
}

func TestLegalVersionIncrements(t *testing.T) {
	if nextLegalVersion("1.0") != "1.1" || nextLegalVersion("1.9") != "1.10" {
		t.Fatal("legal version was not incremented")
	}
}

func TestRemoveLegalArticleRenumbersWithoutTouchingOtherText(t *testing.T) {
	body, err := migrationfiles.Files.ReadFile("selecto-terms-v1.md")
	if err != nil {
		t.Fatal(err)
	}
	sections := splitLegalSections(string(body))
	index := -1
	for i, section := range sections {
		if strings.HasPrefix(section.Heading, "### Artículo 7.") {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("article 7 not found")
	}
	remaining := removeAndRenumberLegalArticle(sections, index)
	updated := composeLegalSections(remaining)
	if len(remaining) != len(sections)-1 || strings.Contains(updated, "### Artículo 7. Medios de pago") || !strings.Contains(updated, "### Artículo 7. Facturación") || !strings.Contains(updated, "### Artículo 45. Contacto oficial") {
		t.Fatal("article removal did not preserve and renumber the remaining text")
	}
	if !strings.Contains(updated, "# CAPÍTULO I — TÉRMINOS Y CONDICIONES GENERALES") {
		t.Fatal("chapter heading was removed")
	}
	if !strings.Contains(composeLegalSections(sections), "### Artículo 7. Medios de pago") {
		t.Fatal("original version was mutated")
	}
}
