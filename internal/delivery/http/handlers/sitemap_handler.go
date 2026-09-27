package handlers

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"
	"github.com/gin-gonic/gin"
)

type sitemapURL struct {
	Location string `xml:"loc"`
	Modified string `xml:"lastmod,omitempty"`
}
type sitemapSet struct {
	XMLName   xml.Name     `xml:"urlset"`
	Namespace string       `xml:"xmlns,attr"`
	URLs      []sitemapURL `xml:"url"`
}

func SitemapHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		base := strings.TrimRight(cfg.StorefrontURL, "/")
		if base == "" {
			base = "http://localhost:18080"
		}
		set := sitemapSet{Namespace: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, path := range []string{"/", "/contacto", "/ayuda", "/entregas", "/posventa"} {
			set.URLs = append(set.URLs, sitemapURL{Location: base + path})
		}
		// Fail closed instead of silently truncating a catalogue above one file's
		// sitemap limit. An index can be introduced when real volume requires it.
		rows, err := db.Pool.Query(c, `SELECT id,updated_at FROM products WHERE active=TRUE AND archived_at IS NULL ORDER BY id LIMIT 50001`)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		for rows.Next() {
			var id int
			var modified time.Time
			if rows.Scan(&id, &modified) != nil {
				rows.Close()
				c.Status(http.StatusServiceUnavailable)
				return
			}
			set.URLs = append(set.URLs, sitemapURL{Location: base + "/productos/" + url.PathEscape(utils.EncodeID(id)), Modified: modified.UTC().Format(time.RFC3339)})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		rows, err = db.Pool.Query(c, `SELECT c.slug,c.updated_at FROM categories c WHERE c.active=TRUE
		 AND EXISTS(SELECT 1 FROM products p WHERE p.category_id=c.id AND p.active=TRUE AND p.archived_at IS NULL) ORDER BY c.id LIMIT 50001`)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		for rows.Next() {
			var slug string
			var modified time.Time
			if rows.Scan(&slug, &modified) != nil {
				rows.Close()
				c.Status(http.StatusServiceUnavailable)
				return
			}
			set.URLs = append(set.URLs, sitemapURL{Location: base + "/?category=" + url.QueryEscape(slug), Modified: modified.UTC().Format(time.RFC3339)})
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(set.URLs) > 50000 {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		body, err := xml.Marshal(set)
		if err != nil || len(body) > 50*1024*1024 {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Header("Cache-Control", "public, max-age=300")
		c.Data(http.StatusOK, "application/xml; charset=utf-8", append([]byte(xml.Header), body...))
	}
}
