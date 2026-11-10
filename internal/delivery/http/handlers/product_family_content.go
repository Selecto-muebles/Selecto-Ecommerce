package handlers

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"unicode/utf8"
)

func updatePublicationContent(ctx context.Context, tx pgx.Tx, input productFamilyCommand) error {
	var category, description *string
	if input.Category != nil {
		clean := strings.TrimSpace(*input.Category)
		if utf8.RuneCountInString(clean) > 80 {
			return errInvalidFamily
		}
		category = &clean
	}
	if input.Description != nil {
		clean := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(clean) > 5000 {
			return errInvalidFamily
		}
		description = &clean
	}
	var specifications []byte
	if input.Specifications != nil {
		clean, err := normalizeSpecifications(*input.Specifications)
		if err != nil {
			return errInvalidFamily
		}
		specifications, err = encodeSpecifications(clean)
		if err != nil {
			return err
		}
	}
	if len(input.RemoveSpecifications) > 24 {
		return errInvalidFamily
	}
	for _, name := range input.RemoveSpecifications {
		if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 80 {
			return errInvalidFamily
		}
	}
	_, err := tx.Exec(ctx, `UPDATE products SET category=COALESCE($1,category),description=COALESCE($2,description),specifications=(specifications || COALESCE($3,'{}'::jsonb)) - COALESCE($4,ARRAY[]::text[]),updated_at=NOW() WHERE family_id=$5 AND archived_at IS NULL`, category, description, specifications, input.RemoveSpecifications, input.ID)
	return err
}
