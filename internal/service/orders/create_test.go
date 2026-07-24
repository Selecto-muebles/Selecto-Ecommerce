package orders

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/shared/validation"
)

type creatorRepositoryStub struct {
	command CreateCommand
	result  CreateResult
	err     error
}

func (stub *creatorRepositoryStub) Create(_ context.Context, command CreateCommand) (CreateResult, error) {
	stub.command = command
	return stub.result, stub.err
}

func TestCreatorRejectsInvalidIdempotencyKeyBeforePersistence(t *testing.T) {
	repository := &creatorRepositoryStub{}
	creator := NewCreator(repository, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := creator.Create(context.Background(), CreateInput{IdempotencyKey: "short", Items: []Item{{ProductID: 1, Quantity: 1}}})
	if !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("expected invalid idempotency key, got %v", err)
	}
	if repository.command.PreparedItems.Grouped != nil {
		t.Fatal("repository was called for invalid input")
	}
}

func TestCreatorNormalizesShippingAndPreparesItems(t *testing.T) {
	repository := &creatorRepositoryStub{result: CreateResult{OrderID: 7, Status: "pending"}}
	creator := NewCreator(repository, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 7, 24, 18, 0, 0, 0, time.UTC)
	result, err := creator.Create(context.Background(), CreateInput{
		Email: "buyer@example.com", IdempotencyKey: "order-key-123", RawRequest: []byte(`{"items":[]}`), Now: now,
		Items: []Item{{ProductID: 2, Quantity: 1}, {ProductID: 2, Quantity: 2}},
		Shipping: &ShippingInput{
			Profile:               validation.CustomerProfile{FirstName: " Ada ", LastName: " Lovelace ", DNI: "44.000.111", StreetAddress: " Main ", StreetNumber: "10", PostalCode: "1674", Province: "Buenos Aires", Locality: "Centro", PhoneNumber: "+54 11 1234 5678"},
			RequestedDeliveryDate: "2026-07-26",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OrderID != 7 || repository.command.PreparedItems.QuantityByProduct[2] != 3 {
		t.Fatalf("unexpected result or command: result=%+v command=%+v", result, repository.command)
	}
	if !repository.command.ShippingProvided || repository.command.ShippingProfile.FirstName != "Ada" || repository.command.RequestedDeliveryDate == nil {
		t.Fatalf("shipping was not normalized: %+v", repository.command)
	}
}
