package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"github.com/gin-gonic/gin"
)

func TestLegalArticleDraftAndPublicationKeepAcceptedVersionImmutable(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	if _, err := database.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	db := &database.DB{Pool: pool}
	var oldID, newID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM legal_documents WHERE slug='terms' AND published`).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		defer tx.Rollback(ctx)
		if newID != 0 {
			if _, err := tx.Exec(ctx, `UPDATE legal_documents SET published=FALSE WHERE id=$1`, newID); err != nil {
				t.Error(err)
				return
			}
			if _, err := tx.Exec(ctx, `DELETE FROM legal_documents WHERE id=$1`, newID); err != nil {
				t.Error(err)
				return
			}
			if _, err := tx.Exec(ctx, `UPDATE legal_documents SET published=TRUE WHERE id=$1`, oldID); err != nil {
				t.Error(err)
				return
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM legal_term_drafts WHERE slug='terms'`); err != nil {
			t.Error(err)
			return
		}
		if _, err := tx.Exec(ctx, `DELETE FROM audit_logs WHERE actor_email='legal-editor-test@selecto.test'`); err != nil {
			t.Error(err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			t.Error(err)
		}
	})
	call := func(method, path, body string, handler gin.HandlerFunc, index string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("email", "legal-editor-test@selecto.test")
		if index != "" {
			c.Params = gin.Params{{Key: "index", Value: index}}
		}
		handler(c)
		return recorder
	}
	initial := call(http.MethodGet, "/admin/legal/terms/editor", "", AdminLegalEditorHandler(db), "")
	if initial.Code != http.StatusOK {
		t.Fatalf("load editor: %s", initial.Body.String())
	}
	var editor legalEditorResponse
	if err := json.Unmarshal(initial.Body.Bytes(), &editor); err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, section := range editor.Sections {
		if strings.Contains(section.Heading, "Artículo 7.") {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("article 7 not found")
	}
	updatedText := "El cliente recibe información clara sobre los medios de pago disponibles."
	updateJSON, _ := json.Marshal(gin.H{"base_sha256": editor.BaseSHA256, "revision": editor.Revision, "body": updatedText})
	saved := call(http.MethodPatch, fmt.Sprintf("/admin/legal/terms/draft/%d", index), string(updateJSON), AdminLegalArticleSaveHandler(db), fmt.Sprint(index))
	if saved.Code != http.StatusOK {
		t.Fatalf("save draft: %s", saved.Body.String())
	}
	var draft legalEditorResponse
	if err := json.Unmarshal(saved.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if !draft.Dirty || draft.Revision != 1 {
		t.Fatalf("draft revision: %+v", draft)
	}
	removedIndex := -1
	for i, section := range draft.Sections {
		if strings.Contains(section.Heading, "Artículo 8.") {
			removedIndex = i
			break
		}
	}
	if removedIndex < 0 {
		t.Fatal("article 8 not found")
	}
	removeJSON, _ := json.Marshal(gin.H{"base_sha256": draft.BaseSHA256, "revision": draft.Revision, "remove": true})
	removed := call(http.MethodPatch, fmt.Sprintf("/admin/legal/terms/draft/%d", removedIndex), string(removeJSON), AdminLegalArticleSaveHandler(db), fmt.Sprint(removedIndex))
	if removed.Code != http.StatusOK {
		t.Fatalf("remove article: %s", removed.Body.String())
	}
	if err := json.Unmarshal(removed.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Revision != 2 || strings.Contains(composeLegalSections(draft.Sections), "Artículo 8. Facturación") || !strings.Contains(composeLegalSections(draft.Sections), "Artículo 8. Compromiso con la confidencialidad") {
		t.Fatal("article removal did not renumber the remaining articles")
	}
	stale := call(http.MethodPatch, fmt.Sprintf("/admin/legal/terms/draft/%d", index), string(updateJSON), AdminLegalArticleSaveHandler(db), fmt.Sprint(index))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale edit status: %d", stale.Code)
	}
	before := call(http.MethodGet, "/legal/terms", "", PublicTermsHandler(db), "")
	if before.Code != http.StatusOK || bytes.Contains(before.Body.Bytes(), []byte(updatedText)) {
		t.Fatal("draft leaked to public terms")
	}
	publishJSON, _ := json.Marshal(gin.H{"base_sha256": draft.BaseSHA256, "revision": draft.Revision})
	published := call(http.MethodPost, "/admin/legal/terms/publish", string(publishJSON), AdminLegalPublishHandler(db), "")
	if published.Code != http.StatusOK {
		t.Fatalf("publish: %s", published.Body.String())
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM legal_documents WHERE slug='terms' AND published`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	after := call(http.MethodGet, "/legal/terms", "", PublicTermsHandler(db), "")
	if after.Code != http.StatusOK || !bytes.Contains(after.Body.Bytes(), []byte(updatedText)) || !bytes.Contains(after.Body.Bytes(), []byte(`"version":"1.1"`)) || bytes.Contains(after.Body.Bytes(), []byte("Artículo 8. Facturación")) {
		t.Fatalf("public terms after publication: %s", after.Body.String())
	}
	var oldPublished bool
	if err := pool.QueryRow(ctx, `SELECT published FROM legal_documents WHERE id=$1`, oldID).Scan(&oldPublished); err != nil || oldPublished {
		t.Fatalf("accepted version changed or disappeared: %v", err)
	}
}

func TestLegalPublicationWithoutDraftCreatesNewVersion(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	if _, err := database.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	db := &database.DB{Pool: pool}
	var oldID, newID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM legal_documents WHERE slug='terms' AND published`).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		defer tx.Rollback(ctx)
		if newID != 0 {
			if _, err := tx.Exec(ctx, `UPDATE legal_documents SET published=FALSE WHERE id=$1`, newID); err != nil {
				t.Error(err)
				return
			}
			if _, err := tx.Exec(ctx, `DELETE FROM legal_documents WHERE id=$1`, newID); err != nil {
				t.Error(err)
				return
			}
			if _, err := tx.Exec(ctx, `UPDATE legal_documents SET published=TRUE WHERE id=$1`, oldID); err != nil {
				t.Error(err)
				return
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM audit_logs WHERE actor_email='legal-version-test@selecto.test'`); err != nil {
			t.Error(err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			t.Error(err)
		}
	})
	call := func(method, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("email", "legal-version-test@selecto.test")
		handler(c)
		return recorder
	}
	loaded := call(http.MethodGet, "/admin/legal/terms/editor", "", AdminLegalEditorHandler(db))
	if loaded.Code != http.StatusOK {
		t.Fatalf("load editor: %s", loaded.Body.String())
	}
	var editor legalEditorResponse
	if err := json.Unmarshal(loaded.Body.Bytes(), &editor); err != nil {
		t.Fatal(err)
	}
	if editor.Dirty || editor.Revision != 0 {
		t.Fatalf("expected clean editor: %+v", editor)
	}
	request, _ := json.Marshal(gin.H{"base_sha256": editor.BaseSHA256, "revision": editor.Revision})
	published := call(http.MethodPost, "/admin/legal/terms/publish", string(request), AdminLegalPublishHandler(db))
	if published.Code != http.StatusOK {
		t.Fatalf("publish unchanged text: %s", published.Body.String())
	}
	var document publishedLegalDocument
	if err := json.Unmarshal(published.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != nextLegalVersion(editor.BaseVersion) || !strings.Contains(document.Body, "Artículo 46. Contacto oficial") {
		t.Fatalf("unexpected publication: %+v", document)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM legal_documents WHERE slug='terms' AND published`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	replayed := call(http.MethodPost, "/admin/legal/terms/publish", string(request), AdminLegalPublishHandler(db))
	if replayed.Code != http.StatusConflict {
		t.Fatalf("stale publication status: %d", replayed.Code)
	}
}
