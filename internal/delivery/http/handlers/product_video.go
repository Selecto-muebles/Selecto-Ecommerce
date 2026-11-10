package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

// Video reads use byte ranges directly in PostgreSQL instead of copying an
// entire clip into memory for every playback/seek. ServeContent handles Range,
// If-Range, HEAD, 206 and 416 consistently with the HTTP standard.
type productVideoReader struct {
	ctx            context.Context
	db             *database.DB
	id             int
	size, position int64
	cache          []byte
	cacheStart     int64
}

func (r *productVideoReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.position >= r.size {
		return 0, io.EOF
	}
	if r.position < r.cacheStart || r.position >= r.cacheStart+int64(len(r.cache)) {
		length := min(r.size-r.position, 512<<10)
		if err := r.db.Pool.QueryRow(r.ctx, "SELECT substring(content FROM $1::integer FOR $2::integer) FROM product_videos WHERE id=$3", r.position+1, length, r.id).Scan(&r.cache); err != nil {
			return 0, err
		}
		r.cacheStart = r.position
	}
	n := copy(p, r.cache[r.position-r.cacheStart:])
	r.position += int64(n)
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (r *productVideoReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.position
	case io.SeekEnd:
		offset += r.size
	default:
		return 0, fmt.Errorf("invalid seek origin")
	}
	if offset < 0 {
		return 0, fmt.Errorf("invalid seek offset")
	}
	r.position = offset
	return offset, nil
}

func serveProductVideo(c *gin.Context, db *database.DB, id, size int, created time.Time) {
	c.Header("Content-Type", "video/mp4")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("ETag", fmt.Sprintf(`"clip-%d-%d"`, id, size))
	http.ServeContent(c.Writer, c.Request, "product.mp4", created, &productVideoReader{ctx: c, db: db, id: id, size: int64(size)})
}

func productVideosByProduct(ctx context.Context, db *database.DB, ids []int) (map[int][]productImageResponse, error) {
	result := map[int][]productImageResponse{}
	rows, err := db.Pool.Query(ctx, "SELECT product_id,id,alt_text,sort_order FROM product_videos WHERE product_id=ANY($1) ORDER BY product_id,sort_order,id", ids)
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
		item.ID = "video-" + utils.EncodeID(id)
		item.URL = "/product-images/" + item.ID
		result[productID] = append(result[productID], item)
	}
	return result, rows.Err()
}

// Video IDs have a distinct namespace while reusing the existing Gateway route.
// A rollback to an older backend still sees only photos in product_images.
func productMediaIdentity(raw string) (int, string, error) {
	table := "product_images"
	if strings.HasPrefix(raw, "video-") {
		table = "product_videos"
		raw = strings.TrimPrefix(raw, "video-")
	}
	id, err := utils.DecodeID(raw)
	return id, table, err
}
