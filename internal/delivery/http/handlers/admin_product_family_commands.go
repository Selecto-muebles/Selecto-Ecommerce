package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"
	"github.com/gin-gonic/gin"
)

type productFamilyCommand struct {
	Action               string                 `json:"action"`
	ID                   int                    `json:"id"`
	Name                 string                 `json:"name"`
	Axes                 []string               `json:"axes"`
	Active               *bool                  `json:"active"`
	ProductID            string                 `json:"product_id"`
	Values               map[string]string      `json:"variant_values"`
	Category             *string                `json:"category"`
	Description          *string                `json:"description"`
	Specifications       *productSpecifications `json:"specifications"`
	RemoveSpecifications []string               `json:"remove_specifications"`
	Stock                []struct {
		ProductID string `json:"product_id"`
		Quantity  int    `json:"quantity"`
	} `json:"stock"`
}

// Publication commands use the existing protected route and one transaction.
// All members are selected by family_id, independently of Admin pagination.
func adminProductFamilyCommand(c *gin.Context, db *database.DB, input productFamilyCommand) {
	tx, err := db.Pool.Begin(c)
	if err != nil {
		apperrors.Internal(c)
		return
	}
	defer tx.Rollback(c)
	family := productFamily{ID: input.ID}
	if input.Action == "group" {
		family.Name = strings.TrimSpace(input.Name)
		family.Axes, err = normalizeFamilyAxes(input.Axes)
		if err != nil || !validFamilyName(family.Name) {
			apperrors.BadRequest(c, "invalid publication name or attributes")
			return
		}
		values, e := validateVariantValues(family.Axes, input.Values)
		id, idErr := utils.DecodeID(input.ProductID)
		if e != nil || idErr != nil {
			apperrors.BadRequest(c, "complete the current product attributes")
			return
		}
		axes, _ := json.Marshal(family.Axes)
		raw, _ := json.Marshal(values)
		if err = tx.QueryRow(c, "INSERT INTO product_families(name,axes) VALUES($1,$2) RETURNING id", family.Name, axes).Scan(&family.ID); err != nil {
			handleFamilyWriteError(c, err)
			return
		}
		result, e := tx.Exec(c, "UPDATE products SET family_id=$1,variant_values=$2,updated_at=NOW() WHERE id=$3 AND family_id IS NULL AND archived_at IS NULL", family.ID, raw, id)
		if e != nil {
			handleFamilyWriteError(c, e)
			return
		}
		if result.RowsAffected() != 1 {
			apperrors.BadRequest(c, "product already belongs to a publication or is unavailable")
			return
		}
	} else {
		if input.ID <= 0 {
			apperrors.BadRequest(c, "invalid publication")
			return
		}
		var axes []byte
		if err = tx.QueryRow(c, "SELECT name,axes FROM product_families WHERE id=$1 FOR UPDATE", input.ID).Scan(&family.Name, &axes); err != nil {
			handleAdminLookupErr(c, err, "publication not found")
			return
		}
		if err = json.Unmarshal(axes, &family.Axes); err != nil {
			apperrors.Internal(c)
			return
		}
		switch input.Action {
		case "rename":
			family.Name = strings.TrimSpace(input.Name)
			if !validFamilyName(family.Name) {
				apperrors.BadRequest(c, "invalid publication name")
				return
			}
			_, err = tx.Exec(c, "UPDATE product_families SET name=$1 WHERE id=$2", family.Name, input.ID)
			if err == nil {
				err = updatePublicationContent(c, tx, input)
			}
		case "status":
			if input.Active == nil {
				apperrors.BadRequest(c, "active is required")
				return
			}
			_, err = tx.Exec(c, "UPDATE products SET active=$1,updated_at=NOW() WHERE family_id=$2 AND archived_at IS NULL", *input.Active, input.ID)
		case "delete":
			_, err = tx.Exec(c, "UPDATE products SET active=FALSE,archived_at=COALESCE(archived_at,NOW()),updated_at=NOW() WHERE family_id=$1", input.ID)
		case "stock":
			if len(input.Stock) == 0 || len(input.Stock) > 1000 {
				apperrors.BadRequest(c, "invalid stock adjustments")
				return
			}
			for _, item := range input.Stock {
				id, e := utils.DecodeID(item.ProductID)
				if e != nil || item.Quantity < 0 || item.Quantity > 2147483647 {
					apperrors.BadRequest(c, "invalid stock quantity")
					return
				}
				result, e := tx.Exec(c, "UPDATE products SET stock=$1,updated_at=NOW() WHERE id=$2 AND family_id=$3 AND archived_at IS NULL", item.Quantity, id, input.ID)
				if e != nil {
					apperrors.Internal(c)
					return
				}
				if result.RowsAffected() != 1 {
					apperrors.BadRequest(c, "variant does not belong to publication")
					return
				}
			}
		default:
			apperrors.BadRequest(c, "invalid publication action")
			return
		}
		if err != nil {
			handleFamilyWriteError(c, err)
			return
		}
	}
	if err = writeAuditTx(c, tx, adminActor(c), "product_family_"+input.Action, "product_family", family.ID, gin.H{"name": family.Name, "active": input.Active, "stock": input.Stock, "common_fields": gin.H{"category": input.Category != nil, "description": input.Description != nil, "specifications": input.Specifications != nil}}); err != nil {
		apperrors.Internal(c)
		return
	}
	if err = tx.Commit(c); err != nil {
		handleFamilyWriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, family)
}
