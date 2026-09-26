package email

import (
	"Selecto-Ecommerce/internal/infrastructure/database"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOrderPaymentShipmentAndUnsubscribeSendWithTracking(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	messages := make(chan string, 4)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			serveTrackedSMTP(t, conn, pool, messages)
		}
	}()
	defer func() { _ = listener.Close(); wg.Wait() }()
	host, portString, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	worker := NewWorker(&database.DB{Pool: pool}, NewSMTPMailer(SMTPConfig{Host: host, Port: port, From: "Selecto <no-reply@selectosport.com>", TLSMode: "none"}), slog.New(slog.NewTextHandler(io.Discard, nil)), 0, 10)
	suffix := time.Now().UnixNano()
	ids := []int64{}
	defer func() {
		for _, id := range ids {
			_, _ = pool.Exec(context.Background(), "DELETE FROM email_outbox WHERE id=$1", id)
		}
	}()
	for _, template := range []string{"order_created", "payment_status", "shipment_status", "newsletter_unsubscribe"} {
		key := fmt.Sprintf("smtp-certification:%d:%s", suffix, template)
		var id int64
		raw := json.RawMessage(`{"order_id":"SEL-TEST","total":"$ 100","status_label":"confirmado","url":"https://selectosport.com/cuenta/ordenes/SEL-TEST","tracking_url":"https://carrier.example.test/track"}`)
		if err := pool.QueryRow(ctx, "INSERT INTO email_outbox(event_key,recipient,template,payload) VALUES($1,'smtp-only@selecto.test',$2,$3) RETURNING id", key, template, raw).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if err := worker.ProcessOne(ctx, id); err != nil {
			t.Fatal(err)
		}
		var status, delivery string
		var payload []byte
		if err := pool.QueryRow(ctx, "SELECT status,delivery_status,payload FROM email_outbox WHERE id=$1", id).Scan(&status, &delivery, &payload); err != nil {
			t.Fatal(err)
		}
		if status != "sent" || delivery != "delivered" || string(payload) != "{}" {
			t.Fatalf("ack overwrote callback or retained payload: %s/%s %s", status, delivery, payload)
		}
		if err := worker.ProcessOne(ctx, id); err != nil {
			t.Fatal(err)
		}
		select {
		case message := <-messages:
			if !strings.Contains(message, "From: Selecto <no-reply@selectosport.com>") || !strings.Contains(message, `X-Mailin-custom: {"event_key":"`+key+`"}`) {
				t.Fatalf("missing branding/tracking for %s", template)
			}
			if !strings.Contains(message, "Selecto") || strings.Contains(message, "Destry") {
				t.Fatal("wrong email branding")
			}
		case <-ctx.Done():
			t.Fatal("SMTP message not received")
		}
	}
	if len(messages) != 0 {
		t.Fatal("idempotent tasks resent email")
	}
}
func serveTrackedSMTP(t *testing.T, conn net.Conn, pool *pgxpool.Pool, messages chan<- string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	wire := textproto.NewConn(conn)
	_ = wire.PrintfLine("220 isolated SMTP")
	for {
		line, err := wire.ReadLine()
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			_ = wire.PrintfLine("250 isolated SMTP")
		case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"), line == "RSET":
			_ = wire.PrintfLine("250 OK")
		case line == "DATA":
			_ = wire.PrintfLine("354 Send data")
			body, err := wire.ReadDotBytes()
			if err != nil {
				return
			}
			message := string(body)
			for _, header := range strings.Split(message, "\n") {
				if strings.HasPrefix(header, "X-Mailin-custom: ") {
					var tracking map[string]string
					if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(header, "X-Mailin-custom: "))), &tracking); err != nil {
						t.Error(err)
						return
					}
					// Simulate a fast delivery callback before the SMTP worker acknowledges.
					if _, err := pool.Exec(context.Background(), "UPDATE email_outbox SET delivery_status='delivered',last_provider_event_at=NOW() WHERE event_key=$1", tracking["event_key"]); err != nil {
						t.Error(err)
						return
					}
				}
			}
			messages <- message
			_ = wire.PrintfLine("250 Accepted")
		case line == "QUIT":
			_ = wire.PrintfLine("221 Bye")
			return
		default:
			_ = wire.PrintfLine("502 unsupported")
		}
	}
}
