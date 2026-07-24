package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileLibraryItemsHandler exposes the Desktop-facing, shared Mobile library.
// Documents and recordings keep their own storage/lifecycle records; this is a
// read-only projection that lets people discover both in one place.
//
//	GET /api/mobile/library/items
//	GET /api/mobile/library/items/{itemId}
func MobileLibraryItemsHandler(identity *auth.IdentityService) http.HandlerFunc {
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

		itemID := strings.TrimSpace(r.PathValue("itemId"))
		if itemID != "" {
			item, ok := mobileLibraryItemByID(ownerID, principal.TenantID, itemID)
			if !ok {
				writeError(w, http.StatusNotFound, "LIBRARY_ITEM_NOT_FOUND", "library item not found")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"item": item})
			return
		}

		includeDocuments, includeAudio := mobileLibraryRequestedTypes(r.URL.Query().Get("types"))
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 80)
		if limit > 200 {
			limit = 200
		}
		items := mobileLibraryItems(ownerID, principal.TenantID, includeDocuments, includeAudio)
		if len(items) > limit {
			items = items[:limit]
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
	}
}

func mobileLibraryRequestedTypes(raw string) (documents, audio bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return true, true
	}
	for _, part := range strings.Split(raw, ",") {
		switch strings.TrimSpace(part) {
		case "document", "documents":
			documents = true
		case "audio", "recording", "recordings":
			audio = true
		}
	}
	return documents, audio
}

func mobileLibraryItems(ownerID, tenantID string, includeDocuments, includeAudio bool) []map[string]any {
	items := make([]map[string]any, 0)
	if includeDocuments {
		// Take one immutable relationship snapshot before rendering every draft.
		// Calling mobileMeetingRecordingResultOwnerID per draft would repeatedly
		// lock and scan the recording map, which becomes noticeable in a large
		// shared library full of generated transcripts and minutes.
		resultOwners := mobileMeetingRecordingResultOwners(ownerID, tenantID)
		mobileDocuments.Lock()
		for _, draft := range mobileDocuments.drafts {
			if draft.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(tenantID, draft.TenantID) {
				items = append(items, mobileLibraryDocumentItemWithParent(draft, false, resultOwners[draft.ID]))
			}
		}
		mobileDocuments.Unlock()
	}
	if includeAudio {
		mobileMeetingRecordings.Lock()
		for _, recording := range mobileMeetingRecordings.items {
			if recording.OwnerID != ownerID || !mobileRecordingTenantMatches(&auth.ViewerPrincipal{TenantID: tenantID}, recording) || !mobileLibraryRecordingVisible(recording) {
				continue
			}
			items = append(items, mobileLibraryRecordingItem(recording))
		}
		mobileMeetingRecordings.Unlock()
	}
	sort.SliceStable(items, func(i, j int) bool {
		return stringFromAny(items[i]["updated_at"]) > stringFromAny(items[j]["updated_at"])
	})
	return items
}

func mobileLibraryItemByID(ownerID, tenantID, itemID string) (map[string]any, bool) {
	mobileMeetingRecordings.Lock()
	recording, recordingOK := mobileMeetingRecordings.items[itemID]
	mobileMeetingRecordings.Unlock()
	if recordingOK && recording.OwnerID == ownerID && mobileRecordingTenantMatches(&auth.ViewerPrincipal{TenantID: tenantID}, recording) && mobileLibraryRecordingVisible(recording) {
		return mobileLibraryRecordingItem(recording), true
	}

	mobileDocuments.Lock()
	draft, draftOK := mobileDocuments.drafts[itemID]
	mobileDocuments.Unlock()
	if draftOK && draft.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(tenantID, draft.TenantID) {
		return mobileLibraryDocumentItemWithParent(draft, true, mobileMeetingRecordingResultOwnerID(ownerID, tenantID, draft.ID)), true
	}
	return nil, false
}

func mobileLibraryDocumentItem(draft mobileDocumentDraftRecord, includeMarkdown bool) map[string]any {
	return mobileLibraryDocumentItemWithParent(draft, includeMarkdown, mobileMeetingRecordingResultOwnerID(draft.OwnerID, draft.TenantID, draft.ID))
}

func mobileLibraryDocumentItemWithParent(draft mobileDocumentDraftRecord, includeMarkdown bool, recordingID string) map[string]any {
	display := mobileDraftListPreviewMarkdown(draft)
	item := map[string]any{
		"id":         draft.ID,
		"type":       "document",
		"title":      draft.Title,
		"template":   draft.Template,
		"updated_at": draft.UpdatedAt.UTC().Format(time.RFC3339),
		"rune_count": utf8.RuneCountInString(display),
		"preview":    mobileClipRunes(display, 160),
	}
	if includeMarkdown {
		item["markdown"] = mobileDraftDisplayMarkdown(draft)
	}
	if mobileDraftHasOriginal(draft) {
		item["has_original"] = true
		item["source_filename"] = strings.TrimSpace(draft.SourceFilename)
		item["source_content_type"] = strings.TrimSpace(draft.SourceContentType)
		item["source_size"] = mobileDraftSourceSize(draft)
		item["source_download_url"] = "/api/mobile/documents/drafts/" + draft.ID + "/source"
	}
	// Generated meeting results share the document list with ordinary drafts.
	// Include their parent recording so clients can use the recording lifecycle
	// delete action instead of attempting to remove a protected result directly.
	if recordingID = strings.TrimSpace(recordingID); recordingID != "" {
		item["managed_by_recording_id"] = recordingID
	}
	return item
}

func mobileLibraryRecordingVisible(recording mobileMeetingRecording) bool {
	if recording.Status == "uploading" || recording.Status == "finalizing" || strings.TrimSpace(recording.Status) == "" {
		return false
	}
	if mobileMeetingRecordingAudioAvailable(recording) {
		return true
	}
	return strings.TrimSpace(recording.TranscriptDraftID) != "" || strings.TrimSpace(recording.MinutesDraftID) != ""
}

func mobileLibraryRecordingItem(recording mobileMeetingRecording) map[string]any {
	available := mobileMeetingRecordingAudioAvailable(recording)
	updated := recording.UpdatedAt.UTC().Format(time.RFC3339)
	if recording.UpdatedAt.IsZero() {
		updated = recording.CreatedAt.UTC().Format(time.RFC3339)
	}
	preview := strings.TrimSpace(recording.Purpose)
	if preview == "" {
		preview = strings.TrimSpace(recording.Message)
	}
	if preview == "" {
		preview = "Meeting recording"
	}
	item := map[string]any{
		"id":         recording.ID,
		"type":       "audio",
		"title":      mobileMeetingRecordingLibraryTitle(recording.Title),
		"updated_at": updated,
		"preview":    mobileClipRunes(preview, 160),
		"audio": map[string]any{
			"content_type": recording.ContentType,
			"size_bytes":   recording.SizeBytes,
			"duration_sec": recording.DurationSec,
			"available":    available,
		},
		"processing": map[string]any{
			"status":       recording.Status,
			"mode":         recording.ProcessMode,
			"progress":     recording.Progress,
			"message":      recording.Message,
			"failure_code": recording.FailureCode,
		},
		"derived_documents": map[string]any{
			"transcript_draft_id": recording.TranscriptDraftID,
			"minutes_draft_id":    recording.MinutesDraftID,
		},
	}
	if !recording.RetentionUntil.IsZero() {
		item["retention_until"] = recording.RetentionUntil.UTC().Format(time.RFC3339)
	}
	if available {
		item["audio"].(map[string]any)["download_url"] = "/api/mobile/meeting-recordings/" + recording.ID + "/audio"
	}
	return item
}

func mobileMeetingRecordingLibraryTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Meeting recording"
	}
	return title
}

// MobileMeetingRecordingAudioHandler streams a finalized recording only to its
// authenticated owner. http.ServeContent handles byte ranges needed by native
// audio players without exposing the Hub's filesystem path.
func MobileMeetingRecordingAudioHandler(identity *auth.IdentityService) http.HandlerFunc {
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
		recordingID := strings.TrimSpace(r.PathValue("recordingId"))
		recording, ok := mobileMeetingRecordingOwned(ownerID, recordingID)
		if !ok || !mobileRecordingTenantMatches(principal, recording) {
			writeError(w, http.StatusNotFound, "AUDIO_NOT_AVAILABLE", "meeting audio not found")
			return
		}
		if recording.Status == "uploading" || recording.Status == "finalizing" || strings.TrimSpace(recording.Dir) == "" {
			writeError(w, http.StatusNotFound, "AUDIO_NOT_AVAILABLE", "meeting audio is not available")
			return
		}
		path := filepath.Join(recording.Dir, meetingRecordingFilename(recording.ContentType))
		file, err := os.Open(path)
		if err != nil {
			writeError(w, http.StatusNotFound, "AUDIO_NOT_AVAILABLE", "meeting audio is not available")
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() || info.Size() <= 0 {
			writeError(w, http.StatusNotFound, "AUDIO_NOT_AVAILABLE", "meeting audio is not available")
			return
		}
		filename := mobileMeetingRecordingDownloadName(recording.Title, recording.ContentType)
		w.Header().Set("Content-Type", recording.ContentType)
		w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(filename))
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeContent(w, r, filename, info.ModTime(), file)
	}
}

func mobileRecordingTenantMatches(principal *auth.ViewerPrincipal, recording mobileMeetingRecording) bool {
	if principal == nil {
		return false
	}
	return mobileMeetingRecordingTenantMatches(principal.TenantID, recording.TenantID)
}

// mobileMeetingRecordingTenantMatches maps legacy blank tenant IDs to the
// default tenant. This preserves access to pre-tenant recordings without
// allowing a non-default tenant to claim those records.
func mobileMeetingRecordingTenantMatches(viewerTenantID, recordingTenantID string) bool {
	return mobileMeetingRecordingTenantID(viewerTenantID) == mobileMeetingRecordingTenantID(recordingTenantID)
}

func mobileMeetingRecordingTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

func mobileMeetingRecordingDownloadName(title, contentType string) string {
	title = strings.TrimSpace(filepath.Base(title))
	if title == "" || title == "." {
		title = "meeting-recording"
	}
	for _, old := range []string{"/", "\\", "\r", "\n", "\""} {
		title = strings.ReplaceAll(title, old, "_")
	}
	ext := filepath.Ext(meetingRecordingFilename(contentType))
	if strings.EqualFold(filepath.Ext(title), ext) {
		title = strings.TrimSuffix(title, filepath.Ext(title))
	}
	return title + ext
}
