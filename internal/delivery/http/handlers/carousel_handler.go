package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"Selecto-Ecommerce/internal/service/editorial"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"github.com/gin-gonic/gin"
)

func ListCarouselHandler(store editorial.Store, public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := store.List(c, public)
		if err != nil {
			carouselError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func SaveCarouselHandler(store editorial.Store, create bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := 0
		if !create {
			var ok bool
			id, ok = carouselID(c)
			if !ok {
				return
			}
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, editorial.MaxImageBytes+(64<<10))
		if err := c.Request.ParseMultipartForm(editorial.MaxImageBytes + (64 << 10)); err != nil {
			carouselError(c, editorial.ErrInvalid)
			return
		}
		defer c.Request.MultipartForm.RemoveAll()
		var in editorial.Input
		if err := json.Unmarshal([]byte(c.PostForm("payload")), &in); err != nil {
			carouselError(c, editorial.ErrInvalid)
			return
		}
		var img *editorial.Image
		file, _, err := c.Request.FormFile("image")
		if err == nil {
			defer file.Close()
			content, err := io.ReadAll(io.LimitReader(file, editorial.MaxImageBytes+1))
			if err != nil {
				carouselError(c, editorial.ErrInvalid)
				return
			}
			img = &editorial.Image{Content: content}
		} else if !errors.Is(err, http.ErrMissingFile) {
			carouselError(c, editorial.ErrInvalid)
			return
		}
		saved, err := editorial.Save(c, store, id, in, img, adminActor(c))
		if err != nil {
			carouselError(c, err)
			return
		}
		status := http.StatusOK
		if create {
			status = http.StatusCreated
		}
		c.JSON(status, gin.H{"id": saved})
	}
}

func DeleteCarouselHandler(store editorial.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := carouselID(c)
		if !ok {
			return
		}
		version, err := strconv.Atoi(c.Query("version"))
		if err != nil || version < 1 {
			carouselError(c, editorial.ErrInvalid)
			return
		}
		if err := store.Delete(c, id, version, adminActor(c)); err != nil {
			carouselError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func CarouselImageHandler(store editorial.Store, public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := carouselID(c)
		if !ok {
			return
		}
		img, err := store.Image(c, id, public)
		if err != nil {
			carouselError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, img.MIME, img.Content)
	}
}

func carouselID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		carouselError(c, editorial.ErrInvalid)
		return 0, false
	}
	return id, true
}

func carouselError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, editorial.ErrInvalid):
		apperrors.BadRequest(c, "invalid carousel: check fields and PNG/JPEG image (maximum 2 MB)")
	case errors.Is(err, editorial.ErrConflict):
		apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, err.Error(), nil)
	case errors.Is(err, editorial.ErrNotFound):
		apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, err.Error(), nil)
	default:
		apperrors.Internal(c)
	}
}
