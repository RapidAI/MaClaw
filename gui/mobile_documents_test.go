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
			_, _ = io.WriteString(w, `{"id":"recording-4"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/library/items/recording-4":
			getCalls++
			_, _ = io.WriteString(w, `{"item":{"id":"recording-4","type":"audio","audio":{"available":false},"derived_documents":{"minutes_draft_id":"minutes-4"}}}`)
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item, err := newMobileDocumentsTestApp(server.URL).DeleteMobileMeetingRecording("recording-4")
	if err != nil {
		t.Fatalf("DeleteMobileMeetingRecording: %v", err)
	}
	if deleteCalls != 1 || getCalls != 1 || item == nil || item.Audio == nil || item.Audio.Available || item.DerivedDocuments == nil || item.DerivedDocuments.MinutesDraftID != "minutes-4" {
		t.Fatalf("delete=%d get=%d item=%#v", deleteCalls, getCalls, item)
	}
}
