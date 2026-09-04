package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMarketingSubscriptionLifecycle(t *testing.T) {
	pool := marketingIntegrationPool(t)
	db := &database.DB{Pool: pool}
	email := fmt.Sprintf("newsletter-%d@selecto.test", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM marketing_subscriptions WHERE email=$1", email)
	})

	subscribe := httptest.NewRecorder()
	subscribeContext, _ := gin.CreateTestContext(subscribe)
	subscribeContext.Request = httptest.NewRequest(
		http.MethodPost,
		"/marketing/newsletter",
		bytes.NewBufferString(fmt.Sprintf(`{"email":%q,"consent":true,"source":"integration-test"}`, email)),
	)
	subscribeContext.Request.Header.Set("Content-Type", "application/json")
	NewsletterSubscribeHandler(db)(subscribeContext)
	if subscribe.Code != http.StatusAccepted {
		t.Fatalf("subscribe status = %d body=%s", subscribe.Code, subscribe.Body.String())
	}

	list := httptest.NewRecorder()
	listContext, _ := gin.CreateTestContext(list)
	listContext.Request = httptest.NewRequest(
		http.MethodGet,
		"/admin/marketing/subscriptions?status=subscribed&page=1&page_size=100",
		nil,
	)
	AdminListMarketingSubscriptionsHandler(db)(listContext)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var result struct {
		Items []struct {
			Email  string `json:"email"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, item := range result.Items {
		if item.Email == email && item.Status == "subscribed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("subscribed email %q not returned by admin list", email)
	}

	unsubscribe := httptest.NewRecorder()
	unsubscribeContext, _ := gin.CreateTestContext(unsubscribe)
	unsubscribeContext.Request = httptest.NewRequest(
		http.MethodPost,
		"/marketing/newsletter/unsubscribe",
		bytes.NewBufferString(fmt.Sprintf(`{"email":%q}`, email)),
	)
	unsubscribeContext.Request.Header.Set("Content-Type", "application/json")
	NewsletterUnsubscribeHandler(db)(unsubscribeContext)
	if unsubscribe.Code != http.StatusOK {
		t.Fatalf("unsubscribe status = %d body=%s", unsubscribe.Code, unsubscribe.Body.String())
	}
	var status string
	if err := pool.QueryRow(context.Background(), "SELECT status FROM marketing_subscriptions WHERE email=$1", email).Scan(&status); err != nil {
		t.Fatalf("read subscription: %v", err)
	}
	if status != "unsubscribed" {
		t.Fatalf("subscription status = %q, want unsubscribed", status)
	}
}

func marketingIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run marketing integration tests")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = "commerce"
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("open marketing test pool: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping marketing test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
