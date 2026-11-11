package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func ProductImageHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, table, err := productMediaIdentity(c.Param("id"))
		if err != nil || id <= 0 {
			apperrors.BadRequest(c, "invalid image id")
			return
		}
		width := 0
		if raw := c.Query("w"); raw != "" {
			width, _ = strconv.Atoi(raw)
			if width != 240 && width != 640 && width != 1280 && width != 1920 {
				apperrors.BadRequest(c, "invalid image width")
				return
			}
		}
		var mimeType string
		var content []byte
		var size int
		var created time.Time
		if err := db.Pool.QueryRow(c, fmt.Sprintf(`SELECT mime_type,CASE WHEN mime_type='video/mp4' OR $2::integer>0 THEN NULL ELSE content END,size_bytes,created_at FROM %s WHERE id=$1`, table), id, width).Scan(&mimeType, &content, &size, &created); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.Header("Cache-Control", "no-store")
				apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "image not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		c.Header("Cache-Control", "public, max-age=86400, immutable")
		if mimeType == "video/mp4" {
			serveProductVideo(c, db, id, size, created)
			return
		}
		if width > 0 {
			key := imageRenditionKey{pool: db.Pool, id: id, width: width, size: size, created: created.UnixNano()}
			variant, err := productRenditions.load(key, func() ([]byte, string, error) {
				var original []byte
				err := db.Pool.QueryRow(c, "SELECT content FROM product_images WHERE id=$1", id).Scan(&original)
				return original, mimeType, err
			})
			if err != nil {
				c.Header("Cache-Control", "no-store")
				if errors.Is(err, pgx.ErrNoRows) {
					apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "image not found", nil)
					return
				}
				apperrors.Internal(c)
				return
			}
			c.Header("Content-Type", variant.mime)
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("ETag", variant.etag)
			http.ServeContent(c.Writer, c.Request, "product-image", created, bytes.NewReader(variant.content))
			return
		}
		c.Data(http.StatusOK, mimeType, content)
	}
}
