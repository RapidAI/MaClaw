package cloudagent

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

func cjkFontBytesForTest(t *testing.T) []byte {
	t.Helper()
	data, err := docgen.SystemCJKFontBytes()
	if err != nil || len(data) == 0 {
		t.Skip("no CJK font available on this machine")
	}
	return data
}

func fontServerForTest(t *testing.T, data []byte) (*httptest.Server, *int) {
	t.Helper()
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func TestProvisionCJKFontDownloadsValidatesAndCaches(t *testing.T) {
	font := cjkFontBytesForTest(t)
	dir := t.TempDir()
	t.Setenv("MACLAW_FONT_DIR", dir)
	srv, hits := fontServerForTest(t, font)
	t.Setenv("MACLAW_CJK_FONT_URL", srv.URL)
	if err := provisionCJKFont(context.Background(), srv.Client()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	landed, err := os.ReadFile(filepath.Join(dir, cloudProvisionedFontName))
	if err != nil {
		t.Fatalf("font must land in the cache dir: %v", err)
	}
	if !bytes.Equal(landed, font) {
		t.Fatal("cached font bytes differ from the download")
	}
	// A second run must not hit the network again.
	if err := provisionCJKFont(context.Background(), srv.Client()); err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if *hits != 1 {
		t.Fatalf("server hits = %d, want exactly 1", *hits)
	}
}

func TestProvisionCJKFontRedownloadsCorruptCacheFile(t *testing.T) {
	font := cjkFontBytesForTest(t)
	dir := t.TempDir()
	t.Setenv("MACLAW_FONT_DIR", dir)
	// A corrupt file already sitting at the target name must not brick
	// self-repair: it is removed and the download runs again.
	if err := os.WriteFile(filepath.Join(dir, cloudProvisionedFontName), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, hits := fontServerForTest(t, font)
	t.Setenv("MACLAW_CJK_FONT_URL", srv.URL)
	if err := provisionCJKFont(context.Background(), srv.Client()); err != nil {
		t.Fatalf("provision over corrupt cache: %v", err)
	}
	landed, err := os.ReadFile(filepath.Join(dir, cloudProvisionedFontName))
	if err != nil || !bytes.Equal(landed, font) {
		t.Fatalf("corrupt cache must be replaced by the download: %v", err)
	}
	if *hits != 1 {
		t.Fatalf("server hits = %d, want exactly 1", *hits)
	}
}

func TestProvisionCJKFontRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_FONT_DIR", dir)
	srv, _ := fontServerForTest(t, []byte("this is not a font"))
	t.Setenv("MACLAW_CJK_FONT_URL", srv.URL)
	if err := provisionCJKFont(context.Background(), srv.Client()); err == nil {
		t.Fatal("garbage payload must fail validation")
	}
	if _, err := os.Stat(filepath.Join(dir, cloudProvisionedFontName)); !os.IsNotExist(err) {
		t.Fatal("a rejected payload must never land in the cache dir")
	}
	// No partial temp files either.
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) != 0 {
		t.Fatalf("cache dir must stay clean, got %v", entries)
	}
}

func TestProvisionCJKFontRejectsOversize(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_FONT_DIR", dir)
	srv, _ := fontServerForTest(t, make([]byte, cloudFontMaxBytes+2))
	t.Setenv("MACLAW_CJK_FONT_URL", srv.URL)
	if err := provisionCJKFont(context.Background(), srv.Client()); err == nil {
		t.Fatal("oversize payload must be rejected")
	}
}

func TestMaybeProvisionCJKFontRespectsKillSwitch(t *testing.T) {
	t.Setenv("MACLAW_FONT_DOWNLOAD", "off")
	// Must be a no-op regardless of detection state — and must not panic.
	maybeProvisionCJKFont()
}

func TestCloudPDFMissingGlyphNote(t *testing.T) {
	prev := cloudPDFFontInFallback
	t.Cleanup(func() { cloudPDFFontInFallback = prev })

	cloudPDFFontInFallback = func() bool { return false }
	if note := cloudPDFMissingGlyphNote("生僻字：㵘𠀀"); note != "" {
		t.Fatalf("a real font must produce no note, got %q", note)
	}

	cloudPDFFontInFallback = func() bool { return true }
	if note := cloudPDFMissingGlyphNote("今日天气晴，气温 11℃。微风小于3级。"); note != "" {
		t.Fatalf("common characters must produce no note, got %q", note)
	}
	// U+20BB7 (ext-B) is far outside the GB2312 subset.
	note := cloudPDFMissingGlyphNote("标题\n\n正文有𠮷字。")
	if note == "" || !strings.Contains(note, "𠮷") || !strings.Contains(note, "缺少字形") {
		t.Fatalf("rare character must produce a warning, got %q", note)
	}
	// The blame segment — between the lead-in and 缺少字形 — must name only
	// the uncovered character, not the covered ones around it.
	lead := strings.Index(note, "渲染，")
	tail := strings.Index(note, "缺少字形")
	if lead < 0 || tail < 0 || tail < lead {
		t.Fatalf("note shape unexpected: %q", note)
	}
	blamed := note[lead+len("渲染，") : tail]
	if strings.ContainsAny(blamed, "正文有字。标题") {
		t.Fatalf("covered characters must not be blamed: %q", blamed)
	}
	// ASCII/control noise never triggers the note either.
	if note := cloudPDFMissingGlyphNote("# Title\n\n- list\n"); note != "" {
		t.Fatalf("ascii-only content must produce no note, got %q", note)
	}
}

func TestCleanStaleFontTempFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".fontdl-old")
	fresh := filepath.Join(dir, ".fontdl-new")
	keep := filepath.Join(dir, "wqy-microhei.ttc")
	for _, p := range []string{stale, fresh, keep} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	cleanStaleFontTempFiles(dir)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale temp file must be removed")
	}
	for _, p := range []string{fresh, keep} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s must survive cleanup: %v", p, err)
		}
	}
	// A missing directory is a no-op, not an error or panic.
	cleanStaleFontTempFiles(filepath.Join(dir, "missing"))
}
