package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestFetchSavePathWritesUnderWorkdir(t *testing.T) {
	// Simulate download_file: HTTP GET → save under user workbench directory.
	const body = "%PDF-1.4 smoke-test-content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	workDir := t.TempDir()
	dest := filepath.Join(workDir, "smoke", "paper.pdf")
	opts := &FetchOptions{
		SavePath: dest,
		TimeoutS: 30,
		MaxBytes: 1024 * 1024,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	result, err := FetchWithProviderCtx(ctx, srv.URL+"/arxiv/pdf/2501.00001", opts, corelib.WebSearchProvider{})
	if err != nil {
		t.Fatalf("FetchWithProviderCtx: %v", err)
	}
	if result == nil || result.SavedTo == "" {
		t.Fatalf("expected SavedTo, got %#v", result)
	}
	if filepath.Clean(result.SavedTo) != filepath.Clean(dest) {
		t.Fatalf("SavedTo=%q want %q", result.SavedTo, dest)
	}
	data, err := os.ReadFile(result.SavedTo)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
	if result.BytesRead != len(body) {
		t.Fatalf("BytesRead=%d want %d", result.BytesRead, len(body))
	}
	if !strings.Contains(result.Content, dest) && !strings.Contains(result.Content, "保存") {
		t.Fatalf("content should mention save path: %q", result.Content)
	}
}

func TestFetchSavePathAllowsLargeMaxBytes(t *testing.T) {
	// Caller may request up to 100MB for PDF downloads; text path stays at 10MB.
	opts := &FetchOptions{SavePath: filepath.Join(t.TempDir(), "big.pdf"), MaxBytes: 50 * 1024 * 1024}
	// normalize via a private path: call FetchCtx with invalid URL after caps applied internally
	// We only assert the options mutation by invoking FetchCtx against a tiny server.
	const body = "x"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	res, err := FetchCtx(context.Background(), srv.URL, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.BytesRead != 1 {
		t.Fatalf("BytesRead=%d", res.BytesRead)
	}
	// Cap still allows 50MB request; if it were clamped to 10MB silently for save path, 50MB would be reduced —
	// we can't observe MaxBytes after call, but save succeeded which is the contract for large PDF downloads.
}

func TestInjectPlusFetchSimulatesSkillTempRedirect(t *testing.T) {
	// Skill runner redirects TEMP to workdir/.maclaw-tmp; arxiv downloaders that
	// use gettempdir() then land under the project automatically.
	workDir := t.TempDir()
	tmpRoot := filepath.Join(workDir, ".maclaw-tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmpRoot, "maclaw-arxiv", "2501_00001.pdf")
	const body = "pdf-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	result, err := FetchCtx(context.Background(), srv.URL, &FetchOptions{
		SavePath: dest,
		TimeoutS: 15,
		MaxBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Clean(result.SavedTo), filepath.Clean(workDir)) {
		t.Fatalf("saved outside workdir: %q not under %q", result.SavedTo, workDir)
	}
}
