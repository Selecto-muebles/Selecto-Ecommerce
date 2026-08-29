package domain_test

import (
	"testing"

	"Selecto-Ecommerce/internal/domain"
)

func TestCalculateVolumePrice(t *testing.T) {
	basePrice := int64(100000) // $1,000.00 in cents

	// 1 unit -> 0% discount
	r1 := domain.CalculateVolumePrice(basePrice, 1)
	if r1.DiscountPercent != 0.0 || r1.DiscountedUnitPrice != 100000 || r1.Subtotal != 100000 {
		t.Fatalf("expected 0%% discount for 1 unit, got %v", r1)
	}
	if r1.UnitsToNextTier != 1 || r1.NextTierQuantity != 2 {
		t.Fatalf("expected 1 unit to next tier, got %d", r1.UnitsToNextTier)
	}

	// 2 units -> 5% discount
	r2 := domain.CalculateVolumePrice(basePrice, 2)
	if r2.DiscountPercent != 5.0 || r2.DiscountedUnitPrice != 95000 || r2.Subtotal != 190000 {
		t.Fatalf("expected 5%% discount for 2 units, got %v", r2)
	}
	if r2.TotalSavings != 10000 {
		t.Fatalf("expected 10000 total savings, got %d", r2.TotalSavings)
	}

	// 5 units -> 10% discount
	r5 := domain.CalculateVolumePrice(basePrice, 5)
	if r5.DiscountPercent != 10.0 || r5.DiscountedUnitPrice != 90000 || r5.Subtotal != 450000 {
		t.Fatalf("expected 10%% discount for 5 units, got %v", r5)
	}

	// 12 units -> 15% discount
	r12 := domain.CalculateVolumePrice(basePrice, 12)
	if r12.DiscountPercent != 15.0 || r12.DiscountedUnitPrice != 85000 || r12.Subtotal != 1020000 {
		t.Fatalf("expected 15%% discount for 12 units, got %v", r12)
	}
}
