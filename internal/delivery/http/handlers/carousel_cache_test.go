package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"Selecto-Ecommerce/internal/service/editorial"
	"github.com/gin-gonic/gin"
)

type cacheImageStore struct{}

func (cacheImageStore) List(context.Context, bool) ([]editorial.Slide, error) { return nil, nil }
func (cacheImageStore) Save(context.Context, int, editorial.Input, int, *editorial.Image, string) (int, error) {
	return 0, nil
}
func (cacheImageStore) Delete(context.Context, int, int, string) error { return nil }
func (cacheImageStore) Image(context.Context, int, bool) (editorial.Image, error) {
	return editorial.Image{Content: []byte("image"), MIME: "image/jpeg"}, nil
}

func TestCarouselImageCachingKeepsAdminPrivate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		path   string
		public bool
		cache  string
	}{
		{"/carousel-images/1?v=3", true, "public, max-age=3600"},
		{"/carousel-images/1", true, "no-store"},
		{"/admin/carousel-slides/1/image?v=3", false, "no-store"},
	} {
		r := gin.New()
		r.GET("/*path", func(c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: "1"}}
			CarouselImageHandler(cacheImageStore{}, tc.public)(c)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != tc.cache {
			t.Errorf("%s: status=%d cache=%q", tc.path, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}
