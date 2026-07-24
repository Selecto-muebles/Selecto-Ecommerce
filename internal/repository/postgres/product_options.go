package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func ValidateSelectedOptions(ctx context.Context, tx pgx.Tx, productID int, selected map[string]string) error {
	rows, err := tx.Query(ctx, "SELECT name, values FROM product_options WHERE product_id=$1", productID)
	if err != nil {
		return err
	}
	defer rows.Close()

	allowed := make(map[string]map[string]struct{})
	for rows.Next() {
		var name string
		var raw []byte
		var values []string
		if err := rows.Scan(&name, &raw); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		allowed[name] = make(map[string]struct{}, len(values))
		for _, value := range values {
			allowed[name][value] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(selected) != len(allowed) {
		return fmt.Errorf("all product options must be selected")
	}
	for name, value := range selected {
		values, exists := allowed[name]
		if !exists {
			return fmt.Errorf("invalid product option")
		}
		if _, exists := values[value]; !exists {
			return fmt.Errorf("invalid product option value")
		}
	}
	return nil
}
