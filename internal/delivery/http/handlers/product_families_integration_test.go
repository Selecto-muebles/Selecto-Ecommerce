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
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func TestProductFamiliesPersistAndAppearInStorefrontWithoutChangingPrices(t *testing.T) {
	pool := integrationPool(t)
	db := &database.DB{Pool: pool}
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	request := func(method, path string, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(body)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, path, bytes.NewReader(payload))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("email", "families-test@selecto.test")
		handler(c)
		return recorder
	}

	name := fmt.Sprintf("Test family %d", time.Now().UnixNano())
	familyCreated := request(http.MethodPost, "/admin/product-families", map[string]any{
		"name": name, "axes": []string{"Largo", "Color"},
	}, AdminCreateProductFamilyHandler(db))
	if familyCreated.Code != http.StatusCreated {
		t.Fatalf("family create: %d %s", familyCreated.Code, familyCreated.Body.String())
	}
	var family productFamily
	if err := json.Unmarshal(familyCreated.Body.Bytes(), &family); err != nil {
		t.Fatal(err)
	}
	created := []int{}
	t.Cleanup(func() {
		for _, id := range created {
			_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product' AND entity_id=$1", id)
			_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", id)
		}
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product_family' AND entity_id=$1", family.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM product_families WHERE id=$1", family.ID)
	})

	create := func(color string, price float64) (string, int) {
		t.Helper()
		response := request(http.MethodPost, "/admin/products", map[string]any{
			"name": name + " " + color, "price": price, "stock": 7, "active": true,
			"category": "Calistenia", "family_id": family.ID,
			"variant_values": map[string]string{"Largo": "40 cm", "Color": color},
		}, AdminCreateProductHandler(db, logger))
		if response.Code != http.StatusCreated {
			t.Fatalf("product create: %d %s", response.Code, response.Body.String())
		}
		var item struct {
			ID     string        `json:"id"`
			Family productFamily `json:"family"`
			Price  float64       `json:"price"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		id, err := utils.DecodeID(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, id)
		if item.Family.ID != family.ID || item.Family.Values["Color"] != color || item.Price != price {
			t.Fatalf("created item %+v", item)
		}
		return item.ID, id
	}
	firstPublicID, _ := create("Natural", 49000)
	secondPublicID, _ := create("Negro", 56000)

	duplicate := request(http.MethodPost, "/admin/products", map[string]any{
		"name": name + " duplicate", "price": 1, "stock": 1, "family_id": family.ID,
		"variant_values": map[string]string{"Largo": "40 cm", "Color": "Negro"},
	}, AdminCreateProductHandler(db, logger))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate variant: %d %s", duplicate.Code, duplicate.Body.String())
	}

	storefront := request(http.MethodGet, "/products", nil, GetProductsHandler(db, logger))
	if storefront.Code != http.StatusOK {
		t.Fatalf("storefront: %d %s", storefront.Code, storefront.Body.String())
	}
	var products []ProductResponse
	if err := json.Unmarshal(storefront.Body.Bytes(), &products); err != nil {
		t.Fatal(err)
	}
	seen := map[string]float64{}
	for _, product := range products {
		if product.ID != firstPublicID && product.ID != secondPublicID {
			continue
		}
		if product.Family == nil || product.Family.ID != family.ID {
			t.Fatalf("missing family for %s", product.ID)
		}
		seen[product.ID] = product.Price
	}
	if seen[firstPublicID] != 49000 || seen[secondPublicID] != 56000 {
		t.Fatalf("storefront prices: %+v", seen)
	}
}
