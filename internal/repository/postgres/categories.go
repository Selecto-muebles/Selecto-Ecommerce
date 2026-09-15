package postgres

import (
	"context"

	"Selecto-Ecommerce/internal/service/catalog"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ListCategories(ctx context.Context, pool *pgxpool.Pool, public bool, limit, offset int) ([]catalog.Category, error) {
	rows, err := pool.Query(ctx, `SELECT c.id,c.name,c.slug,c.active,c.sort_order FROM categories c
 WHERE (NOT $1 OR (c.active AND EXISTS(SELECT 1 FROM products p WHERE p.category_id=c.id AND p.active AND p.archived_at IS NULL)))
 ORDER BY c.sort_order,LOWER(c.name),c.id LIMIT $2 OFFSET $3`, public, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []catalog.Category{}
	for rows.Next() {
		var item catalog.Category
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Active, &item.SortOrder); err != nil {
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
	err = tx.QueryRow(ctx, `INSERT INTO categories(name,slug) VALUES ($1,$2) RETURNING id`, item.Name, item.Slug).Scan(&item.ID)
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
