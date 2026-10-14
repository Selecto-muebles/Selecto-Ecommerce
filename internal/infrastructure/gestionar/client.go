package gestionar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const pickupTimesPath = "/api/external-client/pickup-times"
const maxResponseBytes = 64 << 10

type PickupTime struct {
	ID   int    `json:"id"`
	Time string `json:"time"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string, transport *http.Client) *Client {
	if transport == nil {
		transport = &http.Client{Timeout: 10 * time.Second}
	}
	client := *transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &client}
}

// PickupTimes performs only the documented read-only request. Do not forward
// the provider response wholesale: other endpoints include client secrets.
func (client *Client) PickupTimes(ctx context.Context) ([]PickupTime, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+pickupTimesPath, nil)
	if err != nil {
		return nil, errors.New("invalid Gestionar endpoint")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("secret-token-key", client.token)
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Gestionar pickup-times request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Gestionar pickup-times returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return nil, errors.New("Gestionar pickup-times response is invalid or too large")
	}
	var payload struct {
		Status  string       `json:"status"`
		Success bool         `json:"success"`
		Data    []PickupTime `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || !payload.Success || !strings.EqualFold(payload.Status, "Success") {
		return nil, errors.New("Gestionar pickup-times returned an unsuccessful response")
	}
	for _, slot := range payload.Data {
		if slot.ID <= 0 || strings.TrimSpace(slot.Time) == "" || len(slot.Time) > 120 {
			return nil, errors.New("Gestionar pickup-times response has invalid slots")
		}
	}
	if payload.Data == nil {
		return []PickupTime{}, nil
	}
	return payload.Data, nil
}
