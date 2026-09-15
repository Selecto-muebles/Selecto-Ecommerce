package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/service/editorial"
	"Selecto-Ecommerce/internal/shared/utils"
)

func TestCarouselCategoryDestinationUsesCanonicalSlug(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	store := postgres.CarouselStore{Pool: pool}
	suffix := time.Now().UnixNano()
	name := fmt.Sprintf("Entrenamiento %d", suffix)
	slug := fmt.Sprintf("entrenamiento-%d", suffix)
	var categoryID int
	if err := pool.QueryRow(ctx, `INSERT INTO categories(name,slug,active) VALUES ($1,$2,TRUE) RETURNING id`, name, slug).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM categories WHERE id=$1", categoryID) })
	var productID int
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,price,stock,category,category_id,active)
	 VALUES ($1,100,1,$2,$3,TRUE) RETURNING id`, "Destino "+name, name, categoryID).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID) })

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	in := editorial.Input{
		Title:      "Colección de " + name,
		AltText:    "Equipamiento para " + name,
		CTALabel:   "Ver categoría",
		TargetKind: "category",
		TargetID:   strconv.Itoa(categoryID),
		Active:     true,
	}
	id, err := editorial.Save(ctx, store, 0, in, &editorial.Image{Content: buf.Bytes()}, "carousel@test.invalid")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM carousel_slides WHERE id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='carousel_slide' AND entity_id=$1", id)
	})

	slides, err := store.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, slide := range slides {
		if slide.ID == id {
			if slide.DestinationName != name || slide.Href != "/?category="+slug {
				t.Fatalf("category destination mismatch: %+v", slide)
			}
			return
		}
	}
	t.Fatal("published category slide missing")
}

func TestCarouselPublicationConcurrencyAndDeletedDestination(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	store := postgres.CarouselStore{Pool: pool}
	var product int
	if err := pool.QueryRow(ctx, "INSERT INTO products(name,price,stock) VALUES('Carousel test',100,1) RETURNING id").Scan(&product); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", product) })
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	in := editorial.Input{Title: "Colección", AltText: "Equipamiento", CTALabel: "Ver producto", TargetKind: "product", TargetID: utils.EncodeID(product)}
	id, err := editorial.Save(ctx, store, 0, in, &editorial.Image{Content: buf.Bytes()}, "carousel@test.invalid")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM carousel_slides WHERE id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='carousel_slide' AND entity_id=$1", id)
	})
	if _, err := store.Image(ctx, id, true); !errors.Is(err, editorial.ErrNotFound) {
		t.Fatal("draft image leaked")
	}
	if _, err := store.Image(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	in.Active = true
	in.Version = 1
	if _, err := editorial.Save(ctx, store, id, in, nil, "carousel@test.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := editorial.Save(ctx, store, id, in, nil, "carousel@test.invalid"); !errors.Is(err, editorial.ErrConflict) {
		t.Fatal("stale version overwrote content")
	}
	slides, err := store.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range slides {
		if s.ID == id {
			found = s.DestinationAvailable && s.Href == "/productos/"+in.TargetID
		}
	}
	if !found {
		t.Fatal("published slide missing")
	}
	in.Version = 2
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := editorial.Save(ctx, store, id, in, nil, "carousel@test.invalid"); results <- err }()
	}
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, editorial.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent writes: success=%d conflict=%d", success, conflict)
	}
	if _, err := pool.Exec(ctx, "UPDATE products SET active=FALSE WHERE id=$1", product); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Image(ctx, id, true); !errors.Is(err, editorial.ErrNotFound) {
		t.Fatal("inactive destination image leaked")
	}
	in.Version = 3
	if _, err := editorial.Save(ctx, store, id, in, nil, "carousel@test.invalid"); !errors.Is(err, editorial.ErrConflict) {
		t.Fatal("inactive destination published")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM products WHERE id=$1", product); err != nil {
		t.Fatal("carousel blocked product deletion", err)
	}
	slides, err = store.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slides {
		if s.ID == id {
			t.Fatal("deleted destination visible")
		}
	}
	if err := store.Delete(ctx, id, 1, "carousel@test.invalid"); !errors.Is(err, editorial.ErrConflict) {
		t.Fatal("stale delete accepted")
	}
	if err := store.Delete(ctx, id, 3, "carousel@test.invalid"); err != nil {
		t.Fatal(err)
	}
	var audits int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_logs WHERE entity_type='carousel_slide' AND entity_id=$1", id).Scan(&audits); err != nil || audits != 4 {
		t.Fatalf("audit %d %v", audits, err)
	}
}

func TestCarouselAuditFailureRollsBackActualSave(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	store := postgres.CarouselStore{Pool: pool}
	// A test-only audit trigger rejects just this actor; no schema/database CREATE needed.
	_, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION reject_carousel_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.actor_email='carousel-reject@test.invalid' THEN RAISE EXCEPTION 'test audit rejected'; END IF; RETURN NEW; END $$;
 CREATE OR REPLACE TRIGGER reject_carousel_test_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_carousel_test_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DROP TRIGGER IF EXISTS reject_carousel_test_audit ON audit_logs; DROP FUNCTION IF EXISTS reject_carousel_test_audit()")
	})
	var product int
	if err := pool.QueryRow(ctx, "INSERT INTO products(name,price,stock) VALUES('Audit rollback',100,1) RETURNING id").Scan(&product); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", product) })
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	title := fmt.Sprintf("rollback-%d", product)
	in := editorial.Input{Title: title, AltText: "Imagen", CTALabel: "Ver", TargetKind: "product", TargetID: utils.EncodeID(product)}
	if _, err := editorial.Save(ctx, store, 0, in, &editorial.Image{Content: buf.Bytes()}, "carousel-reject@test.invalid"); err == nil {
		t.Fatal("audit failure accepted")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM carousel_slides WHERE title=$1", title).Scan(&count); err != nil || count != 0 {
		t.Fatalf("save not rolled back %d %v", count, err)
	}
}
