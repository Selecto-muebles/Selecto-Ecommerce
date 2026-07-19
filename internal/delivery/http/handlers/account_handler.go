package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/validation"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	emailVerificationTTL = 24 * time.Hour
	passwordResetTTL     = 30 * time.Minute
)

type tokenInput struct {
	Token string `json:"token"`
}
type emailInput struct {
	Email string `json:"email"`
}
type resetPasswordInput struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}
type updateProfileInput struct {
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	DNI           string `json:"dni"`
	StreetAddress string `json:"street_address"`
	StreetNumber  string `json:"street_number"`
	PostalCode    string `json:"postal_code"`
	Province      string `json:"province"`
	Locality      string `json:"locality"`
	PhoneNumber   string `json:"phone_number"`
}

func validEmail(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && strings.Contains(value, "@") && len(value) <= 254
}

func newAccountToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashAccountToken(token), nil
}

func hashAccountToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func createAccountToken(ctx *gin.Context, tx pgx.Tx, userID int, purpose string, ttl time.Duration) (string, error) {
	token, hash, err := newAccountToken()
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, "UPDATE account_tokens SET consumed_at=NOW() WHERE user_id=$1 AND purpose=$2 AND consumed_at IS NULL", userID, purpose); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO account_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1, $2, $3, NOW() + make_interval(secs => $4))", userID, purpose, hash, int(ttl.Seconds())); err != nil {
		return "", err
	}
	return token, nil
}

func accountURL(base, path, token string) string {
	return strings.TrimRight(base, "/") + path + "?token=" + url.QueryEscape(token)
}

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
