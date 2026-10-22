package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const customerRefreshTTL = 30 * 24 * time.Hour

func refreshCookieName(cfg *config.Config) string {
	if cfg.AppEnv == "production" {
		return "__Host-selecto_refresh"
	}
	return "selecto_refresh_dev"
}

func newRefreshToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func refreshTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func setRefreshCookie(c *gin.Context, cfg *config.Config, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: refreshCookieName(cfg), Value: token, Path: "/",
		MaxAge: int(customerRefreshTTL.Seconds()), Secure: cfg.AppEnv == "production",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func clearRefreshCookie(c *gin.Context, cfg *config.Config) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: refreshCookieName(cfg), Path: "/", MaxAge: -1,
		Secure: cfg.AppEnv == "production", HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func issueCustomerRefreshSession(c *gin.Context, db *database.DB, cfg *config.Config, userID int, sessionVersion int64) error {
	token, err := newRefreshToken()
	if err != nil {
		return err
	}
	tx, err := db.Pool.Begin(c)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(c) }()
	if old, err := c.Cookie(refreshCookieName(cfg)); err == nil && old != "" {
		if _, err := tx.Exec(c, "DELETE FROM customer_refresh_sessions WHERE token_hash=$1", refreshTokenHash(old)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(c, "INSERT INTO customer_refresh_sessions (user_id, token_hash, session_version, expires_at) VALUES ($1, $2, $3, NOW() + INTERVAL '30 days')", userID, refreshTokenHash(token), sessionVersion); err != nil {
		return err
	}
	if err := tx.Commit(c); err != nil {
		return err
	}
	setRefreshCookie(c, cfg, token)
	return nil
}

func validCustomerSessionOrigin(c *gin.Context, cfg *config.Config) bool {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if cfg.AppEnv != "production" && origin == "" {
		return true
	}
	return origin != "" && origin == strings.TrimSuffix(cfg.StorefrontURL, "/")
}

func RefreshCustomerSessionHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !validCustomerSessionOrigin(c, cfg) {
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "invalid origin", nil)
			return
		}
		old, err := c.Cookie(refreshCookieName(cfg))
		if err != nil || old == "" {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "session not found", nil)
			return
		}
		newToken, err := newRefreshToken()
		if err != nil {
			apperrors.Internal(c)
			return
		}
		var email, role string
		var version int64
		err = db.Pool.QueryRow(c, `UPDATE customer_refresh_sessions AS s
			SET token_hash=$2, expires_at=NOW() + INTERVAL '30 days', rotated_at=NOW()
			FROM users AS u
			WHERE s.token_hash=$1 AND s.user_id=u.id AND s.expires_at>NOW()
			AND s.session_version=u.session_version AND u.role='user' AND u.email_verified_at IS NOT NULL
			RETURNING u.email, u.role, u.session_version`, refreshTokenHash(old), refreshTokenHash(newToken)).Scan(&email, &role, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			clearRefreshCookie(c, cfg)
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "session expired", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		accessToken, err := utils.GenerateTokenWithVersion(email, role, version, cfg.JWTSecret, cfg.JWTTTL)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		setRefreshCookie(c, cfg, newToken)
		c.JSON(http.StatusOK, gin.H{"token": accessToken, "role": role})
	}
}

func LogoutCustomerSessionHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !validCustomerSessionOrigin(c, cfg) {
			apperrors.JSON(c, http.StatusForbidden, apperrors.CodeForbidden, "invalid origin", nil)
			return
		}
		if token, err := c.Cookie(refreshCookieName(cfg)); err == nil && token != "" {
			if _, err := db.Pool.Exec(c, "DELETE FROM customer_refresh_sessions WHERE token_hash=$1", refreshTokenHash(token)); err != nil {
				apperrors.Internal(c)
				return
			}
		}
		clearRefreshCookie(c, cfg)
		c.Status(http.StatusNoContent)
	}
}
