package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func setupVisionOCRTestApp(t *testing.T, url string, supportsVision bool, protocol string) *App {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if protocol == "" {
		protocol = "openai"
	}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "Vision Test", URL: url, Key: "k", Model: "test-model",
			Protocol: protocol, SupportsVision: supportsVision, IsCustom: true,
		}},
		MaclawLLMCurrentProvider: "Vision Test",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	return app
}

func TestVisionRecognizeImage_RejectsNonVisionModel(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	app := setupVisionOCRTestApp(t, srv.URL, false, "openai")
	if _, err := app.visionRecognizeImage("aGVsbG8=", "prompt"); err == nil {
		t.Fatal("visionRecognizeImage() = nil error for a non-vision model, want error")
	} else if !errors.Is(err, browser.ErrVisionUnsupported) {
		t.Fatalf("error = %v, want wrapped ErrVisionUnsupported (breaker must ignore it)", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("non-vision model triggered %d HTTP requests, want 0", requests.Load())
	}
}

func TestVisionRecognizeImage_OpenAISendsImageBlock(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" recognized text "}}]}`))
	}))
	defer srv.Close()

	app := setupVisionOCRTestApp(t, srv.URL, true, "openai")
	got, err := app.visionRecognizeImage("aGVsbG8=", "read this screenshot")
	if err != nil {
		t.Fatalf("visionRecognizeImage() error = %v", err)
	}
	if got != "recognized text" {
		t.Fatalf("visionRecognizeImage() = %q, want %q", got, "recognized text")
	}
	if !strings.Contains(body, `"image_url"`) || !strings.Contains(body, "data:image/png;base64,aGVsbG8=") {
		t.Fatalf("request missing OpenAI image block: %s", body)
	}
	if !strings.Contains(body, "read this screenshot") {
		t.Fatalf("request missing prompt: %s", body)
	}
}

func TestVisionRecognizeImage_AnthropicSendsImageBlock(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"anthropic recognized"}]}`))
	}))
	defer srv.Close()

	app := setupVisionOCRTestApp(t, srv.URL, true, "anthropic")
	got, err := app.visionRecognizeImage("aGVsbG8=", "read this screenshot")
	if err != nil {
		t.Fatalf("visionRecognizeImage() error = %v", err)
	}
	if got != "anthropic recognized" {
		t.Fatalf("visionRecognizeImage() = %q, want %q", got, "anthropic recognized")
	}
	if !strings.Contains(body, `"image"`) || !strings.Contains(body, "aGVsbG8=") {
		t.Fatalf("request missing Anthropic image block: %s", body)
	}
}

func TestOCRConfiguredEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	// No config file → default true.
	if !ocrConfiguredEnabled() {
		t.Fatal("ocrConfiguredEnabled() = false without config, want default true")
	}

	// Old config without the flag → default true.
	if err := os.MkdirAll(filepath.Join(tmpHome, ".maclaw"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ocrConfiguredEnabled() {
		t.Fatal("ocrConfiguredEnabled() = false for config without ocr_enabled, want true")
	}

	// Explicitly disabled → false.
	if err := os.WriteFile(cfgPath, []byte(`{"ocr_enabled":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if ocrConfiguredEnabled() {
		t.Fatal("ocrConfiguredEnabled() = true with ocr_enabled=false, want false")
	}
}

type stubLocalOCRProvider struct{ calls int }

func (s *stubLocalOCRProvider) Recognize(string) ([]browser.OCRResult, error) {
	s.calls++
	return []browser.OCRResult{{Text: "native"}}, nil
}
func (s *stubLocalOCRProvider) IsAvailable() bool { return true }
func (s *stubLocalOCRProvider) Close()            {}

func writeOCREnabledConfig(t *testing.T, enabled bool) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	if err := os.MkdirAll(filepath.Join(tmpHome, ".maclaw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpHome, ".maclaw", "config.json"),
		[]byte(fmt.Sprintf(`{"ocr_enabled":%v}`, enabled)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigGatedOCRProvider_RespectsToggle(t *testing.T) {
	writeOCREnabledConfig(t, false)
	stub := &stubLocalOCRProvider{}
	gated := &configGatedOCRProvider{inner: stub}

	if gated.IsAvailable() {
		t.Fatal("IsAvailable = true with ocr_enabled=false")
	}
	if _, err := gated.Recognize("png"); err == nil {
		t.Fatal("Recognize = nil error with ocr_enabled=false")
	}
	if stub.calls != 0 {
		t.Fatalf("inner engine called %d times while disabled", stub.calls)
	}
}

func TestConfigGatedOCRProvider_EnabledDelegates(t *testing.T) {
	writeOCREnabledConfig(t, true)
	stub := &stubLocalOCRProvider{}
	gated := &configGatedOCRProvider{inner: stub}

	if !gated.IsAvailable() {
		t.Fatal("IsAvailable = false with ocr_enabled=true")
	}
	got, err := gated.Recognize("png")
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(got) != 1 || got[0].Text != "native" || stub.calls != 1 {
		t.Fatalf("Recognize = %+v (calls=%d), want delegated result", got, stub.calls)
	}
}

func TestWarmComputerUseOCR_SkipsWhenDisabled(t *testing.T) {
	writeOCREnabledConfig(t, false)
	out := warmComputerUseOCR()
	if skipped, _ := out["skipped"].(bool); !skipped {
		t.Fatalf("warmComputerUseOCR not skipped with ocr_enabled=false: %v", out)
	}
	note, _ := out["note"].(string)
	if !strings.Contains(note, "disabled") {
		t.Fatalf("missing disabled note: %v", out)
	}
	if warm, _ := out["warm_ok"].(bool); warm {
		t.Fatalf("engine warmed despite disabled toggle: %v", out)
	}
}
