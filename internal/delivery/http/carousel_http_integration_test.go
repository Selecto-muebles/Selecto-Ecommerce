package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthenticatedCategoryProductAndCarouselHTTP(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	email := fmt.Sprintf("editorial-http-%d@test.invalid", time.Now().UnixNano())
	password := "test-only-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO users(email,password,role,email_verified_at) VALUES($1,$2,'admin',NOW())", email, string(hash)); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM users WHERE email=$1", email)
	cfg := &config.Config{JWTSecret: "isolated-test-secret-not-production", JWTTTL: time.Hour, RateLimitPerMinute: 1000}
	router := SetupRouter(&database.DB{Pool: pool}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	token := ""
	request := func(method, path, contentType string, body []byte, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: %d %s want %d", method, path, rec.Code, rec.Body.String(), want)
		}
		return rec
	}
	request("GET", "/admin/carousel-slides", "", nil, 401)
	loginBody, _ := json.Marshal(map[string]string{"email": email, "password": password})
	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(request("POST", "/login", "application/json", loginBody, 200).Body.Bytes(), &login); err != nil || login.Token == "" {
		t.Fatal("login failed", err)
	}
	token = login.Token
	name := fmt.Sprintf("HTTP-%d", time.Now().UnixNano())
	slug := fmt.Sprintf("http-%d", time.Now().UnixNano())
	categoryBody, _ := json.Marshal(map[string]string{"name": name, "slug": slug})
	var category struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(request("POST", "/admin/categories", "application/json", categoryBody, 201).Body.Bytes(), &category); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM categories WHERE id=$1", category.ID)
	defer pool.Exec(ctx, "DELETE FROM audit_logs WHERE actor_email=$1", email)
	productBody, _ := json.Marshal(map[string]any{"name": "HTTP product", "category": name, "price": 100, "stock": 1, "active": true})
	var product struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(request("POST", "/admin/products", "application/json", productBody, 201).Body.Bytes(), &product); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM products WHERE category_id=$1", category.ID)
	var linked int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM products WHERE category_id=$1 AND category=$2", category.ID, name).Scan(&linked); err != nil || linked != 1 {
		t.Fatal("category selection not persisted", err)
	}
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	payload, _ := json.Marshal(map[string]any{"title": "HTTP editorial", "alt_text": "Vista", "cta_label": "Ver producto", "target_kind": "product", "target_id": product.ID, "active": true})
	_ = writer.WriteField("payload", string(payload))
	part, err := writer.CreateFormFile("image", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(img.Bytes())
	_ = writer.Close()
	var slide struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(request("POST", "/admin/carousel-slides", writer.FormDataContentType(), upload.Bytes(), 201).Body.Bytes(), &slide); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM carousel_slides WHERE id=$1", slide.ID)
	token = ""
	imageResponse := request("GET", fmt.Sprintf("/carousel-images/%d", slide.ID), "", nil, 200)
	if imageResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatal("image MIME mismatch")
	}
	request("GET", fmt.Sprintf("/admin/carousel-slides/%d/image", slide.ID), "", nil, 401)
	if _, err := pool.Exec(ctx, "UPDATE users SET role='user' WHERE email=$1", email); err != nil {
		t.Fatal(err)
	}
	token = login.Token
	request("GET", "/admin/carousel-slides", "", nil, http.StatusForbidden)
}
