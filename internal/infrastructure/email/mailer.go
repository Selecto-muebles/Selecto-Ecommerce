package email

import (
	"context"
	"crypto/tls"
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

type SMTPMailer struct{ cfg SMTPConfig }

func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer { return &SMTPMailer{cfg: cfg} }

func (m *SMTPMailer) Send(ctx context.Context, to, subject, htmlBody string) error {
	if m.cfg.Host == "" || m.cfg.From == "" {
		return fmt.Errorf("SMTP is not configured")
	}
	from, err := mail.ParseAddress(m.cfg.From)
	if err != nil {
		return fmt.Errorf("parse SMTP sender: %w", err)
	}
	address := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var connection net.Conn
	if m.cfg.TLSMode == "tls" {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.cfg.Host})
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	defer connection.Close()

	// Prevent SMTP server hangs on subsequent network reads/writes
	connection.SetDeadline(time.Now().Add(30 * time.Second))

	client, err := smtp.NewClient(connection, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()
	if m.cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.cfg.Host}); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP body: %w", err)
	}
	message := strings.Join([]string{"From: " + m.cfg.From, "To: " + to, "Subject: " + subject, "MIME-Version: 1.0", "Content-Type: text/html; charset=UTF-8", "Content-Transfer-Encoding: 8bit", "", htmlBody}, "\r\n")
	if _, err := w.Write([]byte(message)); err != nil {
		_ = w.Close()
		return fmt.Errorf("write SMTP body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close SMTP body: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit SMTP: %w", err)
	}
	return nil
}

func (m *SMTPMailer) SendBatch(ctx context.Context, recipients []string, subjects []string, htmlBodies []string) []error {
	errorsList := make([]error, len(recipients))
	if len(recipients) == 0 {
		return errorsList
	}
	if len(subjects) != len(recipients) || len(htmlBodies) != len(recipients) {
		return fillBatchErrors(errorsList, fmt.Errorf(
			"invalid SMTP batch: recipients=%d subjects=%d bodies=%d",
			len(recipients), len(subjects), len(htmlBodies),
		))
	}
	if m.cfg.Host == "" || m.cfg.From == "" {
		return fillBatchErrors(errorsList, fmt.Errorf("SMTP is not configured"))
	}
	from, err := mail.ParseAddress(m.cfg.From)
	if err != nil {
		err = fmt.Errorf("parse SMTP sender: %w", err)
		return fillBatchErrors(errorsList, err)
	}

	address := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var connection net.Conn
	if m.cfg.TLSMode == "tls" {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.cfg.Host})
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		err = fmt.Errorf("connect SMTP: %w", err)
		return fillBatchErrors(errorsList, err)
	}
	defer connection.Close()

	// Initial connection/handshake deadline
	connection.SetDeadline(time.Now().Add(60 * time.Second))

	client, err := smtp.NewClient(connection, m.cfg.Host)
	if err != nil {
		err = fmt.Errorf("create SMTP client: %w", err)
		return fillBatchErrors(errorsList, err)
	}
	defer client.Close()

	if m.cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			err = fmt.Errorf("SMTP server does not support STARTTLS")
			return fillBatchErrors(errorsList, err)
		}
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.cfg.Host}); err != nil {
			err = fmt.Errorf("start SMTP TLS: %w", err)
			return fillBatchErrors(errorsList, err)
		}
	}
	if m.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			err = fmt.Errorf("authenticate SMTP: %w", err)
			return fillBatchErrors(errorsList, err)
		}
	}

	for i := 0; i < len(recipients); i++ {
		if err := ctx.Err(); err != nil {
			fillBatchErrors(errorsList[i:], err)
			break
		}
		// Set a fresh deadline of 30 seconds for sending each email
		if err := connection.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			fillBatchErrors(errorsList[i:], fmt.Errorf("set SMTP deadline: %w", err))
			break
		}
		err = sendOne(client, from.Address, recipients[i], subjects[i], htmlBodies[i])
		if err != nil {
			errorsList[i] = err
			// Reset the connection state to recover from the error for the next email
			if resetErr := client.Reset(); resetErr != nil {
				fillBatchErrors(errorsList[i+1:], fmt.Errorf("reset SMTP session: %w", resetErr))
				break
			}
		}
	}
	_ = client.Quit()
	return errorsList
}

func fillBatchErrors(target []error, err error) []error {
	for i := range target {
		target[i] = err
	}
	return target
}

func sendOne(client *smtp.Client, from, to, subject, htmlBody string) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP body: %w", err)
	}
	message := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		htmlBody,
	}, "\r\n")
	if _, err := w.Write([]byte(message)); err != nil {
		_ = w.Close()
		return fmt.Errorf("write SMTP body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close SMTP body: %w", err)
	}
	return nil
}
