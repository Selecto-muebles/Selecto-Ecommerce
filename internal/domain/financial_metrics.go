package domain

import (
	"errors"
	"fmt"
	"math/big"
)

const BasisPointsScale int64 = 10_000

type FinancialInputs struct {
	GrossRevenueCents   int64
	RefundsCents        int64
	CostOfGoodsCents    int64
	PaymentFeesCents    int64
	ChannelFeesCents    int64
	ShippingCents       int64
	TaxesCents          int64
	CampaignSpendCents  int64
	OperatingCostsCents int64
}

type FinancialSummary struct {
	NetRevenueCents         int64
	ContributionProfitCents int64
	NetProfitCents          int64
	ContributionMargin      Ratio
}

type CampaignInputs struct {
	AttributedRevenueCents int64
	RefundsCents           int64
	CostOfGoodsCents       int64
	FeesCents              int64
	SpendCents             int64
}

type CampaignMetrics struct {
	NetAttributedRevenueCents  int64
	ContributionBeforeAdsCents int64
	ProfitAfterAdsCents        int64
	ROAS                       Ratio
	ROI                        Ratio
}

type Ratio struct {
	Numerator   int64
	Denominator int64
}

func (r Ratio) BasisPoints() (int64, error) {
	if r.Denominator <= 0 {
		return 0, errors.New("ratio denominator must be positive")
	}
	value := new(big.Int).Mul(big.NewInt(r.Numerator), big.NewInt(BasisPointsScale))
	value.Quo(value, big.NewInt(r.Denominator))
	if !value.IsInt64() {
		return 0, errors.New("ratio exceeds int64 basis-point range")
	}
	return value.Int64(), nil
}

func CalculateFinancialSummary(input FinancialInputs) (FinancialSummary, error) {
	if err := validateNonNegative(
		input.GrossRevenueCents,
		input.RefundsCents,
		input.CostOfGoodsCents,
		input.PaymentFeesCents,
		input.ChannelFeesCents,
		input.ShippingCents,
		input.TaxesCents,
		input.CampaignSpendCents,
		input.OperatingCostsCents,
	); err != nil {
		return FinancialSummary{}, err
	}
	if input.RefundsCents > input.GrossRevenueCents {
		return FinancialSummary{}, errors.New("refunds cannot exceed gross revenue")
	}

	netRevenue := input.GrossRevenueCents - input.RefundsCents
	contribution, err := subtractFinancialValues(netRevenue, input.CostOfGoodsCents, input.PaymentFeesCents,
		input.ChannelFeesCents, input.ShippingCents, input.TaxesCents)
	if err != nil {
		return FinancialSummary{}, err
	}
	netProfit, err := subtractFinancialValues(contribution, input.CampaignSpendCents, input.OperatingCostsCents)
	if err != nil {
		return FinancialSummary{}, err
	}

	return FinancialSummary{
		NetRevenueCents:         netRevenue,
		ContributionProfitCents: contribution,
		NetProfitCents:          netProfit,
		ContributionMargin:      Ratio{Numerator: contribution, Denominator: netRevenue},
	}, nil
}

func CalculateCampaignMetrics(input CampaignInputs) (CampaignMetrics, error) {
	if err := validateNonNegative(
		input.AttributedRevenueCents,
		input.RefundsCents,
		input.CostOfGoodsCents,
		input.FeesCents,
		input.SpendCents,
	); err != nil {
		return CampaignMetrics{}, err
	}
	if input.RefundsCents > input.AttributedRevenueCents {
		return CampaignMetrics{}, errors.New("campaign refunds cannot exceed attributed revenue")
	}
	if input.SpendCents == 0 {
		return CampaignMetrics{}, errors.New("campaign spend must be positive")
	}

	netRevenue := input.AttributedRevenueCents - input.RefundsCents
	contribution, err := subtractFinancialValues(netRevenue, input.CostOfGoodsCents, input.FeesCents)
	if err != nil {
		return CampaignMetrics{}, err
	}
	profitAfterAds, err := subtractFinancialValues(contribution, input.SpendCents)
	if err != nil {
		return CampaignMetrics{}, err
	}

	return CampaignMetrics{
		NetAttributedRevenueCents:  netRevenue,
		ContributionBeforeAdsCents: contribution,
		ProfitAfterAdsCents:        profitAfterAds,
		ROAS: Ratio{
			Numerator:   netRevenue,
			Denominator: input.SpendCents,
		},
		ROI: Ratio{
			Numerator:   profitAfterAds,
			Denominator: input.SpendCents,
		},
	}, nil
}

type PartnerShare struct {
	PartnerID        string
	ShareBasisPoints int64
}

type PartnerAllocation struct {
	PartnerID   string
	AmountCents int64
}

func AllocatePartnerResult(totalCents int64, shares []PartnerShare) ([]PartnerAllocation, error) {
	if len(shares) == 0 {
		return nil, errors.New("at least one partner share is required")
	}

	seen := make(map[string]struct{}, len(shares))
	var totalShares int64
	for _, share := range shares {
		if share.PartnerID == "" {
			return nil, errors.New("partner ID is required")
		}
		if _, exists := seen[share.PartnerID]; exists {
			return nil, fmt.Errorf("duplicate partner ID %q", share.PartnerID)
		}
		seen[share.PartnerID] = struct{}{}
		if share.ShareBasisPoints < 0 || share.ShareBasisPoints > BasisPointsScale {
			return nil, fmt.Errorf("invalid share for partner %q", share.PartnerID)
		}
		totalShares += share.ShareBasisPoints
		if totalShares > BasisPointsScale {
			return nil, fmt.Errorf("partner shares exceed %d basis points", BasisPointsScale)
		}
	}
	if totalShares != BasisPointsScale {
		return nil, fmt.Errorf("partner shares must total %d basis points", BasisPointsScale)
	}

	allocations := make([]PartnerAllocation, len(shares))
	var allocated int64
	for index, share := range shares {
		amount := new(big.Int).Mul(big.NewInt(totalCents), big.NewInt(share.ShareBasisPoints))
		amount.Quo(amount, big.NewInt(BasisPointsScale))
		if !amount.IsInt64() {
			return nil, errors.New("partner allocation exceeds int64 range")
		}
		allocations[index] = PartnerAllocation{PartnerID: share.PartnerID, AmountCents: amount.Int64()}
		allocated += amount.Int64()
	}

	remainder := totalCents - allocated
	step := int64(1)
	if remainder < 0 {
		step = -1
	}
	for remainder != 0 {
		progressed := false
		for index, share := range shares {
			if share.ShareBasisPoints == 0 || remainder == 0 {
				continue
			}
			allocations[index].AmountCents += step
			remainder -= step
			progressed = true
		}
		if !progressed {
			return nil, errors.New("unable to distribute allocation remainder")
		}
	}
	return allocations, nil
}

func validateNonNegative(values ...int64) error {
	for _, value := range values {
		if value < 0 {
			return errors.New("financial inputs cannot be negative")
		}
	}
	return nil
}

func subtractFinancialValues(base int64, values ...int64) (int64, error) {
	result := big.NewInt(base)
	for _, value := range values {
		result.Sub(result, big.NewInt(value))
	}
	if !result.IsInt64() {
		return 0, errors.New("financial result exceeds int64 range")
	}
	return result.Int64(), nil
}
