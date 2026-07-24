package identity

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

type GoogleIdentity struct {
	Subject    string
	Email      string
	FirstName  string
	LastName   string
	EmailValid bool
}

type GoogleIdentityVerifier interface {
	Verify(context.Context, string, string) (*GoogleIdentity, error)
}

type GoogleVerifier struct {
	client  *http.Client
	jwksURL string
	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

func NewGoogleVerifier(client *http.Client, jwksURL string) *GoogleVerifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if strings.TrimSpace(jwksURL) == "" {
		jwksURL = googleJWKSURL
	}
	return &GoogleVerifier{client: client, jwksURL: jwksURL}
}

func (verifier *GoogleVerifier) Verify(ctx context.Context, credential, audience string) (*GoogleIdentity, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(credential, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("Google token has no key id")
		}
		key, err := verifier.key(ctx, kid, false)
		if err != nil {
			key, err = verifier.key(ctx, kid, true)
		}
		return key, err
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithAudience(audience), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid Google token")
	}
	issuer, _ := claims["iss"].(string)
	if issuer != "accounts.google.com" && issuer != "https://accounts.google.com" {
		return nil, errors.New("invalid Google issuer")
	}
	identity := &GoogleIdentity{
		Subject: claimString(claims, "sub"), Email: strings.ToLower(strings.TrimSpace(claimString(claims, "email"))),
		FirstName: strings.TrimSpace(claimString(claims, "given_name")), LastName: strings.TrimSpace(claimString(claims, "family_name")),
		EmailValid: claimBool(claims, "email_verified"),
	}
	if identity.Subject == "" || identity.Email == "" || !identity.EmailValid {
		return nil, errors.New("google identity is incomplete")
	}
	return identity, nil
}

func (verifier *GoogleVerifier) key(ctx context.Context, kid string, force bool) (*rsa.PublicKey, error) {
	verifier.mu.RLock()
	key := verifier.keys[kid]
	validCache := time.Now().Before(verifier.expires)
	verifier.mu.RUnlock()
	if key != nil && validCache && !force {
		return key, nil
	}
	if err := verifier.refresh(ctx); err != nil {
		return nil, err
	}
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	key = verifier.keys[kid]
	if key == nil {
		return nil, errors.New("Google signing key not found")
	}
	return key, nil
}

func (verifier *GoogleVerifier) refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, verifier.jwksURL, nil)
	if err != nil {
		return err
	}
	response, err := verifier.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Google JWKS returned status %d", response.StatusCode)
	}
	var payload struct {
		Keys []struct {
			KID string `json:"kid"`
			KTY string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey, len(payload.Keys))
	for _, item := range payload.Keys {
		if item.KTY != "RSA" || item.KID == "" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(item.N)
		if err != nil {
			return err
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(item.E)
		if err != nil {
			return err
		}
		exponent := 0
		for _, current := range exponentBytes {
			exponent = exponent<<8 + int(current)
		}
		if exponent <= 0 {
			return errors.New("invalid Google RSA exponent")
		}
		keys[item.KID] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}
	}
	if len(keys) == 0 {
		return errors.New("Google JWKS contained no RSA keys")
	}
	verifier.mu.Lock()
	verifier.keys = keys
	verifier.expires = time.Now().Add(cacheMaxAge(response.Header.Get("Cache-Control")))
	verifier.mu.Unlock()
	return nil
}

func cacheMaxAge(header string) time.Duration {
	for _, directive := range strings.Split(header, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(directive), "=")
		if found && key == "max-age" {
			seconds, err := strconv.Atoi(value)
			if err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return time.Hour
}

func claimString(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return value
}

func claimBool(claims map[string]any, key string) bool {
	value, _ := claims[key].(bool)
	return value
}
