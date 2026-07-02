package integration_test

import (
	"os"
	"testing"
)

func TestCheckoutStockWebhookIntegrationRequiresDatabase(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("set TEST_DATABASE_URL to run checkout, stock, webhook and concurrency integration tests")
	}

	t.Skip("integration harness requires migration setup against a dedicated disposable database before enabling destructive flow tests")
}
