package config

import (
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// ValidateEmailTaskWorker validates config for the serverless email-outbox worker.
func (c *Config) ValidateEmailTaskWorker() error {
	if err := c.validateDatabase(); err != nil {
		return err
	}
	if err := c.validateSMTP(); err != nil {
		return err
	}
	if c.JobTimeout <= 0 || c.JobTimeout > time.Hour {
		return errors.New("JOB_TIMEOUT must be positive and at most 1h")
	}
	return nil
}

func (c *Config) validateEmailTasks() error {
	if !c.EmailTasksEnabled {
		return nil
	}
	if c.EmailTasksProject == "" || c.EmailTasksLocation == "" || c.EmailTasksQueue == "" {
		return errors.New("EMAIL_TASKS_PROJECT, EMAIL_TASKS_LOCATION and EMAIL_TASKS_QUEUE are required")
	}
	if c.EmailTasksWorkerURL == "" || c.EmailTasksServiceAccount == "" || c.EmailTasksAudience == "" {
		return errors.New("EMAIL_TASKS_WORKER_URL, EMAIL_TASKS_SERVICE_ACCOUNT and EMAIL_TASKS_AUDIENCE are required")
	}
	if c.EmailTasksDispatchTimeout <= 0 || c.EmailTasksDispatchTimeout > 10*time.Second {
		return errors.New("EMAIL_TASKS_DISPATCH_TIMEOUT must be positive and at most 10s")
	}
	if c.AppEnv == "production" {
		if !isHTTPSURL(c.EmailTasksWorkerURL) || !isHTTPSURL(c.EmailTasksAudience) {
			return errors.New("email task worker URL and audience must use HTTPS in production")
		}
	}
	if _, err := mail.ParseAddress(c.EmailTasksServiceAccount); err != nil {
		return errors.New("EMAIL_TASKS_SERVICE_ACCOUNT must be a valid service account email")
	}
	return nil
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}