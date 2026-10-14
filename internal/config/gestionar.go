package config

import (
	"errors"
	"net/url"
	"strings"
)

func (c *Config) GestionarConfigured() bool {
	return c.GestionarAPIBaseURL != "" && c.GestionarSecretToken != ""
}

func (c *Config) validateGestionar() error {
	if c.GestionarAPIBaseURL == "" && c.GestionarSecretToken == "" {
		return nil
	}
	if !c.GestionarConfigured() {
		return errors.New("GESTIONAR_API_BASE_URL and GESTIONAR_SECRET_TOKEN_KEY must be configured together")
	}
	parsed, err := url.Parse(c.GestionarAPIBaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("GESTIONAR_API_BASE_URL must be an HTTPS origin without credentials, path or query")
	}
	if len(c.GestionarSecretToken) < 16 || strings.ContainsAny(c.GestionarSecretToken, "\r\n") {
		return errors.New("GESTIONAR_SECRET_TOKEN_KEY is invalid")
	}
	return nil
}
