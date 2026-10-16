package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	checkoutservice "Selecto-Ecommerce/internal/service/checkout"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/serviceauth"
	"Selecto-Ecommerce/internal/shared/serviceidentity"
)

type CardPaymentInput struct {
	OrderID              int    `json:"order_id"`
	PreferenceID         string `json:"preference_id"`
	Token                string `json:"token"`
	PaymentMethodID      string `json:"payment_method_id"`
	IssuerID             int    `json:"issuer_id"`
	Installments         int    `json:"installments"`
	PayerEmail           string `json:"payer_email"`
	IdentificationType   string `json:"identification_type,omitempty"`
	IdentificationNumber string `json:"identification_number,omitempty"`
	IdempotencyKey       string `json:"idempotency_key"`
}

type CardPaymentResult struct {
	PaymentID           int64  `json:"payment_id"`
	Status              string `json:"status"`
	VerificationPending bool   `json:"verification_pending"`
}

func (client *Client) CreateCardPayment(ctx context.Context, input CardPaymentInput, requestID, correlationID string) (CardPaymentResult, int, error) {
	if client.baseURL == "" {
		return CardPaymentResult{}, 0, checkoutservice.ErrGatewayNotConfigured
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return CardPaymentResult{}, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/create-card-payment", bytes.NewReader(payload))
	if err != nil {
		return CardPaymentResult{}, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", input.IdempotencyKey)
	serviceauth.AddHeaders(request, client.secret, payload, requestID, correlationID)
	response, err := client.http.Do(request)
	if err != nil {
		return CardPaymentResult{}, 0, err
	}
	defer response.Body.Close()
	var result CardPaymentResult
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&result); err != nil {
			return CardPaymentResult{}, 0, err
		}
		return result, response.StatusCode, nil
	}
	return CardPaymentResult{}, response.StatusCode, nil
}

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func NewClient(baseURL string, timeout time.Duration, secret, audience string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    NewHTTPClient(timeout, audience),
	}
}

func NewHTTPClient(timeout time.Duration, audience string) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true,
		MaxIdleConns: 100, MaxIdleConnsPerHost: 100, IdleConnTimeout: 90 * time.Second,
	}
	return serviceidentity.NewHTTPClient(timeout, audience, transport)
}

func (client *Client) CreatePreference(ctx context.Context, orderID int, amount money.Cents, customer checkoutservice.Customer, requestID, correlationID string) (checkoutservice.Preference, error) {
	if client.baseURL == "" {
		return checkoutservice.Preference{}, checkoutservice.ErrGatewayNotConfigured
	}
	payload, err := json.Marshal(map[string]any{
		"order_id": orderID, "amount": amount.Float64(),
		"customer": map[string]string{
			"email": customer.Email, "name": customer.Name, "identification": customer.Identification,
		},
	})
	if err != nil {
		return checkoutservice.Preference{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/create-preference", bytes.NewReader(payload))
	if err != nil {
		return checkoutservice.Preference{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	serviceauth.AddHeaders(request, client.secret, payload, requestID, correlationID)
	response, err := client.http.Do(request)
	if err != nil {
		return checkoutservice.Preference{}, checkoutservice.GatewayError{Kind: checkoutservice.GatewayUnreachable}
	}
	defer response.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return checkoutservice.Preference{}, checkoutservice.GatewayError{Kind: checkoutservice.GatewayInvalid}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return checkoutservice.Preference{}, checkoutservice.GatewayError{Kind: checkoutservice.GatewayRejected, Payload: result}
	}
	preferenceID, _ := result["preference_id"].(string)
	checkoutURL, _ := result["checkout_url"].(string)
	environment, _ := result["environment"].(string)
	if preferenceID == "" || checkoutURL == "" {
		return checkoutservice.Preference{}, checkoutservice.GatewayError{Kind: checkoutservice.GatewayIncomplete}
	}
	return checkoutservice.Preference{ID: preferenceID, CheckoutURL: checkoutURL, Environment: environment, Payload: result}, nil
}
