package domain

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusFailed    OrderStatus = "failed"
	OrderStatusCancelled OrderStatus = "cancelled"
)

func (s OrderStatus) Valid() bool {
	switch s {
	case OrderStatusPending, OrderStatusPaid, OrderStatusFailed, OrderStatusCancelled:
		return true
	default:
		return false
	}
}

func (s OrderStatus) Terminal() bool {
	return s == OrderStatusPaid || s == OrderStatusFailed || s == OrderStatusCancelled
}

func CanTransitionOrder(from, to OrderStatus) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case OrderStatusPending:
		return to == OrderStatusPaid || to == OrderStatusFailed || to == OrderStatusCancelled
	default:
		return false
	}
}
