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
	orphaned := mobileMeetingRecording{ID: "meeting-expired-orphan", OwnerID: owner, Status: "ready", CreatedAt: now, UpdatedAt: now}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[withResults.ID] = withResults
	mobileMeetingRecordings.items[orphaned.ID] = orphaned
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, withResults.ID)
		delete(mobileMeetingRecordings.items, orphaned.ID)
		mobileMeetingRecordings.Unlock()
	})

	items := mobileLibraryItems(owner, "", false, true)
	joined := ""
	for _, item := range items {
		joined += stringFromAny(item["id"]) + "\n"
	}
	if !strings.Contains(joined, withResults.ID) || strings.Contains(joined, orphaned.ID) {
		t.Fatalf("unexpected items: %q", joined)
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
