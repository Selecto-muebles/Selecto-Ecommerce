package config

import (
	"errors"
	"net/url"
	"strings"
)

func (c *Config) validateBrevo() error {
	if c.MarketingWorkerBatchSize < 0 || c.MarketingWorkerBatchSize > 100 || (c.BrevoContactsEnabled && c.MarketingWorkerBatchSize == 0) {
		return errors.New("MARKETING_WORKER_BATCH_SIZE must be between 1 and 100")
	}
	if c.BrevoWebhookToken != "" && len(c.BrevoWebhookToken) < 32 {
		return errors.New("BREVO_WEBHOOK_TOKEN must be at least 32 characters")
	}
	if !c.BrevoContactsEnabled {
		return nil
	}
	if c.BrevoAPIKey == "" || c.BrevoListID <= 0 {
		return errors.New("BREVO_API_KEY and BREVO_LIST_ID are required when contact sync is enabled")
	}
	parsed, err := url.Parse(strings.TrimSpace(c.BrevoAPIBaseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("BREVO_API_BASE_URL must be an HTTPS URL")
	}
	return nil
}
