package handlers

import (
	"errors"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/service/catalog"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

func ListCategoriesHandler(db *database.DB, public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := adminPagination(c)
		items, err := postgres.ListCategories(c, db.Pool, public, page.PageSize+1, page.Offset)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		more := len(items) > page.PageSize
		if more {
			items = items[:page.PageSize]
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "page": page.Page, "page_size": page.PageSize, "has_more": more})
	}
}

func CreateCategoryHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid category")
			return
		}
		item, err := catalog.NormalizeCategory(input.Name, input.Slug)
		if err != nil {
			apperrors.BadRequest(c, err.Error())
			return
		}
		item, err = postgres.CreateCategory(c, db.Pool, item, adminActor(c))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "category name or slug already exists", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusCreated, item)
	}
}
