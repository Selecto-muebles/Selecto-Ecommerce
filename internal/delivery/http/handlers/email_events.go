package handlers

import (
	"context"
	"encoding/json"
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

func enqueuePaymentStatusEmail(ctx context.Context, db mailinfra.QueryRower, cfg *config.Config, orderID int, paymentID int64, recipient, status string) (int64, error) {
	publicID := utils.EncodeID(orderID)
	label := paymentStatusLabels[status]
	if label == "" {
		label = status
	}
	payload := map[string]any{
		"order_id": publicID, "status_label": label, "url": cfg.StorefrontURL + "/cuenta/ordenes/" + publicID,
	}
	if status == "paid" {
		var receipt mailinfra.PurchaseReceipt
		if err := db.QueryRow(ctx, `SELECT ROUND(total * 100)::BIGINT,
			TO_CHAR(paid_at AT TIME ZONE 'America/Argentina/Buenos_Aires', 'DD/MM/YYYY HH24:MI'),
			COALESCE(NULLIF(provider_payment_id,''), payment_id::TEXT, '')
			FROM orders WHERE id=$1 AND status='paid'`, orderID).
			Scan(&receipt.TotalCents, &receipt.PaidAt, &receipt.PaymentReference); err != nil {
			return 0, err
		}
		payload["total"] = fmt.Sprintf("$ %.2f", money.Cents(receipt.TotalCents).Float64())
		var itemsJSON string
		if err := db.QueryRow(ctx, `SELECT COALESCE(JSONB_AGG(JSONB_BUILD_OBJECT(
			'name',COALESCE(NULLIF(oi.product_name_snapshot,''),p.name,'Producto'),
			'quantity',oi.quantity,
			'unit_price_cents',ROUND(oi.price * 100)::BIGINT,
			'original_unit_price_cents',ROUND(COALESCE(oi.original_unit_price,oi.price) * 100)::BIGINT,
			'line_total_cents',ROUND(oi.price * oi.quantity * 100)::BIGINT,
			'selected_options',oi.selected_options
			) ORDER BY oi.id),'[]'::JSONB)::TEXT
			FROM order_items oi LEFT JOIN products p ON p.id=oi.product_id WHERE oi.order_id=$1`, orderID).Scan(&itemsJSON); err != nil {
			return 0, err
		}
		if err := json.Unmarshal([]byte(itemsJSON), &receipt.Lines); err != nil {
			return 0, err
		}
		var finalLines, originalLines int64
		for _, item := range receipt.Lines {
			finalLines += item.LineTotalCents
			originalLines += item.OriginalUnitPriceCents * int64(item.Quantity)
		}
		if len(receipt.Lines) > 0 && finalLines == receipt.TotalCents && originalLines >= receipt.TotalCents {
			receipt.SubtotalCents = originalLines
			receipt.DiscountCents = originalLines - receipt.TotalCents
			receipt.ShowBreakdown = true
		}
		var deliverySummary string
		if err := db.QueryRow(ctx, `SELECT COALESCE(CONCAT_WS(', ',
			NULLIF(TRIM(CONCAT_WS(' ',s.recipient_first_name,s.recipient_last_name)),''),
			NULLIF(TRIM(CONCAT_WS(' ',s.street_address,s.street_number)),''),
			NULLIF(s.locality,''),NULLIF(s.province,''),NULLIF(s.postal_code,'')),'')
			FROM orders o LEFT JOIN order_shipping_addresses s ON s.order_id=o.id WHERE o.id=$1`, orderID).Scan(&deliverySummary); err != nil {
			return 0, err
		}
		receipt.DeliverySummary = deliverySummary
		payload["receipt"] = receipt
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
