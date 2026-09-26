package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func AdminOperationsListMarketingSubscriptionsHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		status := strings.TrimSpace(strings.ToLower(c.Query("status")))
		if status != "" && status != "subscribed" && status != "unsubscribed" {
			apperrors.BadRequest(c, "invalid status")
			return
		}
		syncStatus := strings.TrimSpace(strings.ToLower(c.Query("sync_status")))
		if syncStatus != "" && !validMarketingSyncStatus(syncStatus) {
			apperrors.BadRequest(c, "invalid sync status")
			return
		}
		where := "($1='' OR status=$1) AND ($2='' OR sync_status=$2)"
		var total int
		if err := db.Pool.QueryRow(c, "SELECT COUNT(*) FROM commerce.marketing_subscriptions WHERE "+where, status, syncStatus).Scan(&total); err != nil {
			apperrors.Internal(c)
			return
		}
		rows, err := db.Pool.Query(c, `SELECT id,email,status,source,consent_at,unsubscribed_at,
		 sync_status,sync_attempts,sync_error,synced_at,last_provider_event_at,suppression_reason,created_at,updated_at
		 FROM commerce.marketing_subscriptions WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`,
			status, syncStatus, page.PageSize, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id int64
			var email, itemStatus, source, itemSyncStatus, syncError, suppressionReason string
			var syncAttempts int
			var consentAt, unsubscribedAt, syncedAt, lastProviderEventAt sql.NullTime
			var createdAt, updatedAt time.Time
			if err := rows.Scan(&id, &email, &itemStatus, &source, &consentAt, &unsubscribedAt,
				&itemSyncStatus, &syncAttempts, &syncError, &syncedAt, &lastProviderEventAt,
				&suppressionReason, &createdAt, &updatedAt); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, gin.H{
				"id": id, "email": email, "status": itemStatus, "source": source,
				"consent_at": nullableTime(consentAt), "unsubscribed_at": nullableTime(unsubscribedAt),
				"sync_status": itemSyncStatus, "sync_attempts": syncAttempts, "sync_error": syncError,
				"synced_at": nullableTime(syncedAt), "last_provider_event_at": nullableTime(lastProviderEventAt),
				"suppression_reason": suppressionReason, "created_at": createdAt, "updated_at": updatedAt,
			})
		}
		if rows.Err() != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "total": total})
	}
}

func validMarketingSyncStatus(status string) bool {
	switch status {
	case "pending", "syncing", "synced", "failed", "suppressed":
		return true
	default:
		return false
	}
}
