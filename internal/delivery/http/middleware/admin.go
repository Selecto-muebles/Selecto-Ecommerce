package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func RequireAdmin(db *database.DB, logger *slog.Logger) gin.HandlerFunc {
	return RequireRole(db, logger, "admin")
}

func RequireRole(db *database.DB, logger *slog.Logger, requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		emailValue, _ := c.Get("email")
		tokenRoleValue, _ := c.Get("role")
		email := fmt.Sprint(emailValue)
		tokenRole := fmt.Sprint(tokenRoleValue)
		if email == "" {
			logger.Warn(logging.EventAdminAccessRejected, "reason", "missing_authenticated_user", "required_role", requiredRole, "token_role", tokenRole)
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid token", nil)
			c.Abort()
			return
		}

		var currentRole string
		err := db.Pool.QueryRow(c, "SELECT role FROM users WHERE email=$1", email).Scan(&currentRole)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				logger.Warn(logging.EventAdminAccessRejected, "reason", "user_not_found", "required_role", requiredRole, "token_role", tokenRole)
				apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid token", nil)
				c.Abort()
				return
			}
			apperrors.Internal(c)
			c.Abort()
			return
		}

		if currentRole != requiredRole {
			logger.Warn(logging.EventAdminAccessRejected, "reason", "insufficient_role", "required_role", requiredRole, "current_role", currentRole, "token_role", tokenRole)
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "forbidden", nil)
			c.Abort()
			return
		}

		c.Set("role", currentRole)
		c.Next()
	}
}
