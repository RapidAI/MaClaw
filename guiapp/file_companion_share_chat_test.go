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

// The host's question is uploaded when the turn starts, carrying the revision
// it read, so a later answer can replace it and a late copy of the question
// cannot replace the answer.
func TestPublishFileCompanionShareQuestionBeforeTheAnswer(t *testing.T) {
	const token = "cd_abcdefabcdefabcdefabcdefabcdefab"
	var body string
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/mobile/documents/shares/"+token+"/chat" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		body = string(raw)
		base = r.Header.Get("X-Companion-Chat-Base")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":%q,"revision":1,"chat_revision":5}`, token)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
	}
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	paper := filepath.Join(app.testHomeDir, "a.pdf")
	if err := os.WriteFile(paper, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "a.pdf", Path: paper, Source: "cloud",
		Visibility: "public", Mine: true, Host: true, HostKnown: true,
		Revision: 1, ChatRevision: 4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.writeFileCompanionProjectSession(fileCompanionSessionID(paper), []FileCompanionMessage{
		{Role: "user", Text: "主持人的新问题"},
		{Role: "assistant", Text: "上一轮回答"},
	}); err != nil {
		t.Fatal(err)
	}
	app.publishFileCompanionShareQuestion(paper, "主持人的新问题")
	if base != "4" {
		t.Fatalf("question base = %q, want 4", base)
	}
	if strings.Count(body, "主持人的新问题") != 2 || !strings.Contains(body, "上一轮回答") {
		t.Fatalf("question body = %s", body)
	}
	if got := libraryChatRevision(t, app, token); got != 5 {
		t.Fatalf("library chat revision = %d, want 5", got)
	}
}

func TestFileCompanionChatBodyAhead(t *testing.T) {
	local := []byte(`[{"role":"user","text":"问"},{"role":"assistant","text":"答"}]`)
	question := []byte(`[{"role":"user","text":"问"}]`)
	if !fileCompanionChatBodyAhead(local, question) {
		t.Fatal("the answer should replace the question that started the turn")
	}
	newer := []byte(`[{"role":"user","text":"问"},{"role":"assistant","text":"答"},{"role":"user","text":"下一问"}]`)
	if fileCompanionChatBodyAhead(local, newer) {
		t.Fatal("an older answer must not replace a question that already landed")
	}
	if fileCompanionChatBodyAhead(local, local) {
		t.Fatal("the same transcript should stay")
	}
}

// An answer that lost the race with the next question must not replace it.
func TestSyncFileCompanionShareChatKeepsANewerQuestion(t *testing.T) {
	const token = "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var posts int
	const newer = `[{"role":"user","text":"问"},{"role":"assistant","text":"答"},{"role":"user","text":"下一问"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mobile/documents/shares/"+token+"/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.Header().Set("X-Companion-Chat-Revision", "2")
			_, _ = io.WriteString(w, newer)
			return
		}
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		posts++
		if got := r.Header.Get("X-Companion-Chat-Base"); got != "1" {
			t.Errorf("base = %q", got)
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"CHAT_STALE","chat_revision":2}`)
	}))
	defer server.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"},
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
	if err := app.upsertFileCompanionCloud(FileCompanionLibraryItem{
		ID: token, Token: token, Name: "论文.pdf", Path: opened.Path,
		Source: "cloud", Visibility: "person", Mine: true, Host: true, HostKnown: true,
		Revision: 1, ChatRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.writeFileCompanionProjectSession(fileCompanionSessionID(opened.Path), []FileCompanionMessage{
		{Role: "user", Text: "问"},
		{Role: "assistant", Text: "答"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := app.SyncFileCompanionShareChat(opened.Path, "[]")
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatalf("posted %d times", posts)
	}
	if result.ChatRevision != 2 {
		t.Fatalf("chat revision = %d, want 2", result.ChatRevision)
	}
}
