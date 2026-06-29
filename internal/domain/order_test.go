package domain

import "testing"

func TestCanTransitionOrder(t *testing.T) {
	tests := []struct {
		name string
		from OrderStatus
		to   OrderStatus
		want bool
	}{
		{name: "pending to paid", from: OrderStatusPending, to: OrderStatusPaid, want: true},
		{name: "pending to cancelled", from: OrderStatusPending, to: OrderStatusCancelled, want: true},
		{name: "paid to cancelled rejected", from: OrderStatusPaid, to: OrderStatusCancelled, want: false},
		{name: "failed to paid rejected", from: OrderStatusFailed, to: OrderStatusPaid, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransitionOrder(tt.from, tt.to); got != tt.want {
				t.Fatalf("CanTransitionOrder(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
