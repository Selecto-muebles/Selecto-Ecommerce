package email

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
)

// PurchaseReceipt is a snapshot for the paid email. It is informational: the
// fiscal invoice is a separate process and this message never claims to be one.
type PurchaseReceipt struct {
	PaidAt           string                `json:"paid_at"`
	PaymentReference string                `json:"payment_reference"`
	DeliverySummary  string                `json:"delivery_summary"`
	Lines            []PurchaseReceiptLine `json:"lines"`
	SubtotalCents    int64                 `json:"subtotal_cents"`
	DiscountCents    int64                 `json:"discount_cents"`
	TotalCents       int64                 `json:"total_cents"`
	ShowBreakdown    bool                  `json:"show_breakdown"`
}

type PurchaseReceiptLine struct {
	Name                   string         `json:"name"`
	Quantity               int            `json:"quantity"`
	UnitPriceCents         int64          `json:"unit_price_cents"`
	OriginalUnitPriceCents int64          `json:"original_unit_price_cents"`
	LineTotalCents         int64          `json:"line_total_cents"`
	SelectedOptions        map[string]any `json:"selected_options"`
}

func renderPurchaseReceipt(orderID, orderURL string, receipt PurchaseReceipt) string {
	var body strings.Builder
	body.WriteString(`<section style="border:1px solid #d7d7d7;border-top:4px solid #171717;border-radius:8px;padding:22px;margin:24px 0;background:#fff">`)
	body.WriteString(`<p style="margin:0 0 8px;color:#775234;font-size:11px;font-weight:bold;letter-spacing:2px;text-transform:uppercase">Comprobante de compra · Selecto</p>`)
	body.WriteString(`<h3 style="margin:0 0 18px;font-size:20px;color:#171717">Orden ` + orderID + `</h3>`)
	body.WriteString(`<table role="presentation" style="width:100%;border-collapse:collapse;margin:0 0 20px;font-size:13px">`)
	if receipt.PaidAt != "" {
		body.WriteString(receiptMetaRow("Pago confirmado", html.EscapeString(receipt.PaidAt)+" h (Argentina)"))
	}
	if receipt.PaymentReference != "" {
		body.WriteString(receiptMetaRow("Referencia de pago", html.EscapeString(receipt.PaymentReference)))
	}
	body.WriteString(`</table>`)
	if len(receipt.Lines) > 0 {
		body.WriteString(`<p style="margin:0 0 10px;font-size:12px;font-weight:bold;text-transform:uppercase;letter-spacing:1px">Productos</p>`)
		body.WriteString(`<table role="presentation" style="width:100%;border-collapse:collapse;font-size:14px">`)
		for _, item := range receipt.Lines {
			if item.Quantity <= 0 {
				continue
			}
			name := item.Name
			if name == "" {
				name = "Producto"
			}
			body.WriteString(`<tr><td style="padding:12px 8px 12px 0;border-top:1px solid #e5e5e5;vertical-align:top"><strong>` + html.EscapeString(name) + `</strong><br><span style="color:#666">` + strconv.Itoa(item.Quantity) + ` × ` + formatARS(item.UnitPriceCents) + `</span>`)
			if options := receiptOptions(item.SelectedOptions); options != "" {
				body.WriteString(`<br><span style="color:#666">` + options + `</span>`)
			}
			body.WriteString(`</td><td style="padding:12px 0;border-top:1px solid #e5e5e5;text-align:right;white-space:nowrap;vertical-align:top"><strong>` + formatARS(item.LineTotalCents) + `</strong></td></tr>`)
		}
		body.WriteString(`</table>`)
	}
	body.WriteString(`<table role="presentation" style="width:100%;border-collapse:collapse;margin:14px 0 0;font-size:14px">`)
	if receipt.ShowBreakdown {
		body.WriteString(receiptAmountRow("Subtotal", formatARS(receipt.SubtotalCents), false))
		if receipt.DiscountCents > 0 {
			body.WriteString(receiptAmountRow("Descuento", "− "+formatARS(receipt.DiscountCents), false))
		}
	}
	body.WriteString(receiptAmountRow("Total pagado", formatARS(receipt.TotalCents), true) + `</table>`)
	if receipt.DeliverySummary != "" {
		body.WriteString(`<p style="margin:18px 0 0;font-size:13px"><strong>Entrega coordinada</strong><br>` + html.EscapeString(receipt.DeliverySummary) + `</p>`)
	}
	body.WriteString(`<p style="margin:20px 0 0;padding-top:14px;border-top:1px solid #e5e5e5;font-size:12px;color:#666">Documento informativo para identificar esta compra en Selecto. No válido como factura fiscal. El costo de envío se coordina por separado.</p>`)
	body.WriteString(`</section><p style="margin:20px 0"><a style="display:inline-block;background:#171717;color:#fff;padding:13px 18px;text-decoration:none;border-radius:4px;font-weight:bold" href="` + orderURL + `">Ver detalle e imprimir comprobante</a></p>`)
	return body.String()
}

func receiptMetaRow(label, value string) string {
	return `<tr><td style="padding:4px 12px 4px 0;color:#666;vertical-align:top">` + label + `</td><td style="padding:4px 0;overflow-wrap:anywhere">` + value + `</td></tr>`
}

func receiptAmountRow(label, amount string, strong bool) string {
	style := `padding:7px 0;border-top:1px solid #e5e5e5`
	if strong {
		style += `;font-size:18px;font-weight:bold;color:#171717`
	}
	return `<tr><td style="` + style + `">` + label + `</td><td style="` + style + `;text-align:right;white-space:nowrap">` + amount + `</td></tr>`
}

func receiptOptions(options map[string]any) string {
	if len(options) == 0 {
		return ""
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		switch value := options[key].(type) {
		case string:
			parts = append(parts, html.EscapeString(key)+": "+html.EscapeString(value))
		case float64, bool:
			parts = append(parts, html.EscapeString(key)+": "+html.EscapeString(fmt.Sprint(value)))
		}
	}
	return strings.Join(parts, " · ")
}

func formatARS(cents int64) string {
	negative := cents < 0
	if negative {
		cents = -cents
	}
	digits := strconv.FormatInt(cents/100, 10)
	groups := ""
	for len(digits) > 3 {
		groups = "." + digits[len(digits)-3:] + groups
		digits = digits[:len(digits)-3]
	}
	prefix := "$ "
	if negative {
		prefix = "− $ "
	}
	return prefix + digits + groups + fmt.Sprintf(",%02d", cents%100)
}
