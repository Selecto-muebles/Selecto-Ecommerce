package postgres

import (
	"context"
	"encoding/json"
	"sort"

	"Selecto-Ecommerce/internal/service/catalog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ListCategories(ctx context.Context, pool *pgxpool.Pool, public bool, limit, offset int) ([]catalog.Category, error) {
	rows, err := pool.Query(ctx, `SELECT c.id,c.name,c.slug,c.active,c.sort_order,c.updated_at,
	 COUNT(p.id),COUNT(p.id) FILTER(WHERE p.active AND p.archived_at IS NULL)
	 FROM categories c LEFT JOIN products p ON p.category_id=c.id
	 WHERE (NOT $1 OR (c.active AND EXISTS(SELECT 1 FROM products visible WHERE visible.category_id=c.id AND visible.active AND visible.archived_at IS NULL)))
	 GROUP BY c.id ORDER BY c.sort_order,LOWER(c.name),c.id LIMIT $2 OFFSET $3`, public, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []catalog.Category{}
	for rows.Next() {
		var item catalog.Category
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Active, &item.SortOrder, &item.UpdatedAt, &item.ProductCount, &item.ActiveProductCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func CreateCategory(ctx context.Context, pool *pgxpool.Pool, item catalog.Category, actor string) (catalog.Category, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return item, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO categories(name,slug) VALUES ($1,$2) RETURNING id,updated_at`, item.Name, item.Slug).Scan(&item.ID, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,metadata)
	 VALUES ($1,'category_created','category',$2,jsonb_build_object('name',$3::text,'slug',$4::text))`, actor, item.ID, item.Name, item.Slug)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func UpdateCategory(ctx context.Context, pool *pgxpool.Pool, item catalog.Category, actor string) (catalog.Category, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return item, err
	}
	defer tx.Rollback(ctx)
	var previous catalog.Category
	err = tx.QueryRow(ctx, `SELECT id,name,slug,active,sort_order FROM categories WHERE id=$1 FOR UPDATE`, item.ID).Scan(
		&previous.ID, &previous.Name, &previous.Slug, &previous.Active, &previous.SortOrder,
	)
	if err != nil {
		return item, err
	}
	err = tx.QueryRow(ctx, `UPDATE categories SET name=$2,slug=$3,active=$4,sort_order=$5,updated_at=NOW()
	 WHERE id=$1 RETURNING updated_at`, item.ID, item.Name, item.Slug, item.Active, item.SortOrder).Scan(&item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if previous.Name != item.Name {
		if _, err := tx.Exec(ctx, `UPDATE products SET category=$2,updated_at=NOW() WHERE category_id=$1`, item.ID, item.Name); err != nil {
			return item, err
		}
	}
	before, _ := json.Marshal(previous)
	after, _ := json.Marshal(item)
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,metadata)
	 VALUES($1,'category_updated','category',$2,jsonb_build_object('before',$3::jsonb,'after',$4::jsonb))`, actor, item.ID, before, after)
	if err != nil {
		return item, err
	}
	item.ProductCount, item.ActiveProductCount, err = categoryCounts(ctx, tx, item.ID)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func ReorderCategories(ctx context.Context, pool *pgxpool.Pool, ids []int64, actor string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ordered := append([]int64(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, id := range ordered {
		var locked int64
		if err := tx.QueryRow(ctx, "SELECT id FROM categories WHERE id=$1 FOR UPDATE", id).Scan(&locked); err != nil {
			return err
		}
	}
	for position, id := range ids {
		tag, updateErr := tx.Exec(ctx, `UPDATE categories SET sort_order=$2,updated_at=NOW() WHERE id=$1`, id, position*10)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
	}
	encoded, _ := json.Marshal(ids)
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,metadata)
	 VALUES($1,'categories_reordered','category',0,jsonb_build_object('ids',$2::jsonb))`, actor, encoded); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func categoryCounts(ctx context.Context, tx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64) (int, int, error) {
	var total, active int
	err := tx.QueryRow(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE active AND archived_at IS NULL) FROM products WHERE category_id=$1`, id).Scan(&total, &active)
	return total, active, err
}
