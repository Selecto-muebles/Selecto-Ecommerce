package shipping

import "testing"

func TestGestionarReadiness(t *testing.T) {
	valid := GestionarReadiness{
		OrderStatus: "paid", ShipmentStatus: "preparing", HasAddress: true,
		RecipientName: "Ada Lovelace", Street: "Calle", StreetNumber: "123",
		Locality: "CABA", Province: "Buenos Aires", PostalCode: "1000",
		Phone: "1112345678", Email: "ada@example.test", ItemCount: 1,
	}
	if blockers := valid.Blockers(); len(blockers) != 0 {
		t.Fatalf("valid order blocked: %+v", blockers)
	}
	valid.ShipmentStatus = "ready_for_dispatch"
	if blockers := valid.Blockers(); len(blockers) != 0 {
		t.Fatalf("ready shipment blocked: %+v", blockers)
	}
	valid.OrderStatus = "pending"
	valid.ShipmentStatus = "delivered"
	valid.HasAddress = false
	valid.Email = ""
	valid.ItemCount = 0
	blockers := valid.Blockers()
	for _, code := range []string{"order_not_paid", "shipment_not_pending", "address_missing", "email_missing", "items_missing"} {
		found := false
		for _, blocker := range blockers {
			if blocker.Code == code {
				found = true
			}
		}
		if !found {
			t.Errorf("missing blocker %s: %+v", code, blockers)
		}
	}
}

func TestGestionarReadinessBlocksIncompleteAddressAndSKU(t *testing.T) {
	input := GestionarReadiness{OrderStatus: "paid", ShipmentStatus: "preparing", HasAddress: true,
		RecipientName: " ", Street: "Calle", Locality: "CABA", ItemCount: 2, MissingSKUs: 1}
	blockers := input.Blockers()
	for _, code := range []string{"recipient_missing", "street_missing", "location_missing", "phone_missing", "sku_missing"} {
		found := false
		for _, blocker := range blockers {
			if blocker.Code == code {
				found = true
			}
		}
		if !found {
			t.Errorf("missing blocker %s: %+v", code, blockers)
		}
	}
}
