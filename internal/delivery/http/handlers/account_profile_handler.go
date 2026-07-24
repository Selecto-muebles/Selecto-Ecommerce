package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/validation"

	"github.com/gin-gonic/gin"
)

func UpdateMeHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input updateProfileInput
		if c.ShouldBindJSON(&input) != nil {
			apperrors.BadRequest(c, "invalid profile")
			return
		}
		profile := validation.NormalizeCustomerProfile(validation.CustomerProfile{FirstName: input.FirstName, LastName: input.LastName, DNI: input.DNI, StreetAddress: input.StreetAddress, StreetNumber: input.StreetNumber, PostalCode: input.PostalCode, Province: input.Province, Locality: input.Locality, PhoneNumber: input.PhoneNumber})
		if profile.Validate() != nil {
			apperrors.BadRequest(c, "customer profile must be valid")
			return
		}
		email := strings.ToLower(strings.TrimSpace(fmt.Sprint(c.MustGet("email"))))
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		command, err := tx.Exec(c, `UPDATE users SET first_name=$1,last_name=$2,dni=$3,street_address=$4,street_number=$5,postal_code=$6,province=$7,locality=$8,phone_number=$9 WHERE email=$10 AND role='user'`, profile.FirstName, profile.LastName, profile.DNI, profile.StreetAddress, profile.StreetNumber, profile.PostalCode, profile.Province, profile.Locality, profile.PhoneNumber, email)
		if err != nil {
			if uniqueViolation(err) {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "DNI already belongs to another account", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		if command.RowsAffected() != 1 {
			apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "user not found", nil)
			return
		}
		if _, err := tx.Exec(c, "INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) SELECT email, 'customer_profile_updated', 'user', id, '{}'::jsonb FROM users WHERE email=$1", email); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, profileResponse(email, profile, true))
	}
}

func profileResponse(email string, profile validation.CustomerProfile, verified bool) gin.H {
	return gin.H{"email": email, "email_verified": verified, "first_name": profile.FirstName, "last_name": profile.LastName, "dni": profile.DNI, "street_address": profile.StreetAddress, "street_number": profile.StreetNumber, "postal_code": profile.PostalCode, "province": profile.Province, "locality": profile.Locality, "phone_number": profile.PhoneNumber}
}
