package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type ShippingAddressResponse struct {
	RecipientName         string     `json:"recipient_name"`
	FirstName             string     `json:"-"`
	LastName              string     `json:"-"`
	DNI                   string     `json:"dni"`
	StreetAddress         string     `json:"street_address"`
	StreetNumber          string     `json:"street_number"`
	PostalCode            string     `json:"postal_code"`
	Province              string     `json:"province"`
	Locality              string     `json:"locality"`
	PhoneNumber           string     `json:"phone_number"`
	RequestedDeliveryDate *time.Time `json:"requested_delivery_date"`
}

type ShipmentResponse struct {
	ID                  string     `json:"id"`
	Status              string     `json:"status"`
	Carrier             string     `json:"carrier"`
	TrackingNumber      string     `json:"tracking_number"`
	TrackingURL         string     `json:"tracking_url"`
	EstimatedDeliveryAt *time.Time `json:"estimated_delivery_at"`
	ShippedAt           *time.Time `json:"shipped_at"`
	DeliveredAt         *time.Time `json:"delivered_at"`
	CustomerNote        string     `json:"customer_note"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type shippingQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadOrderShipping(ctx context.Context, queryer shippingQueryer, orderID int) (*ShippingAddressResponse, *ShipmentResponse, error) {
	var address ShippingAddressResponse
	var shipmentID sql.NullInt64
	var status, carrier, trackingNumber, trackingURL, customerNote sql.NullString
	var requestedDeliveryDate, estimatedDeliveryAt, shippedAt, deliveredAt, shipmentCreatedAt, shipmentUpdatedAt sql.NullTime
	err := queryer.QueryRow(ctx, `
		SELECT
			a.recipient_first_name,
			a.recipient_last_name,
			a.dni,
			a.street_address,
			a.street_number,
			a.postal_code,
			a.province,
			a.locality,
			a.phone_number,
			a.requested_delivery_date,
			s.id,
			s.status,
			s.carrier,
			s.tracking_number,
			s.tracking_url,
			s.estimated_delivery_at,
			s.shipped_at,
			s.delivered_at,
			s.customer_note,
			s.created_at,
			s.updated_at
		FROM order_shipping_addresses a
		LEFT JOIN shipments s ON s.order_id = a.order_id
		WHERE a.order_id = $1`, orderID).Scan(
		&address.FirstName,
		&address.LastName,
		&address.DNI,
		&address.StreetAddress,
		&address.StreetNumber,
		&address.PostalCode,
		&address.Province,
		&address.Locality,
		&address.PhoneNumber,
		&requestedDeliveryDate,
		&shipmentID,
		&status,
		&carrier,
		&trackingNumber,
		&trackingURL,
		&estimatedDeliveryAt,
		&shippedAt,
		&deliveredAt,
		&customerNote,
		&shipmentCreatedAt,
		&shipmentUpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	address.RecipientName = strings.TrimSpace(address.FirstName + " " + address.LastName)
	if !shipmentID.Valid {
		address.RequestedDeliveryDate = nullTimePointer(requestedDeliveryDate)
		return &address, nil, nil
	}
	address.RequestedDeliveryDate = nullTimePointer(requestedDeliveryDate)
	shipment := &ShipmentResponse{
		ID:                  utils.EncodeID(int(shipmentID.Int64)),
		Status:              status.String,
		Carrier:             carrier.String,
		TrackingNumber:      trackingNumber.String,
		TrackingURL:         trackingURL.String,
		EstimatedDeliveryAt: nullTimePointer(estimatedDeliveryAt),
		ShippedAt:           nullTimePointer(shippedAt),
		DeliveredAt:         nullTimePointer(deliveredAt),
		CustomerNote:        customerNote.String,
		CreatedAt:           shipmentCreatedAt.Time,
		UpdatedAt:           shipmentUpdatedAt.Time,
	}
	return &address, shipment, nil
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

type UpdateShipmentInput struct {
	Status              *string `json:"status"`
	Carrier             *string `json:"carrier"`
	TrackingNumber      *string `json:"tracking_number"`
	TrackingURL         *string `json:"tracking_url"`
	EstimatedDeliveryAt *string `json:"estimated_delivery_at"`
	CustomerNote        *string `json:"customer_note"`
}

func AdminUpdateShipmentHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input UpdateShipmentInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid shipment payload")
			return
		}
		if input.empty() {
			apperrors.BadRequest(c, "shipment update must contain at least one field")
			return
		}

		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)

		var orderStatus string
		if err := tx.QueryRow(c, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", orderID).Scan(&orderStatus); err != nil {
			handleAdminLookupErr(c, err, "order not found")
			return
		}
		if orderStatus != string(domain.OrderStatusPaid) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "shipping is available only for paid orders", gin.H{"status": orderStatus})
			return
		}
		if _, err := tx.Exec(c, "INSERT INTO shipments (order_id, status) VALUES ($1, $2) ON CONFLICT (order_id) DO NOTHING", orderID, domain.ShipmentStatusPreparing); err != nil {
			apperrors.Internal(c)
			return
		}

		var current ShipmentResponse
		var shipmentID int
		var estimatedDeliveryAt, shippedAt, deliveredAt sql.NullTime
		if err := tx.QueryRow(c, `SELECT id, status, carrier, tracking_number, tracking_url, estimated_delivery_at, shipped_at, delivered_at, customer_note, created_at, updated_at FROM shipments WHERE order_id=$1 FOR UPDATE`, orderID).Scan(
			&shipmentID, &current.Status, &current.Carrier, &current.TrackingNumber, &current.TrackingURL, &estimatedDeliveryAt, &shippedAt, &deliveredAt, &current.CustomerNote, &current.CreatedAt, &current.UpdatedAt,
		); err != nil {
			apperrors.Internal(c)
			return
		}

		next, err := normalizeShipmentUpdate(current, input)
		if err != nil {
			apperrors.BadRequest(c, err.Error())
			return
		}
		if !domain.CanTransitionShipment(domain.ShipmentStatus(current.Status), domain.ShipmentStatus(next.Status)) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "invalid shipment status transition", gin.H{"current_status": current.Status, "requested_status": next.Status})
			return
		}

		_, err = tx.Exec(c, `
			UPDATE shipments
			SET status=$1,
			    carrier=$2,
			    tracking_number=$3,
			    tracking_url=$4,
			    estimated_delivery_at=$5,
			    customer_note=$6,
			    shipped_at=CASE WHEN $1='shipped' AND status <> 'shipped' THEN NOW() ELSE shipped_at END,
			    delivered_at=CASE WHEN $1='delivered' AND status <> 'delivered' THEN NOW() ELSE delivered_at END,
			    updated_at=NOW()
			WHERE id=$7`,
			next.Status, next.Carrier, next.TrackingNumber, next.TrackingURL, next.EstimatedDeliveryAt, next.CustomerNote, shipmentID,
		)
		if err != nil {
			apperrors.Internal(c)
			return
		}

		actor, _ := c.Get("email")
		metadata, _ := json.Marshal(gin.H{"shipment_id": shipmentID, "from": current.Status, "to": next.Status, "carrier": next.Carrier, "tracking_number": next.TrackingNumber})
		if _, err := tx.Exec(c, "INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)", fmt.Sprint(actor), "shipment_updated", "order", orderID, metadata); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}

		logger.Info("shipment_updated", "order_id", orderID, "shipment_id", shipmentID, "status", next.Status)
		order, err := adminOrderDetail(c, db, orderID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, order)
	}
}

func (input UpdateShipmentInput) empty() bool {
	return input.Status == nil && input.Carrier == nil && input.TrackingNumber == nil && input.TrackingURL == nil && input.EstimatedDeliveryAt == nil && input.CustomerNote == nil
}

type normalizedShipmentUpdate struct {
	Status              string
	Carrier             string
	TrackingNumber      string
	TrackingURL         string
	EstimatedDeliveryAt *time.Time
	CustomerNote        string
}

func normalizeShipmentUpdate(current ShipmentResponse, input UpdateShipmentInput) (normalizedShipmentUpdate, error) {
	next := normalizedShipmentUpdate{
		Status:              current.Status,
		Carrier:             current.Carrier,
		TrackingNumber:      current.TrackingNumber,
		TrackingURL:         current.TrackingURL,
		EstimatedDeliveryAt: current.EstimatedDeliveryAt,
		CustomerNote:        current.CustomerNote,
	}
	if input.Status != nil {
		next.Status = strings.TrimSpace(*input.Status)
	}
	if !domain.ShipmentStatus(next.Status).Valid() {
		return next, errors.New("invalid shipment status")
	}
	if input.Carrier != nil {
		next.Carrier = strings.TrimSpace(*input.Carrier)
	}
	if input.TrackingNumber != nil {
		next.TrackingNumber = strings.TrimSpace(*input.TrackingNumber)
	}
	if input.TrackingURL != nil {
		next.TrackingURL = strings.TrimSpace(*input.TrackingURL)
	}
	if input.CustomerNote != nil {
		next.CustomerNote = strings.TrimSpace(*input.CustomerNote)
	}
	if len(next.Carrier) > 120 || len(next.TrackingNumber) > 160 || len(next.TrackingURL) > 1000 || len(next.CustomerNote) > 1000 {
		return next, errors.New("shipment field exceeds maximum length")
	}
	if next.TrackingURL != "" {
		parsed, err := url.ParseRequestURI(next.TrackingURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return next, errors.New("tracking_url must be an absolute HTTP URL")
		}
	}
	if input.EstimatedDeliveryAt != nil {
		raw := strings.TrimSpace(*input.EstimatedDeliveryAt)
		if raw == "" {
			next.EstimatedDeliveryAt = nil
		} else {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return next, errors.New("estimated_delivery_at must use RFC3339")
			}
			next.EstimatedDeliveryAt = &parsed
		}
	}
	return next, nil
}
