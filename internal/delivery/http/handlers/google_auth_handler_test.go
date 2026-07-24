package handlers

import "testing"

func TestGoogleRegistrationTokenIsPurposeBound(t *testing.T) {
	secret := "test-secret-with-at-least-32-characters"
	raw, err := generateGoogleRegistrationToken(&GoogleIdentity{Subject: "subject", Email: "person@example.com"}, secret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := validateGoogleRegistrationToken(raw, secret)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "subject" || claims.Email != "person@example.com" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if _, err := validateGoogleRegistrationToken(raw, "another-test-secret-with-32-characters"); err == nil {
		t.Fatal("registration token signed with another key was accepted")
	}
}
