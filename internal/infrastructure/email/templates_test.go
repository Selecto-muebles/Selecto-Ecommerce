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

func TestPaidEmailConfirmsPurchaseWithoutClaimingFiscalInvoice(t *testing.T) {
	subject, body, err := Render("payment_status", json.RawMessage(`{"order_id":"ABC123","total":"$ 1250.00","items_summary":"2 × Par de mancuernas\n1 × <script>","delivery_summary":"Calle 123, CABA","url":"https://shop.example/order"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(subject, "Compra confirmada") || !strings.Contains(body, "Comprobante de pedido") ||
		!strings.Contains(body, "$ 1250.00") || !strings.Contains(body, "Calle 123, CABA") || !strings.Contains(body, "2 × Par de mancuernas") ||
		strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "No válido como factura fiscal") {
		t.Fatalf("paid email must confirm purchase without claiming a fiscal invoice: %s", body)
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
