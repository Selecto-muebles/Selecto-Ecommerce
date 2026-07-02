package domain_test

import (
	"testing"

	"Selecto-Ecommerce/internal/domain"
)

func TestCanTransitionOrder(t *testing.T) {
	tests := []struct {
		name string
		from domain.OrderStatus
		to   domain.OrderStatus
		want bool
	}{
		{name: "pending to paid", from: domain.OrderStatusPending, to: domain.OrderStatusPaid, want: true},
		{name: "pending to cancelled", from: domain.OrderStatusPending, to: domain.OrderStatusCancelled, want: true},
		{name: "paid to cancelled rejected", from: domain.OrderStatusPaid, to: domain.OrderStatusCancelled, want: false},
		{name: "failed to paid rejected", from: domain.OrderStatusFailed, to: domain.OrderStatusPaid, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.CanTransitionOrder(tt.from, tt.to); got != tt.want {
				t.Fatalf("CanTransitionOrder(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
