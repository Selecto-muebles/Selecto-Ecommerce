package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/service/editorial"
	"Selecto-Ecommerce/internal/shared/utils"
	"github.com/gin-gonic/gin"
)

func TestCarouselMediaCacheVideoAndPublicationIsolation(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	store := postgres.CarouselStore{Pool: pool}
	var productID int
	if err := pool.QueryRow(ctx, "INSERT INTO products(name,price,stock,active) VALUES('Carousel media test',100,5,TRUE) RETURNING id").Scan(&productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID) })
	source := image.NewRGBA(image.Rect(0, 0, 2400, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 2400; x++ {
			source.SetRGBA(x, y, color.RGBA{uint8(x / 10), uint8(y / 5), 80, 255})
		}
	}
	var pngData bytes.Buffer
	_ = png.Encode(&pngData, source)
	in := editorial.Input{Title: "Portada", AltText: "Entrenamiento", CTALabel: "Ver producto", TargetKind: "product", TargetID: utils.EncodeID(productID), Active: true}
	id, err := editorial.Save(ctx, store, 0, in, &editorial.Image{Content: pngData.Bytes()}, "carousel-media@test.invalid")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM carousel_slides WHERE id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='carousel_slide' AND entity_id=$1", id)
	})
	r := gin.New()
	r.GET("/carousel-images/:id", CarouselImageHandler(store, true))
	r.GET("/admin/carousel-slides/:id/image", CarouselImageHandler(store, false))
	get := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		r.ServeHTTP(w, req)
		return w
	}
	path := "/carousel-images/" + strconv.Itoa(id)
	w := get(path+"?w=960", nil)
	if w.Code != 200 || w.Header().Get("ETag") == "" || w.Header().Get("Cache-Control") != "public, max-age=60, must-revalidate" {
		t.Fatalf("cover headers %d %v", w.Code, w.Header())
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil || cfg.Width != 960 || cfg.Height != 400 {
		t.Fatalf("cover dimensions %+v %v", cfg, err)
	}
	if w = get(path+"?w=960", map[string]string{"If-None-Match": w.Header().Get("ETag")}); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatalf("cache miss %d", w.Code)
	}
	clip, err := os.ReadFile("../../../service/catalog/testdata/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	in.Version = 1
	if _, err = editorial.Save(ctx, store, id, in, &editorial.Image{Video: clip}, "carousel-media@test.invalid"); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list {
		if s.ID == id {
			found = s.HasVideo
		}
	}
	if !found {
		t.Fatal("video not announced")
	}
	w = get(path+"?media=video", map[string]string{"Range": "bytes=0-31"})
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), clip[:32]) || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("playback %d %s", w.Code, w.Body.String())
	}
	if original, err := store.Image(ctx, id, false); err != nil || !bytes.Equal(original.Content, pngData.Bytes()) {
		t.Fatal("poster original was overwritten", err)
	}
	in.Version = 2
	in.Active = false
	if _, err = editorial.Save(ctx, store, id, in, nil, "carousel-media@test.invalid"); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"?w=960", "?media=video"} {
		w = get(path+suffix, nil)
		if w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("draft leaked %d %s", w.Code, suffix)
		}
	}
	w = get(fmt.Sprintf("/admin/carousel-slides/%d/image?w=960", id), nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-cache, must-revalidate" {
		t.Fatal("private preview unavailable")
	}
	in.Version = 3
	in.Active = true
	in.RemoveVideo = true
	if _, err = editorial.Save(ctx, store, id, in, nil, "carousel-media@test.invalid"); err != nil {
		t.Fatal(err)
	}
	w = get(path+"?media=video", nil)
	if w.Code != 404 {
		t.Fatal("removed clip still served")
	}
	if original, err := store.Image(ctx, id, false); err != nil || !bytes.Equal(original.Content, pngData.Bytes()) {
		t.Fatal("video removal changed original poster", err)
	}
	// Older revisions can still edit the original. A later metadata-only save
	// must not relabel an obsolete derivative as fresh.
	var replacement bytes.Buffer
	_ = png.Encode(&replacement, image.NewRGBA(image.Rect(0, 0, 8, 4)))
	if _, err := pool.Exec(ctx, "UPDATE carousel_slides SET content=$1,mime_type='image/png',version=version+1 WHERE id=$2", replacement.Bytes(), id); err != nil {
		t.Fatal(err)
	}
	in.Version = 5
	if _, err := editorial.Save(ctx, store, id, in, nil, "carousel-media@test.invalid"); err != nil {
		t.Fatal(err)
	}
	w = get(path+"?w=960", nil)
	cfg, _, err = image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil || cfg.Width != 8 || cfg.Height != 4 {
		t.Fatal("stale derivative survived legacy write", cfg, err)
	}
}
