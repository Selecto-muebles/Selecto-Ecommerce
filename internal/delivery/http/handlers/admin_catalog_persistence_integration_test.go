package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func TestAdminProductPaginationIsBoundedStableAndComplete(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("pagination-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `
		INSERT INTO products (name, sku, price, stock, active, description, category)
		SELECT $1 || '-' || value, $1 || '-' || value, 100, 5, TRUE, '', 'test'
		FROM generate_series(1, 225) AS value`, prefix); err != nil {
		t.Fatalf("seed paginated products: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE sku LIKE $1", prefix+"-%")
	})

	type pageResponse struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Total    int `json:"total"`
	}
	list := func(page int) pageResponse {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/products?q=%s&active=true&stock=with_stock&sort=price&page=%d&page_size=1000", prefix, page), nil)
		AdminListProductsHandler(&database.DB{Pool: pool})(c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("page %d status = %d body=%s", page, recorder.Code, recorder.Body.String())
		}
		var response pageResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode page %d: %v", page, err)
		}
		return response
	}

	first, middle, last, outside := list(1), list(2), list(3), list(4)
	if first.Page != 1 || first.PageSize != 100 || first.Total != 225 || len(first.Items) != 100 {
		t.Fatalf("first page metadata/items = %d/%d/%d/%d, want 1/100/225/100", first.Page, first.PageSize, first.Total, len(first.Items))
	}
	if middle.Page != 2 || middle.PageSize != 100 || middle.Total != 225 || len(middle.Items) != 100 {
		t.Fatalf("middle page metadata/items = %d/%d/%d/%d, want 2/100/225/100", middle.Page, middle.PageSize, middle.Total, len(middle.Items))
	}
	if last.Page != 3 || last.PageSize != 100 || last.Total != 225 || len(last.Items) != 25 {
		t.Fatalf("last page metadata/items = %d/%d/%d/%d, want 3/100/225/25", last.Page, last.PageSize, last.Total, len(last.Items))
	}
	if outside.Page != 4 || outside.PageSize != 100 || outside.Total != 225 || len(outside.Items) != 0 {
		t.Fatalf("outside page metadata/items = %d/%d/%d/%d, want 4/100/225/0", outside.Page, outside.PageSize, outside.Total, len(outside.Items))
	}
	seen := make(map[string]struct{}, 225)
	items := append(append(first.Items, middle.Items...), last.Items...)
	for _, item := range items {
		if _, duplicated := seen[item.ID]; duplicated {
			t.Fatalf("product %q appears in more than one page", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	if len(seen) != 225 {
		t.Fatalf("unique paginated products = %d, want 225", len(seen))
	}
}

func TestAdminProductImageCRUDPersistsBinaryAndMetadata(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	var productID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO products (name, sku, price, stock, active)
		VALUES ('Image persistence test', $1, 100, 1, TRUE)
		RETURNING id`, fmt.Sprintf("image-%d", time.Now().UnixNano())).Scan(&productID); err != nil {
		t.Fatalf("seed image product: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE entity_type='product' AND entity_id=$1", productID)
		_, _ = pool.Exec(ctx, "DELETE FROM products WHERE id=$1", productID)
	})

	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode fixture PNG: %v", err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="catalog.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create image form part: %v", err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatalf("write image form part: %v", err)
	}
	if err := writer.WriteField("alt_text", "Vista frontal certificada"); err != nil {
		t.Fatalf("write alt text: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}

	uploadRecorder := httptest.NewRecorder()
	uploadContext, _ := gin.CreateTestContext(uploadRecorder)
	uploadContext.Request = httptest.NewRequest(http.MethodPost, "/admin/products/"+utils.EncodeID(productID)+"/images", &body)
	uploadContext.Request.Header.Set("Content-Type", writer.FormDataContentType())
	uploadContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(productID)}}
	uploadContext.Set("email", "catalog-certification@selecto.test")
	AdminUploadProductImageHandler(&database.DB{Pool: pool})(uploadContext)
	if uploadRecorder.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", uploadRecorder.Code, uploadRecorder.Body.String())
	}

	var imageID, sortOrder, sizeBytes int
	var mimeType, altText string
	var persisted []byte
	if err := pool.QueryRow(ctx, `
		SELECT id, mime_type, alt_text, sort_order, content, size_bytes
		FROM product_images WHERE product_id=$1`, productID).
		Scan(&imageID, &mimeType, &altText, &sortOrder, &persisted, &sizeBytes); err != nil {
		t.Fatalf("read persisted image: %v", err)
	}
	if mimeType != "image/png" || altText != "Vista frontal certificada" || sortOrder != 0 || sizeBytes != len(png) || !bytes.Equal(persisted, png) {
		t.Fatalf("persisted image metadata/content does not match upload")
	}

	readRecorder := httptest.NewRecorder()
	readContext, _ := gin.CreateTestContext(readRecorder)
	readContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(imageID)}}
	readContext.Request = httptest.NewRequest(http.MethodGet, "/product-images/"+utils.EncodeID(imageID), nil)
	ProductImageHandler(&database.DB{Pool: pool})(readContext)
	if readRecorder.Code != http.StatusOK || readRecorder.Header().Get("Cache-Control") != "public, max-age=86400, immutable" || !bytes.Equal(readRecorder.Body.Bytes(), png) {
		t.Fatalf("binary image response status/cache/content is invalid")
	}

	updateRecorder := httptest.NewRecorder()
	updateContext, _ := gin.CreateTestContext(updateRecorder)
	updateContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(productID)}, {Key: "image_id", Value: utils.EncodeID(imageID)}}
	updateContext.Request = httptest.NewRequest(http.MethodPatch, "/admin/products/images", bytes.NewBufferString(`{"alt_text":"Vista lateral","sort_order":3}`))
	updateContext.Request.Header.Set("Content-Type", "application/json")
	AdminUpdateProductImageHandler(&database.DB{Pool: pool})(updateContext)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	if err := pool.QueryRow(ctx, "SELECT alt_text, sort_order FROM product_images WHERE id=$1", imageID).Scan(&altText, &sortOrder); err != nil {
		t.Fatalf("read updated image: %v", err)
	}
	if altText != "Vista lateral" || sortOrder != 3 {
		t.Fatalf("updated metadata = %q/%d, want Vista lateral/3", altText, sortOrder)
	}

	deleteRecorder := httptest.NewRecorder()
	deleteContext, _ := gin.CreateTestContext(deleteRecorder)
	deleteContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(productID)}, {Key: "image_id", Value: utils.EncodeID(imageID)}}
	deleteContext.Request = httptest.NewRequest(http.MethodDelete, "/admin/products/images", nil)
	deleteContext.Set("email", "catalog-certification@selecto.test")
	AdminDeleteProductImageHandler(&database.DB{Pool: pool})(deleteContext)
	deleteContext.Writer.WriteHeaderNow()
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM product_images WHERE id=$1", imageID).Scan(&remaining); err != nil {
		t.Fatalf("count deleted image: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("deleted image remains persisted")
	}
}
