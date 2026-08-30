package domain

// VolumeDiscountTier represents a quantity threshold and its discount percentage.
type VolumeDiscountTier struct {
	MinQuantity     int     `json:"min_quantity"`
	DiscountPercent float64 `json:"discount_percent"`
	Label           string  `json:"label"`
}

// DefaultVolumeTiers defines the calibrated business discount scale.
var DefaultVolumeTiers = []VolumeDiscountTier{
	{MinQuantity: 10, DiscountPercent: 15.0, Label: "15% OFF (10+ un)"},
	{MinQuantity: 4, DiscountPercent: 10.0, Label: "10% OFF (4-9 un)"},
	{MinQuantity: 2, DiscountPercent: 5.0, Label: "5% OFF (2-3 un)"},
}

// TieredPriceResult holds the outcome of volume pricing calculation.
type TieredPriceResult struct {
	OriginalUnitPrice   int64   `json:"original_unit_price"`
	DiscountedUnitPrice int64   `json:"discounted_unit_price"`
	Quantity            int     `json:"quantity"`
	DiscountPercent     float64 `json:"discount_percent"`
	Subtotal            int64   `json:"subtotal"`
	OriginalSubtotal    int64   `json:"original_subtotal"`
	TotalSavings        int64   `json:"total_savings"`
	NextTierQuantity    int     `json:"next_tier_quantity,omitempty"`
	NextTierPercent     float64 `json:"next_tier_percent,omitempty"`
	UnitsToNextTier     int     `json:"units_to_next_tier,omitempty"`
}

// CalculateVolumePrice calculates unit price, subtotal, and savings based on quantity.
func CalculateVolumePrice(baseUnitPrice int64, quantity int) TieredPriceResult {
	if quantity <= 0 {
		quantity = 1
	}

	discountPercent := 0.0
	for _, tier := range DefaultVolumeTiers {
		if quantity >= tier.MinQuantity {
			discountPercent = tier.DiscountPercent
			break
		}
	}

	discountAmount := int64(float64(baseUnitPrice) * (discountPercent / 100.0))
	discountedUnitPrice := baseUnitPrice - discountAmount
	if discountedUnitPrice < 0 {
		discountedUnitPrice = 0
	}

	originalSubtotal := baseUnitPrice * int64(quantity)
	subtotal := discountedUnitPrice * int64(quantity)
	totalSavings := originalSubtotal - subtotal

	res := TieredPriceResult{
		OriginalUnitPrice:   baseUnitPrice,
		DiscountedUnitPrice: discountedUnitPrice,
		Quantity:            quantity,
		DiscountPercent:     discountPercent,
		Subtotal:            subtotal,
		OriginalSubtotal:    originalSubtotal,
		TotalSavings:        totalSavings,
	}

	if quantity < 2 {
		res.NextTierQuantity = 2
		res.NextTierPercent = 5.0
		res.UnitsToNextTier = 2 - quantity
	} else if quantity < 4 {
		res.NextTierQuantity = 4
		res.NextTierPercent = 10.0
		res.UnitsToNextTier = 4 - quantity
	} else if quantity < 10 {
		res.NextTierQuantity = 10
		res.NextTierPercent = 15.0
		res.UnitsToNextTier = 10 - quantity
	}

	return res
}
