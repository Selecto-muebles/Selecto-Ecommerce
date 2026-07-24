package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func VerifyEmailHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input tokenInput
		if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Token) == "" {
			apperrors.BadRequest(c, "verification token is required")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var tokenID int64
		var userID int
		err = tx.QueryRow(c, `SELECT id, user_id FROM account_tokens WHERE token_hash=$1 AND purpose='email_verification' AND consumed_at IS NULL AND expires_at > NOW() FOR UPDATE`, hashAccountToken(input.Token)).Scan(&tokenID, &userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusBadRequest, apperrors.CodeInvalidInput, "verification token is invalid or expired", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "UPDATE users SET email_verified_at=COALESCE(email_verified_at, NOW()) WHERE id=$1", userID); err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "UPDATE account_tokens SET consumed_at=NOW() WHERE id=$1", tokenID); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"verified": true})
	}
}

func ResendVerificationHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input emailInput
		if c.ShouldBindJSON(&input) != nil {
			apperrors.BadRequest(c, "email is required")
			return
		}
		email := strings.ToLower(strings.TrimSpace(input.Email))
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var userID int
		var verifiedAt sql.NullTime
		err = tx.QueryRow(c, "SELECT id, email_verified_at FROM users WHERE email=$1 AND role='user' FOR UPDATE", email).Scan(&userID, &verifiedAt)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && verifiedAt.Valid) {
			_ = tx.Commit(c)
			c.JSON(http.StatusAccepted, gin.H{"message": "if the account requires verification, an email will be sent"})
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		token, err := createAccountToken(c, tx, userID, "email_verification", emailVerificationTTL)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if err := mailinfra.Enqueue(c, tx, fmt.Sprintf("verify:%d:%s", userID, hashAccountToken(token)[:16]), email, "verify_email", gin.H{"url": accountURL(cfg.StorefrontURL, "/verificar-email", token)}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"message": "if the account requires verification, an email will be sent"})
	}
}

func ForgotPasswordHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input emailInput
		if c.ShouldBindJSON(&input) != nil {
			apperrors.BadRequest(c, "email is required")
			return
		}
		email := strings.ToLower(strings.TrimSpace(input.Email))
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var userID int
		var password sql.NullString
		err = tx.QueryRow(c, "SELECT id, password FROM users WHERE email=$1 AND role='user' FOR UPDATE", email).Scan(&userID, &password)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !password.Valid) {
			_ = tx.Commit(c)
			c.JSON(http.StatusAccepted, gin.H{"message": "if the account exists, reset instructions will be sent"})
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		token, err := createAccountToken(c, tx, userID, "password_reset", passwordResetTTL)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if err := mailinfra.Enqueue(c, tx, fmt.Sprintf("reset:%d:%s", userID, hashAccountToken(token)[:16]), email, "password_reset", gin.H{"url": accountURL(cfg.StorefrontURL, "/restablecer-contrasena", token)}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"message": "if the account exists, reset instructions will be sent"})
	}
}
