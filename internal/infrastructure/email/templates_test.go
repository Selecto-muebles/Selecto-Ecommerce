package email

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderEscapesPayload(t *testing.T) {
	_, body, err := Render("order_created", json.RawMessage(`{"order_id":"<script>","total":"$ 10","url":"https://shop.example/order"}`))
	if err != nil { t.Fatal(err) }
	if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") { t.Fatal("email template did not escape customer-controlled data") }
}
