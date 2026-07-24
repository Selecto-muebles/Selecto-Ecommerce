package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
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
