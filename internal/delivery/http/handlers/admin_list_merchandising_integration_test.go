package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"
	"github.com/gin-gonic/gin"
)

func TestAdminListProductsIncludesImagesAndOptionsForEditing(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	name := fmt.Sprintf("list-media-%d", time.Now().UnixNano())
	var productID, imageID int
	if err := pool.QueryRow(ctx, "INSERT INTO products(name,price,stock,active) VALUES ($1,100,2,FALSE) RETURNING id", name).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID) })
	if err := pool.QueryRow(ctx, `INSERT INTO product_images(product_id,mime_type,alt_text,sort_order,content,size_bytes) VALUES ($1,'image/png','Vista local',0,$2,1) RETURNING id`, productID, []byte{0}).Scan(&imageID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_options(product_id,name,values,sort_order) VALUES ($1,'Color','["Negro","Blanco"]',0)`, productID); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/products?q="+name, nil)
	AdminListProductsHandler(&database.DB{Pool: pool})(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Items []struct {
			Active  bool                   `json:"active"`
			Images  []productImageResponse `json:"images"`
			Options []productOption        `json:"options"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Active {
		t.Fatalf("inactive product missing from unfiltered list: %+v", result)
	}
	item := result.Items[0]
	if len(item.Images) != 1 || item.Images[0].URL != "/product-images/"+utils.EncodeID(imageID) || item.Images[0].AltText != "Vista local" {
		t.Fatalf("image metadata missing: %+v", item.Images)
	}
	if len(item.Options) != 1 || item.Options[0].Name != "Color" || len(item.Options[0].Values) != 2 {
		t.Fatalf("options missing from edit snapshot: %+v", item.Options)
	}
}
