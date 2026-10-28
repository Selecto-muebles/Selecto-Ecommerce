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
	if !strings.Contains(subject, "Compra confirmada") || !strings.Contains(body, "Gracias por tu compra") || !strings.Contains(body, "Comprobante de pedido") ||
		!strings.Contains(body, "$ 1250.00") || !strings.Contains(body, "Calle 123, CABA") || !strings.Contains(body, "2 × Par de mancuernas") ||
		strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "No válido como factura fiscal") {
		t.Fatalf("paid email must confirm purchase without claiming a fiscal invoice: %s", body)
	}
}

func TestPaidEmailRendersTraceablePurchaseReceipt(t *testing.T) {
	subject, body, err := Render("payment_status", json.RawMessage(`{
		"order_id":"SEL-123", "total":"$ 850.00", "url":"https://selectosport.com/cuenta/ordenes/SEL-123",
		"receipt":{
			"paid_at":"24/10/2026 19:30", "payment_reference":"185961653824",
			"delivery_summary":"Calle de prueba 123, Buenos Aires (1676)",
			"subtotal_cents":100000, "discount_cents":15000, "total_cents":85000, "show_breakdown":true,
			"lines":[{"name":"Producto de prueba", "quantity":10,"unit_price_cents":8500,
				"original_unit_price_cents":10000,"line_total_cents":85000,
				"selected_options":{"color":"Negro","modelo":"<script>"}}]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Compra confirmada", "Gracias por tu compra", "Comprobante de compra", "SEL-123", "185961653824", "24/10/2026 19:30", "10 × $ 85,00", "$ 1.000,00", "− $ 150,00", "$ 850,00", "Calle de prueba 123", "Ver detalle e imprimir comprobante", "No válido como factura fiscal", "modelo: &lt;script&gt;"} {
		if !strings.Contains(subject+body, want) {
			t.Errorf("paid receipt missing %q", want)
		}
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "FACTURA A") {
		t.Fatal("paid receipt rendered unescaped markup or a fiscal invoice claim")
	}
}

func TestPaidEmailWithMalformedNewReceiptFallsBackToQueuedFormat(t *testing.T) {
	_, body, err := Render("payment_status", json.RawMessage(`{"order_id":"OLD-1","total":"$ 10.00","items_summary":"1 × Prueba","receipt":{"total_cents":"broken"},"url":"https://selectosport.com/orden"}`))
	if err != nil || !strings.Contains(body, "1 × Prueba") || !strings.Contains(body, "Comprobante de pedido") {
		t.Fatalf("older queued email should still render: %v %s", err, body)
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
