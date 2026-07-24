package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMobileLibraryListsOwnerAudioAndStreamsRange(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enrollment := issueViewerToken(t, identity, "library-owner@example.com")
	foreignToken, _ := issueViewerToken(t, identity, "library-foreign@example.com")
	dir := t.TempDir()
	audio := []byte("0123456789")
	if err := os.WriteFile(dir+"/recording.m4a", audio, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	recording := mobileMeetingRecording{
		ID: "meeting-library-audio", OwnerID: enrollment.UserID, TenantID: enrollment.TenantID,
		Title: "Quarterly review", Dir: dir, ContentType: "audio/mp4", Status: "uploaded",
		SizeBytes: int64(len(audio)), DurationSec: 42, CreatedAt: now, UpdatedAt: now,
		RetentionUntil: now.Add(24 * time.Hour),
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[recording.ID] = recording
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, recording.ID)
		mobileMeetingRecordings.Unlock()
	})

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/library/items?types=audio", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRec := httptest.NewRecorder()
	MobileLibraryItemsHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), `"type":"audio"`) || !strings.Contains(listRec.Body.String(), recording.ID) {
		t.Fatalf("list=%d %s", listRec.Code, listRec.Body.String())
	}

	foreignReq := httptest.NewRequest(http.MethodGet, "/api/mobile/meeting-recordings/"+recording.ID+"/audio", nil)
	foreignReq.SetPathValue("recordingId", recording.ID)
	foreignReq.Header.Set("Authorization", "Bearer "+foreignToken)
	foreignRec := httptest.NewRecorder()
	MobileMeetingRecordingAudioHandler(identity).ServeHTTP(foreignRec, foreignReq)
	if foreignRec.Code != http.StatusNotFound {
		t.Fatalf("foreign stream=%d %s", foreignRec.Code, foreignRec.Body.String())
	}

	streamReq := httptest.NewRequest(http.MethodGet, "/api/mobile/meeting-recordings/"+recording.ID+"/audio", nil)
	streamReq.SetPathValue("recordingId", recording.ID)
	streamReq.Header.Set("Authorization", "Bearer "+token)
	streamReq.Header.Set("Range", "bytes=2-5")
	streamRec := httptest.NewRecorder()
	MobileMeetingRecordingAudioHandler(identity).ServeHTTP(streamRec, streamReq)
	if streamRec.Code != http.StatusPartialContent || streamRec.Body.String() != "2345" {
		t.Fatalf("range=%d body=%q headers=%v", streamRec.Code, streamRec.Body.String(), streamRec.Header())
	}
}

func TestMobileLibraryKeepsExpiredResultsButHidesOrphanedAudio(t *testing.T) {
	owner := "library-owner"
	now := time.Now().UTC()
	withResults := mobileMeetingRecording{ID: "meeting-expired-with-results", OwnerID: owner, Status: "ready", MinutesDraftID: "mobdoc_minutes", CreatedAt: now, UpdatedAt: now}
	// Stale draft ID but no document row → treated as orphan and hidden.
	staleIDs := mobileMeetingRecording{ID: "meeting-expired-stale-ids", OwnerID: owner, Status: "ready", MinutesDraftID: "mobdoc_missing", CreatedAt: now, UpdatedAt: now}
	orphaned := mobileMeetingRecording{ID: "meeting-expired-orphan", OwnerID: owner, Status: "ready", CreatedAt: now, UpdatedAt: now}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[withResults.ID] = withResults
	mobileMeetingRecordings.items[staleIDs.ID] = staleIDs
	mobileMeetingRecordings.items[orphaned.ID] = orphaned
	mobileMeetingRecordings.Unlock()
	mobileDocuments.Lock()
	mobileDocuments.drafts[withResults.MinutesDraftID] = mobileDocumentDraftRecord{ID: withResults.MinutesDraftID, OwnerID: owner, UpdatedAt: now}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, withResults.ID)
		delete(mobileMeetingRecordings.items, staleIDs.ID)
		delete(mobileMeetingRecordings.items, orphaned.ID)
		mobileMeetingRecordings.Unlock()
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, withResults.MinutesDraftID)
		mobileDocuments.Unlock()
	})

	// Audio-only list uses a dedicated draft-ID snapshot.
	items := mobileLibraryItems(owner, "", false, true)
	joined := ""
	for _, item := range items {
		joined += stringFromAny(item["id"]) + "\n"
	}
	if !strings.Contains(joined, withResults.ID) || strings.Contains(joined, orphaned.ID) || strings.Contains(joined, staleIDs.ID) {
		t.Fatalf("unexpected audio-only items: %q", joined)
	}
	// Full library list reuses the document scan for the draft-ID set (no second pass bug).
	full := mobileLibraryItems(owner, "", true, true)
	joined = ""
	for _, item := range full {
		joined += stringFromAny(item["id"]) + "\n"
	}
	if !strings.Contains(joined, withResults.ID) ||
		!strings.Contains(joined, withResults.MinutesDraftID) ||
		strings.Contains(joined, orphaned.ID) ||
		strings.Contains(joined, staleIDs.ID) {
		t.Fatalf("unexpected full-library items: %q", joined)
	}
}

func TestMobileLibraryLabelsGeneratedMeetingResultsWithParentRecording(t *testing.T) {
	owner := "library-meeting-result-owner"
	recording := mobileMeetingRecording{ID: "meeting-parent", OwnerID: owner, Status: "ready", MinutesDraftID: "meeting-minutes"}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[recording.ID] = recording
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, recording.ID)
		mobileMeetingRecordings.Unlock()
	})

	item := mobileLibraryDocumentItem(mobileDocumentDraftRecord{ID: recording.MinutesDraftID, OwnerID: owner}, false)
	if item["managed_by_recording_id"] != recording.ID {
		t.Fatalf("managed parent=%#v", item)
	}
}

func TestMobileLibraryListsMultipleGeneratedResultsWithTheirParents(t *testing.T) {
	owner := "library-meeting-result-list-owner"
	now := time.Now().UTC()
	first := mobileMeetingRecording{ID: "meeting-parent-a", OwnerID: owner, Status: "ready", TranscriptDraftID: "meeting-transcript-a"}
	second := mobileMeetingRecording{ID: "meeting-parent-b", OwnerID: owner, Status: "ready", MinutesDraftID: "meeting-minutes-b"}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[first.ID] = first
	mobileMeetingRecordings.items[second.ID] = second
	mobileMeetingRecordings.Unlock()
	mobileDocuments.Lock()
	mobileDocuments.drafts[first.TranscriptDraftID] = mobileDocumentDraftRecord{ID: first.TranscriptDraftID, OwnerID: owner, UpdatedAt: now}
	mobileDocuments.drafts[second.MinutesDraftID] = mobileDocumentDraftRecord{ID: second.MinutesDraftID, OwnerID: owner, UpdatedAt: now}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, first.ID)
		delete(mobileMeetingRecordings.items, second.ID)
		mobileMeetingRecordings.Unlock()
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, first.TranscriptDraftID)
		delete(mobileDocuments.drafts, second.MinutesDraftID)
		mobileDocuments.Unlock()
	})

	items := mobileLibraryItems(owner, "", true, false)
	parents := map[string]string{}
	for _, item := range items {
		parents[stringFromAny(item["id"])] = stringFromAny(item["managed_by_recording_id"])
	}
	if parents[first.TranscriptDraftID] != first.ID || parents[second.MinutesDraftID] != second.ID {
		t.Fatalf("result parents = %#v", parents)
	}
}

func TestMobileLibraryRecordingVisibleAfterAudioDelete(t *testing.T) {
	owner := "library-visible-owner"
	if mobileLibraryRecordingVisible(mobileMeetingRecording{Status: "uploading"}, nil) {
		t.Fatal("incomplete upload must not be visible")
	}
	// No audio, no derived docs → orphan, hidden (desktop delete does not need GET).
	if mobileLibraryRecordingVisible(mobileMeetingRecording{Status: "ready"}, nil) {
		t.Fatal("audio-less orphan without results must be hidden")
	}
	// Stale draft IDs with no actual documents must not keep a ghost row.
	if mobileLibraryRecordingVisible(mobileMeetingRecording{
		Status:         "ready",
		OwnerID:        owner,
		MinutesDraftID: "mobdoc_missing_minutes",
		Message:        "raw audio deleted; transcript and minutes remain available",
	}, map[string]struct{}{}) {
		t.Fatal("audio-deleted recording with stale draft IDs must be hidden")
	}
	// Audio deleted/expired but minutes document still exists → still listed.
	existing := map[string]struct{}{"mobdoc_minutes": {}, "mobdoc_transcript": {}}
	if !mobileLibraryRecordingVisible(mobileMeetingRecording{
		Status:         "ready",
		OwnerID:        owner,
		MinutesDraftID: "mobdoc_minutes",
		Message:        "raw audio deleted; transcript and minutes remain available",
	}, existing) {
		t.Fatal("audio-deleted recording with minutes must remain visible")
	}
	if !mobileLibraryRecordingVisible(mobileMeetingRecording{
		Status:            "ready",
		OwnerID:           owner,
		TranscriptDraftID: "mobdoc_transcript",
	}, existing) {
		t.Fatal("audio-deleted recording with transcript must remain visible")
	}
}

func TestMobileLibraryItemByIDAfterAudioDeleteWithMinutes(t *testing.T) {
	owner := "library-audio-delete-owner"
	tenant := "tenant-audio-delete"
	rec := mobileMeetingRecording{
		ID:             "meeting-audio-delete-with-minutes",
		OwnerID:        owner,
		TenantID:       tenant,
		Title:          "Has minutes",
		Status:         "ready",
		Message:        "raw audio deleted; transcript and minutes remain available",
		MinutesDraftID: "mobdoc_minutes_1",
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	mobileDocuments.Lock()
	mobileDocuments.drafts[rec.MinutesDraftID] = mobileDocumentDraftRecord{ID: rec.MinutesDraftID, OwnerID: owner, TenantID: tenant, UpdatedAt: time.Now().UTC()}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, rec.MinutesDraftID)
		mobileDocuments.Unlock()
	})

	item, ok := mobileLibraryItemByID(owner, tenant, rec.ID)
	if !ok {
		t.Fatal("audio-deleted recording with minutes must remain a library item")
	}
	audio, _ := item["audio"].(map[string]any)
	if available, _ := audio["available"].(bool); available {
		t.Fatalf("expected unavailable audio: %#v", item)
	}
	if item["type"] != "audio" || item["id"] != rec.ID {
		t.Fatalf("item = %#v", item)
	}
}

func TestMobileLibraryItemByIDHidesStaleAudioDeletedRecording(t *testing.T) {
	owner := "library-audio-delete-stale-owner"
	tenant := "tenant-audio-delete-stale"
	rec := mobileMeetingRecording{
		ID:             "meeting-audio-delete-stale",
		OwnerID:        owner,
		TenantID:       tenant,
		Title:          "Stale minutes id",
		Status:         "ready",
		Message:        "raw audio deleted; transcript and minutes remain available",
		MinutesDraftID: "mobdoc_minutes_missing",
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})

	if _, ok := mobileLibraryItemByID(owner, tenant, rec.ID); ok {
		t.Fatal("audio-deleted recording with missing minutes draft must not be a library item")
	}
}

func TestMobileLibraryKeepsAudioTenantIsolatedAndOmitsZeroRetention(t *testing.T) {
	owner := "library-tenant-owner"
	now := time.Now().UTC()
	first := mobileMeetingRecording{ID: "meeting-tenant-a", OwnerID: owner, TenantID: "tenant-a", Title: "", ContentType: "audio/mp4", Status: "uploaded", Dir: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	second := mobileMeetingRecording{ID: "meeting-tenant-b", OwnerID: owner, TenantID: "tenant-b", ContentType: "audio/mp4", Status: "uploaded", Dir: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	for _, recording := range []mobileMeetingRecording{first, second} {
		if err := os.WriteFile(recording.Dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[first.ID] = first
	mobileMeetingRecordings.items[second.ID] = second
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, first.ID)
		delete(mobileMeetingRecordings.items, second.ID)
		mobileMeetingRecordings.Unlock()
	})

	items := mobileLibraryItems(owner, "tenant-a", false, true)
	if len(items) != 1 || items[0]["id"] != first.ID {
		t.Fatalf("tenant isolation failed: %#v", items)
	}
	if item := mobileLibraryRecordingItem(first); item["title"] != "Meeting recording" {
		t.Fatalf("fallback title=%q", item["title"])
	} else if _, ok := item["retention_until"]; ok {
		t.Fatalf("zero retention must be omitted: %#v", item)
	}
}

func TestMobileMeetingRecordingOwnedForTenantRejectsTenantMismatch(t *testing.T) {
	recording := mobileMeetingRecording{ID: "meeting-cross-tenant-operation", OwnerID: "shared-owner", TenantID: "tenant-a"}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[recording.ID] = recording
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, recording.ID)
		mobileMeetingRecordings.Unlock()
	})

	if _, ok := mobileMeetingRecordingOwnedForTenant(recording.OwnerID, "tenant-b", recording.ID); ok {
		t.Fatal("recording must not be accessible through a different tenant")
	}
	if _, ok := mobileMeetingRecordingOwnedForTenant(recording.OwnerID, "tenant-a", recording.ID); !ok {
		t.Fatal("recording must remain accessible to its tenant")
	}
}
