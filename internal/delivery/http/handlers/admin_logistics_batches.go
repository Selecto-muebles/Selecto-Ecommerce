package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/service/logistics"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

func AdminLogisticsBatchListHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := (logistics.Store{Pool: db.Pool}).List(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		next := logistics.NextCutoff(logistics.LatestDueCutoff(time.Now()))
		phone := strings.TrimSpace(os.Getenv("GESTIONAR_WHATSAPP_DESTINATION"))
		for _, digit := range phone {
			if digit < '0' || digit > '9' {
				phone = ""
				break
			}
		}
		if len(phone) < 7 || len(phone) > 15 {
			phone = ""
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "next_cutoff_at": next, "timezone": "America/Argentina/Buenos_Aires", "cutoff_hour": logistics.CutoffHour, "whatsapp_destination": phone})
	}
}

func AdminLogisticsBatchPrepareHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		created, err := (logistics.Store{Pool: db.Pool}).PrepareDue(c, time.Now())
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"created": created})
	}
}

func logisticsBatchID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apperrors.BadRequest(c, "invalid batch id")
		return 0, false
	}
	return id, true
}

func AdminLogisticsBatchExportHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := logisticsBatchID(c)
		if !ok {
			return
		}
		content, hash, err := (logistics.Store{Pool: db.Pool}).CSV(c, id)
		if errors.Is(err, logistics.ErrNotFound) {
			apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "batch not found", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Content-SHA256", hash)
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="selecto-tanda-%d.csv"`, id))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", content)
	}
}

func AdminLogisticsBatchHandoffHandler(db *database.DB) gin.HandlerFunc {
	type request struct {
		Channel   string `json:"channel"`
		Recipient string `json:"recipient"`
		Note      string `json:"note"`
	}
	return func(c *gin.Context) {
		id, ok := logisticsBatchID(c)
		if !ok {
			return
		}
		var input request
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid handoff")
			return
		}
		err := (logistics.Store{Pool: db.Pool}).ConfirmHandoff(c, id, c.GetString("email"), input.Channel, input.Recipient, input.Note)
		if errors.Is(err, logistics.ErrNotFound) {
			apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "batch not found", nil)
			return
		}
		if errors.Is(err, logistics.ErrInvalidHandoff) {
			apperrors.BadRequest(c, "invalid handoff")
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "handed_off"})
	}
}
