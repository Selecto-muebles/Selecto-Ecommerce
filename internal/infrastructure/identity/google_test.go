package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGoogleVerifierValidatesSignatureAndClaims(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
			"kid": kid, "kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
		}}})
	}))
	defer server.Close()

	verifier := NewGoogleVerifier(server.Client(), server.URL)
	credential := signedGoogleCredential(t, privateKey, kid, "selecto-client")
	identity, err := verifier.Verify(context.Background(), credential, "selecto-client")
	if err != nil {
		t.Fatalf("verify credential: %v", err)
	}
	if identity.Subject != "google-user-1" || identity.Email != "customer@example.com" || !identity.EmailValid {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if _, err := verifier.Verify(context.Background(), credential, "another-client"); err == nil {
		t.Fatal("credential with another audience was accepted")
	}
}

func signedGoogleCredential(t *testing.T, key *rsa.PrivateKey, kid, audience string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": audience, "sub": "google-user-1",
		"email": "Customer@Example.com", "email_verified": true, "given_name": "Ada", "family_name": "Lovelace",
		"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
