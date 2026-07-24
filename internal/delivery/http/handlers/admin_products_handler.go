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
		rows, err := db.Pool.Query(c, "SELECT id, name, COALESCE(sku, ''), price, stock, active, description, category, created_at, updated_at FROM products WHERE "+whereSQL+" ORDER BY "+adminservice.ProductSort(c.Query("sort"))+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
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
