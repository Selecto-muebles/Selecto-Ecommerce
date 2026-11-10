package handlers

import "fmt"

// Page publications first, then load every member of the selected publications.
// A family is never split between pages or reduced to the matching variant.
func productPublicationQueries(whereSQL, orderSQL string, argumentCount int, archived *bool) (string, string) {
	common := `WITH filtered AS (SELECT * FROM products WHERE ` + whereSQL + `), publication_groups AS (
   SELECT family_id,MIN(id) id,MIN(COALESCE((SELECT f.name FROM product_families f WHERE f.id=filtered.family_id),name)) name,
     MIN(price) price,SUM(stock) stock,MAX(created_at) created_at
   FROM filtered GROUP BY family_id,CASE WHEN family_id IS NULL THEN id END
 ) `
	visibility := "p.archived_at IS NULL"
	if archived != nil && *archived {
		visibility = "p.archived_at IS NOT NULL"
	}
	count := common + "SELECT COUNT(*) FROM publication_groups"
	rows := common + fmt.Sprintf(`, selected AS (
   SELECT *,ROW_NUMBER() OVER(ORDER BY %s) publication_rank FROM publication_groups
   ORDER BY %s LIMIT $%d OFFSET $%d
 ) SELECT p.id,p.name,COALESCE(p.sku,''),p.price,p.stock,p.active,p.description,p.category,p.specifications,p.archived_at,p.created_at,p.updated_at
 FROM products p JOIN selected s ON (s.family_id IS NOT NULL AND p.family_id=s.family_id) OR (s.family_id IS NULL AND p.id=s.id)
 WHERE %s ORDER BY s.publication_rank,p.id`, orderSQL, orderSQL, argumentCount+1, argumentCount+2, visibility)
	return count, rows
}
