package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestConfigUIUsesSnapshotFromConfigSaveMsg(t *testing.T) {
	m := newConfigUIModel(corelib.AppConfig{})
	snapshot := corelib.AppConfig{MaclawLLMCurrentProvider: "Ollama Local", MaclawLLMUrl: "http://localhost:11434/v1", MaclawLLMModel: "qwen2.5-coder:32b"}
	updated, _ := m.Update(views.ConfigSaveMsg{Key: "maclaw_llm_provider_preset", Value: "Ollama Local", Config: snapshot, HasConfig: true})
	got := updated.(configUIModel)
	if got.cfg.MaclawLLMUrl != snapshot.MaclawLLMUrl || got.cfg.MaclawLLMModel != snapshot.MaclawLLMModel {
		t.Fatalf("config UI did not adopt snapshot: %#v", got.cfg)
	}
}

func TestConfigUISaveCmdReturnsFailureMessage(t *testing.T) {
	dataDirFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dataDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_DATA_DIR", dataDirFile)

	m := newConfigUIModel(corelib.AppConfig{})
	msg := m.saveCmd()()
	failed, ok := msg.(views.ConfigSaveFailedMsg)
	if !ok {
		t.Fatalf("saveCmd returned %T, want ConfigSaveFailedMsg", msg)
	}
	if failed.Key != "configuration" || failed.Error == "" {
		t.Fatalf("unexpected failure message: %#v", failed)
	}
}

func TestConfigUISaveCmdReturnsConfigurationLabelOnSuccess(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	m := newConfigUIModel(corelib.AppConfig{MaclawLLMModel: "qwen2.5-coder:32b"})
	msg := m.saveCmd()()
	saved, ok := msg.(views.ConfigSavedMsg)
	if !ok {
		t.Fatalf("saveCmd returned %T, want ConfigSavedMsg", msg)
	}
	if saved.Key != "configuration" {
		t.Fatalf("saved key = %q, want configuration", saved.Key)
	}
}

func TestConfigUIShowsSaveFailureStatus(t *testing.T) {
	m := newConfigUIModel(corelib.AppConfig{})
	updated, _ := m.Update(views.ConfigSaveFailedMsg{Key: "configuration", Error: "disk full"})
	got := updated.(configUIModel)
	if !strings.Contains(got.status, "disk full") {
		t.Fatalf("status = %q, want save error", got.status)
	}
}

func TestConfigUIStatusFollowsLanguage(t *testing.T) {
	zh := newConfigUIModel(corelib.AppConfig{Language: "zh"})
	if !strings.Contains(zh.status, "选择/执行") {
		t.Fatalf("zh status = %q, want Chinese text", zh.status)
	}
	en := newConfigUIModel(corelib.AppConfig{Language: "en"})
	if !strings.Contains(en.status, "Enter opens choices/actions") {
		t.Fatalf("en status = %q, want English text", en.status)
	}
}

func TestConfigUIStatusUsesLocalizedConfigLabels(t *testing.T) {
	zh := newConfigUIModel(corelib.AppConfig{Language: "zh"})
	updated, _ := zh.Update(views.ConfigSaveMsg{Key: "maclaw_llm_provider_preset", Value: "Ollama Local"})
	got := updated.(configUIModel)
	if !strings.Contains(got.status, "LLM 服务商") || strings.Contains(got.status, "maclaw_llm_provider_preset") {
		t.Fatalf("Chinese changed status should use localized label: %q", got.status)
	}

	en := newConfigUIModel(corelib.AppConfig{Language: "en"})
	updated, _ = en.Update(views.ConfigSavedMsg{Key: "maclaw_llm_provider_preset"})
	got = updated.(configUIModel)
	if !strings.Contains(got.status, "LLM provider") || strings.Contains(got.status, "maclaw_llm_provider_preset") {
		t.Fatalf("English saved status should use localized label: %q", got.status)
	}
}

func TestConfigUIViewFooterFitsWidth(t *testing.T) {
	m := newConfigUIModel(corelib.AppConfig{Language: "en"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 32, Height: 18})
	got := updated.(configUIModel)
	got.status = "Service Redeem is in the full TUI: run maclaw-tui redeem, press F5 in maclaw-tui, or type /redeem in chat."
	view := got.View()
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	footer := stripANSIForConfigUITest(lines[len(lines)-1])
	if width := lipgloss.Width(footer); width > 32 {
		t.Fatalf("footer width = %d, want <= 32: %q", width, footer)
	}
}

func stripANSIForConfigUITest(s string) string {
	ansiRE := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	return ansiRE.ReplaceAllString(s, "")
}

func TestConfigUIHandlesSetupNavigationMessage(t *testing.T) {
	m := newConfigUIModel(corelib.AppConfig{Language: "en"})
	updated, cmd := m.Update(views.ConfigOpenSetupMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(configUIModel)
	if !strings.Contains(got.status, "maclaw-tui setup") || !strings.Contains(got.status, "press F1") || !strings.Contains(got.status, "/setup") {
		t.Fatalf("status = %q", got.status)
	}
}

func TestConfigUIHandlesRedeemNavigationMessage(t *testing.T) {
	m := newConfigUIModel(corelib.AppConfig{Language: "en"})
	updated, cmd := m.Update(views.ConfigOpenServiceRedeemMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(configUIModel)
	if !strings.Contains(got.status, "maclaw-tui redeem") || !strings.Contains(got.status, "press F5") || !strings.Contains(got.status, "/redeem") {
		t.Fatalf("status = %q", got.status)
	}
}

func TestConfigUIHandlesToolsNavigationMessage(t *testing.T) {
	m := newConfigUIModel(corelib.AppConfig{Language: "en"})
	updated, cmd := m.Update(views.ConfigOpenToolsMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(configUIModel)
	if !strings.Contains(got.status, "maclaw-tui mcp") || !strings.Contains(got.status, "press F3") || !strings.Contains(got.status, "/mcp") {
		t.Fatalf("status = %q", got.status)
	}
}
