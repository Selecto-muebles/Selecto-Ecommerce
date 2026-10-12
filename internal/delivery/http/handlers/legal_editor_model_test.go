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
