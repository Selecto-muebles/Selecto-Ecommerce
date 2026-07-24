package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	catalogservice "Selecto-Ecommerce/internal/service/catalog"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

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
			options, err = catalogservice.NormalizeOptions(*input.Options)
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
			options, err := catalogservice.NormalizeOptions(*input.Options)
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
