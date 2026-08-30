package handlers

import (
	"errors"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func ResetPasswordHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input resetPasswordInput
		if c.ShouldBindJSON(&input) != nil || len(input.Password) < 8 || len(input.Password) > maxBcryptPasswordBytes {
			apperrors.BadRequest(c, "token and valid password are required")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var userID int
		err = tx.QueryRow(c, `SELECT user_id FROM account_tokens WHERE token_hash=$1 AND purpose='password_reset' AND consumed_at IS NULL AND expires_at > NOW() FOR UPDATE`, hashAccountToken(strings.TrimSpace(input.Token))).Scan(&userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, "reset token is invalid or expired", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "UPDATE users SET password=$1, session_version=session_version+1 WHERE id=$2", string(hash), userID); err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "UPDATE account_tokens SET consumed_at=NOW() WHERE user_id=$1 AND purpose='password_reset' AND consumed_at IS NULL", userID); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "password updated"})
	}
}
