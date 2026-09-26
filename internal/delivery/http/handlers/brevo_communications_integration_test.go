package handlers

import (
	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func marketingRequest(handler gin.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	c.Writer.WriteHeaderNow()
	return recorder
}
func TestNewsletterBajaRequiresSingleUseProofAndKeepsTransactionalMail(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	db := &database.DB{Pool: pool}
	email := fmt.Sprintf("optout-%d@selecto.test", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_outbox WHERE recipient=$1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM commerce.marketing_unsubscribe_tokens WHERE email=$1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM commerce.marketing_subscriptions WHERE email=$1", email)
	})
	res := marketingRequest(NewsletterSubscribeHandler(db), "/marketing/newsletter", fmt.Sprintf(`{"email":%q,"consent":true}`, email))
	if res.Code != 202 {
		t.Fatalf("subscribe: %d %s", res.Code, res.Body)
	}
	res = marketingRequest(NewsletterUnsubscribeHandler(db, &config.Config{StorefrontURL: "https://selectosport.com"}), "/marketing/newsletter/unsubscribe", fmt.Sprintf(`{"email":%q}`, email))
	if res.Code != 202 {
		t.Fatalf("request: %d %s", res.Code, res.Body)
	}
	var raw []byte
	var status string
	if err := pool.QueryRow(ctx, "SELECT payload FROM email_outbox WHERE recipient=$1 AND template='newsletter_unsubscribe'", email).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(payload["url"])
	if err != nil || link.Host != "selectosport.com" || link.Path != "/newsletter/baja" {
		t.Fatalf("bad link %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM commerce.marketing_subscriptions WHERE email=$1", email).Scan(&status); err != nil || status != "subscribed" {
		t.Fatal("request alone opted out")
	}
	token := link.Query().Get("token")
	for i, want := range []int{200, 400} {
		res = marketingRequest(NewsletterUnsubscribeConfirmHandler(db), "/marketing/newsletter/unsubscribe/confirm", fmt.Sprintf(`{"token":%q}`, token))
		if res.Code != want {
			t.Fatalf("confirm %d: %d %s", i, res.Code, res.Body)
		}
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM commerce.marketing_subscriptions WHERE email=$1", email).Scan(&status); err != nil || status != "unsubscribed" {
		t.Fatalf("optout: %s %v", status, err)
	}
	var version int
	if err := pool.QueryRow(ctx, "SELECT sync_version FROM commerce.marketing_subscriptions WHERE email=$1", email).Scan(&version); err != nil || version != 2 {
		t.Fatalf("version: %d %v", version, err)
	}
}
func TestBrevoWebhookDuplicateLateDeliveryAndSuppression(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	db := &database.DB{Pool: pool}
	email := fmt.Sprintf("webhook-%d@selecto.test", time.Now().UnixNano())
	key := email
	secret := strings.Repeat("x", 40)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM brevo_webhook_events WHERE email=$1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM email_outbox WHERE recipient=$1", email)
		_, _ = pool.Exec(ctx, "DELETE FROM commerce.marketing_subscriptions WHERE email=$1", email)
	})
	if _, err := pool.Exec(ctx, "INSERT INTO email_outbox(event_key,recipient,template,payload) VALUES($1,$2,'order_created','{}')", key, email); err != nil {
		t.Fatal(err)
	}
	res := marketingRequest(NewsletterSubscribeHandler(db), "/marketing/newsletter", fmt.Sprintf(`{"email":%q,"consent":true}`, email))
	if res.Code != 202 {
		t.Fatal(res.Body)
	}
	handler := BrevoWebhookHandler(db, &config.Config{BrevoWebhookToken: secret})
	send := func(event string, ts int64, auth bool) int {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		payload := fmt.Sprintf(`{"id":124,"event":%q,"email":%q,"ts_event":%d,"message-id":"provider-id","X-Mailin-custom":{"event_key":%q}}`, event, email, ts, key)
		c.Request = httptest.NewRequest(http.MethodPost, "/communications/brevo/webhook", strings.NewReader(payload))
		c.Request.Header.Set("Content-Type", "application/json")
		if auth {
			c.Request.Header.Set("X-Forwarded-Authorization", "Bearer "+secret)
			c.Request.Header.Set("Authorization", "Bearer google-gateway-token")
		}
		handler(c)
		c.Writer.WriteHeaderNow()
		return recorder.Code
	}
	if send("delivered", 200, false) != 401 {
		t.Fatal("unauthenticated webhook accepted")
	}
	for _, event := range []struct {
		event string
		ts    int64
	}{{"delivered", 200}, {"delivered", 200}, {"request", 100}, {"opened", 300}} {
		if code := send(event.event, event.ts, true); code != 204 {
			t.Fatalf("webhook %s: %d", event.event, code)
		}
	}
	var status string
	var count int
	if err := pool.QueryRow(ctx, "SELECT delivery_status FROM email_outbox WHERE event_key=$1", key).Scan(&status); err != nil || status != "delivered" {
		t.Fatalf("late event regressed delivery: %s %v", status, err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM brevo_webhook_events WHERE email=$1", email).Scan(&count); err != nil || count != 3 {
		t.Fatalf("duplicate: %d %v", count, err)
	}
	if send("hardBounce", 400, true) != 204 {
		t.Fatal("marketing bounce not accepted")
	}
	res = marketingRequest(NewsletterSubscribeHandler(db), "/marketing/newsletter", fmt.Sprintf(`{"email":%q,"consent":true}`, email))
	if res.Code != 409 {
		t.Fatalf("suppressed address resubscribed: %d %s", res.Code, res.Body)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM commerce.marketing_subscriptions WHERE email=$1", email).Scan(&status); err != nil || status != "unsubscribed" {
		t.Fatalf("suppression lost: %s %v", status, err)
	}
	if brevoOutboxEventKey(map[string]any{"other": "value"}) != "" {
		t.Fatal("missing event key falsely correlated")
	}
}
