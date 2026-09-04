package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	adminservice "Selecto-Ecommerce/internal/service/admin"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func AdminListOrdersHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		q := strings.TrimSpace(c.Query("q"))
		status := strings.TrimSpace(c.Query("status"))
		paymentStatus := strings.TrimSpace(c.Query("payment_status"))
		where := []string{"($1 = '' OR u.email ILIKE '%' || $1 || '%' OR COALESCE(u.dni, '') ILIKE '%' || $1 || '%' OR COALESCE(u.first_name, '') ILIKE '%' || $1 || '%' OR COALESCE(u.last_name, '') ILIKE '%' || $1 || '%')"}
		args := []any{q}
		if status != "" {
			args = append(args, status)
			where = append(where, fmt.Sprintf("o.status=$%d", len(args)))
		}
		if paymentStatus != "" {
			args = append(args, paymentStatus)
			where = append(where, fmt.Sprintf("COALESCE(o.payment_status, '')=$%d", len(args)))
		}
		whereSQL := strings.Join(where, " AND ")
		var total int
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM orders o JOIN users u ON u.id=o.user_id WHERE "+whereSQL, args...).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		args = append(args, page.PageSize, page.Offset)
		rows, err := db.Pool.Query(c, "SELECT o.id, o.status, COALESCE(o.payment_status, ''), o.total, o.created_at, o.paid_at, o.cancelled_at, o.payment_id, COALESCE(o.payment_provider, CASE WHEN o.payment_id IS NOT NULL THEN 'mercadopago' ELSE '' END), COALESCE(o.provider_payment_id, o.payment_id::TEXT, ''), u.id, u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, '') FROM orders o JOIN users u ON u.id=o.user_id WHERE "+whereSQL+" ORDER BY "+adminservice.OrderSort(c.Query("sort"))+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id, userID int
			var status, paymentStatus, paymentProvider, providerPaymentID, email, firstName, lastName string
			var totalValue float64
			var createdAt time.Time
			var paidAt, cancelledAt sql.NullTime
			var paymentID sql.NullInt64
			if err := rows.Scan(&id, &status, &paymentStatus, &totalValue, &createdAt, &paidAt, &cancelledAt, &paymentID, &paymentProvider, &providerPaymentID, &userID, &email, &firstName, &lastName); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"id": utils.EncodeID(id), "status": status, "payment_status": paymentStatus, "total": totalValue, "created_at": createdAt, "paid_at": nullableTime(paidAt), "cancelled_at": nullableTime(cancelledAt), "payment_id": nullableInt(paymentID), "payment_provider": paymentProvider, "provider_payment_id": providerPaymentID, "customer": gin.H{"id": utils.EncodeID(userID), "email": email, "first_name": firstName, "last_name": lastName}})
		}
		if err := rows.Err(); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func AdminGetOrderHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		order, err := adminOrderDetail(c, db, id)
		if err != nil {
			handleAdminLookupErr(c, err, "order not found")
			return
		}
		c.JSON(http.StatusOK, order)
	}
}

func adminOrderDetail(ctx context.Context, db *database.DB, id int) (gin.H, error) {
	var userID int
	var status, paymentStatus, paymentProvider, providerPaymentID, preferenceID, checkoutURL, environment string
	var total float64
	var createdAt time.Time
	var expiresAt, paidAt, cancelledAt sql.NullTime
	var paymentID sql.NullInt64
	var email, firstName, lastName, dni, streetAddress, streetNumber, postalCode, province, locality, phone string
	err := db.Pool.QueryRow(ctx, `SELECT o.user_id, o.status, COALESCE(o.payment_status, ''), o.total, o.created_at, o.expires_at, o.paid_at, o.cancelled_at, o.payment_id, COALESCE(o.payment_provider, CASE WHEN o.payment_id IS NOT NULL THEN 'mercadopago' ELSE '' END), COALESCE(o.provider_payment_id, o.payment_id::TEXT, ''), COALESCE(o.active_payment_preference_id, ''), COALESCE(o.active_checkout_url, ''), COALESCE(o.active_payment_environment, ''), u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.dni, ''), COALESCE(u.street_address, ''), COALESCE(u.street_number, ''), COALESCE(u.postal_code, ''), COALESCE(u.province, ''), COALESCE(u.locality, ''), COALESCE(u.phone_number, '') FROM orders o JOIN users u ON u.id=o.user_id WHERE o.id=$1`, id).Scan(&userID, &status, &paymentStatus, &total, &createdAt, &expiresAt, &paidAt, &cancelledAt, &paymentID, &paymentProvider, &providerPaymentID, &preferenceID, &checkoutURL, &environment, &email, &firstName, &lastName, &dni, &streetAddress, &streetNumber, &postalCode, &province, &locality, &phone)
	if err != nil {
		return nil, err
	}
	items, err := adminOrderItems(ctx, db, id)
	if err != nil {
		return nil, err
	}
	audit, err := adminAuditLogs(ctx, db, "", "order", id, adminPage{Page: 1, PageSize: 50, Offset: 0})
	if err != nil {
		return nil, err
	}
	shippingAddress, shipment, err := loadOrderShipping(ctx, db.Pool, id)
	if err != nil {
		return nil, err
	}
	return gin.H{"id": utils.EncodeID(id), "status": status, "payment_status": paymentStatus, "total": total, "created_at": createdAt, "expires_at": nullableTime(expiresAt), "paid_at": nullableTime(paidAt), "cancelled_at": nullableTime(cancelledAt), "payment_id": nullableInt(paymentID), "payment_provider": paymentProvider, "provider_payment_id": providerPaymentID, "active_payment_preference_id": preferenceID, "active_checkout_url": checkoutURL, "active_payment_environment": environment, "customer": gin.H{"id": utils.EncodeID(userID), "email": email, "first_name": firstName, "last_name": lastName, "dni": dni, "street_address": streetAddress, "street_number": streetNumber, "postal_code": postalCode, "province": province, "locality": locality, "phone_number": phone}, "shipping_address": shippingAddress, "shipment": shipment, "items": items, "audit_logs": audit}, nil
}

func adminOrderItems(ctx context.Context, db *database.DB, orderID int) ([]gin.H, error) {
	rows, err := db.Pool.Query(ctx, "SELECT oi.id, oi.product_id, p.name, oi.quantity, oi.price, oi.selected_options FROM order_items oi JOIN products p ON p.id=oi.product_id WHERE oi.order_id=$1 ORDER BY oi.id", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, productID, quantity int
		var name string
		var price float64
		var selectedOptions map[string]string
		if err := rows.Scan(&id, &productID, &name, &quantity, &price, &selectedOptions); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": utils.EncodeID(id), "product_id": utils.EncodeID(productID), "name": name, "quantity": quantity, "price": price, "subtotal": price * float64(quantity), "selected_options": selectedOptions})
	}
	return items, rows.Err()
}

func AdminCancelOrderHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			logger.Error(logging.EventAdminOrderCancellationFailed, "stage", "begin_transaction", "order_id", id, "error", err)
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var status string
		if err := tx.QueryRow(c, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", id).Scan(&status); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				logger.Error(logging.EventAdminOrderCancellationFailed, "stage", "lock_order", "order_id", id, "error", err)
			}
			handleAdminLookupErr(c, err, "order not found")
			return
		}
		if domain.OrderStatus(status) != domain.OrderStatusPending {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "only pending orders can be cancelled", gin.H{"status": status})
			return
		}
		if err := postgresrepo.RestoreOrderStock(c, tx, id); err != nil {
			logger.Error(logging.EventAdminOrderCancellationFailed, "stage", "restore_stock", "order_id", id, "error", err)
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "UPDATE orders SET status='cancelled', payment_status=COALESCE(payment_status, 'cancelled'), cancelled_at=NOW() WHERE id=$1", id); err != nil {
			logger.Error(logging.EventAdminOrderCancellationFailed, "stage", "update_order", "order_id", id, "error", err)
			apperrors.Internal(c)
			return
		}
		if err := writeAuditTx(c, tx, adminActor(c), "order_cancelled", "order", id, gin.H{"previous_status": status}); err != nil {
			logger.Error(logging.EventAdminOrderCancellationFailed, "stage", "write_audit", "order_id", id, "error", err)
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			logger.Error(logging.EventAdminOrderCancellationFailed, "stage", "commit_transaction", "order_id", id, "error", err)
			apperrors.Internal(c)
			return
		}
		logger.Info(logging.EventAdminOrderCancellationCompleted, "order_id", id)
		order, err := adminOrderDetail(c, db, id)
		if err != nil {
			logger.Error(logging.EventAdminOrderCancellationFailed, "stage", "load_response", "order_id", id, "error", err)
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, order)
	}
}
