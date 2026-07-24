package handlers

import (
	"testing"
	"time"

	shippingservice "Selecto-Ecommerce/internal/service/shipping"
)

func TestNormalizeShipmentUpdate(t *testing.T) {
	status := "shipped"
	carrier := "Correo Argentino"
	trackingURL := "https://tracking.example/ABC"
	estimated := "2026-07-25T15:00:00Z"
	next, err := shippingservice.Normalize(shippingservice.Current{Status: "ready_for_dispatch"}, shippingservice.Update{
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
	if _, err := shippingservice.Normalize(shippingservice.Current{Status: "preparing"}, shippingservice.Update{TrackingURL: &trackingURL}); err == nil {
		t.Fatal("unsafe tracking URL was accepted")
	}
}

func TestNormalizeShipmentUpdateRequiresHTTPS(t *testing.T) {
	trackingURL := "http://tracking.example/ABC"
	if _, err := shippingservice.Normalize(shippingservice.Current{Status: "preparing"}, shippingservice.Update{TrackingURL: &trackingURL}); err == nil {
		t.Fatal("insecure tracking URL was accepted")
	}
}

func TestShipmentEventVersionIsStableAndChangesWithCustomerVisibleState(t *testing.T) {
	first := shippingservice.NormalizedUpdate{Status: "shipped", Carrier: "Correo Argentino", TrackingNumber: "ABC"}
	if shippingservice.EventVersion(first) != shippingservice.EventVersion(first) {
		t.Fatal("identical shipment state produced different event versions")
	}
	second := first
	second.TrackingNumber = "DEF"
	if shippingservice.EventVersion(first) == shippingservice.EventVersion(second) {
		t.Fatal("different shipment state produced the same event version")
	}
}
