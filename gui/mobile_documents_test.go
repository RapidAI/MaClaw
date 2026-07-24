package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func newMobileDocumentsTestApp(serverURL string) *App {
	return &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      serverURL + "/",
			RemoteViewerToken: "viewer-token",
		},
	}
}

func TestListMobileLibraryItemsUsesAuthenticatedBoundedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/library/items" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("limit") != "200" {
			t.Fatalf("limit = %q, want 200", r.URL.Query().Get("limit"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"id":"recording-1","type":"audio","title":"Review","audio":{"available":true}}]}`)
	}))
	defer server.Close()

	items, err := newMobileDocumentsTestApp(server.URL).ListMobileLibraryItems(999)
	if err != nil {
		t.Fatalf("ListMobileLibraryItems: %v", err)
	}
	if len(items) != 1 || items[0].ID != "recording-1" || items[0].Type != "audio" || items[0].Audio == nil || !items[0].Audio.Available {
		t.Fatalf("items = %#v", items)
	}
}

func TestProcessMobileMeetingRecordingRequestsMinutesAndReloadsLibraryItem(t *testing.T) {
	var processCalls, getCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/meeting-recordings/recording-2/process":
			processCalls++
			if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("content type = %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `{"mode":"minutes"}` {
				t.Fatalf("process body = %s", body)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/library/items/recording-2":
			getCalls++
			_, _ = io.WriteString(w, `{"item":{"id":"recording-2","type":"audio","processing":{"status":"processing"}}}`)
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item, err := newMobileDocumentsTestApp(server.URL).ProcessMobileMeetingRecording("recording-2")
	if err != nil {
		t.Fatalf("ProcessMobileMeetingRecording: %v", err)
	}
	if processCalls != 1 || getCalls != 1 || item == nil || item.Processing == nil || item.Processing.Status != "processing" {
		t.Fatalf("process=%d get=%d item=%#v", processCalls, getCalls, item)
	}
}

func TestFetchMobileMeetingRecordingAudioRejectsOversizedResponseBeforeReading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mobile/meeting-recordings/recording-3/audio" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Length", "1025")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ignored")
	}))
	defer server.Close()

	_, _, _, err := newMobileDocumentsTestApp(server.URL).fetchMobileMeetingRecordingAudio("recording-3", 1024)
	if err == nil || !strings.Contains(err.Error(), "exceeds the 0 MB desktop limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestDeleteMobileMeetingRecordingDeletesOnlyOriginalAudio(t *testing.T) {
	var deleteCalls, getCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/mobile/meeting-recordings/recording-4/audio":
			deleteCalls++
			// Match Hub's mobileMeetingRecordingPayload after DELETE /audio.
			_, _ = io.WriteString(w, `{"recording_id":"recording-4","title":"Standup","status":"ready","message":"raw audio deleted; transcript and minutes remain available","audio_available":true,"duration_sec":12.5,"size_bytes":4096,"minutes_draft_id":"minutes-4","retention_until":"2026-08-22T13:36:00Z","updated_at":"2026-07-25T01:40:00Z"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/library/items/recording-4":
			getCalls++
			t.Fatal("mapped DELETE body must not fall back to library GET")
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item, err := newMobileDocumentsTestApp(server.URL).DeleteMobileMeetingRecording("recording-4")
	if err != nil {
		t.Fatalf("DeleteMobileMeetingRecording: %v", err)
	}
	// audio_available in the body is ignored: successful DELETE forces unavailable.
	if deleteCalls != 1 || getCalls != 0 || item == nil || item.ID != "recording-4" || item.Type != "audio" || item.Audio == nil || item.Audio.Available || item.DerivedDocuments == nil || item.DerivedDocuments.MinutesDraftID != "minutes-4" {
		t.Fatalf("delete=%d get=%d item=%#v", deleteCalls, getCalls, item)
	}
	if item.Processing == nil || !strings.Contains(item.Processing.Message, "raw audio deleted") {
		t.Fatalf("processing message missing: %#v", item.Processing)
	}
	if item.RetentionUntil != "2026-08-22T13:36:00Z" {
		t.Fatalf("retention_until = %q", item.RetentionUntil)
	}
}

func TestDeleteMobileMeetingRecordingFallsBackToGetWhenBodyUnusable(t *testing.T) {
	var deleteCalls, getCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/mobile/meeting-recordings/recording-4b/audio":
			deleteCalls++
			// Empty body (legacy / intermediary): map fails → GET.
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/library/items/recording-4b":
			getCalls++
			_, _ = io.WriteString(w, `{"item":{"id":"recording-4b","type":"audio","title":"From library","audio":{"available":true},"derived_documents":{"minutes_draft_id":"minutes-new"}}}`)
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item, err := newMobileDocumentsTestApp(server.URL).DeleteMobileMeetingRecording("recording-4b")
	if err != nil {
		t.Fatalf("DeleteMobileMeetingRecording: %v", err)
	}
	if deleteCalls != 1 || getCalls != 1 || item == nil || item.Title != "From library" || item.Audio == nil || item.Audio.Available || item.DerivedDocuments == nil || item.DerivedDocuments.MinutesDraftID != "minutes-new" {
		t.Fatalf("delete=%d get=%d item=%#v", deleteCalls, getCalls, item)
	}
}

func TestDeleteMobileMeetingRecordingStubsWhenBodyAndGetFail(t *testing.T) {
	var deleteCalls, getCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/mobile/meeting-recordings/recording-4c/audio":
			deleteCalls++
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/library/items/recording-4c":
			getCalls++
			http.Error(w, `{"code":"LIBRARY_ITEM_NOT_FOUND","message":"library item not found"}`, http.StatusNotFound)
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item, err := newMobileDocumentsTestApp(server.URL).DeleteMobileMeetingRecording("recording-4c")
	if err != nil {
		t.Fatalf("DeleteMobileMeetingRecording: %v", err)
	}
	if deleteCalls != 1 || getCalls != 1 || item == nil || item.ID != "recording-4c" || item.Type != "audio" || item.Audio == nil || item.Audio.Available {
		t.Fatalf("delete=%d get=%d item=%#v", deleteCalls, getCalls, item)
	}
	if item.Processing == nil || !strings.Contains(item.Processing.Message, "raw audio deleted") {
		t.Fatalf("stub message missing: %#v", item.Processing)
	}
}

func TestMobileLibraryItemFromMeetingRecordingPayloadOmitsZeroRetention(t *testing.T) {
	item, ok := mobileLibraryItemFromMeetingRecordingPayload("fallback-id", []byte(`{
		"recording_id":"rec-z","title":"T","audio_available":true,
		"retention_until":"0001-01-01T00:00:00Z","transcript_draft_id":"tr-1"
	}`))
	if !ok || item == nil || item.ID != "rec-z" || item.RetentionUntil != "" || item.DerivedDocuments == nil || item.DerivedDocuments.TranscriptDraftID != "tr-1" {
		t.Fatalf("item=%#v ok=%v", item, ok)
	}
	if item.Audio == nil || item.Audio.Available {
		t.Fatalf("mapped delete payload must force unavailable audio: %#v", item.Audio)
	}
	if _, ok := mobileLibraryItemFromMeetingRecordingPayload("x", nil); ok {
		t.Fatal("empty body must not map")
	}
	if _, ok := mobileLibraryItemFromMeetingRecordingPayload("x", []byte(`not-json`)); ok {
		t.Fatal("invalid JSON must not map")
	}
	if _, ok := mobileLibraryItemFromMeetingRecordingPayload("x", []byte(`{}`)); ok {
		t.Fatal("empty object must not map as a recording")
	}
	if _, ok := mobileLibraryItemFromMeetingRecordingPayload("x", []byte(`{"code":"LIBRARY_ITEM_NOT_FOUND"}`)); ok {
		t.Fatal("error envelope must not map as a recording")
	}
}


func TestDeleteMobileMeetingRecordingAndResultsUsesFullRecordingEndpoint(t *testing.T) {
	var deleteCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("authorization = %q", got)
		}
		if r.Method != http.MethodDelete || r.URL.Path != "/api/mobile/meeting-recordings/recording-5" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		deleteCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := newMobileDocumentsTestApp(server.URL).DeleteMobileMeetingRecordingAndResults("recording-5"); err != nil {
		t.Fatalf("DeleteMobileMeetingRecordingAndResults: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d", deleteCalls)
	}
}
