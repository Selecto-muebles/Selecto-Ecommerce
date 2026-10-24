package handlers

import "testing"

func TestSafeDispatchCSVCell(t *testing.T) {
	tests := map[string]string{
		"=HYPERLINK(\"https://example.com\")": "'=HYPERLINK(\"https://example.com\")",
		"  +1+1":                              "'  +1+1",
		"\t@SUM(1)":                           "'\t@SUM(1)",
		"01234":                               "'01234",
		"Calle 123":                           "Calle 123",
	}
	for input, want := range tests {
		if got := safeDispatchCSVCell(input); got != want {
			t.Errorf("safeDispatchCSVCell(%q)=%q, want %q", input, got, want)
		}
	}
}
