package email

import (
	"encoding/json"
	"fmt"
	"html"
)

func Render(template string, raw json.RawMessage) (string, string, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", "", err
	}
	value := func(key string) string { return html.EscapeString(fmt.Sprint(payload[key])) }
	button := func(label, url string) string {
		return `<p><a style="display:inline-block;background:#111;color:#fff;padding:12px 18px;text-decoration:none" href="` + url + `">` + label + `</a></p>`
	}
	wrapper := func(content string) string {
		return `<!doctype html><html><body style="font-family:Arial,sans-serif;color:#171717;line-height:1.5"><main style="max-width:640px;margin:auto;padding:24px"><h1 style="font-size:24px">Selecto</h1>` + content + `<hr><p style="font-size:12px;color:#666">Este mensaje fue generado automÃ¡ticamente. No compartas enlaces de seguridad.</p></main></body></html>`
	}
	switch template {
	case "verify_email":
		return "VerificÃ¡ tu cuenta Selecto", wrapper(`<h2>ConfirmÃ¡ tu email</h2><p>UsÃ¡ este enlace para activar tu cuenta.</p>` + button("Verificar email", value("url"))), nil
	case "password_reset":
		return "RestablecÃ© tu contraseÃ±a Selecto", wrapper(`<h2>Restablecer contraseÃ±a</h2><p>El enlace vence en 30 minutos. Si no lo pediste, ignorÃ¡ este mensaje.</p>` + button("Crear nueva contraseÃ±a", value("url"))), nil
	case "order_created":
		content := `<h2>Orden ` + value("order_id") + ` creada</h2><p>Reservamos tus productos mientras completÃ¡s el pago.</p>`
		content += `<section style="border:1px solid #bbb;padding:16px;margin:20px 0"><p style="margin:0 0 8px;font-size:12px;text-transform:uppercase">Constancia de orden</p><p style="margin:0"><strong>Total de productos: ` + value("total") + `</strong></p><p style="margin:8px 0 0;font-size:12px;color:#666">Documento informativo. No vÃ¡lido como factura fiscal.</p></section>`
		return "Recibimos tu orden " + value("order_id"), wrapper(content + button("Ver mi orden", value("url"))), nil
	case "payment_status":
		return "ActualizaciÃ³n de pago de la orden " + value("order_id"), wrapper(`<h2>Estado del pago: ` + value("status_label") + `</h2><p>Orden ` + value("order_id") + `.</p>` + button("Consultar orden", value("url"))), nil
	case "shipment_status":
		content := `<h2>Tu entrega estÃ¡ ` + value("status_label") + `</h2><p>Orden ` + value("order_id") + `.</p>`
		if payload["tracking_url"] != nil && fmt.Sprint(payload["tracking_url"]) != "" {
			content += button("Seguir envÃ­o", value("tracking_url"))
		}
		content += button("Ver detalle", value("url"))
		return "ActualizaciÃ³n de entrega de la orden " + value("order_id"), wrapper(content), nil
	default:
		return "", "", fmt.Errorf("unknown email template %q", template)
	}
}
