package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	catalogservice "Selecto-Ecommerce/internal/service/catalog"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const maxProductImageBytes = 5 << 20

type productImageResponse struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	AltText   string `json:"alt_text"`
	SortOrder int    `json:"sort_order"`
}

type productOption = catalogservice.Option

func productImages(ctx context.Context, db *database.DB, productID int) ([]productImageResponse, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, alt_text, sort_order FROM product_images WHERE product_id=$1 ORDER BY sort_order, id`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]productImageResponse, 0)
	for rows.Next() {
		var id int
		var item productImageResponse
		if err := rows.Scan(&id, &item.AltText, &item.SortOrder); err != nil {
			return nil, err
		}
		item.ID = utils.EncodeID(id)
		item.URL = "/product-images/" + item.ID
		items = append(items, item)
	}
	return items, rows.Err()
}

func productImagesByProduct(ctx context.Context, db *database.DB, productIDs []int) (map[int][]productImageResponse, error) {
	result := make(map[int][]productImageResponse, len(productIDs))
	rows, err := db.Pool.Query(ctx, `SELECT product_id,id,alt_text,sort_order FROM product_images WHERE product_id=ANY($1) ORDER BY product_id,sort_order,id`, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var productID, id int
		var item productImageResponse
		if err := rows.Scan(&productID, &id, &item.AltText, &item.SortOrder); err != nil {
			return nil, err
		}
		item.ID = utils.EncodeID(id)
		item.URL = "/product-images/" + item.ID
		result[productID] = append(result[productID], item)
	}
	return result, rows.Err()
}

func productOptions(ctx context.Context, db *database.DB, productID int) ([]productOption, error) {
	rows, err := db.Pool.Query(ctx, `SELECT name, values, sort_order FROM product_options WHERE product_id=$1 ORDER BY sort_order, id`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]productOption, 0)
	for rows.Next() {
		var item productOption
		var raw []byte
		if err := rows.Scan(&item.Name, &raw, &item.SortOrder); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Values); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func productOptionsByProduct(ctx context.Context, db *database.DB, productIDs []int) (map[int][]productOption, error) {
	result := make(map[int][]productOption, len(productIDs))
	rows, err := db.Pool.Query(ctx, `SELECT product_id,name,values,sort_order FROM product_options WHERE product_id=ANY($1) ORDER BY product_id,sort_order,id`, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var productID int
		var item productOption
		var raw []byte
		if err := rows.Scan(&productID, &item.Name, &raw, &item.SortOrder); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Values); err != nil {
			return nil, err
		}
		result[productID] = append(result[productID], item)
	}
	return result, rows.Err()
}

func replaceProductOptions(ctx context.Context, tx pgx.Tx, productID int, options []productOption) error {
	if _, err := tx.Exec(ctx, `DELETE FROM product_options WHERE product_id=$1`, productID); err != nil {
		return err
	}
	for _, option := range options {
		values, err := json.Marshal(option.Values)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO product_options (product_id, name, values, sort_order) VALUES ($1,$2,$3,$4)`, productID, option.Name, values, option.SortOrder); err != nil {
			return err
		}
	}
	return nil
}

func ProductImageHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := utils.DecodeID(c.Param("id"))
		if err != nil || id <= 0 {
			apperrors.BadRequest(c, "invalid image id")
			return
		}
		var mimeType string
		var content []byte
		if err := db.Pool.QueryRow(c, `SELECT mime_type, content FROM product_images WHERE id=$1`, id).Scan(&mimeType, &content); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "image not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		c.Header("Cache-Control", "public, max-age=86400, immutable")
		c.Data(http.StatusOK, mimeType, content)
	}
}

func AdminUploadProductImageHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		productID, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		if err := c.Request.ParseMultipartForm(maxProductImageBytes); err != nil {
			apperrors.BadRequest(c, "invalid image upload")
			return
		}
		file, header, err := c.Request.FormFile("image")
		if err != nil {
			apperrors.BadRequest(c, "image is required")
			return
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, maxProductImageBytes+1))
		if err != nil || len(content) == 0 || len(content) > maxProductImageBytes {
			apperrors.BadRequest(c, "image must be between 1 byte and 5 MB")
			return
		}
		mimeType := http.DetectContentType(content)
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			apperrors.BadRequest(c, "image must be JPEG, PNG or WebP")
			return
		}
		if _, err := io.Copy(io.Discard, bytes.NewReader(content)); err != nil {
			apperrors.Internal(c)
			return
		}
		altText := strings.TrimSpace(c.PostForm("alt_text"))
		if altText == "" {
			altText = strings.TrimSpace(header.Filename)
		}
		if len(altText) > 180 {
			apperrors.BadRequest(c, "alt_text is too long")
			return
		}
		var exists bool
		if err := db.Pool.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM products WHERE id=$1)`, productID).Scan(&exists); err != nil || !exists {
			handleAdminLookupErr(c, pgx.ErrNoRows, "product not found")
			return
		}
		var id int
		if err := db.Pool.QueryRow(c, `INSERT INTO product_images (product_id,mime_type,alt_text,sort_order,content,size_bytes) VALUES ($1,$2,$3,COALESCE((SELECT MAX(sort_order)+1 FROM product_images WHERE product_id=$1),0),$4,$5) RETURNING id`, productID, mimeType, altText, content, len(content)).Scan(&id); err != nil {
			apperrors.Internal(c)
			return
		}
		_ = writeAudit(c, db, adminActor(c), "product_image_uploaded", "product", productID, gin.H{"image_id": utils.EncodeID(id), "size_bytes": len(content), "mime_type": mimeType})
		images, _ := productImages(c, db, productID)
		c.JSON(http.StatusCreated, images)
	}
}

func AdminUpdateProductImageHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		productID, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		imageID, err := utils.DecodeID(c.Param("image_id"))
		if err != nil || imageID <= 0 {
			apperrors.BadRequest(c, "invalid image id")
			return
		}
		var input struct {
			AltText   *string `json:"alt_text"`
			SortOrder *int    `json:"sort_order"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || (input.AltText == nil && input.SortOrder == nil) {
			apperrors.BadRequest(c, "at least one field is required")
			return
		}
		if input.SortOrder != nil && *input.SortOrder < 0 {
			apperrors.BadRequest(c, "sort_order must be non-negative")
			return
		}
		alt := ""
		order := 0
		if err := db.Pool.QueryRow(c, `SELECT alt_text,sort_order FROM product_images WHERE id=$1 AND product_id=$2`, imageID, productID).Scan(&alt, &order); err != nil {
			handleAdminLookupErr(c, err, "image not found")
			return
		}
		if input.AltText != nil {
			alt = strings.TrimSpace(*input.AltText)
			if len(alt) > 180 {
				apperrors.BadRequest(c, "alt_text is too long")
				return
			}
		}
		if input.SortOrder != nil {
			order = *input.SortOrder
		}
		if _, err := db.Pool.Exec(c, `UPDATE product_images SET alt_text=$1,sort_order=$2 WHERE id=$3 AND product_id=$4`, alt, order, imageID, productID); err != nil {
			apperrors.Internal(c)
			return
		}
		images, _ := productImages(c, db, productID)
		c.JSON(http.StatusOK, images)
	}
}

func AdminDeleteProductImageHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		productID, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		imageID, err := utils.DecodeID(c.Param("image_id"))
		if err != nil || imageID <= 0 {
			apperrors.BadRequest(c, "invalid image id")
			return
		}
		command, err := db.Pool.Exec(c, `DELETE FROM product_images WHERE id=$1 AND product_id=$2`, imageID, productID)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if command.RowsAffected() == 0 {
			apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "image not found", nil)
			return
		}
		_ = writeAudit(c, db, adminActor(c), "product_image_deleted", "product", productID, gin.H{"image_id": utils.EncodeID(imageID)})
		c.Status(http.StatusNoContent)
	}
}
