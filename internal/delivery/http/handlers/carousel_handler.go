package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"Selecto-Ecommerce/internal/service/catalog"
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
		c.JSON(http.StatusOK, gin.H{"items": items, "carousel_media": true})
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
		_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(90 * time.Second))
		_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(100 * time.Second))
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, editorial.MaxImageBytes+catalog.MaxVideoBytes+(64<<10))
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
			_ = file.Close()
			if err != nil || len(content) == 0 {
				carouselError(c, editorial.ErrInvalid)
				return
			}
			img = &editorial.Image{Content: content}
		} else if !errors.Is(err, http.ErrMissingFile) {
			carouselError(c, editorial.ErrInvalid)
			return
		}
		video, _, videoErr := c.Request.FormFile("video")
		if videoErr == nil {
			defer video.Close()
			data, err := io.ReadAll(io.LimitReader(video, catalog.MaxVideoBytes+1))
			_ = video.Close()
			if err != nil || len(data) == 0 || len(data) > catalog.MaxVideoBytes {
				carouselError(c, editorial.ErrInvalid)
				return
			}
			if img == nil {
				img = &editorial.Image{}
			}
			img.Video = data
		} else if !errors.Is(videoErr, http.ErrMissingFile) {
			carouselError(c, editorial.ErrInvalid)
			return
		}
		// Free multipart temp storage before decoding/resizing the poster.
		_ = c.Request.MultipartForm.RemoveAll()
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
		c.Header("Cache-Control", "no-store")
		id, ok := carouselID(c)
		if !ok {
			return
		}
		cache := "private, no-cache, must-revalidate"
		if public {
			cache = "public, max-age=60, must-revalidate"
		}
		c.Header("X-Content-Type-Options", "nosniff")
		if c.Query("media") == "video" {
			media, ok := store.(editorial.MediaStore)
			if !ok {
				carouselError(c, editorial.ErrNotFound)
				return
			}
			clip, err := media.Video(c, id, public)
			if err != nil {
				carouselError(c, err)
				return
			}
			c.Header("Cache-Control", cache)
			c.Header("Content-Type", "video/mp4")
			c.Header("ETag", fmt.Sprintf(`"carousel-video-%d-%d"`, id, clip.Version))
			http.ServeContent(c.Writer, c.Request, "banner.mp4", time.Time{}, clip.Content)
			return
		}
		width := 0
		if raw := c.Query("w"); raw != "" {
			width, _ = strconv.Atoi(raw)
			if width != 960 && width != 1920 {
				carouselError(c, editorial.ErrInvalid)
				return
			}
		}
		var img editorial.Image
		var err error
		if media, ok := store.(editorial.MediaStore); ok && width > 0 {
			img, err = media.VariantImage(c, id, public, width)
		} else {
			img, err = store.Image(c, id, public)
		}
		if err != nil {
			carouselError(c, err)
			return
		}
		c.Header("Cache-Control", cache)
		c.Header("Content-Type", img.MIME)
		digest := sha256.Sum256(img.Content)
		c.Header("ETag", fmt.Sprintf(`"carousel-%d-%x"`, id, digest[:12]))
		http.ServeContent(c.Writer, c.Request, "banner", time.Time{}, bytes.NewReader(img.Content))
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
		apperrors.BadRequest(c, "Revisá los campos y la portada PNG/JPEG (hasta 2 MB y 16 megapíxeles). El video debe ser MP4 H.264, hasta 60 segundos y 20 MB")
	case errors.Is(err, editorial.ErrConflict):
		apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, err.Error(), nil)
	case errors.Is(err, editorial.ErrNotFound):
		apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, err.Error(), nil)
	default:
		apperrors.Internal(c)
	}
}
