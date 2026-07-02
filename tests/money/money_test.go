package money_test

import (
	"testing"

	"Selecto-Ecommerce/internal/shared/money"
)

func TestFromFloatRoundsToCents(t *testing.T) {
	got, err := money.FromFloat(123.456)
	if err != nil {
		t.Fatalf("FromFloat() error = %v", err)
	}
	if got != 12346 {
		t.Fatalf("FromFloat() = %d, want 12346", got)
	}
}

func TestDecimalString(t *testing.T) {
	if got := money.Cents(12345).DecimalString(); got != "123.45" {
		t.Fatalf("DecimalString() = %q, want 123.45", got)
	}
}

func TestFromDecimalString(t *testing.T) {
	got, err := money.FromDecimalString("1000.5")
	if err != nil {
		t.Fatalf("FromDecimalString() error = %v", err)
	}
	if got != 100050 {
		t.Fatalf("FromDecimalString() = %d, want 100050", got)
	}
}
