package handlers

import (
	"context"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
)

type dashboardPeriodDefinition struct {
	Key           string
	Days          int
	CurrentStart  time.Time
	CurrentEnd    time.Time
	PreviousStart time.Time
	PreviousEnd   time.Time
}

func dashboardPeriod(value string, now time.Time) (dashboardPeriodDefinition, bool) {
	daysByKey := map[string]int{"7d": 7, "30d": 30, "90d": 90}
	if value == "" {
		value = "30d"
	}
	days, ok := daysByKey[value]
	if !ok {
		return dashboardPeriodDefinition{}, false
	}
	end := now.UTC()
	start := end.AddDate(0, 0, -days)
	return dashboardPeriodDefinition{
		Key: value, Days: days, CurrentStart: start, CurrentEnd: end,
		PreviousStart: start.AddDate(0, 0, -days), PreviousEnd: start,
	}, true
}

func adminComparableMetrics(ctx context.Context, db *database.DB, period dashboardPeriodDefinition) (gin.H, error) {
	var sales, previousSales, averageOrderValue float64
	var orders, previousOrders, units int
	err := db.Pool.QueryRow(ctx, `WITH paid AS (
	 SELECT total,COALESCE(paid_at,created_at) paid_on FROM orders WHERE status='paid'
	), sales AS (
	 SELECT COALESCE(SUM(total) FILTER(WHERE paid_on >= $1 AND paid_on < $2),0),
	 COALESCE(SUM(total) FILTER(WHERE paid_on >= $3 AND paid_on < $1),0),
	 COUNT(*) FILTER(WHERE paid_on >= $1 AND paid_on < $2),
	 COUNT(*) FILTER(WHERE paid_on >= $3 AND paid_on < $1),
	 COALESCE(AVG(total) FILTER(WHERE paid_on >= $1 AND paid_on < $2),0) FROM paid
	), units AS (
	 SELECT COALESCE(SUM(oi.quantity),0) FROM order_items oi JOIN orders o ON o.id=oi.order_id
	 WHERE o.status='paid' AND COALESCE(o.paid_at,o.created_at) >= $1 AND COALESCE(o.paid_at,o.created_at) < $2
	) SELECT sales.*,units.* FROM sales,units`, period.CurrentStart, period.CurrentEnd, period.PreviousStart).Scan(
		&sales, &previousSales, &orders, &previousOrders, &averageOrderValue, &units,
	)
	if err != nil {
		return nil, err
	}
	newCustomers, recurringCustomers, err := adminCustomerMetrics(ctx, db, period.CurrentStart, period.CurrentEnd)
	if err != nil {
		return nil, err
	}
	topProducts, err := adminTopProducts(ctx, db, period.CurrentStart, period.CurrentEnd)
	if err != nil {
		return nil, err
	}
	shipmentMetrics, err := adminShipmentMetrics(ctx, db, period.CurrentEnd)
	if err != nil {
		return nil, err
	}
	change := percentageChange(sales, previousSales)
	orderChange := percentageChange(float64(orders), float64(previousOrders))
	result := gin.H{
		"period": gin.H{
			"key": period.Key, "days": period.Days,
			"current_from": period.CurrentStart, "current_to": period.CurrentEnd,
			"previous_from": period.PreviousStart, "previous_to": period.PreviousEnd,
		},
		"sales_period": sales, "sales_previous_period": previousSales,
		"sales_period_change_percent": change, "orders_period": orders,
		"orders_previous_period": previousOrders, "orders_period_change_percent": orderChange,
		"average_order_value_period": averageOrderValue, "units_sold_period": units,
		"customers_new_period": newCustomers, "customers_recurring_period": recurringCustomers,
		"top_products_period": topProducts,
	}
	for key, value := range shipmentMetrics {
		result[key] = value
	}
	return result, nil
}

func adminShipmentMetrics(ctx context.Context, db *database.DB, now time.Time) (gin.H, error) {
	var preparing, preparingDelayed, ready, readyDelayed int
	var oldestPreparingHours, oldestReadyHours float64
	err := db.Pool.QueryRow(ctx, `SELECT
	 COUNT(*) FILTER(WHERE s.status='preparing'),
	 COUNT(*) FILTER(WHERE s.status='preparing' AND s.status_changed_at < $1::TIMESTAMPTZ-INTERVAL '24 hours'),
	 COUNT(*) FILTER(WHERE s.status='ready_for_dispatch'),
	 COUNT(*) FILTER(WHERE s.status='ready_for_dispatch' AND s.status_changed_at < $1::TIMESTAMPTZ-INTERVAL '24 hours'),
	 COALESCE(EXTRACT(EPOCH FROM ($1::TIMESTAMPTZ-MIN(s.status_changed_at) FILTER(WHERE s.status='preparing')))/3600,0),
	 COALESCE(EXTRACT(EPOCH FROM ($1::TIMESTAMPTZ-MIN(s.status_changed_at) FILTER(WHERE s.status='ready_for_dispatch')))/3600,0)
	 FROM shipments s JOIN orders o ON o.id=s.order_id WHERE o.status='paid'`, now).Scan(&preparing, &preparingDelayed, &ready, &readyDelayed, &oldestPreparingHours, &oldestReadyHours)
	return gin.H{
		"shipments_preparing": preparing, "shipments_preparing_over_24h": preparingDelayed,
		"shipments_ready_for_dispatch": ready, "shipments_ready_over_24h": readyDelayed,
		"oldest_preparing_hours": oldestPreparingHours, "oldest_ready_for_dispatch_hours": oldestReadyHours,
	}, err
}

func percentageChange(current, previous float64) any {
	if previous == 0 {
		return nil
	}
	return ((current - previous) / previous) * 100
}
