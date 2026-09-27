package validation

import (
	"strings"
	"testing"
)

func TestRegistrationDefersCommercialFieldsButCheckoutRequiresThem(t *testing.T) {
	for _, profile := range []CustomerProfile{{}, {FirstName: "Ana", LastName: "Pérez"}} {
		if err := profile.ValidateForRegistration(); err != nil {
			t.Fatal(err)
		}
		if err := profile.Validate(); err == nil {
			t.Fatal("checkout accepted missing commercial fields")
		}
	}
}

func TestOptionalRegistrationFieldsAreStillValidated(t *testing.T) {
	for _, profile := range []CustomerProfile{
		{DNI: "bad"}, {PostalCode: "C1000"}, {PhoneNumber: "letters"},
		{FirstName: strings.Repeat("á", 101)}, {LastName: "bad\x00name"},
	} {
		if profile.ValidateForRegistration() == nil {
			t.Fatalf("invalid optional profile accepted: %#v", profile)
		}
	}
	if err := (CustomerProfile{DNI: "12345678", PhoneNumber: "+541112345678", PostalCode: "1000"}).ValidateForRegistration(); err != nil {
		t.Fatal(err)
	}
}
