package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
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

func RegisterHandler(db *database.DB, cfg *config.Config, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = cfg
		var input RegisterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			logger.Warn(logging.EventUserRegistrationRejected, "reason", "invalid_payload")
			apperrors.BadRequest(c, "invalid input")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))
		customerProfile := validation.NormalizeCustomerProfile(input.customerProfile())
		input.applyCustomerProfile(customerProfile)

		if input.Email == "" || !strings.Contains(input.Email, "@") || len(input.Password) < 8 || len(input.Password) > maxBcryptPasswordBytes {
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

		var userID int
		err = db.Pool.QueryRow(
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

		logger.Info(logging.EventUserRegistrationCompleted, "user_id", userID, "role", "user")
		c.JSON(http.StatusOK, gin.H{"message": "user created"})
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
		if err := db.Pool.QueryRow(c, `SELECT email, COALESCE(first_name, ''), COALESCE(last_name, ''),
			COALESCE(dni, ''), COALESCE(street_address, ''), COALESCE(street_number, ''),
			COALESCE(postal_code, ''), COALESCE(province, ''), COALESCE(locality, ''), COALESCE(phone_number, '')
			FROM users WHERE email=$1 AND role='user'`, email).Scan(
			&profile.Email, &profile.FirstName, &profile.LastName, &profile.DNI,
			&profile.StreetAddress, &profile.StreetNumber, &profile.PostalCode,
			&profile.Province, &profile.Locality, &profile.PhoneNumber,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "user not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"email": profile.Email, "first_name": profile.FirstName, "last_name": profile.LastName,
			"dni": profile.DNI, "street_address": profile.StreetAddress, "street_number": profile.StreetNumber,
			"postal_code": profile.PostalCode, "province": profile.Province, "locality": profile.Locality,
			"phone_number": profile.PhoneNumber,
		})
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

		var storedPassword string
		var role string
		var userID int

		err := db.Pool.QueryRow(
			c,
			"SELECT id, password, role FROM users WHERE email=$1",
			input.Email,
		).Scan(&userID, &storedPassword, &role)

		if err != nil {
			logger.Warn(logging.EventUserLoginRejected, "reason", "invalid_credentials")
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid credentials", nil)
			return
		}

		err = bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(input.Password))
		if err != nil {
			logger.Warn(logging.EventUserLoginRejected, "reason", "invalid_credentials", "user_id", userID)
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid credentials", nil)
			return
		}

		token, err := utils.GenerateToken(input.Email, role, cfg.JWTSecret, cfg.JWTTTL)
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
