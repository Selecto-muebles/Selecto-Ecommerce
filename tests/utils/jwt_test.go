package utils_test

import (
	"testing"
	"time"

	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateAndValidateToken(t *testing.T) {
	secret := "01234567890123456789012345678901"

	token, err := utils.GenerateToken("admin@example.com", "admin", secret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := utils.ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims.Email != "admin@example.com" || claims.Role != "admin" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestGenerateAndValidateTokenPreservesSessionVersion(t *testing.T) {
	secret := "01234567890123456789012345678901"
	token, err := utils.GenerateTokenWithVersion("user@example.com", "user", 7, secret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateTokenWithVersion() error = %v", err)
	}
	claims, err := utils.ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims.SessionVersion != 7 {
		t.Fatalf("SessionVersion = %d, want 7", claims.SessionVersion)
	}
}

func TestValidateTokenRejectsExpiredToken(t *testing.T) {
	token, err := utils.GenerateToken("user@example.com", "user", "01234567890123456789012345678901", -time.Second)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if _, err := utils.ValidateToken(token, "01234567890123456789012345678901"); err == nil {
		t.Fatal("ValidateToken() error = nil, want expired token error")
	}
}

func TestValidateTokenRejectsNoneAlgorithm(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"email": "user@example.com",
		"role":  "user",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	if _, err := utils.ValidateToken(tokenString, "01234567890123456789012345678901"); err == nil {
		t.Fatal("ValidateToken() error = nil, want signing method error")
	}
}

func TestValidateTokenRejectsWrongSecret(t *testing.T) {
	token, err := utils.GenerateToken("user@example.com", "user", "01234567890123456789012345678901", time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if _, err := utils.ValidateToken(token, "abcdefghijklmnopqrstuvwxyz123456"); err == nil {
		t.Fatal("ValidateToken() error = nil, want error")
	}
}
