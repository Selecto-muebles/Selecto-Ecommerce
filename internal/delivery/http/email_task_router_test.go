package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"Selecto-Ecommerce/internal/config"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
)

type fakeEmailProcessor struct {
	id  int64
	err error
}

func (processor *fakeEmailProcessor) ProcessOne(_ context.Context, id int64) error {
	processor.id = id
	return processor.err
}

func TestEmailTaskRouterRejectsRequestsWithoutCloudTasksHeaders(t *testing.T) {
	processor := &fakeEmailProcessor{}
	response := performEmailTaskRequest(t, processor, `{"outbox_id":42}`, false)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if processor.id != 0 {
		t.Fatalf("processor called with id %d", processor.id)
	}
}

func TestEmailTaskRouterProcessesExactOutboxID(t *testing.T) {
	processor := &fakeEmailProcessor{}
	response := performEmailTaskRequest(t, processor, `{"outbox_id":42}`, true)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if processor.id != 42 {
		t.Fatalf("processor id = %d, want 42", processor.id)
	}
}

func TestEmailTaskRouterReportsRetryableAndPermanentErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		err    error
		status int
	}{
		{name: "invalid payload", body: `{"outbox_id":0}`, status: http.StatusBadRequest},
		{name: "not ready", body: `{"outbox_id":42}`, err: mailinfra.ErrEmailNotReady, status: http.StatusServiceUnavailable},
		{name: "processing error", body: `{"outbox_id":42}`, err: errors.New("smtp unavailable"), status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &fakeEmailProcessor{err: test.err}
			response := performEmailTaskRequest(t, processor, test.body, true)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func performEmailTaskRequest(
	t *testing.T,
	processor emailOutboxProcessor,
	body string,
	withTaskHeaders bool,
) *httptest.ResponseRecorder {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := SetupEmailTaskRouter(nil, &config.Config{AppEnv: "testing"}, logger, processor)
	request := httptest.NewRequest(http.MethodPost, "/internal/tasks/email-outbox", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if withTaskHeaders {
		request.Header.Set("X-CloudTasks-TaskName", "email-42")
		request.Header.Set("X-CloudTasks-QueueName", "email-outbox")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
