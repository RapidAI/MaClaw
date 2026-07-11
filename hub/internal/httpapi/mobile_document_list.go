package httpapi

import (
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileDocumentDraftsListHandler lists emergency drafts for the current viewer.
// Shared by Mobile and desktop GUI (same Hub document library).
//
//	GET /api/mobile/documents/drafts
//	GET /api/mobile/documents/drafts/{draftId}
func MobileDocumentDraftsListHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		ownerID := mobilePrincipalOwnerID(principal)
		if ownerID == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer identity missing")
			return
		}

		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID != "" {
			mobileDocuments.Lock()
			record, ok := mobileDocuments.drafts[draftID]
			mobileDocuments.Unlock()
			if !ok || record.OwnerID != ownerID {
				writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"draft": mobileDocumentDraftPayload(record),
			})
			return
		}

		// List: omit full markdown by default; include preview + size for GUI top-bar.
		includeBody := strings.EqualFold(r.URL.Query().Get("include_body"), "1") ||
			strings.EqualFold(r.URL.Query().Get("include_body"), "true")
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
		if limit > 200 {
			limit = 200
		}

		mobileDocuments.Lock()
		items := make([]mobileDocumentDraftRecord, 0)
		for _, rec := range mobileDocuments.drafts {
			if rec.OwnerID != ownerID {
				continue
			}
			items = append(items, rec)
		}
		mobileDocuments.Unlock()

		sort.SliceStable(items, func(i, j int) bool {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		})
		if len(items) > limit {
			items = items[:limit]
		}

		out := make([]map[string]any, 0, len(items))
		for _, rec := range items {
			updated := ""
			if !rec.UpdatedAt.IsZero() {
				updated = rec.UpdatedAt.UTC().Format(time.RFC3339)
			}
			item := map[string]any{
				"id":         rec.ID,
				"title":      rec.Title,
				"template":   rec.Template,
				"updated_at": updated,
				"rune_count": utf8.RuneCountInString(rec.Markdown),
				"preview":    mobileClipRunes(rec.Markdown, 160),
			}
			if n := len(rec.SourceBytes); n > 0 {
				item["has_original"] = true
				item["source_filename"] = strings.TrimSpace(rec.SourceFilename)
				item["source_content_type"] = strings.TrimSpace(rec.SourceContentType)
				item["source_size"] = n
				item["source_download_url"] = "/api/mobile/documents/drafts/" + rec.ID + "/source"
			} else {
				item["has_original"] = false
			}
			if includeBody {
				item["markdown"] = rec.Markdown
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"drafts": out,
			"count":  len(out),
		})
	}
}

// MobileDocumentDraftSourceHandler streams the original uploaded file for a draft
// so Mobile can share the real document (WeChat etc.) and AI pipelines can fetch
// the source of truth.
func MobileDocumentDraftSourceHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != ownerID || len(record.SourceBytes) == 0 {
			writeError(w, http.StatusNotFound, "DRAFT_SOURCE_NOT_FOUND", "original file not found for this draft")
			return
		}
		contentType := strings.TrimSpace(record.SourceContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		filename := strings.TrimSpace(record.SourceFilename)
		if filename == "" {
			filename = filepath.Base(record.Title)
			if filename == "" || filename == "." {
				filename = draftID
			}
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(record.SourceBytes)))
		w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(filename)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(record.SourceBytes)
	}
}
