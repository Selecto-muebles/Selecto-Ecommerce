package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"
	"github.com/gin-gonic/gin"
)

func TestProductImageVariantsPreserveOriginalAndSupportRevalidation(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	var productID, imageID int
	if err := pool.QueryRow(ctx, "INSERT INTO products(name,price,stock) VALUES('Image variant test',100,7) RETURNING id").Scan(&productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID) })
	source := image.NewRGBA(image.Rect(0, 0, 1254, 1254))
	random := rand.New(rand.NewPCG(7, 11))
	for y := 0; y < 1254; y++ {
		for x := 0; x < 1254; x++ {
			source.SetRGBA(x, y, color.RGBA{uint8(random.IntN(256)), uint8(random.IntN(256)), uint8(random.IntN(256)), 255})
		}
	}
	var original bytes.Buffer
	if err := png.Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO product_images(product_id,mime_type,alt_text,content,size_bytes) VALUES($1,'image/png','Original',$2,$3) RETURNING id", productID, original.Bytes(), original.Len()).Scan(&imageID); err != nil {
		t.Fatal(err)
	}
	get := func(query, etag string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: utils.EncodeID(imageID)}}
		c.Request = httptest.NewRequest("GET", "/product-images/"+utils.EncodeID(imageID)+query, nil)
		if etag != "" {
			c.Request.Header.Set("If-None-Match", etag)
		}
		ProductImageHandler(&database.DB{Pool: pool})(c)
		c.Writer.WriteHeaderNow()
		return w
	}
	raw := get("", "")
	if raw.Code != 200 || !bytes.Equal(raw.Body.Bytes(), original.Bytes()) {
		t.Fatal("legacy original changed")
	}
	for _, width := range []int{240, 640, 1280, 1920} {
		path := fmt.Sprintf("?w=%d", width)
		w := get(path, "")
		if w.Code != 200 {
			t.Fatalf("width %d: %d %s", width, w.Code, w.Body.String())
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
		if err != nil || cfg.Width != min(width, 1254) || cfg.Height != min(width, 1254) {
			t.Fatalf("width %d: %+v %v", width, cfg, err)
		}
		if w.Body.Len() >= original.Len() {
			t.Fatal("no byte reduction")
		}
		if w.Header().Get("Cache-Control") != "public, max-age=86400, immutable" || w.Header().Get("ETag") == "" {
			t.Fatal("missing cache headers")
		}
		if repeat := get(path, w.Header().Get("ETag")); repeat.Code != 304 || repeat.Body.Len() != 0 {
			t.Fatal("conditional request did not return 304")
		}
		t.Logf("width=%d original=%d variant=%d", width, original.Len(), w.Body.Len())
	}
	if w := get("?w=100000", ""); w.Code != 400 {
		t.Fatal("unbounded width accepted")
	}
	var saved []byte
	if err := pool.QueryRow(ctx, "SELECT content FROM product_images WHERE id=$1", imageID).Scan(&saved); err != nil || !bytes.Equal(saved, original.Bytes()) {
		t.Fatal("source SQL bytes changed", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM product_images WHERE id=$1", imageID); err != nil {
		t.Fatal(err)
	}
	if w := get("?w=240", ""); w.Code != 404 {
		t.Fatal("deleted image survived through cache")
	}
}

func TestProductRenditionCacheCoalescesAndBoundsMemory(t *testing.T) {
	cache := imageRenditionCache{maxBytes: 8, maxEntries: 2}
	key := imageRenditionKey{id: 1, width: 240}
	var calls atomic.Int32
	fetch := func() ([]byte, string, error) { calls.Add(1); return []byte("raw"), "image/webp", nil }
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			if _, err := cache.load(key, fetch); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate transformation: %d", calls.Load())
	}
	for _, id := range []int{2, 3, 4} {
		if _, err := cache.load(imageRenditionKey{id: id, width: 240}, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if cache.bytes > 8 || cache.order.Len() > 2 {
		t.Fatal("cache exceeded memory bound")
	}
	if _, ok := cache.get(key); ok {
		t.Fatal("least-recent image was not evicted")
	}
}
