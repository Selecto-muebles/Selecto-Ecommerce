package shipping

import "strings"

// GestionarReadiness checks local data only. It does not validate Gestionar's
// unpublished contract or authorize sending a package to the provider.
type GestionarReadiness struct {
	OrderStatus    string
	ShipmentStatus string
	HasAddress     bool
	RecipientName  string
	Street         string
	StreetNumber   string
	Locality       string
	Province       string
	PostalCode     string
	Phone          string
	Email          string
	ItemCount      int
	MissingSKUs    int
}

type GestionarBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r GestionarReadiness) Blockers() []GestionarBlocker {
	blockers := make([]GestionarBlocker, 0)
	add := func(code, message string) {
		blockers = append(blockers, GestionarBlocker{Code: code, Message: message})
	}
	if r.OrderStatus != "paid" {
		add("order_not_paid", "La orden todavía no está pagada.")
	}
	if r.ShipmentStatus != "preparing" && r.ShipmentStatus != "ready_for_dispatch" {
		add("shipment_not_pending", "El envío no está pendiente de despacho.")
	}
	if !r.HasAddress {
		add("address_missing", "Falta la dirección guardada para esta orden.")
	} else {
		if strings.TrimSpace(r.RecipientName) == "" {
			add("recipient_missing", "Falta el nombre del destinatario.")
		}
		if strings.TrimSpace(r.Street) == "" || strings.TrimSpace(r.StreetNumber) == "" {
			add("street_missing", "Faltan la calle o la altura de entrega.")
		}
		if strings.TrimSpace(r.Locality) == "" || strings.TrimSpace(r.Province) == "" || strings.TrimSpace(r.PostalCode) == "" {
			add("location_missing", "Faltan localidad, provincia o código postal.")
		}
		if strings.TrimSpace(r.Phone) == "" {
			add("phone_missing", "Falta el teléfono del destinatario.")
		}
	}
	if strings.TrimSpace(r.Email) == "" {
		add("email_missing", "Falta el correo del cliente.")
	}
	if r.ItemCount == 0 {
		add("items_missing", "La orden no tiene artículos.")
	} else if r.MissingSKUs > 0 {
		add("sku_missing", "Uno o más artículos no tienen SKU.")
	}
	return blockers
}
