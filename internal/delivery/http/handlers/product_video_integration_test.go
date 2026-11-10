package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"
	"github.com/gin-gonic/gin"
)

func TestProductVideoGalleryAndByteRanges(t *testing.T) {
	pool := integrationPool(t)
	db := &database.DB{Pool: pool}
	ctx := context.Background()
	var id int
	if err := pool.QueryRow(ctx, "INSERT INTO products(name,price,stock,active) VALUES('Video test',100,5,TRUE) RETURNING id").Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product' AND entity_id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", id)
	})
	clip, err := os.ReadFile("../../../service/catalog/testdata/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body io.Reader, typ, rangeHeader string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(id)}}
		c.Request = httptest.NewRequest(method, path, body)
		c.Request.Header.Set("Content-Type", typ)
		if rangeHeader != "" {
			c.Request.Header.Set("Range", rangeHeader)
		}
		handler(c)
		c.Writer.WriteHeaderNow()
		return w
	}
	upload := func(content []byte) *httptest.ResponseRecorder {
		var b bytes.Buffer
		writer := multipart.NewWriter(&b)
		part, _ := writer.CreateFormFile("image", "clip.mp4")
		_, _ = part.Write(content)
		_ = writer.WriteField("alt_text", "Demostración")
		_ = writer.Close()
		return request("POST", "/admin/products/"+utils.EncodeID(id)+"/images", &b, writer.FormDataContentType(), "", AdminUploadProductImageHandler(db))
	}
	if w := upload([]byte("not mp4")); w.Code != 400 {
		t.Fatalf("invalid upload %d %s", w.Code, w.Body.String())
	}
	var photo bytes.Buffer
	_ = png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if w := upload(photo.Bytes()); w.Code != 201 {
		t.Fatalf("photo upload %d %s", w.Code, w.Body.String())
	}
	w := upload(clip)
	if w.Code != 201 {
		t.Fatalf("upload %d %s", w.Code, w.Body.String())
	}
	images, err := productImages(ctx, db, id)
	if err != nil || len(images) != 1 {
		t.Fatalf("video leaked into photos: %v %v", images, err)
	}
	videos, err := productVideosByProduct(ctx, db, []int{id})
	if err != nil || len(videos[id]) != 1 {
		t.Fatalf("missing video %v %v", videos, err)
	}
	videoKey := videos[id][0].ID
	serve := func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: videoKey}}
		ProductImageHandler(db)(c)
	}
	for _, tc := range []struct {
		header             string
		status, start, end int
	}{
		{"bytes=0-31", 206, 0, 32}, {"bytes=-16", 206, len(clip) - 16, len(clip)},
		{"bytes=32-", 206, 32, len(clip)}, {fmt.Sprintf("bytes=%d-", len(clip)+1), 416, 0, 0},
		{"", 200, 0, len(clip)},
	} {
		w = request("GET", videos[id][0].URL, nil, "", tc.header, serve)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.header, w.Code, w.Body.String())
		}
		if tc.status != 416 && !bytes.Equal(w.Body.Bytes(), clip[tc.start:tc.end]) {
			t.Fatalf("incorrect byte range %s", tc.header)
		}
	}
	if w.Header().Get("Content-Type") != "video/mp4" || w.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatal("missing playback headers")
	}
	item, err := adminProduct(ctx, db, id)
	if err != nil || len(item["videos"].([]productImageResponse)) != 1 {
		t.Fatalf("admin video missing %v", err)
	}
	var raw bytes.Buffer
	_ = json.NewEncoder(&raw).Encode(map[string]any{"alt_text": "Nuevo texto", "sort_order": 0})
	mutate := func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "image_id", Value: videoKey})
		AdminUpdateProductImageHandler(db)(c)
	}
	if w = request("PATCH", "/admin/products/"+utils.EncodeID(id)+"/images/"+videoKey, &raw, "application/json", "", mutate); w.Code != 200 {
		t.Fatalf("edit %d %s", w.Code, w.Body.String())
	}
	remove := func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "image_id", Value: videoKey})
		AdminDeleteProductImageHandler(db)(c)
	}
	if w = request("DELETE", "/admin/products/"+utils.EncodeID(id)+"/images/"+videoKey, nil, "", "", remove); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
}
