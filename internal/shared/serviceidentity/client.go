package serviceidentity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const metadataIdentityURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity"

var metadataHTTPClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		Proxy:                 nil,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 2 * time.Second,
	},
}

type tokenFetcher func(context.Context, string) (string, time.Time, error)

type authenticatedTransport struct {
	base       http.RoundTripper
	audience   string
	fetchToken tokenFetcher
	mu         sync.Mutex
	token      string
	expiresAt  time.Time
}

func NewHTTPClient(timeout time.Duration, audience string, base *http.Transport) *http.Client {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	var transport http.RoundTripper = base
	if strings.TrimSpace(audience) != "" {
		transport = &authenticatedTransport{base: base, audience: audience, fetchToken: fetchMetadataIdentityToken}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

func (t *authenticatedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	token, err := t.validToken(request.Context())
	if err != nil {
		return nil, fmt.Errorf("obtain service identity token: %w", err)
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(clone)
}

func (t *authenticatedTransport) validToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" && time.Now().Add(5*time.Minute).Before(t.expiresAt) {
		return t.token, nil
	}
	token, expiresAt, err := t.fetchToken(ctx, t.audience)
	if err != nil {
		return "", err
	}
	t.token = token
	t.expiresAt = expiresAt
	return token, nil
}

func fetchMetadataIdentityToken(ctx context.Context, audience string) (string, time.Time, error) {
	endpoint := metadataIdentityURL + "?audience=" + url.QueryEscape(audience)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	request.Header.Set("Metadata-Flavor", "Google")
	response, err := metadataHTTPClient.Do(request)
	if err != nil {
		return "", time.Time{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if err != nil {
		return "", time.Time{}, err
	}
	if response.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("metadata server returned HTTP %d", response.StatusCode)
	}
	token := strings.TrimSpace(string(body))
	expiresAt, err := tokenExpiration(token)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func tokenExpiration(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("metadata server returned an invalid identity token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, errors.New("metadata server returned an invalid identity token payload")
	}
	var claims struct {
		ExpiresAt int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ExpiresAt <= 0 {
		return time.Time{}, errors.New("metadata identity token has no valid expiration")
	}
	return time.Unix(claims.ExpiresAt, 0), nil
}
