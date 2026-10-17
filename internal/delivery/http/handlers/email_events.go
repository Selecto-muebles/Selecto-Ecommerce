package handlers

import (
	"context"
	"fmt"

	"Selecto-Ecommerce/internal/config"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/utils"
)

var paymentStatusLabels = map[string]string{"paid": "aprobado", "failed": "rechazado", "cancelled": "cancelado", "pending": "pendiente"}
var shipmentStatusLabels = map[string]string{"preparing": "en preparación", "ready_for_dispatch": "lista para despachar", "shipped": "en camino", "delivered": "entregada", "delivery_failed": "con una incidencia", "cancelled": "cancelada"}

func enqueueOrderCreatedEmail(ctx context.Context, db mailinfra.QueryRower, cfg *config.Config, orderID int, recipient string, total money.Cents) (int64, error) {
	publicID := utils.EncodeID(orderID)
	return mailinfra.EnqueueReturningID(ctx, db, fmt.Sprintf("order-created:%d", orderID), recipient, "order_created", map[string]any{
		"order_id": publicID,
		"total":    fmt.Sprintf("$ %.2f", total.Float64()),
		"url":      cfg.StorefrontURL + "/cuenta/ordenes/" + publicID,
	})
}

func enqueuePaymentStatusEmail(ctx context.Context, db mailinfra.QueryRower, cfg *config.Config, orderID int, paymentID int, recipient, status string) (int64, error) {
	publicID := utils.EncodeID(orderID)
	label := paymentStatusLabels[status]
	if label == "" {
		label = status
	}
	payload := map[string]any{
		"order_id": publicID, "status_label": label, "url": cfg.StorefrontURL + "/cuenta/ordenes/" + publicID,
	}
	if status == "paid" {
		var total money.Cents
		if err := db.QueryRow(ctx, "SELECT ROUND(total * 100)::BIGINT FROM orders WHERE id=$1", orderID).Scan(&total); err != nil {
			return 0, err
		}
		payload["total"] = fmt.Sprintf("$ %.2f", total.Float64())
		var itemsSummary string
		if err := db.QueryRow(ctx, `SELECT COALESCE(STRING_AGG(oi.quantity::TEXT || ' × ' ||
			COALESCE(NULLIF(oi.product_name_snapshot,''),p.name,'Producto'), E'\n' ORDER BY oi.id),'')
			FROM order_items oi LEFT JOIN products p ON p.id=oi.product_id WHERE oi.order_id=$1`, orderID).Scan(&itemsSummary); err != nil {
			return 0, err
		}
		payload["items_summary"] = itemsSummary
		var deliverySummary string
		if err := db.QueryRow(ctx, `SELECT COALESCE(CONCAT_WS(', ',
			NULLIF(TRIM(CONCAT_WS(' ',s.recipient_first_name,s.recipient_last_name)),''),
			NULLIF(TRIM(CONCAT_WS(' ',s.street_address,s.street_number)),''),
			NULLIF(s.locality,''),NULLIF(s.province,''),NULLIF(s.postal_code,'')),'')
			FROM orders o LEFT JOIN order_shipping_addresses s ON s.order_id=o.id WHERE o.id=$1`, orderID).Scan(&deliverySummary); err != nil {
			return 0, err
		}
		payload["delivery_summary"] = deliverySummary
	}
	return mailinfra.EnqueueReturningID(ctx, db, fmt.Sprintf("payment-status:%d:%d:%s", orderID, paymentID, status), recipient, "payment_status", payload)
}

func enqueueShipmentStatusEmail(ctx context.Context, db mailinfra.QueryRower, cfg *config.Config, orderID int, eventVersion, recipient, status, trackingURL string) (int64, error) {
	publicID := utils.EncodeID(orderID)
	label := shipmentStatusLabels[status]
	if label == "" {
		label = status
	}
	return mailinfra.EnqueueReturningID(ctx, db, fmt.Sprintf("shipment-status:%d:%s:%s", orderID, status, eventVersion), recipient, "shipment_status", map[string]any{
		"order_id": publicID, "status_label": label, "tracking_url": trackingURL, "url": cfg.StorefrontURL + "/cuenta/ordenes/" + publicID,
	})
}
