package domain

type ShipmentStatus string

const (
	ShipmentStatusPreparing        ShipmentStatus = "preparing"
	ShipmentStatusReadyForDispatch ShipmentStatus = "ready_for_dispatch"
	ShipmentStatusShipped          ShipmentStatus = "shipped"
	ShipmentStatusDelivered        ShipmentStatus = "delivered"
	ShipmentStatusDeliveryFailed   ShipmentStatus = "delivery_failed"
	ShipmentStatusCancelled        ShipmentStatus = "cancelled"
)

func (s ShipmentStatus) Valid() bool {
	switch s {
	case ShipmentStatusPreparing,
		ShipmentStatusReadyForDispatch,
		ShipmentStatusShipped,
		ShipmentStatusDelivered,
		ShipmentStatusDeliveryFailed,
		ShipmentStatusCancelled:
		return true
	default:
		return false
	}
}

func CanTransitionShipment(from, to ShipmentStatus) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case ShipmentStatusPreparing:
		return to == ShipmentStatusReadyForDispatch || to == ShipmentStatusShipped || to == ShipmentStatusCancelled
	case ShipmentStatusReadyForDispatch:
		return to == ShipmentStatusShipped || to == ShipmentStatusCancelled
	case ShipmentStatusShipped:
		return to == ShipmentStatusDelivered || to == ShipmentStatusDeliveryFailed
	case ShipmentStatusDeliveryFailed:
		return to == ShipmentStatusShipped || to == ShipmentStatusCancelled
	default:
		return false
	}
}
