package payments

import (
	"errors"
	"math"
	"strings"

	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/shared/money"
)

func NormalizeStatus(value string) (string, error) {
	status := strings.TrimSpace(value)
	if status == "" {
		return "paid", nil
	}
	switch status {
	case "paid", "failed", "cancelled":
		return status, nil
	default:
		return "", errors.New("invalid status")
	}
}

func AmountMatches(received *float64, expected money.Cents) bool {
	if received == nil || math.IsNaN(*received) || math.IsInf(*received, 0) || *received < 0 {
		return false
	}
	amount, err := money.FromFloat(*received)
	return err == nil && amount == expected
}

func CanRecoverPaidOrder(status domain.OrderStatus, hasActivePreference bool) bool {
	return hasActivePreference && (status == domain.OrderStatusFailed || status == domain.OrderStatusCancelled)
}
