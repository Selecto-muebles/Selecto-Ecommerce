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
var shipmentStatusLabels = map[string]string{"preparing": "en preparaciÃ³n", "ready_for_dispatch": "lista para despachar", "shipped": "en camino", "delivered": "entregada", "delivery_failed": "con una incidencia", "cancelled": "cancelada"}

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
	return mailinfra.EnqueueReturningID(ctx, db, fmt.Sprintf("payment-status:%d:%d:%s", orderID, paymentID, status), recipient, "payment_status", map[string]any{
		"order_id": publicID, "status_label": label, "url": cfg.StorefrontURL + "/cuenta/ordenes/" + publicID,
	})
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
