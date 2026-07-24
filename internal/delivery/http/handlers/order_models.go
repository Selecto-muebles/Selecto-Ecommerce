package handlers

import (
	"time"

	"Selecto-Ecommerce/internal/shared/utils"
	"Selecto-Ecommerce/internal/shared/validation"
)

type OrderItemInput struct {
	ProductID       utils.PublicID    `json:"product_id" binding:"required"`
	Quantity        int               `json:"quantity" binding:"required"`
	SelectedOptions map[string]string `json:"selected_options,omitempty"`
}

type CreateOrderInput struct {
	Items           []OrderItemInput          `json:"items" binding:"required"`
	ShippingAddress *CreateOrderShippingInput `json:"shipping_address,omitempty"`
}

type CreateOrderShippingInput struct {
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name"`
	DNI                   string `json:"dni"`
	StreetAddress         string `json:"street_address"`
	StreetNumber          string `json:"street_number"`
	PostalCode            string `json:"postal_code"`
	Province              string `json:"province"`
	Locality              string `json:"locality"`
	PhoneNumber           string `json:"phone_number"`
	RequestedDeliveryDate string `json:"requested_delivery_date"`
}

type OrderItemResponse struct {
	ID              string            `json:"id"`
	ProductID       string            `json:"product_id"`
	Name            string            `json:"name"`
	Quantity        int               `json:"quantity"`
	Price           float64           `json:"price"`
	Subtotal        float64           `json:"subtotal"`
	SelectedOptions map[string]string `json:"selected_options"`
}

type OrderResponse struct {
	ID              string                   `json:"id"`
	UserID          int                      `json:"user_id"`
	Status          string                   `json:"status"`
	Total           float64                  `json:"total"`
	CreatedAt       time.Time                `json:"created_at"`
	Items           []OrderItemResponse      `json:"items"`
	ShippingAddress *ShippingAddressResponse `json:"shipping_address,omitempty"`
	Shipment        *ShipmentResponse        `json:"shipment,omitempty"`
}

func (input CreateOrderShippingInput) normalizedProfile() validation.CustomerProfile {
	return validation.NormalizeCustomerProfile(validation.CustomerProfile{
		FirstName: input.FirstName, LastName: input.LastName, DNI: input.DNI,
		StreetAddress: input.StreetAddress, StreetNumber: input.StreetNumber,
		PostalCode: input.PostalCode, Province: input.Province, Locality: input.Locality,
		PhoneNumber: input.PhoneNumber,
	})
}
