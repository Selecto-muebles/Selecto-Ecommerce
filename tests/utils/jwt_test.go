package utils_test

import (
	"testing"
	"time"

	"Selecto-Ecommerce/internal/shared/utils"
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

func TestValidateTokenRejectsWrongSecret(t *testing.T) {
	token, err := utils.GenerateToken("user@example.com", "user", "01234567890123456789012345678901", time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if _, err := utils.ValidateToken(token, "abcdefghijklmnopqrstuvwxyz123456"); err == nil {
		t.Fatal("ValidateToken() error = nil, want error")
	}
}
