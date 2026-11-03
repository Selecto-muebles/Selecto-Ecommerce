package logistics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const batchLockKey int64 = 731402027
const maxBatchLines = 10000

var ErrNotFound = errors.New("logistics batch not found")
var ErrInvalidHandoff = errors.New("invalid logistics handoff")

var csvHeader = []string{"orden", "pagada_el", "total_ars", "estado_envio", "destinatario", "dni", "telefono", "email", "calle", "numero", "localidad", "provincia", "codigo_postal", "fecha_preferida", "sku", "producto", "unidades", "precio_unitario_ars", "revision"}

type Batch struct {
	ID                   int64      `json:"id"`
	CutoffAt             time.Time  `json:"cutoff_at"`
	OrderCount           int        `json:"order_count"`
	LineCount            int        `json:"line_count"`
	ExcludedOrderCount   int        `json:"excluded_order_count"`
	LegacyReviewRequired bool       `json:"legacy_review_required"`
	State                string     `json:"state"`
	PreparedAt           time.Time  `json:"prepared_at"`
	HandedOffAt          *time.Time `json:"handed_off_at"`
	HandedOffBy          *string    `json:"handed_off_by"`
	HandoffChannel       *string    `json:"handoff_channel"`
	HandoffRecipient     *string    `json:"handoff_recipient"`
	CSVHash              string     `json:"csv_sha256"`
}

type Store struct{ Pool *pgxpool.Pool }

// PrepareDue is safe across Cloud Run instances: the advisory transaction lock
// and unique order membership prevent duplicate batches and repeated exports.
func (s Store) PrepareDue(ctx context.Context, now time.Time) ([]Batch, error) {
	due := LatestDueCutoff(now)
	var last *time.Time
	var previous time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT cutoff_at FROM logistics_cutoff_batches ORDER BY cutoff_at DESC LIMIT 1`).Scan(&previous); err == nil {
		last = &previous
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	next := due
	if last != nil {
		next = NextCutoff(*last)
	}
	created := make([]Batch, 0)
	for !next.After(due) {
		batch, inserted, err := s.prepareOne(ctx, next)
		if err != nil {
			return created, err
		}
		if inserted {
			created = append(created, batch)
		}
		next = NextCutoff(next)
	}
	return created, nil
}

type dispatchLine struct {
	orderID int
	values  []string
	valid   bool
}

func (s Store) prepareOne(ctx context.Context, cutoff time.Time) (Batch, bool, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Batch{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, batchLockKey); err != nil {
		return Batch{}, false, err
	}
	var existing int64
	if err = tx.QueryRow(ctx, `SELECT id FROM logistics_cutoff_batches WHERE cutoff_at=$1`, cutoff).Scan(&existing); err == nil {
		return Batch{}, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, false, err
	}
	rows, err := tx.Query(ctx, `
		SELECT o.id,o.paid_at,o.total::text,COALESCE(s.status,'preparing'),
			TRIM(COALESCE(a.recipient_first_name,'') || ' ' || COALESCE(a.recipient_last_name,'')),
			COALESCE(a.dni,''),COALESCE(a.phone_number,''),u.email,
			COALESCE(a.street_address,''),COALESCE(a.street_number,''),COALESCE(a.locality,''),
			COALESCE(a.province,''),COALESCE(a.postal_code,''),a.requested_delivery_date,
			COALESCE(oi.sku_snapshot,''),COALESCE(oi.product_name_snapshot,''),
			COALESCE(oi.quantity,0),COALESCE(oi.price::text,'')
		FROM orders o JOIN users u ON u.id=o.user_id
		LEFT JOIN shipments s ON s.order_id=o.id
		LEFT JOIN order_shipping_addresses a ON a.order_id=o.id
		LEFT JOIN order_items oi ON oi.order_id=o.id
		LEFT JOIN gestionar_dispatches gd ON gd.order_id=o.id
		WHERE o.status='paid' AND o.paid_at <= $1
			AND (s.id IS NULL OR s.status IN ('preparing','ready_for_dispatch'))
			AND (gd.order_id IS NULL OR gd.package_state='rejected')
			AND NOT EXISTS (SELECT 1 FROM logistics_cutoff_batch_orders bo WHERE bo.order_id=o.id)
		ORDER BY o.paid_at ASC,o.id ASC,oi.id ASC LIMIT $2`, cutoff, maxBatchLines+1)
	if err != nil {
		return Batch{}, false, err
	}
	lines := make([]dispatchLine, 0)
	for rows.Next() {
		var id, quantity int
		var paidAt, preferredDate *time.Time
		var total, shipment, recipient, dni, phone, email, street, number, locality, province, postal, sku, product, unitPrice string
		if err = rows.Scan(&id, &paidAt, &total, &shipment, &recipient, &dni, &phone, &email, &street, &number, &locality, &province, &postal, &preferredDate, &sku, &product, &quantity, &unitPrice); err != nil {
			rows.Close()
			return Batch{}, false, err
		}
		paid, preferred := "", ""
		if paidAt != nil {
			paid = paidAt.Format(time.RFC3339)
		}
		if preferredDate != nil {
			preferred = preferredDate.Format("2006-01-02")
		}
		valid := paid != "" && recipient != "" && dni != "" && phone != "" && email != "" && street != "" && number != "" && locality != "" && province != "" && postal != "" && sku != "" && product != "" && quantity > 0
		lines = append(lines, dispatchLine{orderID: id, valid: valid, values: []string{utils.EncodeID(id), paid, total, shipment, recipient, dni, phone, email, street, number, locality, province, postal, preferred, sku, product, fmt.Sprint(quantity), unitPrice, ""}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Batch{}, false, err
	}
	if len(lines) > maxBatchLines {
		return Batch{}, false, fmt.Errorf("more than %d dispatch lines; no partial batch created", maxBatchLines)
	}
	invalid := make(map[int]bool)
	for _, line := range lines {
		if !line.valid {
			invalid[line.orderID] = true
		}
	}
	var output bytes.Buffer
	output.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&output)
	if err = w.Write(csvHeader); err != nil {
		return Batch{}, false, err
	}
	orderIDs := make(map[int]bool)
	lineCount := 0
	for _, line := range lines {
		if invalid[line.orderID] {
			continue
		}
		for i := range line.values {
			line.values[i] = SafeCSVCell(line.values[i])
		}
		if err = w.Write(line.values); err != nil {
			return Batch{}, false, err
		}
		orderIDs[line.orderID] = true
		lineCount++
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return Batch{}, false, err
	}
	hash := sha256.Sum256(output.Bytes())
	var batch Batch
	var firstBatch bool
	if err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM logistics_cutoff_batches WHERE order_count > 0)`).Scan(&firstBatch); err != nil {
		return Batch{}, false, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO logistics_cutoff_batches(cutoff_at,csv_content,csv_sha256,order_count,line_count,excluded_order_count,legacy_review_required)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,cutoff_at,order_count,line_count,excluded_order_count,legacy_review_required,state,prepared_at,csv_sha256`,
		cutoff, output.Bytes(), hex.EncodeToString(hash[:]), len(orderIDs), lineCount, len(invalid), firstBatch && len(orderIDs) > 0).Scan(&batch.ID, &batch.CutoffAt, &batch.OrderCount, &batch.LineCount, &batch.ExcludedOrderCount, &batch.LegacyReviewRequired, &batch.State, &batch.PreparedAt, &batch.CSVHash)
	if err != nil {
		return Batch{}, false, err
	}
	for id := range orderIDs {
		if _, err = tx.Exec(ctx, `INSERT INTO logistics_cutoff_batch_orders(batch_id,order_id) VALUES($1,$2)`, batch.ID, id); err != nil {
			return Batch{}, false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Batch{}, false, err
	}
	return batch, true, nil
}

func (s Store) List(ctx context.Context) ([]Batch, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,cutoff_at,order_count,line_count,excluded_order_count,legacy_review_required,state,prepared_at,handed_off_at,handed_off_by,handoff_channel,handoff_recipient,csv_sha256 FROM logistics_cutoff_batches ORDER BY cutoff_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Batch, 0)
	for rows.Next() {
		var b Batch
		if err = rows.Scan(&b.ID, &b.CutoffAt, &b.OrderCount, &b.LineCount, &b.ExcludedOrderCount, &b.LegacyReviewRequired, &b.State, &b.PreparedAt, &b.HandedOffAt, &b.HandedOffBy, &b.HandoffChannel, &b.HandoffRecipient, &b.CSVHash); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (s Store) CSV(ctx context.Context, id int64) ([]byte, string, error) {
	var data []byte
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT csv_content,csv_sha256 FROM logistics_cutoff_batches WHERE id=$1`, id).Scan(&data, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, hash, err
}

func (s Store) ConfirmHandoff(ctx context.Context, id int64, actor, channel, recipient, note string) error {
	actor, channel, recipient, note = strings.TrimSpace(actor), strings.TrimSpace(channel), strings.TrimSpace(recipient), strings.TrimSpace(note)
	if actor == "" || recipient == "" || len(recipient) > 200 || len(note) > 1000 || (channel != "email" && channel != "whatsapp" && channel != "otro") {
		return ErrInvalidHandoff
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	if err = tx.QueryRow(ctx, `SELECT state FROM logistics_cutoff_batches WHERE id=$1 FOR UPDATE`, id).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if state == "handed_off" {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE logistics_cutoff_batches SET state='handed_off',handed_off_at=NOW(),handed_off_by=$2,handoff_channel=$3,handoff_recipient=$4,handoff_note=$5 WHERE id=$1`, id, actor, channel, recipient, note); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs(actor_email,action,entity_type,entity_id,metadata) VALUES($1,'logistics_batch_handed_off','logistics_batch',$2,jsonb_build_object('channel',$3::text,'recipient',$4::text))`, actor, id, channel, recipient); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func SafeCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || (trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + value
	}
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		return "'" + value
	}
	return value
}
