package handlers

import (
	"context"
	"encoding/csv"
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
		ProviderProgress   struct {
			PackageState string `json:"package_state"`
			PickupState  string `json:"pickup_state"`
		} `json:"provider_progress"`
		Destination struct {
			RecipientName string `json:"recipient_name"`
			StreetAddress string `json:"street_address"`
		} `json:"destination"`
	}
	if err := json.Unmarshal(previewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.DataReady || preview.ProviderSubmission != "disabled_pending_contract" ||
		preview.ProviderProgress.PackageState != "not_started" || preview.ProviderProgress.PickupState != "not_started" ||
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
	exportRecorder := httptest.NewRecorder()
	exportContext, _ := gin.CreateTestContext(exportRecorder)
	exportContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches/export", nil)
	AdminGestionarDispatchExportHandler(db)(exportContext)
	if exportRecorder.Code != http.StatusOK || !strings.Contains(exportRecorder.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(exportRecorder.Body.String(), utils.EncodeID(orderID)) ||
		!strings.Contains(exportRecorder.Body.String(), "Destino Real") ||
		!strings.Contains(exportRecorder.Body.String(), "12345678") ||
		!strings.Contains(exportRecorder.Body.String(), "SEL-TEST-001") ||
		strings.Contains(exportRecorder.Body.String(), "Dirección de perfil") {
		t.Fatalf("unexpected dispatch export status=%d body=%s", exportRecorder.Code, exportRecorder.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(exportRecorder.Body.String(), "\ufeff"))).ReadAll()
	if err != nil || len(records) < 2 || len(records[0]) != len(records[1]) {
		t.Fatalf("invalid dispatch CSV: %v", err)
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
	var pickupBatchID int64
	if err := pool.QueryRow(ctx, `INSERT INTO gestionar_pickup_batches
		(request_key,pickup_time_id,state,last_error_code,attempted_at)
		VALUES('test-pickup-progress',7,'uncertain','timeout',NOW()) RETURNING id`).Scan(&pickupBatchID); err != nil {
		t.Fatalf("seed pickup progress: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM gestionar_pickup_batches WHERE id=$1`, pickupBatchID) })
	if _, err := pool.Exec(ctx, `INSERT INTO gestionar_dispatches
		(order_id,submission_key,package_state,external_package_id,attempted_at,confirmed_at)
		VALUES($1,'test-package-progress','confirmed','test-external-package',NOW(),NOW())`, orderID); err != nil {
		t.Fatalf("seed package progress: %v", err)
	}
	exportRecorder = httptest.NewRecorder()
	exportContext, _ = gin.CreateTestContext(exportRecorder)
	exportContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches/export", nil)
	AdminGestionarDispatchExportHandler(db)(exportContext)
	if exportRecorder.Code != http.StatusOK || strings.Contains(exportRecorder.Body.String(), utils.EncodeID(orderID)) {
		t.Fatalf("confirmed package must not be exported again: status=%d body=%s", exportRecorder.Code, exportRecorder.Body.String())
	}
	if _, err := pool.Exec(ctx, `INSERT INTO gestionar_pickup_orders(batch_id,order_id)
		VALUES($1,$2)`, pickupBatchID, orderID); err != nil {
		t.Fatalf("seed pickup association: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO gestionar_status_events(order_id,event_key,external_status)
		VALUES($1,'test-event-progress','TEST_PROVIDER_STATUS')`, orderID); err != nil {
		t.Fatalf("seed raw provider status: %v", err)
	}
	previewRecorder = httptest.NewRecorder()
	previewContext, _ = gin.CreateTestContext(previewRecorder)
	previewContext.Request = httptest.NewRequest(http.MethodGet, "/admin/integrations/gestionar/dispatches/x/preview", nil)
	previewContext.Params = gin.Params{{Key: "id", Value: utils.EncodeID(orderID)}}
	AdminGestionarDispatchPreviewHandler(db)(previewContext)
	if previewRecorder.Code != http.StatusOK ||
		!strings.Contains(previewRecorder.Body.String(), `"external_package_id":"test-external-package"`) ||
		!strings.Contains(previewRecorder.Body.String(), `"pickup_state":"uncertain"`) ||
		!strings.Contains(previewRecorder.Body.String(), `"latest_provider_status":"TEST_PROVIDER_STATUS"`) {
		t.Fatalf("provider progress unavailable: %s", previewRecorder.Body.String())
	}
	var shipmentStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM shipments WHERE order_id=$1`, orderID).Scan(&shipmentStatus); err != nil || shipmentStatus != "preparing" {
		t.Fatalf("raw provider event changed customer shipment: %s err=%v", shipmentStatus, err)
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
