package handlers

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

const maxDispatchExportLines = 10000

var dispatchExportHeader = []string{
	"orden", "pagada_el", "total_ars", "estado_envio", "destinatario", "dni",
	"telefono", "email", "calle", "numero", "localidad", "provincia", "codigo_postal",
	"fecha_preferida", "sku", "producto", "unidades", "precio_unitario_ars", "revision",
}

// AdminGestionarDispatchExportHandler produces an internal, read-only CSV.
// Downloading it never creates a package, changes shipment state, or contacts Gestionar.
func AdminGestionarDispatchExportHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := db.Pool.Query(c, `
			SELECT o.id, o.paid_at, o.total::text, COALESCE(s.status,'preparing'),
				TRIM(COALESCE(a.recipient_first_name,'') || ' ' || COALESCE(a.recipient_last_name,'')),
				COALESCE(a.dni,''), COALESCE(a.phone_number,''), u.email,
				COALESCE(a.street_address,''), COALESCE(a.street_number,''),
				COALESCE(a.locality,''), COALESCE(a.province,''), COALESCE(a.postal_code,''),
				a.requested_delivery_date, COALESCE(oi.sku_snapshot,''),
				COALESCE(oi.product_name_snapshot,''), COALESCE(oi.quantity,0), COALESCE(oi.price::text,'')
			FROM orders o JOIN users u ON u.id=o.user_id
			LEFT JOIN shipments s ON s.order_id=o.id
			LEFT JOIN order_shipping_addresses a ON a.order_id=o.id
			LEFT JOIN order_items oi ON oi.order_id=o.id
			LEFT JOIN gestionar_dispatches gd ON gd.order_id=o.id
			WHERE o.status='paid' AND (s.id IS NULL OR s.status IN ('preparing','ready_for_dispatch'))
				AND (gd.order_id IS NULL OR gd.package_state='rejected')
			ORDER BY o.paid_at ASC, o.id ASC, oi.id ASC
			LIMIT $1`, maxDispatchExportLines+1)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer rows.Close()

		var output bytes.Buffer
		output.WriteString("\xef\xbb\xbf") // Excel recognizes UTF-8 without losing accents.
		writer := csv.NewWriter(&output)
		if err := writer.Write(dispatchExportHeader); err != nil {
			apperrors.Internal(c)
			return
		}
		count := 0
		for rows.Next() {
			var id, quantity int
			var paidAt sql.NullTime
			var requestedDate sql.NullTime
			var total, shipment, recipient, dni, phone, email, street, number, locality, province, postalCode, sku, product, unitPrice string
			if err := rows.Scan(&id, &paidAt, &total, &shipment, &recipient, &dni, &phone, &email,
				&street, &number, &locality, &province, &postalCode, &requestedDate,
				&sku, &product, &quantity, &unitPrice); err != nil {
				apperrors.Internal(c)
				return
			}
			count++
			if count > maxDispatchExportLines {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "too many dispatch lines; contact support for a filtered export"})
				return
			}
			date := ""
			if requestedDate.Valid {
				date = requestedDate.Time.Format("2006-01-02")
			}
			review := ""
			if !paidAt.Valid || recipient == "" || dni == "" || street == "" || number == "" || locality == "" || province == "" || postalCode == "" || phone == "" || email == "" || sku == "" || product == "" || quantity <= 0 {
				review = "REVISAR DATOS"
			}
			paidDate := ""
			if paidAt.Valid {
				paidDate = paidAt.Time.Format(time.RFC3339)
			}
			record := []string{utils.EncodeID(id), paidDate, total, shipment,
				recipient, dni, phone, email, street, number, locality, province, postalCode,
				date, sku, product, strconv.Itoa(quantity), unitPrice, review}
			for i := range record {
				record[i] = safeDispatchCSVCell(record[i])
			}
			if err := writer.Write(record); err != nil {
				apperrors.Internal(c)
				return
			}
		}
		if rows.Err() != nil {
			apperrors.Internal(c)
			return
		}
		writer.Flush()
		if writer.Error() != nil {
			apperrors.Internal(c)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Disposition", `attachment; filename="selecto-despachos.csv"`)
		c.Data(http.StatusOK, "text/csv; charset=utf-8", output.Bytes())
	}
}

// CSV escaping alone does not stop Excel/Sheets from evaluating a formula.
func safeDispatchCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || (trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + value
	}
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		return "'" + value // Keep leading zeroes in postal codes and phone numbers.
	}
	return value
}
