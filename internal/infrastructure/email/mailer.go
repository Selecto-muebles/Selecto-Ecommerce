package email

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host, Username, Password, From, TLSMode string
	Port                                    int
}

type Mailer interface {
	Send(ctx context.Context, to, subject, htmlBody string) error
	SendBatch(ctx context.Context, recipients []string, subjects []string, htmlBodies []string) []error
}

type TrackedMailer interface {
	SendBatchTracked(ctx context.Context, recipients, subjects, htmlBodies, eventKeys []string) []error
}

type SMTPMailer struct{ cfg SMTPConfig }

func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer { return &SMTPMailer{cfg: cfg} }

func (m *SMTPMailer) Send(ctx context.Context, to, subject, htmlBody string) error {
	results := m.SendBatch(ctx, []string{to}, []string{subject}, []string{htmlBody})
	if len(results) != 1 {
		return fmt.Errorf("mailer returned an invalid result count")
	}
	return results[0]
}

func (m *SMTPMailer) SendBatch(ctx context.Context, recipients, subjects, htmlBodies []string) []error {
	return m.SendBatchTracked(ctx, recipients, subjects, htmlBodies, make([]string, len(recipients)))
}

func (m *SMTPMailer) SendBatchTracked(ctx context.Context, recipients, subjects, htmlBodies, eventKeys []string) []error {
	errorsList := make([]error, len(recipients))
	if len(recipients) == 0 {
		return errorsList
	}
	if len(subjects) != len(recipients) || len(htmlBodies) != len(recipients) || len(eventKeys) != len(recipients) {
		return fillBatchErrors(errorsList, fmt.Errorf(
			"invalid SMTP batch: recipients=%d subjects=%d bodies=%d event_keys=%d",
			len(recipients), len(subjects), len(htmlBodies), len(eventKeys),
		))
	}
	from, err := m.validateSender()
	if err != nil {
		return fillBatchErrors(errorsList, err)
	}
	connection, client, err := m.connect(ctx)
	if err != nil {
		return fillBatchErrors(errorsList, err)
	}
	defer connection.Close()
	defer client.Close()

	for i := range recipients {
		if err := ctx.Err(); err != nil {
			fillBatchErrors(errorsList[i:], err)
			break
		}
		if err := connection.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			fillBatchErrors(errorsList[i:], fmt.Errorf("set SMTP deadline: %w", err))
			break
		}
		err = sendOne(client, from.Address, m.cfg.From, recipients[i], subjects[i], htmlBodies[i], eventKeys[i])
		if err == nil {
			continue
		}
		errorsList[i] = err
		if resetErr := client.Reset(); resetErr != nil {
			fillBatchErrors(errorsList[i+1:], fmt.Errorf("reset SMTP session: %w", resetErr))
			break
		}
	}
	_ = client.Quit()
	return errorsList
}

func (m *SMTPMailer) validateSender() (*mail.Address, error) {
	if m.cfg.Host == "" || m.cfg.From == "" {
		return nil, fmt.Errorf("SMTP is not configured")
	}
	from, err := mail.ParseAddress(m.cfg.From)
	if err != nil {
		return nil, fmt.Errorf("parse SMTP sender: %w", err)
	}
	return from, nil
}

func (m *SMTPMailer) connect(ctx context.Context) (net.Conn, *smtp.Client, error) {
	address := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var connection net.Conn
	var err error
	if m.cfg.TLSMode == "tls" {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, m.tlsConfig())
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connect SMTP: %w", err)
	}
	_ = connection.SetDeadline(time.Now().Add(60 * time.Second))
	client, err := smtp.NewClient(connection, m.cfg.Host)
	if err != nil {
		connection.Close()
		return nil, nil, fmt.Errorf("create SMTP client: %w", err)
	}
	if err := m.secureAndAuthenticate(client); err != nil {
		client.Close()
		connection.Close()
		return nil, nil, err
	}
	return connection, client, nil
}

func (m *SMTPMailer) secureAndAuthenticate(client *smtp.Client) error {
	if m.cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(m.tlsConfig()); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	return nil
}

func (m *SMTPMailer) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.cfg.Host}
}

func fillBatchErrors(target []error, err error) []error {
	for i := range target {
		target[i] = err
	}
	return target
}

func sendOne(client *smtp.Client, envelopeFrom, headerFrom, to, subject, htmlBody, eventKey string) error {
	if strings.ContainsAny(to+subject+headerFrom, "\r\n") {
		return fmt.Errorf("email headers contain invalid characters")
	}
	if err := client.Mail(envelopeFrom); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP body: %w", err)
	}
	headers := []string{
		"From: " + headerFrom, "To: " + to, "Subject: " + subject,
		"MIME-Version: 1.0", "Content-Type: text/html; charset=UTF-8", "Content-Transfer-Encoding: 8bit",
	}
	if eventKey != "" {
		custom, _ := json.Marshal(map[string]string{"event_key": eventKey})
		headers = append(headers, "X-Mailin-custom: "+string(custom))
	}
	message := strings.Join(append(headers, "", htmlBody), "\r\n")
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close SMTP body: %w", err)
	}
	return nil
}
