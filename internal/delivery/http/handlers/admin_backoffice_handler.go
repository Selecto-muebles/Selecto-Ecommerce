package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const (
	defaultAdminPageSize = 20
	maxAdminPageSize     = 100
)

type adminPage struct{ Page, PageSize, Offset int }

type adminProductInput struct {
	Name        string           `json:"name"`
	SKU         string           `json:"sku"`
	Price       *float64         `json:"price"`
	Stock       *int             `json:"stock"`
	Active      *bool            `json:"active"`
	Description *string          `json:"description"`
	Category    *string          `json:"category"`
	Options     *[]productOption `json:"options"`
}

func adminPagination(c *gin.Context) adminPage {
	page := intQuery(c, "page", 1)
	if page < 1 {
		page = 1
	}
	pageSize := intQuery(c, "page_size", defaultAdminPageSize)
	if pageSize < 1 {
		pageSize = defaultAdminPageSize
	}
	if pageSize > maxAdminPageSize {
		pageSize = maxAdminPageSize
	}
	return adminPage{Page: page, PageSize: pageSize, Offset: (page - 1) * pageSize}
}

func intQuery(c *gin.Context, key string, fallback int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func nullableBoolQuery(c *gin.Context, key string) *bool {
	value := strings.TrimSpace(strings.ToLower(c.Query(key)))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func adminActor(c *gin.Context) string {
	value, _ := c.Get("email")
	return fmt.Sprint(value)
}

func adminIDParam(c *gin.Context, key string) (int, bool) {
	id, err := utils.DecodeID(c.Param(key))
	if err != nil || id <= 0 {
		apperrors.BadRequest(c, "invalid "+key)
		return 0, false
	}
	return id, true
}

func handleAdminLookupErr(c *gin.Context, err error, message string) {
	if errors.Is(err, pgx.ErrNoRows) {
		apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, message, nil)
		return
	}
	apperrors.Internal(c)
}

func nullableTime(value sql.NullTime) any {
	if value.Valid {
		return value.Time
	}
	return nil
}

func nullableInt(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func writeAudit(ctx context.Context, db *database.DB, actor, action, entityType string, entityID int, metadata any) error {
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = db.Pool.Exec(ctx, "INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)", actor, action, entityType, entityID, string(body))
	return err
}

func writeAuditTx(ctx context.Context, tx pgx.Tx, actor, action, entityType string, entityID int, metadata any) error {
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)", actor, action, entityType, entityID, string(body))
	return err
}

func GetAdminMeHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		email := adminActor(c)
		var role string
		if err := db.Pool.QueryRow(c, "SELECT role FROM users WHERE email=$1", email).Scan(&role); err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid token", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"email": email, "role": role})
	}
}

func GetAdminDashboardHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now().UTC()
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		var salesToday, salesMonth float64
		var ordersPending, ordersPaid, ordersCancelled, productsActive, productsWithoutStock int
		err := db.Pool.QueryRow(c, `
			WITH sales AS (
				SELECT
					COALESCE(SUM(total) FILTER (WHERE COALESCE(paid_at, created_at) >= $1), 0) AS today,
					COALESCE(SUM(total), 0) AS month
				FROM orders
				WHERE status='paid'
				  AND COALESCE(paid_at, created_at) >= $2
				  AND COALESCE(paid_at, created_at) < $3
			), order_counts AS (
				SELECT
				COUNT(*) FILTER (WHERE status='pending'),
				COUNT(*) FILTER (WHERE status='paid'),
					COUNT(*) FILTER (WHERE status='cancelled')
				FROM orders
			), product_counts AS (
				SELECT
					COUNT(*) FILTER (WHERE active),
					COUNT(*) FILTER (WHERE active AND stock=0)
				FROM products
			)
			SELECT sales.*, order_counts.*, product_counts.*
			FROM sales, order_counts, product_counts`, dayStart, monthStart, now).Scan(
			&salesToday,
			&salesMonth,
			&ordersPending,
			&ordersPaid,
			&ordersCancelled,
			&productsActive,
			&productsWithoutStock,
		)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		latestOrders, err := adminLatestOrders(c, db, 10)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		logger.Info(logging.EventAdminMetricsRequested, "orders_paid", ordersPaid)
		c.JSON(http.StatusOK, gin.H{"sales_today": salesToday, "sales_month": salesMonth, "orders_pending": ordersPending, "orders_paid": ordersPaid, "orders_cancelled": ordersCancelled, "products_active": productsActive, "products_without_stock": productsWithoutStock, "latest_orders": latestOrders})
	}
}

func adminLatestOrders(ctx context.Context, db *database.DB, limit int) ([]gin.H, error) {
	rows, err := db.Pool.Query(ctx, `SELECT o.id, o.status, COALESCE(o.payment_status, ''), o.total, o.created_at, u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, '') FROM orders o JOIN users u ON u.id=o.user_id ORDER BY o.created_at DESC LIMIT $1`, limit)
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
		var email, firstName, lastName string
		if err := rows.Scan(&id, &status, &paymentStatus, &total, &createdAt, &email, &firstName, &lastName); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": utils.EncodeID(id), "status": status, "payment_status": paymentStatus, "total": total, "created_at": createdAt, "customer": gin.H{"email": email, "first_name": firstName, "last_name": lastName}})
	}
	return items, rows.Err()
}

func AdminListProductsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		q := strings.TrimSpace(c.Query("q"))
		active := nullableBoolQuery(c, "active")
		stock := strings.TrimSpace(c.Query("stock"))
		where := []string{"($1 = '' OR name ILIKE '%' || $1 || '%' OR COALESCE(sku, '') ILIKE '%' || $1 || '%')"}
		args := []any{q}
		if active != nil {
			args = append(args, *active)
			where = append(where, fmt.Sprintf("active=$%d", len(args)))
		}
		if stock == "without_stock" {
			where = append(where, "stock=0")
		}
		if stock == "with_stock" {
			where = append(where, "stock>0")
		}
		whereSQL := strings.Join(where, " AND ")
		var total int
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM products WHERE "+whereSQL, args...).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		args = append(args, page.PageSize, page.Offset)
		rows, err := db.Pool.Query(c, "SELECT id, name, COALESCE(sku, ''), price, stock, active, description, category, created_at, updated_at FROM products WHERE "+whereSQL+" ORDER BY "+productSort(c.Query("sort"))+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id, stockValue int
			var name, sku, description, category string
			var price float64
			var activeValue bool
			var createdAt, updatedAt time.Time
			if err := rows.Scan(&id, &name, &sku, &price, &stockValue, &activeValue, &description, &category, &createdAt, &updatedAt); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"id": utils.EncodeID(id), "name": name, "sku": sku, "price": price, "stock": stockValue, "active": activeValue, "description": description, "category": category, "created_at": createdAt, "updated_at": updatedAt})
		}
		if err := rows.Err(); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func productSort(value string) string {
	switch value {
	case "name":
		return "name ASC"
	case "price":
		return "price ASC"
	case "stock":
		return "stock ASC"
	case "created_at":
		return "created_at ASC"
	default:
		return "created_at DESC, id DESC"
	}
}

func AdminGetProductHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		item, err := adminProduct(c, db, id)
		if err != nil {
			handleAdminLookupErr(c, err, "product not found")
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

func adminProduct(ctx context.Context, db *database.DB, id int) (gin.H, error) {
	var name, sku, description, category string
	var price float64
	var stock int
	var active bool
	var createdAt, updatedAt time.Time
	err := db.Pool.QueryRow(ctx, "SELECT name, COALESCE(sku, ''), price, stock, active, description, category, created_at, updated_at FROM products WHERE id=$1", id).Scan(&name, &sku, &price, &stock, &active, &description, &category, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	images, err := productImages(ctx, db, id)
	if err != nil {
		return nil, err
	}
	options, err := productOptions(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return gin.H{"id": utils.EncodeID(id), "name": name, "sku": sku, "price": price, "stock": stock, "active": active, "description": description, "category": category, "images": images, "options": options, "created_at": createdAt, "updated_at": updatedAt}, nil
}

func AdminCreateProductHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input adminProductInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		if strings.TrimSpace(input.Name) == "" || input.Price == nil || *input.Price < 0 || input.Stock == nil || *input.Stock < 0 {
			apperrors.BadRequest(c, "name, price and stock must be valid")
			return
		}
		active := true
		if input.Active != nil {
			active = *input.Active
		}
		options := []productOption{}
		if input.Options != nil {
			var err error
			options, err = normalizeProductOptions(*input.Options)
			if err != nil {
				apperrors.BadRequest(c, err.Error())
				return
			}
		}
		description, category := "", ""
		if input.Description != nil {
			description = strings.TrimSpace(*input.Description)
		}
		if input.Category != nil {
			category = strings.TrimSpace(*input.Category)
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var id int
		err = tx.QueryRow(c, "INSERT INTO products (name, sku, price, stock, active, description, category, updated_at) VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, NOW()) RETURNING id", strings.TrimSpace(input.Name), strings.TrimSpace(input.SKU), *input.Price, *input.Stock, active, description, category).Scan(&id)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if err := replaceProductOptions(c, tx, id, options); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		_ = writeAudit(c, db, adminActor(c), "product_created", "product", id, gin.H{"name": input.Name, "sku": input.SKU, "price": *input.Price, "stock": *input.Stock, "active": active})
		logger.Info(logging.EventProductCreated, "product_id", id, "public_id", utils.EncodeID(id), "stock", *input.Stock)
		item, _ := adminProduct(c, db, id)
		c.JSON(http.StatusCreated, item)
	}
}

func AdminUpdateProductHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input adminProductInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		if strings.TrimSpace(input.Name) == "" && input.Price == nil && strings.TrimSpace(input.SKU) == "" && input.Description == nil && input.Category == nil && input.Options == nil {
			apperrors.BadRequest(c, "at least one editable field is required")
			return
		}
		if input.Price != nil && *input.Price < 0 {
			apperrors.BadRequest(c, "price must be valid")
			return
		}
		var beforeName, beforeSKU, beforeDescription, beforeCategory string
		var beforePrice float64
		if err := db.Pool.QueryRow(c, "SELECT name, COALESCE(sku, ''), price, description, category FROM products WHERE id=$1", id).Scan(&beforeName, &beforeSKU, &beforePrice, &beforeDescription, &beforeCategory); err != nil {
			handleAdminLookupErr(c, err, "product not found")
			return
		}
		name := strings.TrimSpace(input.Name)
		if name == "" {
			name = beforeName
		}
		sku := strings.TrimSpace(input.SKU)
		if sku == "" {
			sku = beforeSKU
		}
		price := beforePrice
		if input.Price != nil {
			price = *input.Price
		}
		description, category := beforeDescription, beforeCategory
		if input.Description != nil {
			description = strings.TrimSpace(*input.Description)
		}
		if input.Category != nil {
			category = strings.TrimSpace(*input.Category)
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		if _, err := tx.Exec(c, "UPDATE products SET name=$1, sku=NULLIF($2, ''), price=$3, description=$4, category=$5, updated_at=NOW() WHERE id=$6", name, sku, price, description, category, id); err != nil {
			apperrors.Internal(c)
			return
		}
		if input.Options != nil {
			options, err := normalizeProductOptions(*input.Options)
			if err != nil {
				apperrors.BadRequest(c, err.Error())
				return
			}
			if err := replaceProductOptions(c, tx, id, options); err != nil {
				apperrors.Internal(c)
				return
			}
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		_ = writeAudit(c, db, adminActor(c), "product_updated", "product", id, gin.H{"before": gin.H{"name": beforeName, "sku": beforeSKU, "price": beforePrice, "description": beforeDescription, "category": beforeCategory}, "after": gin.H{"name": name, "sku": sku, "price": price, "description": description, "category": category}})
		logger.Info(logging.EventProductCreated, "event", "product_updated", "product_id", id)
		item, _ := adminProduct(c, db, id)
		c.JSON(http.StatusOK, item)
	}
}

func AdminUpdateProductStatusHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input struct {
			Active *bool `json:"active"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || input.Active == nil {
			apperrors.BadRequest(c, "active is required")
			return
		}
		var before bool
		if err := db.Pool.QueryRow(c, "SELECT active FROM products WHERE id=$1", id).Scan(&before); err != nil {
			handleAdminLookupErr(c, err, "product not found")
			return
		}
		if _, err := db.Pool.Exec(c, "UPDATE products SET active=$1, updated_at=NOW() WHERE id=$2", *input.Active, id); err != nil {
			apperrors.Internal(c)
			return
		}
		action := "product_deactivated"
		if *input.Active {
			action = "product_activated"
		}
		_ = writeAudit(c, db, adminActor(c), action, "product", id, gin.H{"before_active": before, "after_active": *input.Active})
		logger.Info(logging.EventProductCreated, "event", action, "product_id", id)
		item, _ := adminProduct(c, db, id)
		c.JSON(http.StatusOK, item)
	}
}

func AdminAdjustProductStockHandler(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input struct {
			Delta    *int   `json:"delta"`
			Mode     string `json:"mode"`
			Quantity *int   `json:"quantity"`
			Reason   string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		reason := strings.TrimSpace(input.Reason)
		if reason == "" {
			apperrors.BadRequest(c, "reason is required")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var before int
		if err := tx.QueryRow(c, "SELECT stock FROM products WHERE id=$1 FOR UPDATE", id).Scan(&before); err != nil {
			handleAdminLookupErr(c, err, "product not found")
			return
		}
		after := before
		if strings.TrimSpace(input.Mode) == "set" {
			if input.Quantity == nil || *input.Quantity < 0 {
				apperrors.BadRequest(c, "quantity must be valid")
				return
			}
			after = *input.Quantity
		} else {
			if input.Delta == nil || *input.Delta == 0 {
				apperrors.BadRequest(c, "delta must be non-zero")
				return
			}
			after = before + *input.Delta
		}
		if after < 0 {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "stock cannot be negative", gin.H{"current_stock": before})
			return
		}
		if _, err := tx.Exec(c, "UPDATE products SET stock=$1, updated_at=NOW() WHERE id=$2", after, id); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := writeAuditTx(c, tx, adminActor(c), "stock_adjusted", "product", id, gin.H{"before_stock": before, "after_stock": after, "delta": after - before, "reason": reason}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		logger.Info(logging.EventStockReserved, "event", "stock_adjusted", "product_id", id, "before_stock", before, "after_stock", after)
		item, _ := adminProduct(c, db, id)
		c.JSON(http.StatusOK, item)
	}
}

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
		rows, err := db.Pool.Query(c, "SELECT o.id, o.status, COALESCE(o.payment_status, ''), o.total, o.created_at, o.paid_at, o.cancelled_at, o.payment_id, u.id, u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, '') FROM orders o JOIN users u ON u.id=o.user_id WHERE "+whereSQL+" ORDER BY "+orderSort(c.Query("sort"))+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id, userID int
			var status, paymentStatus, email, firstName, lastName string
			var totalValue float64
			var createdAt time.Time
			var paidAt, cancelledAt sql.NullTime
			var paymentID sql.NullInt64
			if err := rows.Scan(&id, &status, &paymentStatus, &totalValue, &createdAt, &paidAt, &cancelledAt, &paymentID, &userID, &email, &firstName, &lastName); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{"id": utils.EncodeID(id), "status": status, "payment_status": paymentStatus, "total": totalValue, "created_at": createdAt, "paid_at": nullableTime(paidAt), "cancelled_at": nullableTime(cancelledAt), "payment_id": nullableInt(paymentID), "customer": gin.H{"id": utils.EncodeID(userID), "email": email, "first_name": firstName, "last_name": lastName}})
		}
		if err := rows.Err(); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func orderSort(value string) string {
	switch value {
	case "created_at":
		return "o.created_at ASC"
	case "total":
		return "o.total DESC"
	default:
		return "o.created_at DESC, o.id DESC"
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
	var status, paymentStatus, preferenceID, checkoutURL, environment string
	var total float64
	var createdAt time.Time
	var expiresAt, paidAt, cancelledAt sql.NullTime
	var paymentID sql.NullInt64
	var email, firstName, lastName, dni, streetAddress, streetNumber, postalCode, province, locality, phone string
	err := db.Pool.QueryRow(ctx, `SELECT o.user_id, o.status, COALESCE(o.payment_status, ''), o.total, o.created_at, o.expires_at, o.paid_at, o.cancelled_at, o.payment_id, COALESCE(o.active_payment_preference_id, ''), COALESCE(o.active_checkout_url, ''), COALESCE(o.active_payment_environment, ''), u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.dni, ''), COALESCE(u.street_address, ''), COALESCE(u.street_number, ''), COALESCE(u.postal_code, ''), COALESCE(u.province, ''), COALESCE(u.locality, ''), COALESCE(u.phone_number, '') FROM orders o JOIN users u ON u.id=o.user_id WHERE o.id=$1`, id).Scan(&userID, &status, &paymentStatus, &total, &createdAt, &expiresAt, &paidAt, &cancelledAt, &paymentID, &preferenceID, &checkoutURL, &environment, &email, &firstName, &lastName, &dni, &streetAddress, &streetNumber, &postalCode, &province, &locality, &phone)
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
	return gin.H{"id": utils.EncodeID(id), "status": status, "payment_status": paymentStatus, "total": total, "created_at": createdAt, "expires_at": nullableTime(expiresAt), "paid_at": nullableTime(paidAt), "cancelled_at": nullableTime(cancelledAt), "payment_id": nullableInt(paymentID), "active_payment_preference_id": preferenceID, "active_checkout_url": checkoutURL, "active_payment_environment": environment, "customer": gin.H{"id": utils.EncodeID(userID), "email": email, "first_name": firstName, "last_name": lastName, "dni": dni, "street_address": streetAddress, "street_number": streetNumber, "postal_code": postalCode, "province": province, "locality": locality, "phone_number": phone}, "items": items, "audit_logs": audit}, nil
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
		if err := restoreOrderStock(c, tx, id); err != nil {
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
		rows, err := db.Pool.Query(c, "SELECT u.id, u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.dni, ''), COALESCE(u.province, ''), COALESCE(u.locality, ''), COALESCE(u.phone_number, ''), COUNT(o.id), COALESCE(SUM(o.total) FILTER (WHERE o.status='paid'), 0) FROM users u LEFT JOIN orders o ON o.user_id=u.id WHERE "+whereSQL+" GROUP BY u.id ORDER BY "+customerSort(c.Query("sort"))+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
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

func customerSort(value string) string {
	switch value {
	case "email":
		return "u.email ASC"
	case "name":
		return "u.last_name ASC, u.first_name ASC"
	default:
		return "u.id DESC"
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

func AdminListAuditLogsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		logs, total, err := adminAuditLogsWithTotal(c, db, c.Query("actor_email"), c.Query("entity_type"), 0, c.Query("action"), page)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": logs, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func AdminListEntityAuditLogsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		entityID, ok := adminIDParam(c, "entity_id")
		if !ok {
			return
		}
		page := adminPagination(c)
		logs, total, err := adminAuditLogsWithTotal(c, db, "", c.Param("entity_type"), entityID, "", page)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": logs, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func adminAuditLogs(ctx context.Context, db *database.DB, actor, entityType string, entityID int, page adminPage) ([]gin.H, error) {
	logs, _, err := adminAuditLogsWithTotal(ctx, db, actor, entityType, entityID, "", page)
	return logs, err
}

func adminAuditLogsWithTotal(ctx context.Context, db *database.DB, actor, entityType string, entityID int, action string, page adminPage) ([]gin.H, int, error) {
	args := []any{strings.TrimSpace(actor), strings.TrimSpace(entityType), entityID, strings.TrimSpace(action)}
	whereSQL := "($1 = '' OR actor_email=$1) AND ($2 = '' OR entity_type=$2) AND ($3 = 0 OR entity_id=$3) AND ($4 = '' OR action=$4)"
	var total int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, page.PageSize, page.Offset)
	rows, err := db.Pool.Query(ctx, "SELECT id, actor_email, action, entity_type, entity_id, metadata, created_at FROM audit_logs WHERE "+whereSQL+" ORDER BY created_at DESC, id DESC LIMIT $5 OFFSET $6", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, rawEntityID int
		var actorEmail, actionValue, entityTypeValue string
		var metadata []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &actorEmail, &actionValue, &entityTypeValue, &rawEntityID, &metadata, &createdAt); err != nil {
			return nil, 0, err
		}
		var meta any = gin.H{}
		_ = json.Unmarshal(metadata, &meta)
		items = append(items, gin.H{"id": id, "actor_email": actorEmail, "action": actionValue, "entity_type": entityTypeValue, "entity_id": utils.EncodeID(rawEntityID), "metadata": meta, "created_at": createdAt})
	}
	return items, total, rows.Err()
}

func AdminObservabilityHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		dbStatus := "ready"
		if err := db.Pool.Ping(c); err != nil {
			dbStatus = "unavailable"
		}
		var pendingExpired int
		_ = db.Pool.QueryRow(c, "SELECT COUNT(*) FROM orders WHERE status='pending' AND active_payment_preference_id IS NULL AND COALESCE(expires_at, created_at + make_interval(secs => $1)) < NOW()", int(cfg.OrderPendingTTL.Seconds())).Scan(&pendingExpired)
		webhooks, _ := adminRecentPaymentWebhookEvents(c, db)
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": dbStatus, "pending_expired_orders": pendingExpired, "recent_payment_webhook_events": webhooks})
	}
}

func adminRecentPaymentWebhookEvents(ctx context.Context, db *database.DB) ([]gin.H, error) {
	rows, err := db.Pool.Query(ctx, "SELECT event_key, payment_id, order_id, status, received_at, processed_at, COALESCE(result, '') FROM payment_webhook_events ORDER BY received_at DESC LIMIT 20")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var key, status, result string
		var paymentID, orderID int
		var receivedAt time.Time
		var processedAt sql.NullTime
		if err := rows.Scan(&key, &paymentID, &orderID, &status, &receivedAt, &processedAt, &result); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"event_key": key, "payment_id": paymentID, "order_id": utils.EncodeID(orderID), "status": status, "received_at": receivedAt, "processed_at": nullableTime(processedAt), "result": result})
	}
	return items, rows.Err()
}

func AdminPaymentsProxyHandler(cfg *config.Config, targetPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.PaymentsServiceURL == "" {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service is not configured", nil)
			return
		}
		path := targetPath
		for _, param := range c.Params {
			path = strings.ReplaceAll(path, ":"+param.Key, url.PathEscape(param.Value))
		}
		endpoint := cfg.PaymentsServiceURL + path
		if c.Request.URL.RawQuery != "" {
			endpoint += "?" + c.Request.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint, nil)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		addInternalAdminHeaders(req, cfg.InternalWebhookSecret, nil, middleware.RequestID(c), middleware.CorrelationID(c))
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service unreachable", nil)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(resp.StatusCode, contentType, body)
	}
}

func addInternalAdminHeaders(req *http.Request, secret string, body []byte, requestID, correlationID string) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Service-Name", "selecto-ecommerce")
	req.Header.Set("X-Service-Timestamp", timestamp)
	req.Header.Set("X-Service-Signature", "sha256="+internalAdminSignature(secret, timestamp, body))
	req.Header.Set("X-Request-ID", requestID)
	req.Header.Set("X-Correlation-ID", correlationID)
}

func internalAdminSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
