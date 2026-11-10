package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"github.com/gin-gonic/gin"
)

func TestAtomicPublicationCreationAndCompletePublicationPagination(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	db := &database.DB{Pool: pool}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	prefix := fmt.Sprintf("Atomic publication %d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product' AND entity_id IN (SELECT id FROM products WHERE name LIKE $1)", prefix+"%")
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product_family' AND entity_id IN (SELECT id FROM product_families WHERE name LIKE $1)", prefix+"%")
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE name LIKE $1", prefix+"%")
		_, _ = pool.Exec(ctx, "DELETE FROM product_families WHERE name LIKE $1", prefix+"%")
	})
	request := func(method, path string, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest(method, path, bytes.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("email", "catalog-tests@selecto.test")
		handler(c)
		return r
	}
	create := func(name string, price float64, values map[string]string, familyID int, publication bool) *httptest.ResponseRecorder {
		body := map[string]any{"name": name + " variant", "price": price, "stock": 4, "description": "Preserved description", "category": "Calistenia", "specifications": map[string]string{"Material": "Madera"}, "variant_values": values, "auto_sku": true}
		if publication {
			body["publication"] = map[string]any{"name": name, "axes": []string{"Color", "Largo"}}
		} else {
			body["family_id"] = familyID
		}
		return request(http.MethodPost, "/admin/products", body, AdminCreateProductHandler(db, logger))
	}
	for _, test := range []struct {
		name   string
		price  float64
		values map[string]string
		status int
	}{
		{" invalid attributes", 100, map[string]string{"Color": "Negro"}, http.StatusBadRequest},
		{" failing product", 1e20, map[string]string{"Color": "Negro", "Largo": "40 cm"}, http.StatusInternalServerError},
	} {
		r := create(prefix+test.name, test.price, test.values, 0, true)
		if r.Code != test.status {
			t.Fatalf("%s: status=%d body=%s", test.name, r.Code, r.Body.String())
		}
		var count int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM product_families WHERE name=$1", prefix+test.name).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan family count=%d err=%v", count, err)
		}
	}
	r := create(prefix+" A", 56000, map[string]string{"Color": "Negro", "Largo": "40 cm"}, 0, true)
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	var first struct {
		ID     string        `json:"id"`
		SKU    string        `json:"sku"`
		Family productFamily `json:"family"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &first); err != nil || first.Family.ID == 0 {
		t.Fatalf("missing first family: %s %v", r.Body.String(), err)
	}
	if first.SKU != "SEL-"+first.ID {
		t.Fatalf("missing generated dispatch SKU: %q", first.SKU)
	}
	r = create(prefix+" A second", 60000, map[string]string{"Color": "Blanco", "Largo": "50 cm"}, first.Family.ID, false)
	if r.Code != http.StatusCreated {
		t.Fatalf("add variant: %d %s", r.Code, r.Body.String())
	}
	r = create(prefix+" A duplicate", 60000, map[string]string{"Color": "Blanco", "Largo": "50 cm"}, first.Family.ID, false)
	if r.Code != http.StatusConflict {
		t.Fatalf("duplicate variant status=%d", r.Code)
	}
	r = create(prefix+" B", 100, map[string]string{"Color": "Natural", "Largo": "30 cm"}, 0, true)
	if r.Code != http.StatusCreated {
		t.Fatalf("second publication: %d %s", r.Code, r.Body.String())
	}
	for page, wantMembers := range []int{2, 1} {
		r = request(http.MethodGet, fmt.Sprintf("/admin/products?group_publications=true&sort=name&page=%d&page_size=1", page+1), nil, func(c *gin.Context) {
			q := c.Request.URL.Query()
			q.Set("q", prefix)
			c.Request.URL.RawQuery = q.Encode()
			AdminListProductsHandler(db)(c)
		})
		var result struct {
			Total int `json:"total"`
			Items []struct {
				Family         productFamily     `json:"family"`
				Description    string            `json:"description"`
				Specifications map[string]string `json:"specifications"`
			} `json:"items"`
		}
		if r.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s", page+1, r.Code, r.Body.String())
		}
		if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || result.Total != 2 || len(result.Items) != wantMembers {
			t.Fatalf("split publication: page=%d body=%s err=%v", page+1, r.Body.String(), err)
		}
		for _, item := range result.Items {
			if item.Description != "Preserved description" || item.Specifications["Material"] != "Madera" {
				t.Fatalf("content changed: %+v", item)
			}
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE products SET specifications=specifications || jsonb_build_object('Largo',variant_values->>'Largo') WHERE family_id=$1", first.Family.ID); err != nil {
		t.Fatal(err)
	}
	r = request(http.MethodPost, "/admin/product-families", map[string]any{"action": "rename", "id": first.Family.ID, "name": first.Family.Name, "category": "Entrenamiento", "description": "Descripción común", "specifications": map[string]string{"Material": "Acero"}}, AdminCreateProductFamilyHandler(db))
	if r.Code != http.StatusOK {
		t.Fatalf("common content: %d %s", r.Code, r.Body.String())
	}
	var matching int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM products WHERE family_id=$1 AND category='Entrenamiento' AND description='Descripción común' AND specifications->>'Material'='Acero' AND specifications->>'Largo'=variant_values->>'Largo' AND price IN (56000,60000)", first.Family.ID).Scan(&matching); err != nil || matching != 2 {
		t.Fatalf("common fields replaced variant measurements: count=%d err=%v", matching, err)
	}
	r = request(http.MethodPost, "/admin/product-families", map[string]any{"action": "rename", "id": first.Family.ID, "name": prefix + " invalid rename", "specifications": map[string]string{"": ""}}, AdminCreateProductFamilyHandler(db))
	if r.Code != http.StatusBadRequest {
		t.Fatalf("invalid common fields status=%d", r.Code)
	}
	var name string
	if err := pool.QueryRow(ctx, "SELECT name FROM product_families WHERE id=$1", first.Family.ID).Scan(&name); err != nil || name != first.Family.Name {
		t.Fatalf("partial rename committed: %q %v", name, err)
	}
	r = request(http.MethodPost, "/admin/product-families", map[string]any{"action": "rename", "id": first.Family.ID, "name": first.Family.Name, "remove_specifications": []string{"Material"}}, AdminCreateProductFamilyHandler(db))
	if r.Code != http.StatusOK {
		t.Fatalf("remove common specification: %d %s", r.Code, r.Body.String())
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM products WHERE family_id=$1 AND NOT (specifications ? 'Material') AND specifications->>'Largo'=variant_values->>'Largo'", first.Family.ID).Scan(&matching); err != nil || matching != 2 {
		t.Fatalf("removed unrelated measurements: %d %v", matching, err)
	}
}
