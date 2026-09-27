package validation

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

// Registration establishes an account, not a shipping destination. Optional
// legacy fields remain accepted and validated; checkout still calls Validate.
func (profile CustomerProfile) ValidateForRegistration() error {
	for _, field := range []struct {
		value string
		limit int
	}{
		{profile.FirstName, 100}, {profile.LastName, 100},
		{profile.StreetAddress, 200}, {profile.StreetNumber, 30},
		{profile.Province, 100}, {profile.Locality, 100},
	} {
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit {
			return errors.New("customer field is too long or invalid")
		}
		for _, char := range field.value {
			if unicode.IsControl(char) {
				return errors.New("customer field contains control characters")
			}
		}
	}
	if profile.DNI != "" && !validDigits(profile.DNI, 7, 12) {
		return errors.New("invalid dni")
	}
	if profile.PostalCode != "" && !validDigits(profile.PostalCode, 4, 10) {
		return errors.New("invalid postal code")
	}
	if profile.PhoneNumber != "" && !validPhone(profile.PhoneNumber, 8, 20) {
		return errors.New("invalid phone number")
	}
	return nil
}
