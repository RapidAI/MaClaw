package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestStatusTextGuidesFreshMachineToSetup(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{Language: "en"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--text"})
	})
	if err != nil {
		t.Fatalf("status --text error = %v", err)
	}
	for _, want := range []string{"MaClaw TUI status", "HubCenter:", "Hub URL:   auto-selected", "[ ] Setup", "[ ] Hub activation", "[ ] Remote machine", "[ ] LLM", "maclaw-tui setup"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status --text missing %q:\n%s", want, out)
		}
	}
}

func TestStatusTextUsesSavedEmailForActivationNextStep(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:      "en",
		RemoteEmail:   "user@example.com",
		RemoteHubURL:  "",
		RemoteEnabled: true,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--text"})
	})
	if err != nil {
		t.Fatalf("status --text error = %v", err)
	}
	if !strings.Contains(out, "email is saved") || !strings.Contains(out, "press Enter to activate Hub") {
		t.Fatalf("status should use saved email to guide activation:\n%s", out)
	}
}

func TestStatusTextLocalizesChineseNextAction(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:      "zh",
		RemoteEmail:   "user@example.com",
		RemoteEnabled: true,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--text"})
	})
	if err != nil {
		t.Fatalf("status --text error = %v", err)
	}
	if !strings.Contains(out, "邮箱已保存") || !strings.Contains(out, "Hub URL 会根据 HubCenter 自动选择") {
		t.Fatalf("Chinese status should localize next action:\n%s", out)
	}
	if strings.Contains(out, "email is saved") || strings.Contains(out, "press Enter to activate") {
		t.Fatalf("Chinese status should not mix English next action:\n%s", out)
	}
}

func TestStatusTextMarksRemoteMachineOptionalWhenHubReady(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:          "en",
		RemoteEmail:       "user@example.com",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--text"})
	})
	if err != nil {
		t.Fatalf("status --text error = %v", err)
	}
	if !strings.Contains(out, "[ ] Remote machine (optional for remote tasks)") {
		t.Fatalf("status should mark remote machine as optional once Hub service is ready:\n%s", out)
	}
	if !strings.Contains(out, "maclaw-tui redeem") {
		t.Fatalf("Hub-ready status should still guide to service redeem:\n%s", out)
	}

	jsonOut, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("status --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
		t.Fatalf("parse status JSON %q: %v", jsonOut, err)
	}
	if info["remote_activation_state"] != "service_ready" {
		t.Fatalf("remote_activation_state = %#v", info["remote_activation_state"])
	}
}

func TestStatusTextDoesNotMarkKeyedLLMReadyWithoutKey(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:                 "en",
		MaclawLLMUrl:             "https://api.openai.com/v1",
		MaclawLLMModel:           "gpt-4o",
		MaclawLLMCurrentProvider: "OpenAI API Key",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--text"})
	})
	if err != nil {
		t.Fatalf("status --text error = %v", err)
	}
	if !strings.Contains(out, "[ ] LLM (API key needed)") || !strings.Contains(out, "maclaw-tui llm setup") {
		t.Fatalf("status should guide missing-key LLM to TUI key setup:\n%s", out)
	}
	if strings.Contains(out, "[x] LLM") {
		t.Fatalf("missing-key LLM must not be marked ready:\n%s", out)
	}

	jsonOut, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("status --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
		t.Fatalf("parse status JSON %q: %v", jsonOut, err)
	}
	if info["llm_ready"] != false || info["llm_needs_key"] != true || info["next_tui_command"] != "maclaw-tui llm setup" {
		t.Fatalf("missing-key status JSON = ready %#v needs_key %#v next %#v", info["llm_ready"], info["llm_needs_key"], info["next_tui_command"])
	}
}

func TestStatusTextTreatsLocalNetworkLLMWithoutKeyAsReady(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:                 "en",
		MaclawLLMUrl:             "192.168.1.20:11434/v1",
		MaclawLLMModel:           "qwen2.5-coder:32b",
		MaclawLLMCurrentProvider: "Custom",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--text"})
	})
	if err != nil {
		t.Fatalf("status --text error = %v", err)
	}
	if !strings.Contains(out, "[x] LLM") || strings.Contains(out, "API key needed") {
		t.Fatalf("local-network LLM should be ready without API key:\n%s", out)
	}
	if !strings.Contains(out, "maclaw-tui mcp") {
		t.Fatalf("ready local-network LLM should guide to optional MCP templates:\n%s", out)
	}
}

func TestStatusJSONReportsReadyChecklistAndNextMCPTemplate(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		OnboardingDone:           true,
		RemoteHubURL:             "https://hub.example",
		RemoteHubCenterURL:       "https://center.example",
		RemoteMachineID:          "machine-1",
		RemoteMachineToken:       "machine-token",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: "official",
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "qwen-max",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         "official",
			IsHubService: true,
		}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("status --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("parse status JSON %q: %v", out, err)
	}
	for _, key := range []string{"setup_ready", "hub_ready", "remote_machine_ready", "official_service_ready", "llm_ready"} {
		if info[key] != true {
			t.Fatalf("%s = %#v, want true in %s", key, info[key], out)
		}
	}
	if info["hubcenter_url"] != "https://center.example" || info["hub_url"] != "https://hub.example" {
		t.Fatalf("hub fields = center %#v hub %#v", info["hubcenter_url"], info["hub_url"])
	}
	if info["remote_activation_state"] != "active" {
		t.Fatalf("remote_activation_state = %#v", info["remote_activation_state"])
	}
	if info["mcp_count"] != float64(0) {
		t.Fatalf("mcp_count = %#v", info["mcp_count"])
	}
	if info["next_tui_command"] != "maclaw-tui mcp" {
		t.Fatalf("next_tui_command = %#v", info["next_tui_command"])
	}
}

func TestStatusJSONRecognizesEnglishOfficialProviderName(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: "MaClaw Official",
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "qwen-max",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         "MaClaw Official",
			IsHubService: true,
		}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return RunStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("status --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("parse status JSON %q: %v", out, err)
	}
	if info["official_service_ready"] != true {
		t.Fatalf("official_service_ready = %#v", info["official_service_ready"])
	}
}
