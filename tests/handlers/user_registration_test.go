package handlers_test

import (
	"testing"

	"Selecto-Ecommerce/internal/shared/validation"
)

func TestRegisterInputValidationRequiresCustomerProfile(t *testing.T) {
	profile := validation.CustomerProfile{
		FirstName:     "Mauri",
		LastName:      "Lopez",
		DNI:           "12345678",
		StreetAddress: "San Martin",
		StreetNumber:  "123",
		PostalCode:    "1000",
		Province:      "Buenos Aires",
		Locality:      "CABA",
		PhoneNumber:   "1123456789",
	}

	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRegisterInputValidationRejectsMissingCustomerProfile(t *testing.T) {
	profile := validation.CustomerProfile{
		FirstName:     "Mauri",
		LastName:      "Lopez",
		DNI:           "12345678",
		StreetAddress: "San Martin",
		StreetNumber:  "123",
		PostalCode:    "1000",
		Province:      "Buenos Aires",
		PhoneNumber:   "1123456789",
	}

	if err := profile.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestRegisterInputValidationRejectsInvalidDocumentAndPhone(t *testing.T) {
	profile := validation.CustomerProfile{
		FirstName:     "Mauri",
		LastName:      "Lopez",
		DNI:           "abc12345",
		StreetAddress: "San Martin",
		StreetNumber:  "123",
		PostalCode:    "ABCD",
		Province:      "Buenos Aires",
		Locality:      "CABA",
		PhoneNumber:   "phone123",
	}

	if err := profile.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestRegisterInputValidationAcceptsInternationalPhone(t *testing.T) {
	profile := validation.CustomerProfile{
		FirstName:     "Mauri",
		LastName:      "Lopez",
		DNI:           "12345678",
		StreetAddress: "San Martin",
		StreetNumber:  "123",
		PostalCode:    "1000",
		Province:      "Buenos Aires",
		Locality:      "CABA",
		PhoneNumber:   "+541123456789",
	}

	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
