package handlers

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const seoDescription = "Equipamiento para calistenia, entrenamiento funcional y gimnasios. Explorá el catálogo Selecto."

type seoHead struct {
	Title, Description, Canonical, Image, Robots, Type, Price string
}

var seoHeadTemplate = template.Must(template.New("head").Parse(`<title>{{.Title}} | Selecto</title>
<meta name="description" content="{{.Description}}">
<meta name="robots" content="{{.Robots}}">
<link rel="canonical" href="{{.Canonical}}">
<meta property="og:site_name" content="Selecto">
<meta property="og:title" content="{{.Title}} | Selecto">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.Canonical}}">
<meta property="og:type" content="{{.Type}}">
<meta property="og:image" content="{{.Image}}">
<meta property="og:image:alt" content="{{.Title}}">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="{{.Title}} | Selecto">
<meta name="twitter:description" content="{{.Description}}">
<meta name="twitter:image" content="{{.Image}}">
{{if .Price}}<meta property="product:price:amount" content="{{.Price}}"><meta property="product:price:currency" content="ARS">{{end}}
`))

// Nginx includes this public fragment into the initial HTML. There are no
// external fetches, private fields, auth tokens or User-Agent-specific rendering.
func SEOHeadHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		base := strings.TrimRight(cfg.StorefrontURL, "/")
		if base == "" {
			base = "http://localhost:18080"
		}
		head := seoHead{Title: "Equipamiento Deportivo", Description: seoDescription, Canonical: base + "/", Image: base + "/logo-selecto.png", Robots: "index,follow", Type: "website"}
		raw := c.Query("path")
		parsed, err := url.ParseRequestURI(raw)
		if err != nil || parsed.IsAbs() || len(raw) > 2048 {
			head.Robots = "noindex,nofollow"
			err = nil
		} else {
			err = resolveSEOHead(c, db, parsed, base, &head)
		}
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		var body bytes.Buffer
		if err := seoHeadTemplate.Execute(&body, head); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", body.Bytes())
	}
}

func resolveSEOHead(c *gin.Context, db *database.DB, page *url.URL, base string, head *seoHead) error {
	path := page.Path
	if strings.HasPrefix(path, "/productos/") {
		publicID := strings.TrimPrefix(path, "/productos/")
		id, err := utils.DecodeID(publicID)
		if err != nil || id <= 0 {
			head.Title = "Producto no disponible"
			head.Robots = "noindex,nofollow"
			return nil
		}
		var imageID int
		err = db.Pool.QueryRow(c, `SELECT p.name,COALESCE(p.description,''),p.price::text,
		 COALESCE((SELECT i.id FROM product_images i WHERE i.product_id=p.id ORDER BY i.sort_order,i.id LIMIT 1),0)
		 FROM products p WHERE p.id=$1 AND p.active=TRUE AND p.archived_at IS NULL`, id).Scan(&head.Title, &head.Description, &head.Price, &imageID)
		if errors.Is(err, pgx.ErrNoRows) {
			head.Title = "Producto no disponible"
			head.Description = seoDescription
			head.Robots = "noindex,nofollow"
			head.Price = ""
			return nil
		}
		if err != nil {
			return err
		}
		head.Canonical = base + "/productos/" + url.PathEscape(utils.EncodeID(id))
		head.Type = "product"
		if imageID > 0 {
			head.Image = base + "/api/product-images/" + utils.EncodeID(imageID)
		}
		head.Description = seoSummary(head.Description)
		return nil
	}
	if path == "/" {
		if category := page.Query().Get("category"); category != "" {
			var slug string
			err := db.Pool.QueryRow(c, `SELECT c.name,c.slug FROM categories c WHERE c.active=TRUE AND c.slug=$1
			 AND EXISTS(SELECT 1 FROM products p WHERE p.category_id=c.id AND p.active=TRUE AND p.archived_at IS NULL)`, category).Scan(&head.Title, &slug)
			if errors.Is(err, pgx.ErrNoRows) {
				head.Robots = "noindex,nofollow"
				return nil
			}
			if err != nil {
				return err
			}
			head.Canonical = base + "/?category=" + url.QueryEscape(slug)
			head.Description = "Explorá " + head.Title + " en el catálogo de equipamiento Selecto."
		}
		for _, key := range []string{"q", "stock", "sort", "page"} {
			if page.Query().Get(key) != "" || c.Query(key) != "" {
				head.Robots = "noindex,nofollow"
			}
		}
		return nil
	}
	pages := map[string]string{"/contacto": "Contacto", "/ayuda": "Ayuda para tu compra", "/entregas": "Entregas", "/posventa": "Atención posventa"}
	if title, ok := pages[path]; ok {
		head.Title = title
		head.Canonical = base + path
		head.Description = title + " Selecto. Información para acompañarte antes y después de comprar."
		return nil
	}
	// Account, payment, recovery and unknown URLs never expose query tokens or
	// claim to be indexable commercial content.
	if strings.HasPrefix(path, "/") {
		head.Canonical = base + path
	}
	if strings.Contains(path, "login") {
		head.Title = "Iniciar sesión"
	}
	if strings.Contains(path, "registro") {
		head.Title = "Crear cuenta"
	}
	if strings.Contains(path, "newsletter") {
		head.Title = "Novedades, a tu ritmo"
	}
	head.Robots = "noindex,nofollow"
	return nil
}

func seoSummary(value string) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) == 0 {
		return seoDescription
	}
	if len(runes) > 170 {
		return string(runes[:167]) + "…"
	}
	return string(runes)
}
