package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func GoogleLinkHandler(db *database.DB, cfg *config.Config, logger *slog.Logger, verifier GoogleIdentityVerifier) gin.HandlerFunc {
	if verifier == nil {
		verifier = newGoogleIdentityVerifier()
	}
	return func(c *gin.Context) {
		identity, ok := verifyGoogleCredential(c, cfg, verifier, logger)
		if !ok {
			return
		}
		email := strings.ToLower(strings.TrimSpace(c.GetString("email")))
		if c.GetString("role") != "user" || identity.Email != email {
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "Google email must match the authenticated customer", nil)
			return
		}

		ctx := c.Request.Context()
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(ctx)
		// Serialize links for the same customer. A simultaneous retry observes
		// the committed identity and returns success without a second audit entry.
		var userID int
		err = tx.QueryRow(ctx, "SELECT id FROM users WHERE email=$1 AND role='user' FOR UPDATE", email).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "user not found", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		var linkedSubject string
		err = tx.QueryRow(ctx, "SELECT provider_subject FROM user_identities WHERE user_id=$1 AND provider='google'", userID).Scan(&linkedSubject)
		switch {
		case err == nil:
			if linkedSubject != identity.Subject {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "customer already linked to another Google account", nil)
				return
			}
		case errors.Is(err, pgx.ErrNoRows):
			if _, err := tx.Exec(ctx, `INSERT INTO user_identities (user_id, provider, provider_subject, provider_email)
				VALUES ($1, 'google', $2, $3)`, userID, identity.Subject, identity.Email); err != nil {
				if uniqueViolation(err) {
					apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "Google account belongs to another user", nil)
					return
				}
				apperrors.Internal(c)
				return
			}
			if _, err := tx.Exec(ctx, "UPDATE users SET email_verified_at=COALESCE(email_verified_at, NOW()) WHERE id=$1", userID); err != nil {
				apperrors.Internal(c)
				return
			}
			if _, err := tx.Exec(ctx, `INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata)
				VALUES ($1, 'google_account_linked', 'user', $2, $3)`, email, userID, `{"provider":"google"}`); err != nil {
				apperrors.Internal(c)
				return
			}
		default:
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"linked": true})
	}
}
