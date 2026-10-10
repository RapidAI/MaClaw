package guiapp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSanitizeMobileOriginalFilename(t *testing.T) {
	got := sanitizeMobileOriginalFilename("report.docx")
	if got != "report.docx" {
		t.Fatalf("got=%q", got)
	}
	got = sanitizeMobileOriginalFilename("a:b?.png")
	if strings.ContainsAny(got, `<>:"/\|?*`) {
		t.Fatalf("unsafe chars remain: %q", got)
	}
	if got == "" {
		t.Fatal("empty")
	}
	if sanitizeMobileOriginalFilename("") != "original.bin" {
		t.Fatal("empty default")
	}
}

func TestFetchMobileDocumentOriginalViaHub(t *testing.T) {
	raw := []byte("original-bytes-from-hub")
	var sawAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d1", func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"draft": {
				"id":"d1",
				"title":"T",
				"has_original":true,
				"source_filename":"shot.png",
				"source_download_url":"/api/mobile/documents/drafts/d1/source"
			}
		}`))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/d1/source", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}

	name, body, err := app.fetchMobileDocumentOriginal("d1")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if name != "shot.png" {
		t.Fatalf("name=%q", name)
	}
	if string(body) != string(raw) {
		t.Fatalf("body=%q", body)
	}
	if sawAuth != "Bearer viewer-token" {
		t.Fatalf("auth=%q", sawAuth)
	}
}

func TestSaveMobileDocumentOriginalWithoutDialog(t *testing.T) {
	raw := []byte("save-me")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d2", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"d2","has_original":true,"source_filename":"note.txt","source_download_url":"/api/mobile/documents/drafts/d2/source"}}`))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/d2/source", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	app := &App{
		// ctx nil → no SaveFileDialog, writes to temp
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	path, err := app.SaveMobileDocumentOriginal("d2")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(raw) {
		t.Fatalf("file=%q", data)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	_ = filepath.Base(path)
}

func TestMaterializeMobileDocumentOriginalCachesByDraft(t *testing.T) {
	raw := []byte("preview-original-bytes")
	sourceHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d4", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"d4","has_original":true,"source_filename":"deck.pptx","source_size":22,"source_download_url":"/api/mobile/documents/drafts/d4/source"}}`))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/d4/source", func(w http.ResponseWriter, r *http.Request) {
		sourceHits++
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	first, err := app.MaterializeMobileDocumentOriginal("d4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(first, "deck.pptx") {
		t.Fatalf("path=%q", first)
	}
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(raw) {
		t.Fatalf("file=%q", data)
	}
	second, err := app.MaterializeMobileDocumentOriginal("d4")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("cache miss: %q vs %q", first, second)
	}
	if sourceHits != 1 {
		t.Fatalf("source downloads = %d, want 1", sourceHits)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(first)) })
}

func TestMaterializeMobileDocumentOriginalCachesWithoutSourceSize(t *testing.T) {
	raw := []byte("no-size-original")
	sourceHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d5", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"d5","has_original":true,"source_filename":"note.txt","source_download_url":"/api/mobile/documents/drafts/d5/source"}}`))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/d5/source", func(w http.ResponseWriter, r *http.Request) {
		sourceHits++
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	first, err := app.MaterializeMobileDocumentOriginal("d5")
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.MaterializeMobileDocumentOriginal("d5")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("cache miss: %q vs %q", first, second)
	}
	if sourceHits != 1 {
		t.Fatalf("source downloads = %d, want 1", sourceHits)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(first)) })
}

func TestFetchMobileDocumentOriginalRejectsMissingOriginal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/d3", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"d3","has_original":false,"title":"text only"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	_, _, err := app.fetchMobileDocumentOriginal("d3")
	if err == nil {
		t.Fatal("expected error for no original")
	}
}

func TestCompanionSupportsFilename(t *testing.T) {
	if !companionSupportsFilename("paper.PDF") || !companionSupportsFilename("notes.md") || !companionSupportsFilename("sheet.xlsx") {
		t.Fatal("expected companion formats")
	}
	if companionSupportsFilename("app.go") || companionSupportsFilename("lib.zip") || companionSupportsFilename("note") {
		t.Fatal("expected unsupported formats")
	}
}

func TestOpenMobileDocumentInFileCompanionOriginal(t *testing.T) {
	raw := []byte("%PDF-1.4 paper")
	draftHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/pdf1", func(w http.ResponseWriter, r *http.Request) {
		draftHits++
		_, _ = w.Write([]byte(`{"draft":{"id":"pdf1","title":"paper","has_original":true,"source_filename":"paper.pdf","source_size":14,"source_download_url":"/api/mobile/documents/drafts/pdf1/source"}}`))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/pdf1/source", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	var launched string
	app.processHooks.startProcess = func(argv []string) error {
		if len(argv) >= 3 {
			launched = argv[len(argv)-1]
		}
		return nil
	}
	path, err := app.OpenMobileDocumentInFileCompanion("pdf1")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || !strings.HasSuffix(path, "paper.pdf") {
		t.Fatalf("path=%q", path)
	}
	if strings.Contains(path, filepath.Join("companion", "paper.pdf")) {
		t.Fatalf("read-only original stored as an editable companion copy: %s", path)
	}
	if launched != path {
		t.Fatalf("launched=%q path=%q", launched, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(raw) {
		t.Fatalf("file=%q", data)
	}
	if draftHits != 1 {
		t.Fatalf("draft fetches = %d, want 1", draftHits)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(path)) })
}

func TestOpenMobileDocumentInFileCompanionNote(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/note1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"note1","title":"会议纪要","markdown":"# hello"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	var launched string
	app.processHooks.startProcess = func(argv []string) error {
		if len(argv) >= 3 {
			launched = argv[len(argv)-1]
		}
		return nil
	}
	path, err := app.OpenMobileDocumentInFileCompanion("note1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "会议纪要.md") {
		t.Fatalf("path=%q", path)
	}
	if launched != path {
		t.Fatalf("launched=%q", launched)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# hello" {
		t.Fatalf("file=%q", data)
	}
	if err := os.WriteFile(path, []byte("# edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := app.OpenMobileDocumentInFileCompanion("note1")
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Fatalf("reopen path=%q", again)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# edited" {
		t.Fatalf("reopen clobbered companion edits: %q", data)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(path)) })
}

func TestOpenMobileDocumentInFileCompanionKeepsEditableOriginal(t *testing.T) {
	body := []byte("# hub")
	sourceHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/md1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"draft":{"id":"md1","title":"notes","has_original":true,"source_filename":"notes.md","source_size":%d,"source_download_url":"/api/mobile/documents/drafts/md1/source"}}`, len(body))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/md1/source", func(w http.ResponseWriter, r *http.Request) {
		sourceHits++
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	app.processHooks.startProcess = func([]string) error { return nil }
	path, err := app.OpenMobileDocumentInFileCompanion("md1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, filepath.Join("companion", "notes.md")) {
		t.Fatalf("path=%q", path)
	}
	if sourceHits != 1 {
		t.Fatalf("source downloads = %d, want 1", sourceHits)
	}
	if err := os.WriteFile(path, []byte("# edited by companion"), 0o600); err != nil {
		t.Fatal(err)
	}
	body = []byte("# hub updated")
	hubPath, err := app.MaterializeMobileDocumentOriginal("md1")
	if err != nil {
		t.Fatal(err)
	}
	if hubPath == path {
		t.Fatal("preview cache and companion file are the same path")
	}
	again, err := app.OpenMobileDocumentInFileCompanion("md1")
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Fatalf("reopen path=%q", again)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# edited by companion" {
		t.Fatalf("reopen clobbered companion edits: %q", data)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(filepath.Dir(path))) })
}

func TestOpenMobileDocumentInFileCompanionRefreshesUneditedOriginal(t *testing.T) {
	body := []byte("# hub")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/md1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"draft":{"id":"md1","title":"notes","has_original":true,"source_filename":"notes.md","source_size":%d,"source_download_url":"/api/mobile/documents/drafts/md1/source"}}`, len(body))
	})
	mux.HandleFunc("/api/mobile/documents/drafts/md1/source", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	app.processHooks.startProcess = func([]string) error { return nil }
	path, err := app.OpenMobileDocumentInFileCompanion("md1")
	if err != nil {
		t.Fatal(err)
	}
	body = []byte("# hub v2")
	again, err := app.OpenMobileDocumentInFileCompanion("md1")
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Fatalf("reopen path=%q", again)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# hub v2" {
		t.Fatalf("unedited original was not refreshed: %q", data)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(filepath.Dir(path))) })
}

func TestOpenMobileDocumentInFileCompanionRejectsCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/drafts/go1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"draft":{"id":"go1","title":"app.go","has_original":true,"source_filename":"app.go","source_download_url":"/api/mobile/documents/drafts/go1/source"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	started := false
	app.processHooks.startProcess = func([]string) error {
		started = true
		return nil
	}
	if _, err := app.OpenMobileDocumentInFileCompanion("go1"); err == nil {
		t.Fatal("expected unsupported file")
	}
	if started {
		t.Fatal("launched companion for an unsupported file")
	}
}
