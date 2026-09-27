package handlers

import (
	"Selecto-Ecommerce/internal/config"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSEOHeadPreservesAvailabilityMatcherAndNeverReflectsTokens(t *testing.T) {
	for _, path := range []string{"/", "/restablecer-contrasena?token=private-token", "https://attacker.invalid/", "/productos/invalid!"} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/seo/head?path="+url.QueryEscape(path), nil)
		SEOHeadHandler(nil, &config.Config{StorefrontURL: "https://selectosport.com"})(c)
		if rec.Code != 200 {
			t.Fatalf("path %q status %d", path, rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, "private-token") || strings.Contains(body, "attacker.invalid") {
			t.Fatal("untrusted URL or token reflected")
		}
		if path == "/" && !strings.Contains(body, "<title>Equipamiento Deportivo | Selecto</title>") {
			t.Fatal("availability matcher changed")
		}
		if path != "/" && !strings.Contains(body, `content="noindex,nofollow"`) {
			t.Fatal("private/invalid path became indexable")
		}
	}
}

func TestSEODescriptionHasBoundedReadableUnicode(t *testing.T) {
	if len([]rune(seoSummary(strings.Repeat("á", 200)))) > 170 {
		t.Fatal("description exceeds budget")
	}
	if seoSummary("  Uno \n dos ") != "Uno dos" {
		t.Fatal("whitespace was not normalized")
	}
}
