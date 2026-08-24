package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBugReportScreenshotPreviewDataURLCreatesBoundedPNGThumbnail(t *testing.T) {
	app := NewApp()
	path := filepath.Join(t.TempDir(), "screenshot.png")
	source := image.NewRGBA(image.Rect(0, 0, 480, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 480; x++ {
			source.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	preview, err := app.BugReportScreenshotPreviewDataURL(path)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(preview, prefix) {
		t.Fatalf("preview = %q, want PNG data URL", preview)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(preview, prefix))
	if err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	thumbnail, err := png.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if got := thumbnail.Bounds(); got.Dx() != 240 || got.Dy() != 60 {
		t.Fatalf("thumbnail bounds = %v, want 240x60", got)
	}
}

func TestAIAssistantAttachmentPreviewDataURLCreatesCompactThumbnail(t *testing.T) {
	app := NewApp()
	path := filepath.Join(t.TempDir(), "attachment.png")
	source := image.NewRGBA(image.Rect(0, 0, 480, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 480; x++ {
			source.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	preview, err := app.AIAssistantAttachmentPreviewDataURL(path)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(preview, prefix) {
		t.Fatalf("preview = %q, want PNG data URL", preview)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(preview, prefix))
	if err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	thumbnail, err := png.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if got := thumbnail.Bounds(); got.Dx() != 96 || got.Dy() != 24 {
		t.Fatalf("thumbnail bounds = %v, want 96x24", got)
	}
}

func TestAIAssistantAttachmentFullDataURLKeepsOriginalBytes(t *testing.T) {
	app := NewApp()
	path := filepath.Join(t.TempDir(), "attachment.png")
	source := image.NewRGBA(image.Rect(0, 0, 480, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 480; x++ {
			source.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	full, err := app.AIAssistantAttachmentFullDataURL(path)
	if err != nil {
		t.Fatalf("create full preview: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(full, prefix) {
		t.Fatalf("full preview = %q, want PNG data URL", full)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(full, prefix))
	if err != nil {
		t.Fatalf("decode full preview: %v", err)
	}
	if !bytes.Equal(decoded, encoded.Bytes()) {
		t.Fatalf("full preview re-encoded the attachment; want the original %d bytes, got %d", encoded.Len(), len(decoded))
	}
}

func TestAIAssistantAttachmentFullDataURLRejectsNonImages(t *testing.T) {
	app := NewApp()
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := app.AIAssistantAttachmentFullDataURL(path); err == nil {
		t.Fatal("full preview of a text file succeeded, want an error")
	}
	if _, err := app.AIAssistantAttachmentFullDataURL(dir); err == nil {
		t.Fatal("full preview of a directory succeeded, want an error")
	}
	if _, err := app.AIAssistantAttachmentFullDataURL("   "); err == nil {
		t.Fatal("full preview of an empty path succeeded, want an error")
	}
}

func TestNormalizeBugReportScreenshotPathsDeduplicatesAndLimits(t *testing.T) {
	paths := make([]string, 0, maxBugReportScreenshots+3)
	paths = append(paths, "", " screenshot-0.png ", "screenshot-0.png")
	for i := 1; i <= maxBugReportScreenshots+1; i++ {
		paths = append(paths, fmt.Sprintf("screenshot-%d.png", i))
	}
	got := normalizeBugReportScreenshotPaths(paths)
	if len(got) != maxBugReportScreenshots {
		t.Fatalf("selected paths = %d, want %d: %v", len(got), maxBugReportScreenshots, got)
	}
	if got[0] != "screenshot-0.png" || got[1] != "screenshot-1.png" {
		t.Fatalf("unexpected normalized ordering: %v", got)
	}
}

func TestCopyBugReportScreenshotPartClosesSourceFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screenshot.png")
	if err := os.WriteFile(path, []byte("test image"), 0o600); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := copyBugReportScreenshotPart(mw, path); err != nil {
		t.Fatalf("copy screenshot: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	// Windows rejects renames while the source file is still open. This confirms
	// the multipart helper releases every selected screenshot promptly.
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatalf("source screenshot remained open: %v", err)
	}
}

func TestUploadBugReportArchiveIncludesProgramVersionUsingHubCenterFieldName(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	archive := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-test.zip")
	if err := os.WriteFile(archive, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}

	previousVersion := version
	version = "6.6.2.11621"
	t.Cleanup(func() { version = previousVersion })

	var gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/problem-reports" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotVersion = r.FormValue("gui_version")
		if _, _, err := r.FormFile("diagnostics"); err != nil {
			t.Fatalf("diagnostics missing: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"BR-test"}`))
	}))
	defer server.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      server.URL,
		SkillMarketSessionToken: "test-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := app.uploadBugReportArchive(archive, 1, "Windows 11", "test report", nil); err != nil {
		t.Fatalf("upload report: %v", err)
	}
	if gotVersion != "6.6.2.11621" {
		t.Fatalf("gui_version = %q, want program version", gotVersion)
	}
}

func TestSuccessfulBugReportUploadClearsCollectedDiagnostics(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	archive := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-test.zip")
	if err := os.WriteFile(archive, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"logs", "trajectories"} {
		path := filepath.Join(app.getMaclawBaseDir(), dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "captured.txt"), []byte("captured"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"BR-test"}`))
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, SkillMarketSessionToken: "test-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.uploadBugReportArchive(archive, 2, "Windows 11", "test report", nil); err != nil {
		t.Fatalf("upload report: %v", err)
	}
	assertStaleBugReportEntriesGone(t, app.getMaclawBaseDir(), "captured.txt")
}

func TestRejectedBugReportUploadPreservesCollectedDiagnosticsForRetry(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	archive := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-test.zip")
	if err := os.WriteFile(archive, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"logs", "trajectories"} {
		path := filepath.Join(app.getMaclawBaseDir(), dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "captured.txt"), []byte("captured"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, SkillMarketSessionToken: "test-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.uploadBugReportArchive(archive, 2, "Windows 11", "test report", nil); err == nil {
		t.Fatal("expected rejected upload to fail")
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("retry archive was removed after rejected upload: %v", err)
	}
	for _, dir := range []string{"logs", "trajectories"} {
		if _, err := os.Stat(filepath.Join(app.getMaclawBaseDir(), dir, "captured.txt")); err != nil {
			t.Fatalf("%s diagnostics were cleared after rejected upload: %v", dir, err)
		}
	}
}

func TestMalformedSuccessfulBugReportResponsePreservesDiagnosticsForRetry(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	archive := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-test.zip")
	if err := os.WriteFile(archive, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, SkillMarketSessionToken: "test-token"}); err != nil {
		t.Fatal(err)
	}
	app.setPendingBugReportUpload(archive, 1)
	if _, err := app.uploadBugReportArchive(archive, 1, "Windows 11", "test report", nil); err == nil {
		t.Fatal("expected malformed successful response to fail")
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("retry archive was removed after malformed response: %v", err)
	}
	if !app.HasPendingBugReportUpload() {
		t.Fatal("malformed response must leave archive retryable")
	}
}

func TestSubmitBugReportRemovesPartialArchiveBeforeRetryStateExists(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "https://hubcenter.example", SkillMarketSessionToken: "test-token"}); err != nil {
		t.Fatal(err)
	}
	logsDir := filepath.Join(app.getMaclawBaseDir(), "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A FIFO/device-like path cannot be reliably created on every test OS; a
	// regular directory produces a deterministic Walk failure after ZIP creation.
	if err := os.Mkdir(filepath.Join(logsDir, "unreadable-entry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SubmitBugReport("Windows 11", "test report", nil); err == nil {
		t.Fatal("expected report without diagnostic files to fail")
	}
	archives, err := filepath.Glob(filepath.Join(app.GetTempDir(), "maclaw-diagnostics-*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("partial archive remained after failed assembly: %v", archives)
	}
}

func TestSuccessfulBugReportUploadSkipsLinkedDiagnosticRoot(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	archive := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-test.zip")
	if err := os.WriteFile(archive, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalRoot := t.TempDir()
	externalFile := filepath.Join(externalRoot, "must-not-delete.txt")
	if err := os.WriteFile(externalFile, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	logsRoot := filepath.Join(app.getMaclawBaseDir(), "logs")
	if err := os.MkdirAll(filepath.Dir(logsRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalRoot, logsRoot); err != nil {
		t.Skipf("symlinks unavailable in this test environment: %v", err)
	}
	trajectoryRoot := filepath.Join(app.getMaclawBaseDir(), "trajectories")
	if err := os.MkdirAll(trajectoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trajectoryRoot, "captured.txt"), []byte("captured"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"BR-test"}`))
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, SkillMarketSessionToken: "test-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.uploadBugReportArchive(archive, 1, "Windows 11", "test report", nil); err != nil {
		t.Fatalf("upload report: %v", err)
	}
	if content, err := os.ReadFile(externalFile); err != nil || string(content) != "external" {
		t.Fatalf("external linked data was modified: content=%q err=%v", content, err)
	}
	entries, err := os.ReadDir(externalRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "must-not-delete.txt" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("reopen followed logs symlink: %v", names)
	}
}

func assertStaleBugReportEntriesGone(t *testing.T, base, staleName string) {
	t.Helper()
	for _, dir := range []string{"logs", "trajectories"} {
		entries, err := os.ReadDir(filepath.Join(base, dir))
		if err != nil {
			t.Fatalf("read cleared %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.Name() == staleName {
				t.Fatalf("%s kept the stale entry: %v", dir, entries)
			}
		}
	}
}

func TestSetBugReportEnabledClearsDiagnosticsAndRestoresSettings(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	if err := app.SaveConfig(corelib.AppConfig{LLMTrajectoryLogging: false, LogDetailEnabled: true}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	for _, dir := range []string{"logs", "trajectories"} {
		path := filepath.Join(app.getMaclawBaseDir(), dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "stale.txt"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	started, err := app.SetBugReportEnabled(true)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !started.BugReportEnabled || !started.LLMTrajectoryLogging || !started.LogDetailEnabled {
		t.Fatalf("unexpected enabled config: %+v", started)
	}
	assertStaleBugReportEntriesGone(t, app.getMaclawBaseDir(), "stale.txt")
	stopped, err := app.SetBugReportEnabled(false)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if stopped.BugReportEnabled || stopped.LLMTrajectoryLogging || !stopped.LogDetailEnabled {
		t.Fatalf("settings were not restored: %+v", stopped)
	}
}

// A collection session that clears the log directory must leave a live log
// sink behind. Otherwise the session records nothing for the very failure the
// user started it to capture.
func TestSetBugReportEnabledKeepsLoggingAfterClear(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	logPath := filepath.Join(app.getMaclawBaseDir(), "logs", "maclaw.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("stale line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetBugReportEnabled(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	log.Printf("[semantic-routing] plan rejected user=%q reason=%v", "user-1", "semantic route has unmet needs")
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "stale line") {
		t.Fatalf("collection session kept pre-session logs: %s", body)
	}
	if !strings.Contains(string(body), "plan rejected") {
		t.Fatalf("collection session lost the reject line: %s", body)
	}
	if !strings.Contains(string(body), "log sinks reopened") {
		t.Fatalf("collection session did not record the rebound sink: %s", body)
	}
}

// Skip notes are collected while sinks are closed. They must land in the
// rebound maclaw.log, not the disconnected stderr window used during clear.
func TestSetBugReportEnabledRecordsSkippedLinkedRootAfterRebind(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	logsDir := filepath.Join(app.getMaclawBaseDir(), "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	externalRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(externalRoot, "must-not-delete.txt"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	trajectoryLink := filepath.Join(app.getMaclawBaseDir(), "trajectories")
	if err := os.MkdirAll(filepath.Dir(trajectoryLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalRoot, trajectoryLink); err != nil {
		t.Skipf("symlinks unavailable in this test environment: %v", err)
	}
	if _, err := app.SetBugReportEnabled(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(logsDir, "maclaw.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "skipping symbolic-link diagnostic root") {
		t.Fatalf("skip note did not reach rebound log: %s", body)
	}
	if !strings.Contains(string(body), "log sinks reopened") {
		t.Fatalf("collection session did not record the rebound sink: %s", body)
	}
	if content, err := os.ReadFile(filepath.Join(externalRoot, "must-not-delete.txt")); err != nil || string(content) != "external" {
		t.Fatalf("linked trajectory data was modified: content=%q err=%v", content, err)
	}
}

func TestSetBugReportEnabledTruncatesLockedLogOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific locked-file behavior")
	}
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	logDir := filepath.Join(app.getMaclawBaseDir(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logDir, "active.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("old diagnostics"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetBugReportEnabled(true); err != nil {
		t.Fatalf("enable with active log: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("active log size = %d, want 0", info.Size())
	}
}

func TestSetBugReportEnabledContinuesWhenDiagnosticPathIsUnreadable(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	// A file at the logs path makes it impossible to create/read the expected
	// directory. Enabling collection must still persist its settings.
	logsPath := filepath.Join(app.getMaclawBaseDir(), "logs")
	if err := os.MkdirAll(filepath.Dir(logsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logsPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	started, err := app.SetBugReportEnabled(true)
	if err != nil {
		t.Fatalf("enable with inaccessible diagnostics path: %v", err)
	}
	if !started.BugReportEnabled || !started.LLMTrajectoryLogging || !started.LogDetailEnabled {
		t.Fatalf("unexpected enabled config: %+v", started)
	}
}

func TestPendingBugReportUploadSurvivesAppRestartAndCleansMissingArchive(t *testing.T) {
	home := t.TempDir()
	first := NewApp()
	first.testHomeDir = home
	archive := filepath.Join(first.GetTempDir(), "maclaw-diagnostics-test.zip")
	if err := os.WriteFile(archive, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	first.setPendingBugReportUpload(archive, 3)

	restarted := NewApp()
	restarted.testHomeDir = home
	if !restarted.HasPendingBugReportUpload() {
		t.Fatal("expected pending upload to survive app restart")
	}
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	if restarted.HasPendingBugReportUpload() {
		t.Fatal("missing archive should not remain retryable")
	}
	if _, err := os.Stat(restarted.pendingBugReportUploadPath()); !os.IsNotExist(err) {
		t.Fatalf("pending metadata was not cleaned up: %v", err)
	}
}

func TestPendingBugReportUploadRejectsArchiveOutsideTempDir(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	externalArchive := filepath.Join(t.TempDir(), "maclaw-diagnostics-test.zip")
	if err := os.WriteFile(externalArchive, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := []byte(`{"zip_path":"` + filepath.ToSlash(externalArchive) + `","included_files":1}`)
	if err := os.WriteFile(app.pendingBugReportUploadPath(), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	if app.HasPendingBugReportUpload() {
		t.Fatal("archive outside the app temp directory must not be retryable")
	}
}

func TestPendingBugReportUploadRefreshesPersistedMetadata(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	firstArchive := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-first.zip")
	secondArchive := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-second.zip")
	for _, archive := range []string{firstArchive, secondArchive} {
		if err := os.WriteFile(archive, []byte("diagnostics"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app.setPendingBugReportUpload(firstArchive, 1)
	app.setPendingBugReportUpload(secondArchive, 2)
	if _, err := os.Stat(firstArchive); !os.IsNotExist(err) {
		t.Fatalf("superseded archive was not removed: %v", err)
	}
	restarted := NewApp()
	restarted.testHomeDir = app.testHomeDir
	pending := restarted.getPendingBugReportUpload()
	if pending == nil || pending.zipPath != secondArchive || pending.includedFiles != 2 {
		t.Fatalf("unexpected persisted pending upload: %+v", pending)
	}
}

func TestPendingBugReportUploadRejectsSymlink(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	target := filepath.Join(t.TempDir(), "maclaw-diagnostics-target.zip")
	link := filepath.Join(app.GetTempDir(), "maclaw-diagnostics-linked.zip")
	if err := os.WriteFile(target, []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable in this test environment: %v", err)
	}
	metadata := []byte(`{"zip_path":"` + filepath.ToSlash(link) + `","included_files":1}`)
	if err := os.WriteFile(app.pendingBugReportUploadPath(), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	if app.HasPendingBugReportUpload() {
		t.Fatal("symlinked archive must not be retryable")
	}
}

func TestPendingBugReportUploadRejectsUnrelatedZipInTempDir(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	unrelated := filepath.Join(app.GetTempDir(), "backup.zip")
	if err := os.WriteFile(unrelated, []byte("not a diagnostic archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := []byte(`{"zip_path":"` + filepath.ToSlash(unrelated) + `","included_files":1}`)
	if err := os.WriteFile(app.pendingBugReportUploadPath(), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	if app.HasPendingBugReportUpload() {
		t.Fatal("unrelated ZIP files in the temp directory must not be retryable")
	}
}
