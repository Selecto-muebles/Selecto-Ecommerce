package middleware

import (
	"errors"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func AuthMiddleware(db *database.DB, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := clientAuthorizationHeader(c)

		if authHeader == "" {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "missing token", nil)
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)

		if len(parts) != 2 || parts[0] != "Bearer" {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid token format", nil)
			c.Abort()
			return
		}

		tokenStr := parts[1]
		claims, err := utils.ValidateToken(tokenStr, jwtSecret)
		if err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid token", nil)
			c.Abort()
			return
		}

		var currentRole string
		var currentSessionVersion int64
		if err := db.Pool.QueryRow(c, "SELECT role, session_version FROM users WHERE email=$1", claims.Email).Scan(&currentRole, &currentSessionVersion); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid token", nil)
			} else {
				apperrors.Internal(c)
			}
			c.Abort()
			return
		}
		if currentSessionVersion != claims.SessionVersion {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "session expired", nil)
			c.Abort()
			return
		}

		c.Set("email", claims.Email)
		c.Set("role", currentRole)

		c.Next()
	}
}

func clientAuthorizationHeader(c *gin.Context) string {
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Authorization")); forwarded != "" {
		return forwarded
	}
	return c.GetHeader("Authorization")
}
