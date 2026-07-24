package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	adminservice "Selecto-Ecommerce/internal/service/admin"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func AdminListCustomersHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		q := strings.TrimSpace(c.Query("q"))
		province := strings.TrimSpace(c.Query("province"))
		locality := strings.TrimSpace(c.Query("locality"))
		where := []string{"u.role='user'", "($1 = '' OR u.email ILIKE '%' || $1 || '%' OR COALESCE(u.dni, '') ILIKE '%' || $1 || '%' OR COALESCE(u.first_name, '') ILIKE '%' || $1 || '%' OR COALESCE(u.last_name, '') ILIKE '%' || $1 || '%')"}
		args := []any{q}
		if province != "" {
			args = append(args, province)
			where = append(where, fmt.Sprintf("u.province=$%d", len(args)))
		}
		if locality != "" {
			args = append(args, locality)
			where = append(where, fmt.Sprintf("u.locality=$%d", len(args)))
		}
		whereSQL := strings.Join(where, " AND ")
		var total int
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM users u WHERE "+whereSQL, args...).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		args = append(args, page.PageSize, page.Offset)
		rows, err := db.Pool.Query(c, "SELECT u.id, u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.dni, ''), COALESCE(u.province, ''), COALESCE(u.locality, ''), COALESCE(u.phone_number, ''), COUNT(o.id), COALESCE(SUM(o.total) FILTER (WHERE o.status='paid'), 0) FROM users u LEFT JOIN orders o ON o.user_id=u.id WHERE "+whereSQL+" GROUP BY u.id ORDER BY "+adminservice.CustomerSort(c.Query("sort"))+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id, ordersCount int
			var email, firstName, lastName, dni, province, locality, phone string
			var totalSpent float64
			if err := rows.Scan(&id, &email, &firstName, &lastName, &dni, &province, &locality, &phone, &ordersCount, &totalSpent); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"id": utils.EncodeID(id), "email": email, "first_name": firstName, "last_name": lastName, "dni": dni, "province": province, "locality": locality, "phone_number": phone, "orders_count": ordersCount, "total_spent": totalSpent})
		}
		if err := rows.Err(); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func AdminGetCustomerHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var email, firstName, lastName, dni, streetAddress, streetNumber, postalCode, province, locality, phone string
		if err := db.Pool.QueryRow(c, "SELECT email, COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(dni, ''), COALESCE(street_address, ''), COALESCE(street_number, ''), COALESCE(postal_code, ''), COALESCE(province, ''), COALESCE(locality, ''), COALESCE(phone_number, '') FROM users WHERE id=$1 AND role='user'", id).Scan(&email, &firstName, &lastName, &dni, &streetAddress, &streetNumber, &postalCode, &province, &locality, &phone); err != nil {
			handleAdminLookupErr(c, err, "customer not found")
			return
		}
		orders, err := adminCustomerOrders(c, db, id)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": utils.EncodeID(id), "email": email, "first_name": firstName, "last_name": lastName, "dni": dni, "street_address": streetAddress, "street_number": streetNumber, "postal_code": postalCode, "province": province, "locality": locality, "phone_number": phone, "orders": orders})
	}
}

func adminCustomerOrders(ctx context.Context, db *database.DB, userID int) ([]gin.H, error) {
	rows, err := db.Pool.Query(ctx, "SELECT id, status, COALESCE(payment_status, ''), total, created_at FROM orders WHERE user_id=$1 ORDER BY created_at DESC LIMIT 50", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id int
		var status, paymentStatus string
		var total float64
		var createdAt time.Time
		if err := rows.Scan(&id, &status, &paymentStatus, &total, &createdAt); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": utils.EncodeID(id), "status": status, "payment_status": paymentStatus, "total": total, "created_at": createdAt})
	}
	return items, rows.Err()
}
