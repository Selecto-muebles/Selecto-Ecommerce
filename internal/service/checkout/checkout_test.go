package checkout

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"Selecto-Ecommerce/internal/shared/money"
)

type checkoutRepositoryStub struct {
	order      Order
	loadErr    error
	saved      bool
	existing   *Preference
	auditCalls int
}

func (stub *checkoutRepositoryStub) LoadAvailable(context.Context, int, string) (Order, error) {
	return stub.order, stub.loadErr
}

func (stub *checkoutRepositoryStub) SavePreference(context.Context, int, Preference) (bool, error) {
	return stub.saved, nil
}

func (stub *checkoutRepositoryStub) FindPendingPreference(context.Context, int) (*Preference, error) {
	return stub.existing, nil
}

func (stub *checkoutRepositoryStub) WriteAudit(context.Context, int, string, string) error {
	stub.auditCalls++
	return nil
}

type checkoutGatewayStub struct {
	preference Preference
	calls      int
}

func (stub *checkoutGatewayStub) CreatePreference(context.Context, int, money.Cents, Customer, string, string) (Preference, error) {
	stub.calls++
	return stub.preference, nil
}

func checkoutTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStartReusesExistingPreferenceWithoutGatewayCall(t *testing.T) {
	preference := &Preference{ID: "pref-1", CheckoutURL: "https://checkout.example/1"}
	repository := &checkoutRepositoryStub{order: Order{ID: 9, Status: "pending", Preference: preference}}
	gateway := &checkoutGatewayStub{}
	result, err := NewService(repository, gateway, checkoutTestLogger()).Start(context.Background(), Input{OrderID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reused || result.Preference.ID != "pref-1" || gateway.calls != 0 {
		t.Fatalf("unexpected checkout result: %+v gateway_calls=%d", result, gateway.calls)
	}
}

func TestStartRejectsNonPendingOrder(t *testing.T) {
	repository := &checkoutRepositoryStub{order: Order{ID: 9, Status: "paid"}}
	_, err := NewService(repository, &checkoutGatewayStub{}, checkoutTestLogger()).Start(context.Background(), Input{OrderID: 9})
	var notPending OrderNotPendingError
	if !errors.As(err, &notPending) || notPending.Status != "paid" {
		t.Fatalf("expected paid order rejection, got %v", err)
	}
}

func TestStartPersistsNewPreferenceAndAudit(t *testing.T) {
	repository := &checkoutRepositoryStub{order: Order{ID: 9, Status: "pending", Total: 150000}, saved: true}
	gateway := &checkoutGatewayStub{preference: Preference{ID: "pref-2", CheckoutURL: "https://checkout.example/2"}}
	result, err := NewService(repository, gateway, checkoutTestLogger()).Start(context.Background(), Input{OrderID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reused || gateway.calls != 1 || repository.auditCalls != 1 {
		t.Fatalf("unexpected checkout execution: result=%+v gateway_calls=%d audits=%d", result, gateway.calls, repository.auditCalls)
	}
}
