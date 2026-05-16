package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestGroupDiscussionAttachmentDownloadURLAddsSessionAndParticipant(t *testing.T) {
	got, attachmentID, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "/api/ve/files/file-1?x=1", "disc-1", "machine-1")
	if err != nil {
		t.Fatalf("groupDiscussionAttachmentDownloadURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if u.String() != "https://hub.example/api/ve/files/download/file-1?participant_id=machine-1&session_id=disc-1&x=1" {
		t.Fatalf("download url = %q", u.String())
	}
	if attachmentID != "file-1" {
		t.Fatalf("attachment id = %q", attachmentID)
	}
}

func TestGroupDiscussionAttachmentDownloadURLRejectsExternalOrigin(t *testing.T) {
	_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://evil.example/api/ve/files/file-1", "disc-1", "machine-1")
	if err == nil {
		t.Fatalf("expected external origin to be rejected")
	}
}

func TestGroupDiscussionAttachmentDownloadURLForcesCurrentDiscussion(t *testing.T) {
	got, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://hub.example/api/ve/files/file-1?session_id=other&participant_id=other-machine", "disc-1", "machine-1")
	if err != nil {
		t.Fatalf("groupDiscussionAttachmentDownloadURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if got := u.Query().Get("session_id"); got != "disc-1" {
		t.Fatalf("session_id = %q", got)
	}
	if got := u.Query().Get("participant_id"); got != "machine-1" {
		t.Fatalf("participant_id = %q", got)
	}
}

func TestGroupDiscussionAttachmentDownloadURLRejectsNonFileRelayPath(t *testing.T) {
	_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://hub.example/api/admin/export", "disc-1", "machine-1")
	if err == nil {
		t.Fatalf("expected non-file-relay path to be rejected")
	}
}
func TestGroupDiscussionAttachmentDownloadURLRejectsUploadEndpoint(t *testing.T) {
	_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", "https://hub.example/api/ve/files/upload", "disc-1", "machine-1")
	if err == nil {
		t.Fatalf("expected upload endpoint to be rejected as an attachment download")
	}
}
func TestGroupDiscussionAttachmentDownloadURLRejectsAmbiguousFileID(t *testing.T) {
	cases := []string{
		"https://hub.example/api/ve/files/download/file-1/extra",
		"https://hub.example/api/ve/files/file%2F1",
	}
	for _, rawURL := range cases {
		_, _, err := groupDiscussionAttachmentDownloadURL("https://hub.example", rawURL, "disc-1", "machine-1")
		if err == nil {
			t.Fatalf("expected ambiguous file url %q to be rejected", rawURL)
		}
	}
}

func TestGroupDiscussionDownloadAttachmentSavesUnderDiscussionDir(t *testing.T) {
	hitCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		if r.URL.Path != "/api/ve/files/download/file-42" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("session_id"); got != "disc/one" {
			t.Fatalf("session_id = %q", got)
		}
		if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
			t.Fatalf("participant_id = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte("attachment body"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}

	result, err := app.GroupDiscussionDownloadAttachment("disc/one", "/api/ve/files/file-42", "../report?.txt")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if result.AttachmentID != "file-42" || result.Filename != "report_.txt" || result.SizeBytes != int64(len("attachment body")) {
		t.Fatalf("result = %+v", result)
	}
	wantDir := filepath.Join(app.GetDataDir(), "group-discussions", "disc_one", "attachments")
	if filepath.Dir(result.LocalPath) != wantDir {
		t.Fatalf("local dir = %q, want %q", filepath.Dir(result.LocalPath), wantDir)
	}
	data, err := os.ReadFile(result.LocalPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "attachment body" {
		t.Fatalf("downloaded body = %q", string(data))
	}

	second, err := app.GroupDiscussionDownloadAttachment("disc/one", "/api/ve/files/file-42", "../report?.txt")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment cached: %v", err)
	}
	if second.LocalPath != result.LocalPath || second.SizeBytes != result.SizeBytes {
		t.Fatalf("cached result = %+v, want local path %q size %d", second, result.LocalPath, result.SizeBytes)
	}
	if hitCount != 1 {
		t.Fatalf("expected cached second download to avoid HTTP request, hits=%d", hitCount)
	}
}

func TestGroupDiscussionDownloadAttachmentRefetchesWhenCachedFileMissing(t *testing.T) {
	hitCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		_, _ = w.Write([]byte("fresh attachment"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteMachineID:    "machine-1",
			RemoteMachineToken: "token-1",
		},
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	missingPath := filepath.Join(app.GetDataDir(), "group-discussions", "disc-missing", "attachments", "missing.pdf")
	if err := store.UpsertDownloadedAttachment(context.Background(), GroupDiscussionAttachmentRecord{AttachmentID: "file-99", DiscussionID: "disc-missing", Filename: "missing.pdf", HubURL: "/api/ve/files/file-99", LocalPath: missingPath, DownloadState: "downloaded"}); err != nil {
		_ = store.Close()
		t.Fatalf("UpsertDownloadedAttachment: %v", err)
	}
	_ = store.Close()

	result, err := app.GroupDiscussionDownloadAttachment("disc-missing", "/api/ve/files/file-99", "missing.pdf")
	if err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if hitCount != 1 {
		t.Fatalf("expected missing cached file to be refetched once, hits=%d", hitCount)
	}
	data, err := os.ReadFile(result.LocalPath)
	if err != nil {
		t.Fatalf("read refetched attachment: %v", err)
	}
	if string(data) != "fresh attachment" {
		t.Fatalf("refetched body = %q", string(data))
	}
}

func TestGroupDiscussionDownloadAttachmentUsesRemoteClientIDFallback(t *testing.T) {
	var gotParticipantID string
	var gotMachineID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParticipantID = r.URL.Query().Get("participant_id")
		gotMachineID = r.Header.Get("X-Machine-ID")
		_, _ = w.Write([]byte("attachment"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:       server.URL,
			RemoteClientID:     "client-fallback",
			RemoteMachineToken: "token-1",
		},
	}

	if _, err := app.GroupDiscussionDownloadAttachment("disc-1", "/api/ve/files/file-1", "report.txt"); err != nil {
		t.Fatalf("GroupDiscussionDownloadAttachment: %v", err)
	}
	if gotParticipantID != "client-fallback" {
		t.Fatalf("participant_id = %q, want client-fallback", gotParticipantID)
	}
	if gotMachineID != "client-fallback" {
		t.Fatalf("X-Machine-ID = %q, want client-fallback", gotMachineID)
	}
}
