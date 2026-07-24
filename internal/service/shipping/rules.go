package shipping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/domain"
)

type Current struct {
	Status              string
	Carrier             string
	TrackingNumber      string
	TrackingURL         string
	EstimatedDeliveryAt *time.Time
	CustomerNote        string
}

type Update struct {
	Status              *string
	Carrier             *string
	TrackingNumber      *string
	TrackingURL         *string
	EstimatedDeliveryAt *string
	CustomerNote        *string
}

type NormalizedUpdate struct {
	Status              string
	Carrier             string
	TrackingNumber      string
	TrackingURL         string
	EstimatedDeliveryAt *time.Time
	CustomerNote        string
}

func (input Update) Empty() bool {
	return input.Status == nil && input.Carrier == nil && input.TrackingNumber == nil && input.TrackingURL == nil && input.EstimatedDeliveryAt == nil && input.CustomerNote == nil
}

func Normalize(current Current, input Update) (NormalizedUpdate, error) {
	next := NormalizedUpdate{
		Status: current.Status, Carrier: current.Carrier, TrackingNumber: current.TrackingNumber,
		TrackingURL: current.TrackingURL, EstimatedDeliveryAt: current.EstimatedDeliveryAt, CustomerNote: current.CustomerNote,
	}
	if input.Status != nil {
		next.Status = strings.TrimSpace(*input.Status)
	}
	if !domain.ShipmentStatus(next.Status).Valid() {
		return next, errors.New("invalid shipment status")
	}
	if input.Carrier != nil {
		next.Carrier = strings.TrimSpace(*input.Carrier)
	}
	if input.TrackingNumber != nil {
		next.TrackingNumber = strings.TrimSpace(*input.TrackingNumber)
	}
	if input.TrackingURL != nil {
		next.TrackingURL = strings.TrimSpace(*input.TrackingURL)
	}
	if input.CustomerNote != nil {
		next.CustomerNote = strings.TrimSpace(*input.CustomerNote)
	}
	if len(next.Carrier) > 120 || len(next.TrackingNumber) > 160 || len(next.TrackingURL) > 1000 || len(next.CustomerNote) > 1000 {
		return next, errors.New("shipment field exceeds maximum length")
	}
	if next.TrackingURL != "" {
		parsed, err := url.ParseRequestURI(next.TrackingURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return next, errors.New("tracking_url must be an absolute HTTPS URL")
		}
	}
	if input.EstimatedDeliveryAt != nil {
		raw := strings.TrimSpace(*input.EstimatedDeliveryAt)
		if raw == "" {
			next.EstimatedDeliveryAt = nil
		} else {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return next, errors.New("estimated_delivery_at must use RFC3339")
			}
			next.EstimatedDeliveryAt = &parsed
		}
	}
	return next, nil
}

func EventVersion(shipment NormalizedUpdate) string {
	payload, _ := json.Marshal(shipment)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:8])
}
