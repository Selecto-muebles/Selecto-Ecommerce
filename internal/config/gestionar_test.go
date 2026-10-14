package config

import "testing"

func TestGestionarIsOptionalAndRequiresHTTPSOrigin(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  Config
		want bool
	}{
		{"disabled", Config{}, true},
		{"configured", Config{GestionarAPIBaseURL: "https://sandbox.example.test", GestionarSecretToken: "sandbox-test-token-123"}, true},
		{"missing token", Config{GestionarAPIBaseURL: "https://sandbox.example.test"}, false},
		{"insecure URL", Config{GestionarAPIBaseURL: "http://localhost:8000", GestionarSecretToken: "sandbox-test-token-123"}, false},
		{"embedded credentials", Config{GestionarAPIBaseURL: "https://user:pass@sandbox.example.test", GestionarSecretToken: "sandbox-test-token-123"}, false},
		{"unexpected path", Config{GestionarAPIBaseURL: "https://sandbox.example.test/api", GestionarSecretToken: "sandbox-test-token-123"}, false},
		{"header injection", Config{GestionarAPIBaseURL: "https://sandbox.example.test", GestionarSecretToken: "sandbox-test-token-123\r\nX-Other: yes"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.cfg.validateGestionar() == nil; got != test.want {
				t.Fatalf("validation success = %t, want %t", got, test.want)
			}
		})
	}
}
