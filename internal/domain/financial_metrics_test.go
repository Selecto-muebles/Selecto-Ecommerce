package domain

import (
	"math"
	"testing"
)

func TestCalculateFinancialSummary(t *testing.T) {
	summary, err := CalculateFinancialSummary(FinancialInputs{
		GrossRevenueCents:   1_000_000,
		RefundsCents:        50_000,
		CostOfGoodsCents:    300_000,
		PaymentFeesCents:    40_000,
		ChannelFeesCents:    20_000,
		ShippingCents:       30_000,
		TaxesCents:          100_000,
		CampaignSpendCents:  80_000,
		OperatingCostsCents: 30_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.NetRevenueCents != 950_000 || summary.ContributionProfitCents != 460_000 || summary.NetProfitCents != 350_000 {
		t.Fatalf("unexpected financial summary: %+v", summary)
	}
	margin, err := summary.ContributionMargin.BasisPoints()
	if err != nil {
		t.Fatal(err)
	}
	if margin != 4_842 {
		t.Fatalf("margin basis points = %d, want 4842", margin)
	}
}

func TestCalculateCampaignMetrics(t *testing.T) {
	metrics, err := CalculateCampaignMetrics(CampaignInputs{
		AttributedRevenueCents: 500_000,
		RefundsCents:           20_000,
		CostOfGoodsCents:       180_000,
		FeesCents:              30_000,
		SpendCents:             100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	roas, _ := metrics.ROAS.BasisPoints()
	returnOnInvestment, _ := metrics.ROI.BasisPoints()
	if roas != 48_000 || returnOnInvestment != 17_000 {
		t.Fatalf("unexpected campaign ratios: ROAS=%d ROI=%d", roas, returnOnInvestment)
	}
}

func TestCampaignMetricsRejectsZeroSpend(t *testing.T) {
	_, err := CalculateCampaignMetrics(CampaignInputs{AttributedRevenueCents: 100})
	if err == nil {
		t.Fatal("zero campaign spend must be rejected")
	}
}

func TestAllocatePartnerResultExactly(t *testing.T) {
	shares := []PartnerShare{
		{PartnerID: "socio-a", ShareBasisPoints: 5_000},
		{PartnerID: "socio-b", ShareBasisPoints: 5_000},
	}

	positive, err := AllocatePartnerResult(101, shares)
	if err != nil {
		t.Fatal(err)
	}
	if positive[0].AmountCents != 51 || positive[1].AmountCents != 50 {
		t.Fatalf("unexpected positive allocation: %+v", positive)
	}

	negative, err := AllocatePartnerResult(-101, shares)
	if err != nil {
		t.Fatal(err)
	}
	if negative[0].AmountCents != -51 || negative[1].AmountCents != -50 {
		t.Fatalf("unexpected negative allocation: %+v", negative)
	}
}

func TestAllocatePartnerResultValidatesConfiguration(t *testing.T) {
	_, err := AllocatePartnerResult(100, []PartnerShare{{PartnerID: "socio-a", ShareBasisPoints: 4_999}})
	if err == nil {
		t.Fatal("incomplete partner allocation must be rejected")
	}
}

func TestFinancialSummaryRejectsOverflow(t *testing.T) {
	_, err := CalculateFinancialSummary(FinancialInputs{
		GrossRevenueCents: 1,
		CostOfGoodsCents:  math.MaxInt64,
		PaymentFeesCents:  math.MaxInt64,
	})
	if err == nil {
		t.Fatal("overflowing financial result must be rejected")
	}
}
