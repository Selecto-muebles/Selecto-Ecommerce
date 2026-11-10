package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"

	"Selecto-Ecommerce/internal/service/editorial"
	"github.com/jackc/pgx/v5"
)

func saveCarouselMedia(ctx context.Context, tx pgx.Tx, id int, in editorial.Input, img *editorial.Image) error {
	var small, large, video []byte
	var sm, lm string
	if img != nil {
		small, large, video = img.Small.Content, img.Large.Content, img.Video
		sm, lm = img.Small.MIME, img.Large.MIME
	}
	_, err := tx.Exec(ctx, `INSERT INTO carousel_media(slide_id,slide_version,small_content,small_mime,large_content,large_mime,video_content)
 VALUES($1,(SELECT version FROM carousel_slides WHERE id=$1),$2,NULLIF($3,''),$4,NULLIF($5,''),$6)
 ON CONFLICT(slide_id) DO UPDATE SET slide_version=CASE WHEN EXCLUDED.small_content IS NOT NULL OR carousel_media.slide_version=$8 THEN EXCLUDED.slide_version ELSE carousel_media.slide_version END,
 small_content=COALESCE(EXCLUDED.small_content,carousel_media.small_content),small_mime=COALESCE(EXCLUDED.small_mime,carousel_media.small_mime),
 large_content=COALESCE(EXCLUDED.large_content,carousel_media.large_content),large_mime=COALESCE(EXCLUDED.large_mime,carousel_media.large_mime),
 video_content=CASE WHEN $7 THEN NULL ELSE COALESCE(EXCLUDED.video_content,carousel_media.video_content) END`, id, small, sm, large, lm, video, in.RemoveVideo, in.Version)
	return err
}

func (r CarouselStore) VariantImage(ctx context.Context, id int, public bool, width int) (editorial.Image, error) {
	var img editorial.Image
	var data []byte
	var mime string
	err := r.Pool.QueryRow(ctx, `SELECT s.version,COALESCE(CASE WHEN m.slide_version=s.version THEN CASE WHEN $3=960 THEN m.small_content ELSE m.large_content END END,''::bytea),
 COALESCE(CASE WHEN m.slide_version=s.version THEN CASE WHEN $3=960 THEN m.small_mime ELSE m.large_mime END END,'')`+carouselJoins+`LEFT JOIN carousel_media m ON m.slide_id=s.id WHERE s.id=$1 AND (NOT $2 OR (s.active AND `+carouselAvailable+`))`, id, public, width).Scan(&img.Version, &data, &mime)
	if errors.Is(err, pgx.ErrNoRows) {
		return img, editorial.ErrNotFound
	}
	if err != nil {
		return img, err
	}
	if len(data) > 0 {
		img.Content, img.MIME = data, mime
		return img, nil
	}
	// Legacy originals are retained. Populate their derivatives once on first use.
	img, err = r.Image(ctx, id, public)
	if err != nil {
		return img, err
	}
	if err = editorial.PreparePoster(&img); err != nil {
		return img, nil
	}
	_, err = r.Pool.Exec(ctx, `INSERT INTO carousel_media(slide_id,slide_version,small_content,small_mime,large_content,large_mime)
 SELECT id,version,$3,$4,$5,$6 FROM carousel_slides WHERE id=$1 AND version=$2
 ON CONFLICT(slide_id) DO UPDATE SET slide_version=EXCLUDED.slide_version,small_content=EXCLUDED.small_content,small_mime=EXCLUDED.small_mime,large_content=EXCLUDED.large_content,large_mime=EXCLUDED.large_mime
 WHERE carousel_media.slide_version<=EXCLUDED.slide_version`, id, img.Version, img.Small.Content, img.Small.MIME, img.Large.Content, img.Large.MIME)
	if err != nil {
		return img, err
	}
	selected := img.Large
	if width == 960 {
		selected = img.Small
	}
	img.Content, img.MIME = selected.Content, selected.MIME
	return img, nil
}

func (r CarouselStore) Video(ctx context.Context, id int, public bool) (editorial.Clip, error) {
	var out editorial.Clip
	var size int64
	err := r.Pool.QueryRow(ctx, `SELECT s.version,OCTET_LENGTH(m.video_content)`+carouselJoins+`JOIN carousel_media m ON m.slide_id=s.id WHERE s.id=$1 AND m.video_content IS NOT NULL AND (NOT $2 OR (s.active AND `+carouselAvailable+`))`, id, public).Scan(&out.Version, &size)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, editorial.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.Content = &carouselVideoReader{store: r, ctx: ctx, id: id, size: size}
	return out, nil
}

type carouselVideoReader struct {
	store                      CarouselStore
	ctx                        context.Context
	id                         int
	size, position, cacheStart int64
	cache                      []byte
}

func (r *carouselVideoReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.position >= r.size {
		return 0, io.EOF
	}
	if r.position < r.cacheStart || r.position >= r.cacheStart+int64(len(r.cache)) {
		if err := r.store.Pool.QueryRow(r.ctx, "SELECT substring(video_content FROM $1::integer FOR $2::integer) FROM carousel_media WHERE slide_id=$3", r.position+1, min(r.size-r.position, 512<<10), r.id).Scan(&r.cache); err != nil {
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
func (r *carouselVideoReader) Seek(offset int64, whence int) (int64, error) {
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
