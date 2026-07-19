package handlers

import (
	"testing"
	"time"
)

func TestNormalizeShipmentUpdate(t *testing.T) {
	status := "shipped"
	carrier := "Correo Argentino"
	trackingURL := "https://tracking.example/ABC"
	estimated := "2026-07-25T15:00:00Z"
	next, err := normalizeShipmentUpdate(ShipmentResponse{Status: "ready_for_dispatch"}, UpdateShipmentInput{
		Status: &status, Carrier: &carrier, TrackingURL: &trackingURL, EstimatedDeliveryAt: &estimated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != status || next.Carrier != carrier || next.EstimatedDeliveryAt == nil || !next.EstimatedDeliveryAt.Equal(time.Date(2026, 7, 25, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected shipment update: %+v", next)
	}
}

func TestNormalizeShipmentUpdateRejectsUnsafeTrackingURL(t *testing.T) {
	trackingURL := "javascript:alert(1)"
	if _, err := normalizeShipmentUpdate(ShipmentResponse{Status: "preparing"}, UpdateShipmentInput{TrackingURL: &trackingURL}); err == nil {
		t.Fatal("unsafe tracking URL was accepted")
	}
}
