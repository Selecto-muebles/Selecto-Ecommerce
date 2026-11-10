package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type productFamily struct {
	ID     int               `json:"id"`
	Name   string            `json:"name"`
	Axes   []string          `json:"axes"`
	Values map[string]string `json:"variant_values,omitempty"`
}

var errInvalidFamily = errors.New("invalid product family or variant values")
var errDuplicateVariant = errors.New("this combination already exists in the family")

func validFamilyName(name string) bool {
	length := utf8.RuneCountInString(name)
	return length >= 2 && length <= 120
}

func normalizeFamilyAxes(axes []string) ([]string, error) {
	if len(axes) < 1 || len(axes) > 3 {
		return nil, errInvalidFamily
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(axes))
	for _, axis := range axes {
		axis = strings.TrimSpace(axis)
		key := strings.ToLower(axis)
		if utf8.RuneCountInString(axis) < 2 || utf8.RuneCountInString(axis) > 40 || seen[key] {
			return nil, errInvalidFamily
		}
		seen[key] = true
		result = append(result, axis)
	}
	return result, nil
}

func validateVariantValues(axes []string, values map[string]string) (map[string]string, error) {
	if len(values) != len(axes) {
		return nil, errInvalidFamily
	}
	result := make(map[string]string, len(axes))
	for _, axis := range axes {
		value := strings.TrimSpace(values[axis])
		if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 80 {
			return nil, errInvalidFamily
		}
		result[axis] = value
	}
	return result, nil
}

func familyAssignment(ctx context.Context, tx pgx.Tx, familyID int, values map[string]string) ([]byte, error) {
	if familyID == 0 {
		if len(values) > 0 {
			return nil, errInvalidFamily
		}
		return []byte(`{}`), nil
	}
	if familyID < 0 {
		return nil, errInvalidFamily
	}
	var axesRaw []byte
	if err := tx.QueryRow(ctx, "SELECT axes FROM product_families WHERE id=$1 FOR SHARE", familyID).Scan(&axesRaw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errInvalidFamily
		}
		return nil, err
	}
	var axes []string
	if err := json.Unmarshal(axesRaw, &axes); err != nil {
		return nil, err
	}
	clean, err := validateVariantValues(axes, values)
	if err != nil {
		return nil, err
	}
	return json.Marshal(clean)
}

func handleFamilyWriteError(c *gin.Context, err error) {
	var pgErr *pgconn.PgError
	if errors.Is(err, errInvalidFamily) {
		apperrors.BadRequest(c, errInvalidFamily.Error())
	} else if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		message := errDuplicateVariant.Error()
		if pgErr.ConstraintName == "idx_product_families_name" {
			message = "publication name already exists"
		}
		apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, message, nil)
	} else {
		apperrors.Internal(c)
	}
}

func productFamiliesByProduct(ctx context.Context, db *database.DB, ids []int) (map[int]productFamily, error) {
	result := map[int]productFamily{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := db.Pool.Query(ctx, `SELECT p.id,f.id,f.name,f.axes,p.variant_values
		FROM products p JOIN product_families f ON f.id=p.family_id WHERE p.id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var family productFamily
		var axesRaw, valuesRaw []byte
		if err := rows.Scan(&id, &family.ID, &family.Name, &axesRaw, &valuesRaw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(axesRaw, &family.Axes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(valuesRaw, &family.Values); err != nil {
			return nil, err
		}
		result[id] = family
	}
	return result, rows.Err()
}

func AdminListProductFamiliesHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Pool.Query(c, "SELECT id,name,axes FROM product_families ORDER BY LOWER(name),id")
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()
		items := []productFamily{}
		for rows.Next() {
			var family productFamily
			var axesRaw []byte
			if err := rows.Scan(&family.ID, &family.Name, &axesRaw); err != nil {
				apperrors.Internal(c)
				return
			}
			if err := json.Unmarshal(axesRaw, &family.Axes); err != nil {
				apperrors.Internal(c)
				return
			}
			items = append(items, family)
		}
		if rows.Err() != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "publication_commands": true, "atomic_publication_create": true, "publication_content_edit": true, "product_video_upload": true})
	}
}

func AdminCreateProductFamilyHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input productFamilyCommand
		if c.ShouldBindJSON(&input) != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		if input.Action != "" {
			adminProductFamilyCommand(c, db, input)
			return
		}
		name := strings.TrimSpace(input.Name)
		axes, err := normalizeFamilyAxes(input.Axes)
		if !validFamilyName(name) || err != nil {
			apperrors.BadRequest(c, errInvalidFamily.Error())
			return
		}
		axesRaw, _ := json.Marshal(axes)
		var id int
		if err := db.Pool.QueryRow(c, "INSERT INTO product_families(name,axes) VALUES($1,$2) RETURNING id", name, axesRaw).Scan(&id); err != nil {
			handleFamilyWriteError(c, err)
			return
		}
		_ = writeAudit(c, db, adminActor(c), "product_family_created", "product_family", id, gin.H{"name": name, "axes": axes})
		c.JSON(http.StatusCreated, productFamily{ID: id, Name: name, Axes: axes})
	}
}
