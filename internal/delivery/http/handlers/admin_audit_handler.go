package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

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
