package email

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderEscapesPayload(t *testing.T) {
	_, body, err := Render("order_created", json.RawMessage(`{"order_id":"<script>","total":"$ 10","url":"https://shop.example/order"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("email template did not escape customer-controlled data")
	}
}

func TestOrderCreatedIsClearlyAnInformationalReceipt(t *testing.T) {
	_, body, err := Render("order_created", json.RawMessage(`{"order_id":"ABC123","total":"$ 10","url":"https://shop.example/order"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "Constancia de orden") || !strings.Contains(body, "No válido como factura fiscal") {
		t.Fatal("order email must not be presented as a fiscal invoice")
	}
}

func TestWithdrawalEmailsProvideReceiptAndEscapeCustomerData(t *testing.T) {
	_, acknowledgment, err := Render("withdrawal_ack", json.RawMessage(`{"public_code":"SEL-123","order_reference":"<img>","url":"https://shop.example/boton-de-arrepentimiento"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(acknowledgment, "SEL-123") || strings.Contains(acknowledgment, "<img>") || !strings.Contains(acknowledgment, "&lt;img&gt;") {
		t.Fatal("withdrawal acknowledgment must include its code and escape user-provided order data")
	}
	_, update, err := Render("withdrawal_update", json.RawMessage(`{"public_code":"SEL-123","status_label":"en revisión"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(update, "SEL-123") || !strings.Contains(update, "en revisión") {
		t.Fatal("withdrawal status email must identify the request and its current state")
	}
}
