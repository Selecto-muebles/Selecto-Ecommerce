package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"
	"Selecto-Ecommerce/internal/shared/validation"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type RegisterInput struct {
	Email         string `json:"email"`
	Password      string `json:"password"`
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

const maxBcryptPasswordBytes = 72

func RegisterHandler(
	db *database.DB,
	cfg *config.Config,
	logger *slog.Logger,
	notifiers ...mailinfra.DispatchNotifier,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input RegisterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			logger.Warn(logging.EventUserRegistrationRejected, "reason", "invalid_payload")
			apperrors.BadRequest(c, "invalid input")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))
		customerProfile := validation.NormalizeCustomerProfile(input.customerProfile())
		input.applyCustomerProfile(customerProfile)

		if !validEmail(input.Email) || len(input.Password) < 8 || len(input.Password) > maxBcryptPasswordBytes {
			logger.Warn(logging.EventUserRegistrationRejected, "reason", "invalid_credentials_policy")
			apperrors.BadRequest(c, "email and password must be valid")
			return
		}
		if err := customerProfile.Validate(); err != nil {
			logger.Warn(logging.EventUserRegistrationRejected, "reason", "invalid_customer_profile")
			apperrors.BadRequest(c, "customer billing and shipping information must be valid")
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
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
		err = tx.QueryRow(
			c,
			`INSERT INTO users (
				email,
				password,
				role,
				first_name,
				last_name,
				dni,
				street_address,
				street_number,
				postal_code,
				province,
				locality,
				phone_number
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			RETURNING id`,
			input.Email,
			string(hashedPassword),
			"user",
			input.FirstName,
			input.LastName,
			input.DNI,
			input.StreetAddress,
			input.StreetNumber,
			input.PostalCode,
			input.Province,
			input.Locality,
			input.PhoneNumber,
		).Scan(&userID)

		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				logger.Warn(logging.EventUserRegistrationRejected, "reason", "duplicate_user")
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "user already exists", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		verificationToken, err := createAccountToken(c, tx, userID, "email_verification", emailVerificationTTL)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		outboxID, err := mailinfra.EnqueueReturningID(c, tx, fmt.Sprintf("verify:%d:%s", userID, hashAccountToken(verificationToken)[:16]), input.Email, "verify_email", gin.H{"url": accountURL(cfg.StorefrontURL, "/verificar-email", verificationToken)})
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyAfterCommit(c.Request.Context(), outboxID, notifiers...)

		logger.Info(logging.EventUserRegistrationCompleted, "user_id", userID, "role", "user")
		c.JSON(http.StatusOK, gin.H{"message": "user created", "verification_required": true})
	}
}

func (input RegisterInput) customerProfile() validation.CustomerProfile {
	return validation.CustomerProfile{
		FirstName:     input.FirstName,
		LastName:      input.LastName,
		DNI:           input.DNI,
		StreetAddress: input.StreetAddress,
		StreetNumber:  input.StreetNumber,
		PostalCode:    input.PostalCode,
		Province:      input.Province,
		Locality:      input.Locality,
		PhoneNumber:   input.PhoneNumber,
	}
}

func (input *RegisterInput) applyCustomerProfile(profile validation.CustomerProfile) {
	input.FirstName = profile.FirstName
	input.LastName = profile.LastName
	input.DNI = profile.DNI
	input.StreetAddress = profile.StreetAddress
	input.StreetNumber = profile.StreetNumber
	input.PostalCode = profile.PostalCode
	input.Province = profile.Province
	input.Locality = profile.Locality
	input.PhoneNumber = profile.PhoneNumber
}

func GetMeHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		emailValue, _ := c.Get("email")
		email := strings.ToLower(strings.TrimSpace(fmt.Sprint(emailValue)))
		var profile RegisterInput
		var verified bool
		if err := db.Pool.QueryRow(c, `SELECT email, COALESCE(first_name, ''), COALESCE(last_name, ''),
			COALESCE(dni, ''), COALESCE(street_address, ''), COALESCE(street_number, ''),
			COALESCE(postal_code, ''), COALESCE(province, ''), COALESCE(locality, ''), COALESCE(phone_number, ''),
			COALESCE(email_verified_at IS NOT NULL, FALSE)
			FROM users WHERE email=$1 AND role='user'`, email).Scan(
			&profile.Email, &profile.FirstName, &profile.LastName, &profile.DNI,
			&profile.StreetAddress, &profile.StreetNumber, &profile.PostalCode,
			&profile.Province, &profile.Locality, &profile.PhoneNumber, &verified,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "user not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, profileResponse(profile.Email, profile.customerProfile(), verified))
	}
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func LoginHandler(db *database.DB, cfg *config.Config, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input LoginInput

		if err := c.ShouldBindJSON(&input); err != nil {
			logger.Warn(logging.EventUserLoginRejected, "reason", "invalid_payload")
			apperrors.BadRequest(c, "invalid input")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))

		var storedPassword sql.NullString
		var verifiedAt sql.NullTime
		var role string
		var userID int
		var sessionVersion int64

		err := db.Pool.QueryRow(
			c,
			"SELECT id, password, role, email_verified_at, session_version FROM users WHERE email=$1",
			input.Email,
		).Scan(&userID, &storedPassword, &role, &verifiedAt, &sessionVersion)

		if err != nil {
			logger.Warn(logging.EventUserLoginRejected, "reason", "invalid_credentials")
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid credentials", nil)
			return
		}

		if !storedPassword.Valid {
			logger.Warn(logging.EventUserLoginRejected, "reason", "password_not_configured", "user_id", userID)
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid credentials", nil)
			return
		}
		err = bcrypt.CompareHashAndPassword([]byte(storedPassword.String), []byte(input.Password))
		if err != nil {
			logger.Warn(logging.EventUserLoginRejected, "reason", "invalid_credentials", "user_id", userID)
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid credentials", nil)
			return
		}
		if role == "user" && !verifiedAt.Valid {
			logger.Warn(logging.EventUserLoginRejected, "reason", "email_not_verified", "user_id", userID)
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "email verification required", gin.H{"verification_required": true})
			return
		}

		token, err := utils.GenerateTokenWithVersion(input.Email, role, sessionVersion, cfg.JWTSecret, cfg.JWTTTL)
		if err != nil {
			apperrors.Internal(c)
			return
		}

		logger.Info(logging.EventUserLoginCompleted, "user_id", userID, "role", role)
		c.JSON(http.StatusOK, gin.H{
			"token": token,
			"role":  role,
		})
	}
}
