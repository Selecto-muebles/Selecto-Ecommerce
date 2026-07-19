package domain

import "testing"

func TestShipmentTransitions(t *testing.T) {
	tests := []struct {
		from ShipmentStatus
		to   ShipmentStatus
		want bool
	}{
		{ShipmentStatusPreparing, ShipmentStatusReadyForDispatch, true},
		{ShipmentStatusPreparing, ShipmentStatusShipped, true},
		{ShipmentStatusReadyForDispatch, ShipmentStatusDelivered, false},
		{ShipmentStatusShipped, ShipmentStatusDelivered, true},
		{ShipmentStatusDelivered, ShipmentStatusShipped, false},
		{ShipmentStatusDeliveryFailed, ShipmentStatusShipped, true},
	}
	for _, test := range tests {
		if got := CanTransitionShipment(test.from, test.to); got != test.want {
			t.Fatalf("transition %s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}
