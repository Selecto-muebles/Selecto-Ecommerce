package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	identityinfra "Selecto-Ecommerce/internal/infrastructure/identity"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"
	"Selecto-Ecommerce/internal/shared/validation"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const googleRegistrationTTL = 10 * time.Minute

type GoogleIdentity = identityinfra.GoogleIdentity
type GoogleIdentityVerifier = identityinfra.GoogleIdentityVerifier

func newGoogleIdentityVerifier() GoogleIdentityVerifier {
	return identityinfra.NewGoogleVerifier(nil, "")
}

type GoogleCredentialInput struct {
	Credential string `json:"credential"`
}

func GoogleAuthHandler(db *database.DB, cfg *config.Config, logger *slog.Logger, verifier GoogleIdentityVerifier) gin.HandlerFunc {
	if verifier == nil {
		verifier = newGoogleIdentityVerifier()
	}
	return func(c *gin.Context) {
		identity, ok := verifyGoogleCredential(c, cfg, verifier, logger)
		if !ok {
			return
		}

		var userID int
		var role string
		var userEmail string
		err := db.Pool.QueryRow(c, `
			SELECT u.id, u.email, u.role
			FROM user_identities i
			JOIN users u ON u.id=i.user_id
			WHERE i.provider='google' AND i.provider_subject=$1`, identity.Subject).Scan(&userID, &userEmail, &role)
		if err == nil {
			respondWithSession(c, db, cfg, logger, userID, userEmail, role, "google")
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			apperrors.Internal(c)
			return
		}

		err = db.Pool.QueryRow(c, "SELECT id, role FROM users WHERE email=$1", identity.Email).Scan(&userID, &role)
		if err == nil {
			logger.Warn(logging.EventGoogleLoginRejected, "reason", "link_required", "user_id", userID)
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "this email already has an account; sign in with your password to link Google", gin.H{"link_required": true, "email": identity.Email})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			apperrors.Internal(c)
			return
		}

		registrationToken, err := generateGoogleRegistrationToken(identity, cfg.JWTSecret)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{
			"registration_required": true,
			"registration_token":    registrationToken,
			"email":                 identity.Email,
			"first_name":            identity.FirstName,
			"last_name":             identity.LastName,
		})
	}
}

type GoogleRegisterInput struct {
	RegistrationToken string `json:"registration_token"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	DNI               string `json:"dni"`
	StreetAddress     string `json:"street_address"`
	StreetNumber      string `json:"street_number"`
	PostalCode        string `json:"postal_code"`
	Province          string `json:"province"`
	Locality          string `json:"locality"`
	PhoneNumber       string `json:"phone_number"`
}

func GoogleRegisterHandler(db *database.DB, cfg *config.Config, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input GoogleRegisterInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		claims, err := validateGoogleRegistrationToken(strings.TrimSpace(input.RegistrationToken), cfg.JWTSecret)
		if err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid or expired Google registration", nil)
			return
		}
		profile := validation.NormalizeCustomerProfile(validation.CustomerProfile{
			FirstName: input.FirstName, LastName: input.LastName, DNI: input.DNI,
			StreetAddress: input.StreetAddress, StreetNumber: input.StreetNumber,
			PostalCode: input.PostalCode, Province: input.Province, Locality: input.Locality,
			PhoneNumber: input.PhoneNumber,
		})
		if err := profile.Validate(); err != nil {
			apperrors.BadRequest(c, "customer billing and shipping information must be valid")
			return
		}

		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var userID int
		err = tx.QueryRow(c, `INSERT INTO users (email, password, role, first_name, last_name, dni, street_address, street_number, postal_code, province, locality, phone_number, email_verified_at)
			VALUES ($1, NULL, 'user', $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW()) RETURNING id`,
			claims.Email, profile.FirstName, profile.LastName, profile.DNI, profile.StreetAddress, profile.StreetNumber, profile.PostalCode, profile.Province, profile.Locality, profile.PhoneNumber,
		).Scan(&userID)
		if err != nil {
			if uniqueViolation(err) {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "user already exists", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "INSERT INTO user_identities (user_id, provider, provider_subject, provider_email) VALUES ($1, 'google', $2, $3)", userID, claims.Subject, claims.Email); err != nil {
			if uniqueViolation(err) {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "Google account already linked", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, 'google_account_registered', 'user', $2, $3)", claims.Email, userID, `{"provider":"google"}`); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		respondWithSession(c, db, cfg, logger, userID, claims.Email, "user", "google_registration")
	}
}

func GoogleLinkHandler(db *database.DB, cfg *config.Config, logger *slog.Logger, verifier GoogleIdentityVerifier) gin.HandlerFunc {
	if verifier == nil {
		verifier = newGoogleIdentityVerifier()
	}
	return func(c *gin.Context) {
		identity, ok := verifyGoogleCredential(c, cfg, verifier, logger)
		if !ok {
			return
		}
		currentEmail, _ := c.Get("email")
		role, _ := c.Get("role")
		email := strings.ToLower(strings.TrimSpace(fmt.Sprint(currentEmail)))
		if fmt.Sprint(role) != "user" || identity.Email != email {
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "Google email must match the authenticated customer", nil)
			return
		}
		var userID int
		if err := db.Pool.QueryRow(c, "SELECT id FROM users WHERE email=$1 AND role='user'", email).Scan(&userID); err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "user not found", nil)
			return
		}
		var linkedSubject string
		err := db.Pool.QueryRow(c, "SELECT provider_subject FROM user_identities WHERE user_id=$1 AND provider='google'", userID).Scan(&linkedSubject)
		if err == nil {
			if linkedSubject != identity.Subject {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "customer already linked to another Google account", nil)
				return
			}
			c.JSON(http.StatusOK, gin.H{"linked": true})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			apperrors.Internal(c)
			return
		}

		command, err := db.Pool.Exec(c, `INSERT INTO user_identities (user_id, provider, provider_subject, provider_email)
			VALUES ($1, 'google', $2, $3)`, userID, identity.Subject, identity.Email)
		if err != nil {
			if uniqueViolation(err) {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "Google account belongs to another user", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		if command.RowsAffected() > 0 {
			_, _ = db.Pool.Exec(c, "INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, 'google_account_linked', 'user', $2, $3)", email, userID, `{"provider":"google"}`)
			_, _ = db.Pool.Exec(c, "UPDATE users SET email_verified_at=COALESCE(email_verified_at, NOW()) WHERE id=$1", userID)
		}
		c.JSON(http.StatusOK, gin.H{"linked": true})
	}
}

func verifyGoogleCredential(c *gin.Context, cfg *config.Config, verifier GoogleIdentityVerifier, logger *slog.Logger) (*GoogleIdentity, bool) {
	if cfg.GoogleClientID == "" {
		apperrors.JSON(c, http.StatusServiceUnavailable, apperrors.CodeServiceUnavailable, "Google sign-in is not configured", nil)
		return nil, false
	}
	var input GoogleCredentialInput
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Credential) == "" {
		apperrors.BadRequest(c, "Google credential is required")
		return nil, false
	}
	identity, err := verifier.Verify(c, strings.TrimSpace(input.Credential), cfg.GoogleClientID)
	if err != nil {
		logger.Warn(logging.EventGoogleLoginRejected, "reason", "invalid_credential")
		apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid Google credential", nil)
		return nil, false
	}
	return identity, true
}

func respondWithSession(c *gin.Context, db *database.DB, cfg *config.Config, logger *slog.Logger, userID int, email, role, source string) {
	if role != "user" {
		apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "Google sign-in is available only for customer accounts", nil)
		return
	}
	var sessionVersion int64
	if err := db.Pool.QueryRow(c, "SELECT session_version FROM users WHERE id=$1", userID).Scan(&sessionVersion); err != nil {
		apperrors.Internal(c)
		return
	}
	token, err := utils.GenerateTokenWithVersion(email, role, sessionVersion, cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		apperrors.Internal(c)
		return
	}
	logger.Info(logging.EventUserLoginCompleted, "user_id", userID, "role", role, "source", source)
	c.JSON(http.StatusOK, gin.H{"token": token, "role": role})
}

type googleRegistrationClaims struct {
	Email     string `json:"email"`
	Subject   string `json:"google_sub"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Purpose   string `json:"purpose"`
	jwt.RegisteredClaims
}

func generateGoogleRegistrationToken(identity *GoogleIdentity, secret string) (string, error) {
	now := time.Now()
	claims := googleRegistrationClaims{
		Email: identity.Email, Subject: identity.Subject, FirstName: identity.FirstName, LastName: identity.LastName,
		Purpose:          "google_registration",
		RegisteredClaims: jwt.RegisteredClaims{Issuer: "selecto-ecommerce", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(googleRegistrationTTL))},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func validateGoogleRegistrationToken(raw, secret string) (*googleRegistrationClaims, error) {
	claims := &googleRegistrationClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	}, jwt.WithExpirationRequired(), jwt.WithIssuer("selecto-ecommerce"))
	if err != nil || !token.Valid || claims.Purpose != "google_registration" || claims.Email == "" || claims.Subject == "" {
		return nil, errors.New("invalid registration token")
	}
	return claims, nil
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
