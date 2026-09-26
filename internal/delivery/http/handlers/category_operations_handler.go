package handlers

import (
	"errors"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/service/catalog"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type updateCategoryInput struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Active    *bool  `json:"active"`
	SortOrder int    `json:"sort_order"`
}

func UpdateCategoryHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := adminIDParam(c, "id")
		if !ok {
			return
		}
		var input updateCategoryInput
		if c.ShouldBindJSON(&input) != nil || input.Active == nil {
			apperrors.BadRequest(c, "name, slug, active and sort_order are required")
			return
		}
		item, err := catalog.NormalizeCategoryUpdate(input.Name, input.Slug, input.SortOrder)
		if err != nil {
			apperrors.BadRequest(c, err.Error())
			return
		}
		item.ID, item.Active = int64(id), *input.Active
		item, err = postgres.UpdateCategory(c, db.Pool, item, adminActor(c))
		if err != nil {
			handleCategoryOperationError(c, err)
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

func ReorderCategoriesHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			IDs []int64 `json:"ids"`
		}
		if c.ShouldBindJSON(&input) != nil || len(input.IDs) == 0 || len(input.IDs) > 500 {
			apperrors.BadRequest(c, "between 1 and 500 category ids are required")
			return
		}
		seen := make(map[int64]struct{}, len(input.IDs))
		for _, id := range input.IDs {
			if id <= 0 {
				apperrors.BadRequest(c, "category ids must be positive")
				return
			}
			if _, duplicate := seen[id]; duplicate {
				apperrors.BadRequest(c, "category ids must be unique")
				return
			}
			seen[id] = struct{}{}
		}
		if err := postgres.ReorderCategories(c, db.Pool, input.IDs, adminActor(c)); err != nil {
			handleCategoryOperationError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func handleCategoryOperationError(c *gin.Context, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "category not found", nil)
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "category name or slug already exists", nil)
		return
	}
	apperrors.Internal(c)
}
