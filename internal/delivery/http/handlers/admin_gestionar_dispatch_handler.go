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
func AdminGestionarDispatchesHandler(db *database.DB, cutoffHours ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		cutoffHour := shippingservice.DefaultDispatchCutoffHour
		if len(cutoffHours) > 0 {
			cutoffHour = cutoffHours[0]
		}
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
				 WHERE oi.order_id=o.id AND BTRIM(oi.sku_snapshot)=''),
				COALESCE(gd.package_state,'not_started'), COALESCE(gpb.state,'not_started'), b.cutoff_at
			FROM orders o JOIN users u ON u.id=o.user_id
			LEFT JOIN order_shipping_addresses a ON a.order_id=o.id
			LEFT JOIN shipments s ON s.order_id=o.id
			LEFT JOIN gestionar_dispatches gd ON gd.order_id=o.id
			LEFT JOIN gestionar_pickup_orders gpo ON gpo.order_id=o.id
			LEFT JOIN gestionar_pickup_batches gpb ON gpb.id=gpo.batch_id
			LEFT JOIN dispatch_cutoff_batch_orders bo ON bo.order_id=o.id
			LEFT JOIN dispatch_cutoff_batches b ON b.id=bo.batch_id
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
			var assignedCutoff sql.NullTime
			var readiness shippingservice.GestionarReadiness
			var firstName, lastName, packageState, pickupState string
			if err := rows.Scan(&id, &readiness.OrderStatus, &readiness.ShipmentStatus, &paidAt,
				&readiness.HasAddress, &firstName, &lastName, &readiness.Street, &readiness.StreetNumber,
				&readiness.Locality, &readiness.Province, &readiness.PostalCode, &readiness.Phone,
				&readiness.Email, &readiness.ItemCount, &readiness.MissingSKUs,
				&packageState, &pickupState, &assignedCutoff); err != nil {
				apperrors.Internal(c)
				return
			}
			readiness.RecipientName = strings.TrimSpace(firstName + " " + lastName)
			blockers := readiness.Blockers()
			var cutoff any
			if paidAt.Valid {
				cutoff, err = shippingservice.DispatchCutoff(paidAt.Time, cutoffHour)
				if err != nil {
					apperrors.Internal(c)
					return
				}
			}
			if assignedCutoff.Valid {
				cutoff = assignedCutoff.Time
			}
			items = append(items, gin.H{
				"order_id": utils.EncodeID(id), "paid_at": nullableTime(paidAt), "cutoff_at": cutoff,
				"shipment_status": readiness.ShipmentStatus, "recipient_name": readiness.RecipientName,
				"locality": readiness.Locality, "postal_code": readiness.PostalCode,
				"data_ready": len(blockers) == 0, "blockers": blockers,
				"provider_progress": gin.H{"package_state": packageState, "pickup_state": pickupState},
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

func AdminGestionarDispatchPreviewHandler(db *database.DB, cutoffHours ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		cutoffHour := shippingservice.DefaultDispatchCutoffHour
		if len(cutoffHours) > 0 {
			cutoffHour = cutoffHours[0]
		}
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
		var cutoff any
		if paidAt.Valid {
			cutoff, err = shippingservice.DispatchCutoff(paidAt.Time, cutoffHour)
			if err != nil {
				apperrors.Internal(c)
				return
			}
		}
		providerProgress, err := gestionarProviderProgress(c, db, orderID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"order": gin.H{"reference": utils.EncodeID(orderID), "status": status,
				"paid_at": nullableTime(paidAt), "total_ars": totalARS, "cutoff_at": cutoff},
			"shipment_status": readiness.ShipmentStatus, "destination": destination,
			"items": items, "data_ready": len(blockers) == 0, "blockers": blockers,
			"provider_progress":   providerProgress,
			"provider_submission": "disabled_pending_contract",
		})
	}
}

func gestionarProviderProgress(c *gin.Context, db *database.DB, orderID int) (gin.H, error) {
	var packageState, externalPackageID, packageError string
	var pickupState, externalPickupID, pickupError, latestStatus string
	var packageAttemptedAt, pickupAttemptedAt, statusReceivedAt sql.NullTime
	err := db.Pool.QueryRow(c, `SELECT COALESCE(d.package_state,'not_started'),
		COALESCE(d.external_package_id,''), COALESCE(d.last_error_code,''), d.attempted_at,
		COALESCE(b.state,'not_started'), COALESCE(b.external_pickup_id,''),
		COALESCE(b.last_error_code,''), b.attempted_at,
		COALESCE(e.external_status,''), e.received_at
		FROM orders o
		LEFT JOIN gestionar_dispatches d ON d.order_id=o.id
		LEFT JOIN gestionar_pickup_orders po ON po.order_id=o.id
		LEFT JOIN gestionar_pickup_batches b ON b.id=po.batch_id
		LEFT JOIN LATERAL (
			SELECT external_status,received_at FROM gestionar_status_events
			WHERE order_id=o.id ORDER BY received_at DESC,id DESC LIMIT 1
		) e ON TRUE WHERE o.id=$1`, orderID).Scan(
		&packageState, &externalPackageID, &packageError, &packageAttemptedAt,
		&pickupState, &externalPickupID, &pickupError, &pickupAttemptedAt,
		&latestStatus, &statusReceivedAt)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"package_state": packageState, "external_package_id": externalPackageID,
		"package_error_code": packageError, "package_attempted_at": nullableTime(packageAttemptedAt),
		"pickup_state": pickupState, "external_pickup_id": externalPickupID,
		"pickup_error_code": pickupError, "pickup_attempted_at": nullableTime(pickupAttemptedAt),
		"latest_provider_status": latestStatus, "status_received_at": nullableTime(statusReceivedAt),
	}, nil
}

func dateOnly(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format("2006-01-02")
}
