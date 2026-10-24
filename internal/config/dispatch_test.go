package config

import "testing"

func TestValidateDispatch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hour  int
		phone string
		valid bool
	}{
		{name: "default", hour: 13, valid: true},
		{name: "international digits", hour: 13, phone: "5491112345678", valid: true},
		{name: "too late", hour: 24},
		{name: "negative", hour: -1},
		{name: "plus prefix", hour: 13, phone: "+5491112345678"},
		{name: "non digits", hour: 13, phone: "54911-12345678"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{DispatchCutoffHour: tc.hour, GestionarWhatsAppRecipient: tc.phone}
			if got := cfg.validateDispatch(); (got == nil) != tc.valid {
				t.Fatalf("validateDispatch() error = %v, want valid %v", got, tc.valid)
			}
		})
	}
}
