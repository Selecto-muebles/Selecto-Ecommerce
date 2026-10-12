package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type legalBase struct {
	ID      int64
	Version string
	SHA256  string
	Body    string
}

func readLegalBase(c *gin.Context, tx pgx.Tx) (legalBase, error) {
	var base legalBase
	if _, err := tx.Exec(c, `SELECT pg_advisory_xact_lock(hashtextextended('legal:terms',0))`); err != nil {
		return base, err
	}
	err := tx.QueryRow(c, `SELECT id,version,body_sha256,body_markdown FROM legal_documents
		WHERE slug='terms' AND published FOR UPDATE`).Scan(&base.ID, &base.Version, &base.SHA256, &base.Body)
	return base, err
}

func readLegalEditor(c *gin.Context, tx pgx.Tx, base legalBase) (legalEditorResponse, error) {
	response := legalEditorResponse{
		BaseVersion: base.Version, BaseSHA256: base.SHA256,
		Sections: splitLegalSections(base.Body),
	}
	var data []byte
	var draftBaseID int64
	err := tx.QueryRow(c, `SELECT base_document_id,sections,revision FROM legal_term_drafts
		WHERE slug='terms' FOR UPDATE`).Scan(&draftBaseID, &data, &response.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		response.Revision = 0
		return response, nil
	}
	if err != nil {
		return legalEditorResponse{}, err
	}
	if draftBaseID != base.ID {
		return legalEditorResponse{}, errStaleLegalDraft
	}
	if err := json.Unmarshal(data, &response.Sections); err != nil {
		return legalEditorResponse{}, err
	}
	response.Dirty = true
	return response, nil
}

var errStaleLegalDraft = errors.New("legal draft belongs to another published version")

func AdminLegalEditorHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		base, err := readLegalBase(c, tx)
		if err != nil {
			handleAdminLookupErr(c, err, "published legal terms not found")
			return
		}
		response, err := readLegalEditor(c, tx, base)
		if errors.Is(err, errStaleLegalDraft) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "legal draft is outdated", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, response)
	}
}

type legalArticleUpdate struct {
	BaseSHA256 string `json:"base_sha256"`
	Revision   int64  `json:"revision"`
	Body       string `json:"body"`
}

func AdminLegalArticleSaveHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		index, err := strconv.Atoi(c.Param("index"))
		var input legalArticleUpdate
		if err != nil || index < 0 || c.ShouldBindJSON(&input) != nil || input.BaseSHA256 == "" || input.Revision < 0 {
			apperrors.BadRequest(c, "invalid article update")
			return
		}
		body := strings.TrimSpace(input.Body)
		if body == "" || len(body) > 16000 || legalHeadingPattern.MatchString(body) {
			apperrors.BadRequest(c, "article body must be nonempty and cannot contain headings")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		base, err := readLegalBase(c, tx)
		if err != nil {
			handleAdminLookupErr(c, err, "published legal terms not found")
			return
		}
		response, err := readLegalEditor(c, tx, base)
		if errors.Is(err, errStaleLegalDraft) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "legal draft is outdated", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if input.BaseSHA256 != base.SHA256 || input.Revision != response.Revision {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "legal terms changed; reload before editing", nil)
			return
		}
		if index >= len(response.Sections) || !response.Sections[index].Editable {
			apperrors.BadRequest(c, "only numbered articles can be edited")
			return
		}
		response.Sections[index].Body = "\n\n" + body + "\n\n"
		data, err := json.Marshal(response.Sections)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		response.Revision++
		_, err = tx.Exec(c, `INSERT INTO legal_term_drafts(slug,base_document_id,sections,revision,updated_by)
			VALUES('terms',$1,$2,$3,$4) ON CONFLICT(slug) DO UPDATE SET
			base_document_id=excluded.base_document_id,sections=excluded.sections,revision=excluded.revision,
			updated_by=excluded.updated_by,updated_at=NOW()`, base.ID, data, response.Revision, adminActor(c))
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if err := writeAuditTx(c, tx, adminActor(c), "legal_article_draft_saved", "legal_document", int(base.ID), gin.H{"article": response.Sections[index].Heading, "revision": response.Revision}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		response.Dirty = true
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, response)
	}
}

type legalPublishInput struct {
	BaseSHA256 string `json:"base_sha256"`
	Revision   int64  `json:"revision"`
}

func AdminLegalPublishHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input legalPublishInput
		if c.ShouldBindJSON(&input) != nil || input.BaseSHA256 == "" || input.Revision < 1 {
			apperrors.BadRequest(c, "invalid publication request")
			return
		}
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		base, err := readLegalBase(c, tx)
		if err != nil {
			handleAdminLookupErr(c, err, "published legal terms not found")
			return
		}
		response, err := readLegalEditor(c, tx, base)
		if errors.Is(err, errStaleLegalDraft) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "legal draft is outdated", nil)
			return
		}
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if !response.Dirty || input.BaseSHA256 != base.SHA256 || input.Revision != response.Revision {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "legal draft changed; reload before publishing", nil)
			return
		}
		version := nextLegalVersion(base.Version)
		body := composeLegalSections(response.Sections)
		body = legalVersionPattern.ReplaceAllString(body, "**Versión:** "+version+"  ")
		sha := legalDigest(body)
		if sha == base.SHA256 {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "legal draft has no changes", nil)
			return
		}
		if _, err := tx.Exec(c, `UPDATE legal_documents SET published=FALSE WHERE id=$1`, base.ID); err != nil {
			apperrors.Internal(c)
			return
		}
		var published publishedLegalDocument
		err = tx.QueryRow(c, `INSERT INTO legal_documents(slug,version,title,body_markdown,body_sha256,published,effective_at)
			VALUES('terms',$1,'Términos y Condiciones Generales y Políticas Comerciales',$2,$3,TRUE,$4)
			RETURNING version,title,body_markdown,body_sha256,effective_at`, version, body, sha, time.Now().UTC()).Scan(
			&published.Version, &published.Title, &published.Body, &published.SHA256, &published.EffectiveAt)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, `DELETE FROM legal_term_drafts WHERE slug='terms'`); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := writeAuditTx(c, tx, adminActor(c), "legal_terms_published", "legal_document", int(base.ID), gin.H{"previous_version": base.Version, "version": version, "sha256": sha}); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, published)
	}
}

func nextLegalVersion(current string) string {
	parts := strings.Split(current, ".")
	if len(parts) == 2 {
		if minor, err := strconv.Atoi(parts[1]); err == nil {
			return parts[0] + "." + strconv.Itoa(minor+1)
		}
	}
	return current + ".1"
}
