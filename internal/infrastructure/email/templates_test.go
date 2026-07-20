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
