package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func TestGestionarDispatchPreviewUsesPaidOrderSnapshotAndNeverSends(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	var userID, productID, orderID int
	email := fmt.Sprintf("gestionar-%d@selecto.test", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password,role,first_name,last_name,street_address)
		VALUES($1,'unused','user','Cuenta','Original','Dirección de perfil') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO products(name,sku,price,stock,active)
		VALUES('Producto de prueba','SEL-TEST-001',100,1,TRUE) RETURNING id`).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO orders(user_id,status,total,paid_at)
		VALUES($1,'paid',100,NOW()) RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	t.Cleanup(func() { cleanupOrderFixture(ctx, pool, orderID, productID, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO order_items(order_id,product_id,product_name_snapshot,sku_snapshot,quantity,price)
		VALUES($1,$2,'Producto de prueba','SEL-TEST-001',1,100)`, orderID, productID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_shipping_addresses
		(order_id,recipient_first_name,recipient_last_name,dni,street_address,street_number,postal_code,province,locality,phone_number)
		VALUES($1,'Destino','Real','12345678','Calle entrega','456','2000','Santa Fe','Rosario','3412345678')`, orderID); err != nil {
		t.Fatalf("seed address: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO shipments(order_id,status) VALUES($1,'preparing')`, orderID); err != nil {
		t.Fatalf("seed shipment: %v", err)
	}
	db := &database.DB{Pool: pool}

	previewRecorder := httptest.NewRecorder()
	previewContext, _ := gin.CreateTestContext(previewRecorder)
	previewContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches/x/preview", nil)
	previewContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(orderID)}}
	AdminGestionarDispatchPreviewHandler(db)(previewContext)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewRecorder.Code, previewRecorder.Body.String())
	}
	var preview struct {
		DataReady          bool   `json:"data_ready"`
		ProviderSubmission string `json:"provider_submission"`
		Destination        struct {
			RecipientName string `json:"recipient_name"`
			StreetAddress string `json:"street_address"`
		} `json:"destination"`
	}
	if err := json.Unmarshal(previewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.DataReady || preview.ProviderSubmission != "disabled_pending_contract" ||
		preview.Destination.RecipientName != "Destino Real" || preview.Destination.StreetAddress != "Calle entrega" ||
		strings.Contains(previewRecorder.Body.String(), "Dirección de perfil") {
		t.Fatalf("unexpected preview: %s", previewRecorder.Body.String())
	}

	queueRecorder := httptest.NewRecorder()
	queueContext, _ := gin.CreateTestContext(queueRecorder)
	queueContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches", nil)
	AdminGestionarDispatchesHandler(db)(queueContext)
	if queueRecorder.Code != http.StatusOK || !strings.Contains(queueRecorder.Body.String(), utils.EncodeID(orderID)) {
		t.Fatalf("queue status=%d body=%s", queueRecorder.Code, queueRecorder.Body.String())
	}

	if _, err := pool.Exec(ctx, `UPDATE products SET name='Nombre cambiado', sku=NULL WHERE id=$1`, productID); err != nil {
		t.Fatalf("change catalog after purchase: %v", err)
	}
	previewRecorder = httptest.NewRecorder()
	previewContext, _ = gin.CreateTestContext(previewRecorder)
	previewContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches/x/preview", nil)
	previewContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(orderID)}}
	AdminGestionarDispatchPreviewHandler(db)(previewContext)
	if previewRecorder.Code != http.StatusOK || !strings.Contains(previewRecorder.Body.String(), `"sku":"SEL-TEST-001"`) ||
		!strings.Contains(previewRecorder.Body.String(), `"name":"Producto de prueba"`) ||
		strings.Contains(previewRecorder.Body.String(), "Nombre cambiado") || !strings.Contains(previewRecorder.Body.String(), `"data_ready":true`) {
		t.Fatalf("dispatch snapshot changed with catalog: %s", previewRecorder.Body.String())
	}
	queueRecorder = httptest.NewRecorder()
	queueContext, _ = gin.CreateTestContext(queueRecorder)
	queueContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches", nil)
	AdminGestionarDispatchesHandler(db)(queueContext)
	if queueRecorder.Code != http.StatusOK || !strings.Contains(queueRecorder.Body.String(), `"data_ready":true`) {
		t.Fatalf("dispatch queue changed with catalog: %s", queueRecorder.Body.String())
	}
	if _, err := pool.Exec(ctx, `UPDATE order_items SET sku_snapshot='' WHERE order_id=$1`, orderID); err != nil {
		t.Fatalf("remove order sku snapshot: %v", err)
	}
	previewRecorder = httptest.NewRecorder()
	previewContext, _ = gin.CreateTestContext(previewRecorder)
	previewContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches/x/preview", nil)
	previewContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(orderID)}}
	AdminGestionarDispatchPreviewHandler(db)(previewContext)
	if previewRecorder.Code != http.StatusOK || !strings.Contains(previewRecorder.Body.String(), `"code":"sku_missing"`) ||
		!strings.Contains(previewRecorder.Body.String(), `"data_ready":false`) {
		t.Fatalf("missing sku not blocked: %s", previewRecorder.Body.String())
	}
}
