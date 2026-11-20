package guiapp

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSubmitGUIBugReportZipAddsScreenshotAndDescriptionOnly(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	base := app.getMaclawBaseDir()
	logBody := []byte("log-line\n")
	if err := os.MkdirAll(filepath.Join(base, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "logs", "app.log"), logBody, 0o644); err != nil {
		t.Fatal(err)
	}
	pngBytes := tinyBugReportPNG(t)
	shot := filepath.Join(app.GetTempDir(), "gui-screen-shot-test.png")
	if err := os.WriteFile(shot, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	description := "窗口卡住了，期望还能继续对话"
	got := captureBugReportUpload(t, app, func() (map[string]interface{}, error) {
		return app.SubmitGUIBugReport(description, "")
	})
	if got.osVersion != expectedGUIBugReportOS(app) || got.description != description || got.guiVersion == "" {
		t.Fatalf("upload fields os=%q description=%q gui=%q", got.osVersion, got.description, got.guiVersion)
	}
	if got.screenshotParts != 0 {
		t.Fatalf("screenshot multipart parts = %d, want 0", got.screenshotParts)
	}
	if !bytes.Equal(got.files["logs/app.log"], logBody) {
		t.Fatalf("logs/app.log = %q", got.files["logs/app.log"])
	}
	if !bytes.Equal(got.files["screen_shot.png"], pngBytes) {
		t.Fatalf("screen_shot.png missing or different (%d bytes)", len(got.files["screen_shot.png"]))
	}
	if string(got.files["bugreports.txt"]) != "报告类型: 缺陷\n"+description+"\n" {
		t.Fatalf("bugreports.txt = %q", got.files["bugreports.txt"])
	}
	if got.kind != "bug" {
		t.Fatalf("kind = %q", got.kind)
	}
	if _, ok := got.files["screenshot.png"]; ok {
		t.Fatal("zip root must use screen_shot.png")
	}
	for name := range got.files {
		if name == "screen_shot.png" || name == "bugreports.txt" || strings.HasPrefix(name, "logs/") || strings.HasPrefix(name, "trajectories/") {
			continue
		}
		t.Fatalf("unexpected zip entry %s", name)
	}
	if _, err := os.Stat(shot); !os.IsNotExist(err) {
		t.Fatalf("temp screenshot still present: %v", err)
	}
}

func TestSubmitAboutProblemReportFeatureOmitsLogs(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	base := app.getMaclawBaseDir()
	if err := os.MkdirAll(filepath.Join(base, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "logs", "app.log"), []byte("secret-log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "trajectories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "trajectories", "run.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := tinyBugReportPNG(t)
	shot := filepath.Join(t.TempDir(), "pasted.png")
	if err := os.WriteFile(shot, original, 0o644); err != nil {
		t.Fatal(err)
	}
	description := "希望能从标题栏提交新功能"
	got := captureBugReportUpload(t, app, func() (map[string]interface{}, error) {
		return app.SubmitAboutProblemReport("feature", description, []string{shot}, []string{""})
	})
	if got.kind != "feature" || got.description != description || got.guiVersion == "" {
		t.Fatalf("fields kind=%q description=%q gui=%q", got.kind, got.description, got.guiVersion)
	}
	if got.osVersion == "" || strings.EqualFold(got.osVersion, "Unknown") {
		t.Fatalf("os was not submitted: %q", got.osVersion)
	}
	if _, ok := got.files["logs/app.log"]; ok {
		t.Fatal("feature request packed logs")
	}
	if _, ok := got.files["trajectories/run.json"]; ok {
		t.Fatal("feature request packed trajectories")
	}
	if string(got.files["bugreports.txt"]) != "报告类型: 新功能\n"+description+"\n" {
		t.Fatalf("bugreports.txt = %q", got.files["bugreports.txt"])
	}
	if got.screenshotParts != 1 || !bytes.Equal(got.screenshotBodies[0], original) {
		t.Fatalf("unmarked screenshot was not the original file (%d parts)", got.screenshotParts)
	}
}

func TestSubmitAboutProblemReportBugKeepsLogsAndMarkedPNG(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	base := app.getMaclawBaseDir()
	logBody := []byte("about-log\n")
	if err := os.MkdirAll(filepath.Join(base, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "logs", "app.log"), logBody, 0o644); err != nil {
		t.Fatal(err)
	}
	original := tinyBugReportPNG(t)
	marked := sizedBugReportPNG(t, 3, 2)
	shot := filepath.Join(t.TempDir(), "pasted.png")
	if err := os.WriteFile(shot, original, 0o644); err != nil {
		t.Fatal(err)
	}
	description := "贴图上的按钮点了没反应"
	got := captureBugReportUpload(t, app, func() (map[string]interface{}, error) {
		return app.SubmitAboutProblemReport("bug", description, []string{shot}, []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(marked)})
	})
	if got.kind != "bug" || got.description != description {
		t.Fatalf("fields %+v", got.capturedBugReportFields)
	}
	if !bytes.Equal(got.files["logs/app.log"], logBody) {
		t.Fatal("bug report dropped the log")
	}
	if string(got.files["bugreports.txt"]) != "报告类型: 缺陷\n"+description+"\n" {
		t.Fatalf("bugreports.txt = %q", got.files["bugreports.txt"])
	}
	if got.screenshotParts != 1 || !bytes.Equal(got.screenshotBodies[0], marked) || bytes.Equal(got.screenshotBodies[0], original) {
		t.Fatal("marked screenshot did not replace the original bytes")
	}
}

func TestSubmitAboutProblemReportRejectsBadAnnotation(t *testing.T) {
	app := NewApp()
	_, err := app.SubmitAboutProblemReport("bug", "说明", []string{filepath.Join(t.TempDir(), "shot.png")}, []string{"data:image/png;base64,aaaa"})
	if err == nil {
		t.Fatal("invalid annotation was accepted")
	}
}

func TestSubmitBugReportOmitsGUIRootFiles(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	base := app.getMaclawBaseDir()
	if err := os.MkdirAll(filepath.Join(base, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "logs", "app.log"), []byte("about\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := captureBugReportUpload(t, app, func() (map[string]interface{}, error) {
		return app.SubmitBugReport("Windows 11", "about page report", nil)
	})
	if got.osVersion != "Windows 11" || got.description != "about page report" {
		t.Fatalf("about upload fields %+v", got.capturedBugReportFields)
	}
	if _, ok := got.files["logs/app.log"]; !ok {
		t.Fatal("about report dropped the log")
	}
	if _, ok := got.files["screen_shot.png"]; ok {
		t.Fatal("about report included screen_shot.png")
	}
	if _, ok := got.files["bugreports.txt"]; ok {
		t.Fatal("about report included bugreports.txt")
	}
	if got.screenshotParts != 0 {
		t.Fatalf("screenshot parts = %d", got.screenshotParts)
	}
}

func TestSubmitGUIBugReportKeepsScreenshotWhenUploadFails(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	base := app.getMaclawBaseDir()
	if err := os.MkdirAll(filepath.Join(base, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "logs", "app.log"), []byte("log-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pngBytes := tinyBugReportPNG(t)
	shot := filepath.Join(app.GetTempDir(), "gui-screen-shot-retry.png")
	if err := os.WriteFile(shot, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, _, err := r.FormFile("diagnostics"); err != nil {
			t.Errorf("retry diagnostics: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"BR-retry"}`))
	}))
	defer server.Close()
	token := testSkillMarketSignedToken("u_current", "phone:13301168516", time.Now().Add(time.Hour))
	saveBugReportHubPrincipal(t, app, server.URL, "u_current", token)
	if _, err := app.SubmitGUIBugReport("窗口卡住了", ""); err == nil {
		t.Fatal("first upload should fail")
	}
	if app.guiBugReportShot != shot {
		t.Fatalf("screenshot path after failure = %q", app.guiBugReportShot)
	}
	if _, err := os.Stat(shot); err != nil {
		t.Fatalf("screenshot file: %v", err)
	}
	result, err := app.SubmitGUIBugReport("窗口卡住了", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["id"] != "BR-retry" || attempts != 2 {
		t.Fatalf("result=%v attempts=%d", result, attempts)
	}
	if _, err := os.Stat(shot); !os.IsNotExist(err) {
		t.Fatalf("screenshot remained after success: %v", err)
	}
}

func TestSubmitGUIBugReportRejectsEmptyDescriptionAndMissingShot(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	if _, err := app.SubmitGUIBugReport("  ", ""); err == nil {
		t.Fatal("blank description was accepted")
	}
	if _, err := app.SubmitGUIBugReport("the window froze", ""); err == nil {
		t.Fatal("missing screenshot was accepted")
	}
}

func TestSubmitGUIBugReportKeepsShotWhenDescriptionExceedsBytes(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	shot := filepath.Join(t.TempDir(), "kept.png")
	if err := os.WriteFile(shot, tinyBugReportPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	over := strings.Repeat("界", guiBugReportNoteLimit/3+1)
	if len(over) <= guiBugReportNoteLimit {
		t.Fatalf("fixture is only %d bytes", len(over))
	}
	if _, err := app.SubmitGUIBugReport(over, ""); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatal(err)
	}
	if app.guiBugReportShot != shot {
		t.Fatalf("over-long description dropped the screenshot: %q", app.guiBugReportShot)
	}
	if _, err := os.Stat(shot); err != nil {
		t.Fatal(err)
	}

	app.guiBugReportShot = filepath.Join(t.TempDir(), "absent.png")
	_, err := app.SubmitGUIBugReport(strings.Repeat("a", guiBugReportNoteLimit), "")
	if err == nil || strings.Contains(err.Error(), "too long") {
		t.Fatalf("32 KB description was treated as too long: %v", err)
	}
}

func TestSubmitGUIBugReportZipUsesAnnotatedScreenshot(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	base := app.getMaclawBaseDir()
	if err := os.MkdirAll(filepath.Join(base, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "logs", "app.log"), []byte("log-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := sizedBugReportPNG(t, 2, 2)
	annotated := sizedBugReportPNG(t, 3, 1)
	if bytes.Equal(original, annotated) {
		t.Fatal("annotated fixture matches the original capture")
	}
	shot := filepath.Join(app.GetTempDir(), "gui-screen-shot-marked.png")
	if err := os.WriteFile(shot, original, 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	payload := "data:image/png;base64," + base64.StdEncoding.EncodeToString(annotated)
	got := captureBugReportUpload(t, app, func() (map[string]interface{}, error) {
		return app.SubmitGUIBugReport("窗口上画了标记", payload)
	})
	if !bytes.Equal(got.files["screen_shot.png"], annotated) {
		t.Fatal("screen_shot.png was not the marked image")
	}
	if bytes.Equal(got.files["screen_shot.png"], original) {
		t.Fatal("screen_shot.png kept the unmarked capture")
	}
	if _, err := os.Stat(shot); !os.IsNotExist(err) {
		t.Fatalf("temp screenshot still present: %v", err)
	}
}

func TestSubmitGUIBugReportRejectsBadAnnotationAndKeepsShot(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	original := sizedBugReportPNG(t, 2, 2)
	shot := filepath.Join(app.GetTempDir(), "gui-screen-shot-kept.png")
	if err := os.WriteFile(shot, original, 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	if _, err := app.SubmitGUIBugReport("窗口卡住了", "data:image/jpeg;base64,aaaa"); err == nil {
		t.Fatal("jpeg annotation was accepted")
	}
	if app.guiBugReportShot != shot {
		t.Fatalf("bad annotation dropped the screenshot: %q", app.guiBugReportShot)
	}
	body, err := os.ReadFile(shot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, original) {
		t.Fatal("bad annotation rewrote the capture")
	}
	if _, err := app.SubmitGUIBugReport("窗口卡住了", "data:image/png;base64,aGVsbG8="); err == nil {
		t.Fatal("non-png payload was accepted")
	}
	if app.guiBugReportShot != shot {
		t.Fatal("invalid png dropped the screenshot")
	}
}

func TestSubmitGUIBugReportKeepsOriginalFileWhenAnnotatedUploadFails(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	t.Cleanup(closeLogSinks)
	base := app.getMaclawBaseDir()
	if err := os.MkdirAll(filepath.Join(base, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "logs", "app.log"), []byte("log-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := sizedBugReportPNG(t, 2, 2)
	annotated := sizedBugReportPNG(t, 4, 2)
	shot := filepath.Join(app.GetTempDir(), "gui-screen-shot-retry-marked.png")
	if err := os.WriteFile(shot, original, 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	token := testSkillMarketSignedToken("u_current", "phone:13301168516", time.Now().Add(time.Hour))
	saveBugReportHubPrincipal(t, app, server.URL, "u_current", token)
	payload := "data:image/png;base64," + base64.StdEncoding.EncodeToString(annotated)
	if _, err := app.SubmitGUIBugReport("窗口卡住了", payload); err == nil {
		t.Fatal("upload should fail")
	}
	if app.guiBugReportShot != shot {
		t.Fatalf("screenshot path after failure = %q", app.guiBugReportShot)
	}
	body, err := os.ReadFile(shot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, original) {
		t.Fatal("failed upload replaced the capture with the marked image")
	}
}

func TestGUIBugReportScreenshotDataURLReadsStoredShot(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	if _, err := app.GUIBugReportScreenshotDataURL(); err == nil {
		t.Fatal("missing screenshot returned a data URL")
	}
	original := sizedBugReportPNG(t, 2, 2)
	shot := filepath.Join(app.GetTempDir(), "gui-screen-shot-preview.png")
	if err := os.WriteFile(shot, original, 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	got, err := app.GUIBugReportScreenshotDataURL()
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("data URL = %q", got[:min(len(got), 32)])
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, original) {
		t.Fatal("data URL is not the stored capture")
	}
	if _, err := os.Stat(shot); err != nil {
		t.Fatal(err)
	}
}

func TestGUIBugReportPreviewSize(t *testing.T) {
	if w, h := guiBugReportPreviewSize(1920, 1080); w != 1920 || h != 1080 {
		t.Fatalf("fitted window = %d x %d", w, h)
	}
	if w, h := guiBugReportPreviewSize(2000, 8000); w != 1024 || h != guiBugReportPreviewMaxEdge {
		t.Fatalf("tall window = %d x %d", w, h)
	}
	if w, h := guiBugReportPreviewSize(0, 10); w != 0 || h != 0 {
		t.Fatalf("empty window = %d x %d", w, h)
	}
}

func TestGUIBugReportScreenshotDataURLScalesAbovePreviewEdge(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	original := sizedBugReportPNG(t, guiBugReportPreviewMaxEdge+1, 2)
	shot := filepath.Join(app.GetTempDir(), "gui-screen-shot-wide.png")
	if err := os.WriteFile(shot, original, 0o600); err != nil {
		t.Fatal(err)
	}
	app.guiBugReportShot = shot
	got, err := app.GUIBugReportScreenshotDataURL()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != guiBugReportPreviewMaxEdge || cfg.Height != 2 {
		t.Fatalf("preview = %d x %d", cfg.Width, cfg.Height)
	}
	body, err := os.ReadFile(shot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, original) {
		t.Fatal("preview rewrote the capture")
	}
}

func TestDecodeGUIBugReportAnnotatedPNG(t *testing.T) {
	pngBytes := sizedBugReportPNG(t, 2, 2)
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	got, err := decodeGUIBugReportAnnotatedPNG("  ")
	if err != nil || got != nil {
		t.Fatalf("empty annotation = %v %v", got, err)
	}
	got, err = decodeGUIBugReportAnnotatedPNG("data:image/png;base64," + encoded[:8] + "\n" + encoded[8:])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pngBytes) {
		t.Fatal("whitespace in the data URL changed the PNG")
	}
	got, err = decodeGUIBugReportAnnotatedPNG(encoded)
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("raw base64: %v", err)
	}
	if _, err := decodeGUIBugReportAnnotatedPNG("data:image/jpeg;base64," + encoded); err == nil {
		t.Fatal("jpeg data URL was accepted")
	}
	if _, err := decodeGUIBugReportAnnotatedPNG("data:image/png;base64,aGVsbG8="); err == nil {
		t.Fatal("non-png bytes were accepted")
	}
}

func TestDiscardGUIBugReportScreenshotLeavesANewerCapture(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	oldPath := filepath.Join(app.GetTempDir(), "old.png")
	newPath := filepath.Join(app.GetTempDir(), "new.png")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := app.storeGUIBugReportShot(oldPath); got != oldPath {
		t.Fatalf("first token = %q", got)
	}
	// The next capture has already replaced the stored file. The close of
	// the previous dialog then discards the token it was given.
	if got := app.storeGUIBugReportShot(newPath); got != newPath {
		t.Fatalf("second token = %q", got)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("replaced capture still present: %v", err)
	}
	app.DiscardGUIBugReportScreenshot(oldPath)
	if app.guiBugReportShot != newPath {
		t.Fatalf("stored capture = %q", app.guiBugReportShot)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("newer capture: %v", err)
	}
	app.DiscardGUIBugReportScreenshot("  ")
	if app.guiBugReportShot != newPath {
		t.Fatal("blank token cleared the capture")
	}
	app.DiscardGUIBugReportScreenshot(newPath)
	if app.guiBugReportShot != "" {
		t.Fatalf("stored capture after discard = %q", app.guiBugReportShot)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("discarded capture still present: %v", err)
	}
	app.DiscardGUIBugReportScreenshot(newPath)
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("second discard recreated or kept the file: %v", err)
	}
}

func TestGUIBugReportCaptureRectDropsInvisibleMaximizeBorder(t *testing.T) {
	window := guiBugReportRect{Left: -8, Top: -8, Right: 1928, Bottom: 1088}
	visible := guiBugReportRect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	got, ok := guiBugReportCaptureRect(window, visible, true)
	if !ok || got != visible {
		t.Fatalf("visible frame: %+v ok=%v", got, ok)
	}
	outside := guiBugReportRect{Left: -16, Top: 0, Right: 1920, Bottom: 1080}
	if got, ok = guiBugReportCaptureRect(window, outside, true); !ok || got != window {
		t.Fatalf("outside frame: %+v ok=%v", got, ok)
	}
	if got, ok = guiBugReportCaptureRect(window, guiBugReportRect{}, true); !ok || got != window {
		t.Fatalf("empty visible frame: %+v ok=%v", got, ok)
	}
	if got, ok = guiBugReportCaptureRect(window, visible, false); !ok || got != window {
		t.Fatalf("unavailable visible frame: %+v ok=%v", got, ok)
	}
	if got, ok = guiBugReportCaptureRect(window, window, true); !ok || got != window {
		t.Fatalf("identical frame: %+v ok=%v", got, ok)
	}
	// Restored windows often keep the top flush and inset only the shadow
	// sides. That visible frame is still inside the window.
	restored := guiBugReportRect{Left: 100, Top: 100, Right: 914, Bottom: 707}
	shadow := guiBugReportRect{Left: 107, Top: 100, Right: 907, Bottom: 700}
	if got, ok = guiBugReportCaptureRect(restored, shadow, true); !ok || got != shadow {
		t.Fatalf("partial shadow inset: %+v ok=%v", got, ok)
	}
	if _, ok = guiBugReportCaptureRect(guiBugReportRect{}, visible, true); ok {
		t.Fatal("empty window was accepted")
	}
}

func TestValidateGUIBugReportScreenshotAllowsDesktopSize(t *testing.T) {
	if err := validateGUIBugReportScreenshot(pngHeaderOnly(7680, 4320)); err != nil {
		t.Fatal(err)
	}
	if err := validateGUIBugReportScreenshot(tinyBugReportPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := validateGUIBugReportScreenshot(pngHeaderOnly(guiBugReportScreenshotMaxEdge+1, 8)); err == nil {
		t.Fatal("edge above the window capturer was accepted")
	}
	if err := validateGUIBugReportScreenshot(nil); err == nil {
		t.Fatal("empty screenshot was accepted")
	}
}

func pngHeaderOnly(width, height int) []byte {
	payload := make([]byte, 13)
	binary.BigEndian.PutUint32(payload[0:4], uint32(width))
	binary.BigEndian.PutUint32(payload[4:8], uint32(height))
	payload[8] = 8
	payload[9] = 2
	chunk := append([]byte("IHDR"), payload...)
	var out bytes.Buffer
	out.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], 13)
	out.Write(u32[:])
	out.Write(chunk)
	binary.BigEndian.PutUint32(u32[:], crc32.ChecksumIEEE(chunk))
	out.Write(u32[:])
	return out.Bytes()
}

func TestMaybeClearDiagnosticsForNewDayOncePerLocalDay(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	corelib.SetLogDetailEnabled(false)
	t.Cleanup(func() {
		closeLogSinks()
		app.closePetRearingDBLocked()
		corelib.SetLogDetailEnabled(false)
	})
	base := app.getMaclawBaseDir()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	// An older clear-only stamp must not suppress the switch reset.
	if err := os.WriteFile(filepath.Join(base, "diagnostics-day.txt"), []byte("2026-11-19\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(base, "config.json")
	configBody := []byte("{\"bug_report_enabled\":false,\"llm_trajectory_logging\":false,\"log_detail_enabled\":false}\n")
	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 19, 8, 0, 0, 0, time.Local)
	writeDiagnosticFile(t, base, "logs", "app.log")
	writeDiagnosticFile(t, base, "trajectories", "run.json")
	app.maybeClearDiagnosticsForNewDay(day)
	if diagnosticFileExists(base, "logs", "app.log") || diagnosticFileExists(base, "trajectories", "run.json") {
		t.Fatal("first launch of the day left diagnostic files")
	}
	if _, err := os.Stat(filepath.Join(base, "logs")); err != nil {
		t.Fatal(err)
	}
	// The gate starts off. Today's log must still contain the head
	// restatement and the line that records the switches are on.
	logBody, err := os.ReadFile(filepath.Join(base, "logs", "maclaw.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBody), "[heads] diagnostics-cleared") {
		t.Fatalf("daily reset dropped the head status from the fresh log:\n%s", logBody)
	}
	if !strings.Contains(string(logBody), "daily reset: log detail and LLM trajectory on") {
		t.Fatalf("daily reset did not record the enabled switches:\n%s", logBody)
	}
	stamp, err := os.ReadFile(filepath.Join(base, "diagnostics-day.txt"))
	if err != nil || strings.TrimSpace(string(stamp)) != diagnosticsDayStampText("2026-11-19") {
		t.Fatalf("stamp = %q err=%v", stamp, err)
	}
	opened, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !opened.LogDetailEnabled || !opened.LLMTrajectoryLogging {
		t.Fatalf("daily reset left switches off: log=%v trajectory=%v", opened.LogDetailEnabled, opened.LLMTrajectoryLogging)
	}
	if opened.BugReportEnabled {
		t.Fatal("daily reset started the bug-report session")
	}
	if opened.BugReportPreviousTrajectory || opened.BugReportPreviousLogDetail {
		t.Fatal("daily reset rewrote the restore point while the session was off")
	}
	if !corelib.IsLogDetailEnabled() {
		t.Fatal("daily reset left the log detail gate off")
	}
	opened.LogDetailEnabled = false
	opened.LLMTrajectoryLogging = false
	if err := app.SaveConfig(opened); err != nil {
		t.Fatal(err)
	}
	writeDiagnosticFile(t, base, "logs", "later.txt")
	app.maybeClearDiagnosticsForNewDay(day.Add(2 * time.Hour))
	if !diagnosticFileExists(base, "logs", "later.txt") {
		t.Fatal("same-day launch cleared diagnostics again")
	}
	sameDay, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if sameDay.LogDetailEnabled || sameDay.LLMTrajectoryLogging {
		t.Fatal("same-day launch turned the switches back on")
	}
	app.maybeClearDiagnosticsForNewDay(day.Add(24 * time.Hour))
	if diagnosticFileExists(base, "logs", "later.txt") {
		t.Fatal("next-day launch kept the previous diagnostics")
	}
	stamp, err = os.ReadFile(filepath.Join(base, "diagnostics-day.txt"))
	if err != nil || strings.TrimSpace(string(stamp)) != diagnosticsDayStampText("2026-11-20") {
		t.Fatalf("next stamp = %q err=%v", stamp, err)
	}
	nextDay, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !nextDay.LogDetailEnabled || !nextDay.LLMTrajectoryLogging || nextDay.BugReportEnabled {
		t.Fatalf("next day log=%v trajectory=%v bugReport=%v", nextDay.LogDetailEnabled, nextDay.LLMTrajectoryLogging, nextDay.BugReportEnabled)
	}
}

func TestMaybeClearDiagnosticsForNewDayKeepsOpenSessionRestoreOn(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	corelib.SetLogDetailEnabled(false)
	t.Cleanup(func() {
		closeLogSinks()
		app.closePetRearingDBLocked()
		corelib.SetLogDetailEnabled(false)
	})
	base := app.getMaclawBaseDir()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	// Session left on from yesterday, with the switches it would restore
	// still off. Closing it after today's reset must not turn collection off.
	body := []byte("{\"bug_report_enabled\":true,\"bug_report_previous_trajectory\":false,\"bug_report_previous_log_detail\":false,\"llm_trajectory_logging\":true,\"log_detail_enabled\":false}\n")
	if err := os.WriteFile(filepath.Join(base, "config.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	writeDiagnosticFile(t, base, "logs", "old.log")
	day := time.Date(2026, 11, 19, 8, 0, 0, 0, time.Local)
	app.maybeClearDiagnosticsForNewDay(day)
	if diagnosticFileExists(base, "logs", "old.log") {
		t.Fatal("open session kept the previous log")
	}
	opened, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !opened.BugReportEnabled || !opened.LogDetailEnabled || !opened.LLMTrajectoryLogging {
		t.Fatalf("session=%v log=%v trajectory=%v", opened.BugReportEnabled, opened.LogDetailEnabled, opened.LLMTrajectoryLogging)
	}
	if !opened.BugReportPreviousTrajectory || !opened.BugReportPreviousLogDetail {
		t.Fatal("open session kept an off restore point")
	}
	stopped, err := app.SetBugReportEnabled(false)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.BugReportEnabled || !stopped.LogDetailEnabled || !stopped.LLMTrajectoryLogging {
		t.Fatalf("close restored collection off: session=%v log=%v trajectory=%v", stopped.BugReportEnabled, stopped.LogDetailEnabled, stopped.LLMTrajectoryLogging)
	}
	writeDiagnosticFile(t, base, "logs", "after-close.txt")
	app.maybeClearDiagnosticsForNewDay(day.Add(time.Hour))
	if !diagnosticFileExists(base, "logs", "after-close.txt") {
		t.Fatal("same-day launch cleared diagnostics after the session closed")
	}
}

func TestMaybeClearDiagnosticsForNewDaySkipsFileCompanion(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	app.fileCompanion = &fileCompanionRuntime{process: true}
	base := app.getMaclawBaseDir()
	writeDiagnosticFile(t, base, "logs", "stay.txt")
	app.maybeClearDiagnosticsForNewDay(time.Date(2026, 11, 19, 8, 0, 0, 0, time.Local))
	if !diagnosticFileExists(base, "logs", "stay.txt") {
		t.Fatal("file companion cleared diagnostics")
	}
	if _, err := os.Stat(filepath.Join(base, "diagnostics-day.txt")); !os.IsNotExist(err) {
		t.Fatalf("file companion wrote a diagnostics stamp: %v", err)
	}
}

func TestWriteBugReportZipRootRejectsNestedNames(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeBugReportZipRoot(zw, guiBugReportScreenshotName, []byte("png")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "logs/screen_shot.png", `..\screen_shot.png`, "screen_shot.png/extra"} {
		if err := writeBugReportZipRoot(zw, name, []byte("x")); err == nil {
			t.Fatalf("accepted root entry %q", name)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

type capturedBugReportFields struct {
	osVersion        string
	description      string
	guiVersion       string
	kind             string
	screenshotParts  int
	screenshotBodies [][]byte
}

type capturedBugReport struct {
	capturedBugReportFields
	files map[string][]byte
}

func captureBugReportUpload(t *testing.T, app *App, submit func() (map[string]interface{}, error)) capturedBugReport {
	t.Helper()
	var got capturedBugReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/problem-reports" {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		got.osVersion = r.FormValue("os_version")
		got.description = r.FormValue("description")
		got.guiVersion = r.FormValue("gui_version")
		got.kind = r.FormValue("kind")
		got.screenshotBodies = nil
		for _, header := range r.MultipartForm.File["screenshots"] {
			part, err := header.Open()
			if err != nil {
				t.Errorf("open screenshot: %v", err)
				continue
			}
			body, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				t.Errorf("read screenshot: %v", err)
				continue
			}
			got.screenshotBodies = append(got.screenshotBodies, body)
		}
		got.screenshotParts = len(got.screenshotBodies)
		file, header, err := r.FormFile("diagnostics")
		if err != nil {
			t.Errorf("diagnostics part: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
			t.Errorf("diagnostics filename = %s", header.Filename)
		}
		data, err := io.ReadAll(file)
		if err != nil {
			t.Errorf("read diagnostics: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Errorf("open zip: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		got.files = map[string][]byte{}
		for _, entry := range reader.File {
			rc, err := entry.Open()
			if err != nil {
				t.Errorf("open %s: %v", entry.Name, err)
				continue
			}
			body, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Errorf("read %s: %v", entry.Name, err)
				continue
			}
			got.files[entry.Name] = body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"BR-gui"}`))
	}))
	defer server.Close()
	token := testSkillMarketSignedToken("u_current", "phone:13301168516", time.Now().Add(time.Hour))
	saveBugReportHubPrincipal(t, app, server.URL, "u_current", token)
	result, err := submit()
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result["id"] != "BR-gui" {
		t.Fatalf("result = %+v", result)
	}
	return got
}

func expectedGUIBugReportOS(app *App) string {
	osVersion := strings.TrimSpace(app.getOSVersion())
	if osVersion == "" || strings.EqualFold(osVersion, "Unknown") {
		return runtime.GOOS
	}
	return osVersion
}

func tinyBugReportPNG(t *testing.T) []byte {
	t.Helper()
	return sizedBugReportPNG(t, 2, 2)
}

func sizedBugReportPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeDiagnosticFile(t *testing.T, base, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(base, dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, dir, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diagnosticFileExists(base, dir, name string) bool {
	_, err := os.Stat(filepath.Join(base, dir, name))
	return err == nil
}
