package handlers

import (
	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"
	"context"
	"encoding/xml"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDynamicSEOMatchesVisibleCatalogueAndEscapesContent(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var categoryID, activeID, hiddenID int
	slug := fmt.Sprintf("seo-%d", suffix)
	if err := pool.QueryRow(ctx, "INSERT INTO categories(name,slug,active) VALUES('Fuerza SEO',$1,TRUE) RETURNING id", slug).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM categories WHERE id=$1", categoryID) })
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,price,stock,description,category_id,active) VALUES($1,1234.56,0,$2,$3,TRUE) RETURNING id`, `Barra "<script>alert(1)</script>"`, "Acero & entrenamiento", categoryID).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,price,stock,category_id,active) VALUES('Oculto SEO',100,2,$1,FALSE) RETURNING id`, categoryID).Scan(&hiddenID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM products WHERE id=ANY($1)", []int{activeID, hiddenID}) })
	db := &database.DB{Pool: pool}
	cfg := &config.Config{StorefrontURL: "https://selectosport.com"}
	request := func(path string, head bool) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", path, nil)
		if head {
			SEOHeadHandler(db, cfg)(c)
		} else {
			SitemapHandler(db, cfg)(c)
		}
		if rec.Code != 200 {
			t.Fatalf("%s status %d", path, rec.Code)
		}
		return rec
	}
	publicID := utils.EncodeID(activeID)
	head := request("/seo/head?path="+url.QueryEscape("/productos/"+publicID), true).Body.String()
	if strings.Contains(head, "<script>") || !strings.Contains(head, "&lt;script&gt;") || !strings.Contains(head, "1234.56") || !strings.Contains(head, "https://selectosport.com/productos/"+publicID) {
		t.Fatal("product metadata incorrect or unsafe")
	}
	categoryHead := request("/seo/head?path="+url.QueryEscape("/?category="+slug), true).Body.String()
	if !strings.Contains(categoryHead, "Fuerza SEO") || !strings.Contains(categoryHead, "/?category="+slug) {
		t.Fatal("category metadata missing")
	}
	readSitemap := func() map[string]bool {
		var parsed sitemapSet
		if err := xml.Unmarshal(request("/sitemap.xml", false).Body.Bytes(), &parsed); err != nil {
			t.Fatal(err)
		}
		links := map[string]bool{}
		for _, entry := range parsed.URLs {
			links[entry.Location] = true
		}
		return links
	}
	links := readSitemap()
	if !links[cfg.StorefrontURL+"/productos/"+publicID] || !links[cfg.StorefrontURL+"/?category="+slug] || links[cfg.StorefrontURL+"/productos/"+utils.EncodeID(hiddenID)] {
		t.Fatal("sitemap visibility diverged from catalogue")
	}
	if _, err := pool.Exec(ctx, "UPDATE products SET active=FALSE WHERE id=$1", activeID); err != nil {
		t.Fatal(err)
	}
	links = readSitemap()
	if links[cfg.StorefrontURL+"/productos/"+publicID] || links[cfg.StorefrontURL+"/?category="+slug] {
		t.Fatal("inactive product/empty category remained in sitemap")
	}
	if !strings.Contains(request("/seo/head?path="+url.QueryEscape("/productos/"+publicID), true).Body.String(), "noindex,nofollow") {
		t.Fatal("inactive product indexed")
	}
}
