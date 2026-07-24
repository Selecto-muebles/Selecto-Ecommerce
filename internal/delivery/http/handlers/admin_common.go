package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
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
