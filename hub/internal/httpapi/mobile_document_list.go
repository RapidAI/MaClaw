package httpapi

import (
	"net/http"
	"path/filepath"
	"sort"
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
			// Snapshot under lock, then heavy re-extract / PDF parse outside the lock.
			mobileDocuments.Lock()
			record, ok := mobileDocuments.drafts[draftID]
			repaired := false
			if ok && mobileDraftRepairSourceMeta(&record) {
				mobileDocuments.drafts[draftID] = record
				repaired = true
			}
			mobileDocuments.Unlock()
			if !ok || record.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, record.TenantID) {
				if repaired {
					mobilePersistState()
				}
				writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
				return
			}

			heal := mobileDraftHealMarkdownOutsideLock(record)
			display := heal.Display
			if heal.ShouldPersist {
				healPersisted := false
				mobileDocumentQuotaAdmissionMu.Lock()
				mobileDocuments.Lock()
				cur, exists := mobileDocuments.drafts[draftID]
				admitOwner := exists && cur.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(principal.TenantID, cur.TenantID)
				mobileDocuments.Unlock()
				additionalBytes := int64(0)
				if admitOwner {
					additionalBytes = mobileDraftHealQuotaDelta(cur, heal)
				}
				quotaOK := additionalBytes <= 0 || mobileCheckDocumentQuotaForPrincipal(r.Context(), principal, additionalBytes) == nil
				mobileDocuments.Lock()
				if quotaOK {
					cur, exists = mobileDocuments.drafts[draftID]
				}
				if quotaOK && exists && cur.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(principal.TenantID, cur.TenantID) {
					if mobileDraftRepairSourceMeta(&cur) {
						repaired = true
					}
					// Hot-cache only; never pin multi-MB originals on the draft record.
					if len(record.SourceBytes) > 0 && len(cur.SourceBytes) == 0 &&
						len(record.SourceBytes) <= mobileDocumentSourceHotCacheMax {
						cur.SourceBytes = record.SourceBytes
					}
					if mobileDraftApplyHealed(&cur, heal) {
						mobileDocuments.drafts[draftID] = cur
						healPersisted = true
						record = cur
						display = strings.TrimSpace(cur.Markdown)
						if display == "" {
							display = heal.Display
						}
						repaired = true
					} else {
						// Another writer already fixed it — use latest stored body.
						// Never re-extract under the documents lock.
						// If stored is still over-spaced PDF / missing image meta display,
						// keep the heal result computed outside the lock.
						record = cur
						md := strings.TrimSpace(cur.Markdown)
						useStored := md != "" && !mobileDraftRecordBodyUnreadable(cur, md) &&
							!(mobileDraftSourceIsPDF(cur) && mobilePDFTextLooksOverSpaced(md))
						if useStored {
							display = md
						}
						// Prefer live Images meta even when markdown display stays from heal.
					}
				}
				mobileDocuments.Unlock()
				mobileDocumentQuotaAdmissionMu.Unlock()
				if !healPersisted && len(heal.Images) > 0 {
					// Extraction wrote immutable image blobs before admission. Reclaim them
					// if quota rejected the heal or its draft disappeared concurrently.
					mobileDraftDeleteImages(heal.Images)
				}
				if !quotaOK {
					display = strings.TrimSpace(record.Markdown)
				}
			}
			if repaired {
				// Drop dead blob paths / persist healed markdown.
				mobilePersistState()
			}
			payload := mobileDocumentDraftPayloadWithMarkdown(record, display)
			mobileDocumentDraftAddMeetingRecordingParent(payload, record)
			writeJSON(w, http.StatusOK, map[string]any{
				"draft": payload,
			})
			return
		}

		// List: omit full markdown by default; include preview + size for GUI top-bar.
		// Cheap path only — never run PDF extract under the global documents lock.
		includeBody := strings.EqualFold(r.URL.Query().Get("include_body"), "1") ||
			strings.EqualFold(r.URL.Query().Get("include_body"), "true")
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
		if limit > 200 {
			limit = 200
		}

		// Generated results are owned by their meeting recording. Snapshot that
		// relationship once so every list item can expose the parent without
		// repeated recording-map scans while rendering a large document library.
		resultOwners := mobileMeetingRecordingResultOwners(ownerID, principal.TenantID)
		mobileDocuments.Lock()
		items := make([]mobileDocumentDraftRecord, 0)
		repaired := false
		for id, rec := range mobileDocuments.drafts {
			if rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, rec.TenantID) {
				continue
			}
			if mobileDraftRepairSourceMeta(&rec) {
				mobileDocuments.drafts[id] = rec
				repaired = true
			}
			items = append(items, rec)
		}
		mobileDocuments.Unlock()
		if repaired {
			mobilePersistState()
		}

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
			// Lock-safe preview: original-only notice for garbage, never re-parse PDFs here.
			display := mobileDraftListPreviewMarkdown(rec)
			item := map[string]any{
				"id":         rec.ID,
				"title":      rec.Title,
				"template":   rec.Template,
				"updated_at": updated,
				"rune_count": utf8.RuneCountInString(display),
				"preview":    mobileClipRunes(display, 160),
			}
			if mobileDraftHasOriginal(rec) {
				item["has_original"] = true
				item["source_filename"] = strings.TrimSpace(rec.SourceFilename)
				item["source_content_type"] = strings.TrimSpace(rec.SourceContentType)
				item["source_size"] = mobileDraftOriginalSize(rec)
				item["source_storage_size"] = mobileDraftSourceSize(rec)
				item["source_download_url"] = "/api/mobile/documents/drafts/" + rec.ID + "/source"
			} else {
				item["has_original"] = false
			}
			if includeBody {
				item["markdown"] = display
			}
			if recordingID := strings.TrimSpace(resultOwners[rec.ID]); recordingID != "" {
				item["managed_by_recording_id"] = recordingID
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"drafts": out,
			"count":  len(out),
		})
	}
}

func mobileDocumentDraftAddMeetingRecordingParent(payload map[string]any, record mobileDocumentDraftRecord) {
	if payload == nil {
		return
	}
	if recordingID := mobileMeetingRecordingResultOwnerID(record.OwnerID, record.TenantID, record.ID); recordingID != "" {
		payload["managed_by_recording_id"] = recordingID
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
		repaired := false
		if ok && mobileDraftRepairSourceMeta(&record) {
			mobileDocuments.drafts[draftID] = record
			repaired = true
		}
		// Snapshot for streaming outside the lock. Prefer disk path: skip cloning
		// SourceBytes when a blob path is present (avoids extra multi-MB copy).
		path := record.SourcePath
		var mem []byte
		if strings.TrimSpace(path) == "" && len(record.SourceBytes) > 0 {
			mem = append([]byte(nil), record.SourceBytes...)
		}
		contentType := record.SourceContentType
		filename := record.SourceFilename
		title := record.Title
		owner := record.OwnerID
		tenantID := record.TenantID
		hasOrig := mobileDraftHasOriginal(record)
		mobileDocuments.Unlock()
		if repaired {
			mobilePersistState()
		}
		if !ok || owner != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, tenantID) || !hasOrig {
			writeError(w, http.StatusNotFound, "DRAFT_SOURCE_NOT_FOUND", "original file not found for this draft")
			return
		}
		if filename == "" {
			filename = filepath.Base(title)
			if filename == "" || filename == "." {
				filename = draftID
			}
		}
		if !mobileWriteStoredOriginalHTTP(w, contentType, filename, mem, path, record.SourceEncoding, mobileDraftOriginalSize(record)) {
			// Clear stale meta only when the blob is confirmed missing — not during
			// store outages or open errors where the file may still exist.
			if mobileShouldClearSourceMetaAfterStreamFail(path) {
				mobileDocuments.Lock()
				if rec, exists := mobileDocuments.drafts[draftID]; exists && rec.OwnerID == owner && mobileMeetingRecordingTenantMatches(principal.TenantID, rec.TenantID) {
					if len(rec.SourceBytes) == 0 {
						mobileClearDraftSourceMeta(&rec)
						mobileDocuments.drafts[draftID] = rec
						mobileDocuments.Unlock()
						mobilePersistState()
					} else {
						mobileDocuments.Unlock()
					}
				} else {
					mobileDocuments.Unlock()
				}
			}
			writeError(w, http.StatusNotFound, "DRAFT_SOURCE_NOT_FOUND", "original file not found for this draft")
		}
	}
}
