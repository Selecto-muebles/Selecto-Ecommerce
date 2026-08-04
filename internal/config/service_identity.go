package config

import (
	"errors"
	"net/url"
	"strings"
)

func validateIDTokenAudience(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("PAYMENTS_ID_TOKEN_AUDIENCE must be an absolute HTTPS origin")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("PAYMENTS_ID_TOKEN_AUDIENCE must not contain a path")
	}
	return nil
}

func validateAudienceTarget(audience, target string) error {
	if audience == "" || target == "" {
		return nil
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("PAYMENTS_SERVICE_URL must be an absolute URL")
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if !strings.EqualFold(audience, origin) {
		return errors.New("PAYMENTS_ID_TOKEN_AUDIENCE must match the payments service origin")
	}
	return nil
}

func isCloudRunURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".run.app")
}
