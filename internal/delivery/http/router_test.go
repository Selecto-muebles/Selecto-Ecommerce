package http

import (
	"testing"

	"Selecto-Ecommerce/internal/config"
)

func TestCorsAllowsInternalHeadersDuringCutover(t *testing.T) {
	headers := corsConfig(&config.Config{AppEnv: "production"}).AllowHeaders
	for _, expected := range []string{"X-Service-Signature", "X-Selecto-Signature", "X-Destry-Signature"} {
		found := false
		for _, header := range headers {
			if header == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("CORS is missing %s", expected)
		}
	}
}
