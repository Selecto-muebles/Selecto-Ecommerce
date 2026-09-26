package brevo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type ContactClient interface {
	SetSubscription(context.Context, string, bool) error
}

type ClientConfig struct {
	APIKey  string
	ListID  int
	BaseURL string
	Timeout time.Duration
}

type Client struct {
	config ClientConfig
	http   *http.Client
}

func NewClient(config ClientConfig) *Client {
	if config.Timeout <= 0 {
		config.Timeout = 15 * time.Second
	}
	return &Client{config: config, http: &http.Client{Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (client *Client) SetSubscription(ctx context.Context, email string, subscribed bool) error {
	if subscribed {
		return client.request(ctx, http.MethodPost, "/contacts", map[string]any{
			"email": email, "listIds": []int{client.config.ListID}, "updateEnabled": true,
		}, false)
	}
	path := "/contacts/" + url.PathEscape(email)
	return client.request(ctx, http.MethodPut, path, map[string]any{
		"unlinkListIds": []int{client.config.ListID},
	}, true)
}

func (client *Client) request(ctx context.Context, method, path string, payload any, missingIsSuccess bool) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Brevo request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Brevo request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("api-key", client.config.APIKey)
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("send Brevo request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if missingIsSuccess && response.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("Brevo contacts API returned status %d", response.StatusCode)
}
