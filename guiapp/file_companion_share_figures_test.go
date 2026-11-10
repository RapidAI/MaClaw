package guiapp

import (
	"context"
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

func resetFileCompanionShareFiguresLedger(t *testing.T) {
	t.Helper()
	fileCompanionShareFigures.mu.Lock()
	fileCompanionShareFigures.loaded = false
	fileCompanionShareFigures.byToken = nil
	fileCompanionShareFigures.mu.Unlock()
	t.Cleanup(func() {
		fileCompanionShareFigures.mu.Lock()
		fileCompanionShareFigures.loaded = false
		fileCompanionShareFigures.byToken = nil
		fileCompanionShareFigures.mu.Unlock()
	})
}

func TestFileCompanionShareFigureTail(t *testing.T) {
	home := t.TempDir()
	foreign := filepath.Join(home, "data", "file-companion", "figures", "sess_host", "fig-1.png")
	session, name, ok := fileCompanionShareFigureTail(foreign)
	if !ok || session != "sess_host" || name != "fig-1.png" {
		t.Fatalf("tail = %q %q %v", session, name, ok)
	}
	if _, _, ok := fileCompanionShareFigureTail(filepath.Join(home, "elsewhere", "fig-1.png")); ok {
		t.Fatal("non-figure path accepted")
	}
	if _, _, ok := fileCompanionShareFigureTail(filepath.Join(home, "file-companion", "figures", "sess", "a..b.png")); ok {
		t.Fatal("dotted name accepted")
	}
	if _, _, ok := fileCompanionShareFigureTail(filepath.Join(home, "file-companion", "figures", "fig-1.png")); ok {
		t.Fatal("missing session segment accepted")
	}
}

// A participant's transcript names images that only exist on the host. The
// first render pulls the blob and mirrors it; later renders read the mirror.
func TestFetchFileCompanionShareFigureFetchesAndMirrors(t *testing.T) {
	resetFileCompanionShareFiguresLedger(t)
	const token = "cd_0123456789abcdef0123456789abcdef"
	pngPath := filepath.Join(t.TempDir(), "fig-1.png")
	writeCompanionFigurePNG(t, pngPath, 8, 8)
	png, err := os.ReadFile(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mobile/documents/shares/"+token+"/figures/fig-1.png" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Errorf("authorization = %q", got)
		}
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))

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
		Source: "cloud", Visibility: "person", Mine: false, Revision: 1,
	})
	hostFigure := filepath.Join(t.TempDir(), "home", "file-companion", "figures", "sess_host", "fig-1.png")
	got, err := app.FetchFileCompanionShareFigure(opened.Path, hostFigure)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("data url = %q", got)
	}
	if hits != 1 {
		t.Fatalf("hub hits = %d", hits)
	}
	mirror := filepath.Join(app.GetDataDir(), "file-companion", "figures", "sess_host", "fig-1.png")
	if _, err := os.Stat(mirror); err != nil {
		t.Fatalf("mirror missing: %v", err)
	}
	server.Close()
	again, err := app.FetchFileCompanionShareFigure(opened.Path, hostFigure)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatal("mirror read differs from the fetched image")
	}
	if hits != 1 {
		t.Fatalf("mirror still hit the hub: %d", hits)
	}
}

// The host uploads each transcript image once; a later push with the same
// bytes is skipped by the ledger.
func TestPushFileCompanionShareFiguresUploadsOncePerShare(t *testing.T) {
	resetFileCompanionShareFiguresLedger(t)
	const token = "cd_0123456789abcdef0123456789abcdef"
	var posted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/mobile/documents/shares/"+token+"/figures/") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<24))
		posted = append(posted, fmt.Sprintf("%s %d", r.URL.Path, len(body)))
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	figDir := filepath.Join(t.TempDir(), "file-companion", "figures", "sess_host")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	figPath := filepath.Join(figDir, "fig-1.png")
	writeCompanionFigurePNG(t, figPath, 8, 8)
	messages := []FileCompanionMessage{
		{Role: "assistant", Text: "见图1", Figures: []FileCompanionFigure{{Label: "图1", Path: figPath}}},
	}
	shares := []FileCompanionLibraryItem{{Token: token}}
	app.pushFileCompanionShareFigures(server.URL, "viewer", shares, messages)
	if len(posted) != 1 || !strings.Contains(posted[0], "/figures/fig-1.png") {
		t.Fatalf("posted = %#v", posted)
	}
	app.pushFileCompanionShareFigures(server.URL, "viewer", shares, messages)
	if len(posted) != 1 {
		t.Fatalf("second push re-uploaded: %#v", posted)
	}
}
