package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// The host transcript is a small blob of its own: a new host line reaches the
// Hub without re-uploading the paper, and the chat revision it returns is the
// sequence every participant compares against.
func TestSyncFileCompanionShareChatUploadsTheTranscript(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/mobile/documents/shares/"+token+"/chat" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Errorf("authorization = %q", got)
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":%q,"revision":1,"chat_revision":3}`, token)
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "person", Mine: true, Host: true, HostKnown: true, Revision: 1,
	})
	messages := []FileCompanionMessage{
		{Role: "user", Text: "主持人的新问题"},
		{Role: "assistant", Text: "主持人的新回答"},
	}
	if err := app.writeFileCompanionProjectSession(fileCompanionSessionID(opened.Path), messages); err != nil {
		t.Fatal(err)
	}
	result, err := app.SyncFileCompanionShareChat(opened.Path, "[]")
	if err != nil {
		t.Fatal(err)
	}
	if result.ChatRevision != 3 {
		t.Fatalf("chat revision = %d", result.ChatRevision)
	}
	if len(bodies) != 1 {
		t.Fatalf("posted %d times", len(bodies))
	}
	var posted []FileCompanionMessage
	if err := json.Unmarshal([]byte(bodies[0]), &posted); err != nil {
		t.Fatalf("posted body = %q: %v", bodies[0], err)
	}
	if len(posted) != 2 || posted[0].Text != "主持人的新问题" || posted[1].Text != "主持人的新回答" {
		t.Fatalf("posted transcript = %#v", posted)
	}
	fileCompanionLibraryMu.Lock()
	stored, ok := app.readFileCompanionLibraryLocked()
	fileCompanionLibraryMu.Unlock()
	if !ok {
		t.Fatal("library missing")
	}
	if len(stored) != 1 || stored[0].ChatRevision != 3 {
		t.Fatalf("stored chat revision = %+v", stored)
	}
}

// A participant reads the host transcript and also leaves it on disk, so the
// next open paints the same lines without waiting for another poll.
func TestFetchFileCompanionShareChatReadsAndStoresTheTranscript(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	chatBody := `[{"role":"user","text":"主持人的新问题"},{"role":"assistant","text":"主持人的新回答"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares/"+token+"/chat" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Companion-Chat-Revision", "5")
		_, _ = io.WriteString(w, chatBody)
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "person", Mine: false, Revision: 1, ChatRevision: 5,
	})
	got, err := app.FetchFileCompanionShareChat(opened.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 5 || !strings.Contains(got.Messages, "主持人的新回答") {
		t.Fatalf("chat = %q revision = %d", got.Messages, got.Revision)
	}
	session := app.fileCompanionHistory(fileCompanionSessionID(opened.Path))
	if len(session) != 2 || session[1].Text != "主持人的新回答" {
		t.Fatalf("stored transcript = %#v", session)
	}
}

// The library list is the only signal a participant polls. A moved
// chat_revision has to reach the row, or the window never fetches the blob.
func TestListFileCompanionLibraryCarriesChatRevision(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/shares":
			_, _ = fmt.Fprintf(w, `{"mine":[],"received":[{"token":%q,"name":"论文.pdf","visibility":"person","permanent":true,"url":"https://hub.example/hub/documents/shares/%s","created_at":"2026-10-25T00:00:00Z","owner":"host@example.com","size":5,"revision":1,"chat_revision":6}]}`, token, token)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: canonical,
		Source: "cloud", Visibility: "person", Mine: false, Revision: 1,
	})
	listed, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var row FileCompanionLibraryItem
	for _, item := range listed.Items {
		if item.Token == token {
			row = item
		}
	}
	if row.Token == "" || row.ChatRevision != 6 {
		t.Fatalf("listed row = %+v", row)
	}
}

// An older Hub has no transcript route. The chat push falls back to the full
// project sync, which is how the lines traveled before the blob existed. This
// is also what a desktop shows against a Hub that has not been upgraded yet.
func TestSyncFileCompanionShareChatFallsBackOnAnOlderHub(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	var synced bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat"):
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/shares/"+token+"/sync":
			_, _ = io.Copy(io.Discard, r.Body)
			synced = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"token":%q,"name":"论文.pdf","revision":9,"url":"https://hub.example/s/%s","visibility":"person","permanent":true}`, token, token)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "person", Mine: true, Host: true, HostKnown: true, Revision: 1,
	})
	if _, err := app.SyncFileCompanionShareChat(opened.Path, "[]"); err != nil {
		t.Fatal(err)
	}
	if !synced {
		t.Fatal("older Hub did not receive the fallback project sync")
	}
}

// A host turn is still writing this file's chat. Fetching the shared
// transcript may return data for the window, but it must not replace the
// stored session out from under the turn that is answering.
func TestFetchFileCompanionShareChatLeavesAnInFlightTurnAlone(t *testing.T) {
	const token = "cd_0123456789abcdef0123456789abcdef"
	chatBody := `[{"role":"user","text":"另一台机器的问题"},{"role":"assistant","text":"另一台机器的回答"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares/"+token+"/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Companion-Chat-Revision", "4")
		_, _ = io.WriteString(w, chatBody)
	}))
	defer server.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)
	source := filepath.Join(t.TempDir(), "论文.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "person", Mine: false, Revision: 1, ChatRevision: 4,
	})
	sessionID := fileCompanionSessionID(opened.Path)
	if err := app.writeFileCompanionProjectSession(sessionID, []FileCompanionMessage{
		{Role: "user", Text: "本地正在问的问题"},
	}); err != nil {
		t.Fatal(err)
	}
	app.imHandler.beginInFlightTurn("file-companion:"+sessionID, "本地正在问的问题", &LoopContext{})
	got, err := app.FetchFileCompanionShareChat(opened.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 4 || !strings.Contains(got.Messages, "另一台机器的回答") {
		t.Fatalf("fetched chat = %q revision = %d", got.Messages, got.Revision)
	}
	history := app.fileCompanionHistory(sessionID)
	if len(history) != 1 || history[0].Text != "本地正在问的问题" {
		t.Fatalf("in-flight session was replaced: %#v", history)
	}
}
