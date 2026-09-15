package postgres

import (
	"context"
	"errors"
	"fmt"

	"Selecto-Ecommerce/internal/service/editorial"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CarouselStore struct{ Pool *pgxpool.Pool }

const carouselJoins = ` FROM carousel_slides s LEFT JOIN products p ON p.id=s.product_id LEFT JOIN categories c ON c.id=s.category_id `
const carouselAvailable = `((s.target_kind='product' AND COALESCE(p.active,FALSE)) OR
 (s.target_kind='category' AND COALESCE(c.active,FALSE) AND EXISTS(SELECT 1 FROM products cp WHERE cp.category_id=c.id AND cp.active)))`

func (r CarouselStore) List(ctx context.Context, public bool) ([]editorial.Slide, error) {
	rows, err := r.Pool.Query(ctx, `SELECT s.id,s.title,s.subtitle,s.alt_text,s.cta_label,s.target_kind,
 COALESCE(s.product_id,s.category_id,0),s.sort_order,s.active,s.version,`+carouselAvailable+`,
 COALESCE(p.name,c.name,''),COALESCE(c.slug,'')`+carouselJoins+`WHERE NOT $1 OR (s.active AND `+carouselAvailable+`) ORDER BY s.sort_order,s.id LIMIT 20`, public)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []editorial.Slide{}
	for rows.Next() {
		var s editorial.Slide
		var target int
		var categorySlug string
		if err := rows.Scan(&s.ID, &s.Title, &s.Subtitle, &s.AltText, &s.CTALabel, &s.TargetKind, &target, &s.SortOrder, &s.Active, &s.Version, &s.DestinationAvailable, &s.DestinationName, &categorySlug); err != nil {
			return nil, err
		}
		if target > 0 {
			destination := s.DestinationName
			if s.TargetKind == "category" {
				destination = categorySlug
			}
			s.TargetID, s.Href = editorial.Destination(s.TargetKind, target, destination)
		}
		s.ImageURL = fmt.Sprintf("/carousel-images/%d?v=%d", s.ID, s.Version)
		if !public {
			s.ImageURL = fmt.Sprintf("/admin/carousel-slides/%d/image?v=%d", s.ID, s.Version)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r CarouselStore) Image(ctx context.Context, id int, public bool) (editorial.Image, error) {
	var img editorial.Image
	err := r.Pool.QueryRow(ctx, `SELECT s.content,s.mime_type`+carouselJoins+`WHERE s.id=$1 AND (NOT $2 OR (s.active AND `+carouselAvailable+`))`, id, public).Scan(&img.Content, &img.MIME)
	if errors.Is(err, pgx.ErrNoRows) {
		err = editorial.ErrNotFound
	}
	return img, err
}

func (r CarouselStore) Save(ctx context.Context, id int, in editorial.Input, target int, img *editorial.Image, actor string) (int, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// Serialize the bounded editorial collection, including concurrent create/delete.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(731402027)"); err != nil {
		return 0, err
	}
	if id == 0 {
		var count int
		if err = tx.QueryRow(ctx, "SELECT COUNT(*) FROM carousel_slides").Scan(&count); err != nil {
			return 0, err
		}
		if count >= editorial.MaxSlides {
			return 0, editorial.ErrConflict
		}
	} else {
		var version int
		if err = tx.QueryRow(ctx, "SELECT version FROM carousel_slides WHERE id=$1 FOR UPDATE", id).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
			return 0, editorial.ErrNotFound
		} else if err != nil {
			return 0, err
		}
		if version != in.Version {
			return 0, editorial.ErrConflict
		}
	}
	// Lock the target while publishing. Public queries re-check availability on every request.
	query := "SELECT active FROM products WHERE id=$1 FOR SHARE"
	if in.TargetKind == "category" {
		query = "SELECT active AND EXISTS(SELECT 1 FROM products WHERE category_id=$1 AND active) FROM categories WHERE id=$1 FOR SHARE"
	}
	var available bool
	if err = tx.QueryRow(ctx, query, target).Scan(&available); errors.Is(err, pgx.ErrNoRows) {
		return 0, editorial.ErrConflict
	} else if err != nil {
		return 0, err
	}
	if in.Active && !available {
		return 0, editorial.ErrConflict
	}
	var productID, categoryID any
	if in.TargetKind == "product" {
		productID = target
	} else {
		categoryID = target
	}
	var content []byte
	var mime string
	if img != nil {
		content, mime = img.Content, img.MIME
	}
	if id == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO carousel_slides(title,subtitle,alt_text,cta_label,target_kind,product_id,category_id,sort_order,active,content,mime_type)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, in.Title, in.Subtitle, in.AltText, in.CTALabel, in.TargetKind, productID, categoryID, in.SortOrder, in.Active, content, mime).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE carousel_slides SET title=$2,subtitle=$3,alt_text=$4,cta_label=$5,target_kind=$6,product_id=$7,category_id=$8,sort_order=$9,active=$10,
   content=COALESCE($11,content),mime_type=CASE WHEN $11::bytea IS NULL THEN mime_type ELSE $12 END,version=version+1,updated_at=NOW() WHERE id=$1`, id, in.Title, in.Subtitle, in.AltText, in.CTALabel, in.TargetKind, productID, categoryID, in.SortOrder, in.Active, content, mime)
	}
	if err != nil {
		return 0, err
	}
	if err = carouselAudit(ctx, tx, actor, id, "carousel_saved", in.Version); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func (r CarouselStore) Delete(ctx context.Context, id, version int, actor string) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(731402027)"); err != nil {
		return err
	}
	var current int
	if err := tx.QueryRow(ctx, "SELECT version FROM carousel_slides WHERE id=$1 FOR UPDATE", id).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
		return editorial.ErrConflict
	} else if err != nil {
		return err
	}
	if current != version {
		return editorial.ErrConflict
	}
	if err = carouselAudit(ctx, tx, actor, id, "carousel_deleted", version); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "DELETE FROM carousel_slides WHERE id=$1 AND version=$2", id, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return editorial.ErrConflict
	}
	return tx.Commit(ctx)
}

func carouselAudit(ctx context.Context, tx pgx.Tx, actor string, id int, action string, version int) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,metadata)
 VALUES($1,$2,'carousel_slide',$3,jsonb_build_object('previous_version',$4::int,'snapshot',
 (SELECT jsonb_build_object('title',title,'target_kind',target_kind,'product_id',product_id,'category_id',category_id,'active',active,'sort_order',sort_order,'version',version) FROM carousel_slides WHERE id=$3)))`, actor, action, id, version)
	return err
}
