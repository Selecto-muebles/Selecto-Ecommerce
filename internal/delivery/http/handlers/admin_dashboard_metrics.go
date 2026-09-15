package handlers

import (
	"context"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func argentinaPeriods(now time.Time) (time.Time, time.Time, time.Time) {
	location, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		location = time.FixedZone("ART", -3*60*60)
	}
	localNow := now.In(location)
	day := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	month := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, location)
	return day.UTC(), month.UTC(), month.AddDate(0, -1, 0).UTC()
}

func adminCommercialMetrics(ctx context.Context, db *database.DB, now time.Time) (gin.H, error) {
	dayStart, monthStart, previousMonthStart := argentinaPeriods(now)
	var salesToday, salesMonth, salesPreviousMonth, averageOrderValue float64
	var unitsSold, pending, pendingOver24h, paid, failed, cancelled int
	var productsActive, withoutStock, lowStock, communicationPending, communicationFailed int
	err := db.Pool.QueryRow(ctx, `WITH sales AS (
		SELECT COALESCE(SUM(total) FILTER (WHERE COALESCE(paid_at,created_at)>=$1),0),
		COALESCE(SUM(total) FILTER (WHERE COALESCE(paid_at,created_at)>=$2),0),
		COALESCE(SUM(total) FILTER (WHERE COALESCE(paid_at,created_at)>=$3 AND COALESCE(paid_at,created_at)<$2),0),
		COALESCE(AVG(total) FILTER (WHERE COALESCE(paid_at,created_at)>=$2),0)
		FROM orders WHERE status='paid' AND COALESCE(paid_at,created_at)<$4
	), units AS (
		SELECT COALESCE(SUM(oi.quantity),0) FROM order_items oi JOIN orders o ON o.id=oi.order_id
		WHERE o.status='paid' AND COALESCE(o.paid_at,o.created_at)>=$2 AND COALESCE(o.paid_at,o.created_at)<$4
	), order_counts AS (
		SELECT COUNT(*) FILTER(WHERE status='pending'),
		COUNT(*) FILTER(WHERE status='pending' AND created_at<$4-INTERVAL '24 hours'),
		COUNT(*) FILTER(WHERE status='paid'),COUNT(*) FILTER(WHERE status='failed'),
		COUNT(*) FILTER(WHERE status='cancelled') FROM orders
	), product_counts AS (
		SELECT COUNT(*) FILTER(WHERE active AND archived_at IS NULL),
		COUNT(*) FILTER(WHERE active AND archived_at IS NULL AND stock=0),
		COUNT(*) FILTER(WHERE active AND archived_at IS NULL AND stock BETWEEN 1 AND 5) FROM products
	), communications AS (
		SELECT COUNT(*) FILTER(WHERE status IN ('pending','processing')),
		COUNT(*) FILTER(WHERE status='failed') FROM email_outbox
	) SELECT sales.*,units.*,order_counts.*,product_counts.*,communications.*
	FROM sales,units,order_counts,product_counts,communications`, dayStart, monthStart, previousMonthStart, now.UTC()).Scan(
		&salesToday, &salesMonth, &salesPreviousMonth, &averageOrderValue, &unitsSold,
		&pending, &pendingOver24h, &paid, &failed, &cancelled,
		&productsActive, &withoutStock, &lowStock, &communicationPending, &communicationFailed)
	if err != nil {
		return nil, err
	}
	newCustomers, recurringCustomers, err := adminCustomerMetrics(ctx, db, monthStart, now.UTC())
	if err != nil {
		return nil, err
	}
	topProducts, err := adminTopProducts(ctx, db, monthStart, now.UTC())
	if err != nil {
		return nil, err
	}
	change := any(nil)
	if salesPreviousMonth > 0 {
		change = ((salesMonth - salesPreviousMonth) / salesPreviousMonth) * 100
	}
	return gin.H{
		"timezone": "America/Argentina/Buenos_Aires", "sales_today": salesToday,
		"sales_month": salesMonth, "sales_previous_month": salesPreviousMonth, "sales_month_change_percent": change,
		"average_order_value_month": averageOrderValue, "units_sold_month": unitsSold,
		"orders_pending": pending, "orders_pending_over_24h": pendingOver24h, "orders_paid": paid,
		"orders_failed": failed, "orders_cancelled": cancelled, "products_active": productsActive,
		"products_without_stock": withoutStock, "products_low_stock": lowStock,
		"customers_new_month": newCustomers, "customers_recurring_month": recurringCustomers,
		"communications_pending": communicationPending, "communications_failed": communicationFailed,
		"top_products": topProducts,
	}, nil
}

func adminCustomerMetrics(ctx context.Context, db *database.DB, monthStart, now time.Time) (int, int, error) {
	var newCustomers, recurringCustomers int
	err := db.Pool.QueryRow(ctx, `WITH first_paid AS (
		SELECT user_id,MIN(COALESCE(paid_at,created_at)) first_at FROM orders WHERE status='paid' GROUP BY user_id
	), active AS (
		SELECT DISTINCT user_id FROM orders WHERE status='paid' AND COALESCE(paid_at,created_at)>=$1 AND COALESCE(paid_at,created_at)<$2
	) SELECT COUNT(*) FILTER(WHERE f.first_at>=$1),COUNT(*) FILTER(WHERE f.first_at<$1)
	FROM active a JOIN first_paid f ON f.user_id=a.user_id`, monthStart, now).Scan(&newCustomers, &recurringCustomers)
	return newCustomers, recurringCustomers, err
}

func adminTopProducts(ctx context.Context, db *database.DB, monthStart, now time.Time) ([]gin.H, error) {
	rows, err := db.Pool.Query(ctx, `SELECT p.id,p.name,SUM(oi.quantity),SUM(oi.price*oi.quantity)
		FROM order_items oi JOIN orders o ON o.id=oi.order_id JOIN products p ON p.id=oi.product_id
		WHERE o.status='paid' AND COALESCE(o.paid_at,o.created_at)>=$1 AND COALESCE(o.paid_at,o.created_at)<$2
		GROUP BY p.id,p.name ORDER BY SUM(oi.quantity) DESC,SUM(oi.price*oi.quantity) DESC LIMIT 5`, monthStart, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, units int
		var name string
		var revenue float64
		if err := rows.Scan(&id, &name, &units, &revenue); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": utils.EncodeID(id), "name": name, "units": units, "revenue": revenue})
	}
	return items, rows.Err()
}
