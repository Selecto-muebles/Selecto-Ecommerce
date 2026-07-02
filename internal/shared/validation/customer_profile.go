package validation

import (
	"errors"
	"strings"
)

type CustomerProfile struct {
	FirstName     string
	LastName      string
	DNI           string
	StreetAddress string
	StreetNumber  string
	PostalCode    string
	Province      string
	Locality      string
	PhoneNumber   string
}

func NormalizeCustomerProfile(profile CustomerProfile) CustomerProfile {
	return CustomerProfile{
		FirstName:     normalizeText(profile.FirstName),
		LastName:      normalizeText(profile.LastName),
		DNI:           normalizeIdentifier(profile.DNI),
		StreetAddress: normalizeText(profile.StreetAddress),
		StreetNumber:  normalizeIdentifier(profile.StreetNumber),
		PostalCode:    normalizeIdentifier(profile.PostalCode),
		Province:      normalizeText(profile.Province),
		Locality:      normalizeText(profile.Locality),
		PhoneNumber:   normalizePhone(profile.PhoneNumber),
	}
}

func (profile CustomerProfile) Validate() error {
	required := []string{
		profile.FirstName,
		profile.LastName,
		profile.DNI,
		profile.StreetAddress,
		profile.StreetNumber,
		profile.PostalCode,
		profile.Province,
		profile.Locality,
		profile.PhoneNumber,
	}
	for _, value := range required {
		if value == "" {
			return errors.New("missing customer profile field")
		}
	}
	if !validDigits(profile.DNI, 7, 12) {
		return errors.New("invalid dni")
	}
	if !validDigits(profile.PostalCode, 4, 10) {
		return errors.New("invalid postal code")
	}
	if !validPhone(profile.PhoneNumber, 8, 20) {
		return errors.New("invalid phone number")
	}
	return nil
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, ".", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func normalizePhone(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "(", "")
	value = strings.ReplaceAll(value, ")", "")
	return value
}

func validDigits(value string, minLength int, maxLength int) bool {
	if len(value) < minLength || len(value) > maxLength {
		return false
	}
	for _, current := range value {
		if current < '0' || current > '9' {
			return false
		}
	}
	return true
}

func validPhone(value string, minLength int, maxLength int) bool {
	if len(value) < minLength || len(value) > maxLength {
		return false
	}
	start := 0
	if value[0] == '+' {
		start = 1
	}
	if start == len(value) {
		return false
	}
	for _, current := range value[start:] {
		if current < '0' || current > '9' {
			return false
		}
	}
	return true
}
