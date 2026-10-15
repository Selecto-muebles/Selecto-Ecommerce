package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	shippingservice "Selecto-Ecommerce/internal/service/shipping"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// The dispatch queue is a local-data review surface, not a provider submission.
// No endpoint in this file contacts Gestionar or changes an order's shipment.
func AdminGestionarDispatchesHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		const where = `o.status='paid' AND (s.id IS NULL OR s.status IN ('preparing','ready_for_dispatch'))`
		var total int
		if err := db.Pool.QueryRow(c, `SELECT COUNT(*) FROM orders o LEFT JOIN shipments s ON s.order_id=o.id WHERE `+where).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `
			SELECT o.id, o.status, COALESCE(s.status,''), o.paid_at,
				a.order_id IS NOT NULL, COALESCE(a.recipient_first_name,''), COALESCE(a.recipient_last_name,''),
				COALESCE(a.street_address,''), COALESCE(a.street_number,''), COALESCE(a.locality,''),
				COALESCE(a.province,''), COALESCE(a.postal_code,''), COALESCE(a.phone_number,''), u.email,
				(SELECT COUNT(*) FROM order_items oi WHERE oi.order_id=o.id),
				(SELECT COUNT(*) FROM order_items oi
				 WHERE oi.order_id=o.id AND BTRIM(oi.sku_snapshot)='')
			FROM orders o JOIN users u ON u.id=o.user_id
			LEFT JOIN order_shipping_addresses a ON a.order_id=o.id
			LEFT JOIN shipments s ON s.order_id=o.id
			WHERE `+where+`
			ORDER BY o.paid_at DESC NULLS LAST, o.id DESC LIMIT $1 OFFSET $2`, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := make([]gin.H, 0)
		for rows.Next() {
			var id int
			var paidAt sql.NullTime
			var readiness shippingservice.GestionarReadiness
			var firstName, lastName string
			if err := rows.Scan(&id, &readiness.OrderStatus, &readiness.ShipmentStatus, &paidAt,
				&readiness.HasAddress, &firstName, &lastName, &readiness.Street, &readiness.StreetNumber,
				&readiness.Locality, &readiness.Province, &readiness.PostalCode, &readiness.Phone,
				&readiness.Email, &readiness.ItemCount, &readiness.MissingSKUs); err != nil {
				apperrors.Internal(c)
				return
			}
			readiness.RecipientName = strings.TrimSpace(firstName + " " + lastName)
			blockers := readiness.Blockers()
			items = append(items, gin.H{
				"order_id": utils.EncodeID(id), "paid_at": nullableTime(paidAt),
				"shipment_status": readiness.ShipmentStatus, "recipient_name": readiness.RecipientName,
				"locality": readiness.Locality, "postal_code": readiness.PostalCode,
				"data_ready": len(blockers) == 0, "blockers": blockers,
			})
		}
		if rows.Err() != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"items": items, "page": page.Page, "page_size": page.PageSize, "total": total,
			"provider_submission": "disabled_pending_contract",
		})
	}
}

func AdminGestionarDispatchPreviewHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var status, totalARS, email string
		var paidAt sql.NullTime
		err := db.Pool.QueryRow(c, `SELECT o.status, o.total::text, o.paid_at, u.email
			FROM orders o JOIN users u ON u.id=o.user_id WHERE o.id=$1`, orderID).Scan(&status, &totalARS, &paidAt, &email)
		if errors.Is(err, pgx.ErrNoRows) {
			handleAdminLookupErr(c, err, "order not found")
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		address, shipment, err := loadOrderShipping(c, db.Pool, orderID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT oi.sku_snapshot, oi.product_name_snapshot, oi.quantity, oi.price::text
			FROM order_items oi WHERE oi.order_id=$1 ORDER BY oi.id`, orderID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		items := make([]gin.H, 0)
		missingSKUs := 0
		for rows.Next() {
			var sku, name, unitPriceARS string
			var quantity int
			if err := rows.Scan(&sku, &name, &quantity, &unitPriceARS); err != nil {
				rows.Close()
				apperrors.Internal(c)
				return
			}
			if strings.TrimSpace(sku) == "" {
				missingSKUs++
			}
			items = append(items, gin.H{"sku": sku, "name": name, "quantity": quantity, "unit_price_ars": unitPriceARS})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			apperrors.Internal(c)
			return
		}
		readiness := shippingservice.GestionarReadiness{
			OrderStatus: status, Email: email, HasAddress: address != nil,
			ItemCount: len(items), MissingSKUs: missingSKUs,
		}
		var destination any
		if shipment != nil {
			readiness.ShipmentStatus = shipment.Status
		}
		if address != nil {
			readiness.RecipientName = address.RecipientName
			readiness.Street = address.StreetAddress
			readiness.StreetNumber = address.StreetNumber
			readiness.Locality = address.Locality
			readiness.Province = address.Province
			readiness.PostalCode = address.PostalCode
			readiness.Phone = address.PhoneNumber
			destination = gin.H{
				"recipient_name": address.RecipientName, "street_address": address.StreetAddress,
				"street_number": address.StreetNumber, "locality": address.Locality,
				"province": address.Province, "postal_code": address.PostalCode,
				"phone_number": address.PhoneNumber, "email": email,
				"requested_delivery_date": dateOnly(address.RequestedDeliveryDate),
			}
		}
		blockers := readiness.Blockers()
		c.JSON(http.StatusOK, gin.H{
			"order": gin.H{"reference": utils.EncodeID(orderID), "status": status,
				"paid_at": nullableTime(paidAt), "total_ars": totalARS},
			"shipment_status": readiness.ShipmentStatus, "destination": destination,
			"items": items, "data_ready": len(blockers) == 0, "blockers": blockers,
			"provider_submission": "disabled_pending_contract",
		})
	}
}

func dateOnly(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format("2006-01-02")
}
