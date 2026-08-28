package email

import (
	"context"
	"strings"
	"testing"
)

func TestSendBatchRejectsMismatchedInputWithoutPanicking(t *testing.T) {
	mailer := NewSMTPMailer(SMTPConfig{Host: "smtp.example.test", From: "sender@example.test"})

	errors := mailer.SendBatch(
		context.Background(),
		[]string{"one@example.test", "two@example.test"},
		[]string{"only one subject"},
		[]string{"first body", "second body"},
	)

	if len(errors) != 2 {
		t.Fatalf("expected one result per recipient, got %d", len(errors))
	}
	for i, err := range errors {
		if err == nil || !strings.Contains(err.Error(), "invalid SMTP batch") {
			t.Fatalf("result %d should report an invalid batch, got %v", i, err)
		}
	}
}

func TestSendBatchReturnsOneConfigurationErrorPerRecipient(t *testing.T) {
	mailer := NewSMTPMailer(SMTPConfig{})

	errors := mailer.SendBatch(
		context.Background(),
		[]string{"one@example.test", "two@example.test"},
		[]string{"first", "second"},
		[]string{"first body", "second body"},
	)

	for i, err := range errors {
		if err == nil || err.Error() != "SMTP is not configured" {
			t.Fatalf("result %d should report missing configuration, got %v", i, err)
		}
	}
}

func TestFillBatchErrorsPreservesResultCardinality(t *testing.T) {
	errors := make([]error, 3)
	want := context.Canceled

	got := fillBatchErrors(errors, want)

	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	for i, err := range got {
		if err != want {
			t.Fatalf("result %d: expected %v, got %v", i, want, err)
		}
	}
}
