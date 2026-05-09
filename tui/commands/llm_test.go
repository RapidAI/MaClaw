package commands

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLLMStatusUnconfiguredGuidesToTUISetup(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	err = llmStatus(nil)
	_ = w.Close()
	os.Stdout = oldStdout
	if err != nil {
		t.Fatalf("llmStatus() error = %v", err)
	}
	outBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	out := string(outBytes)
	if !strings.Contains(out, "llm setup") || !strings.Contains(out, "llm setup cli") {
		t.Fatalf("unconfigured llm status should guide to TUI setup with CLI escape hatch:\n%s", out)
	}
	if !strings.Contains(out, "maclaw-tui status") {
		t.Fatalf("unconfigured llm status should also point to the TUI status overview:\n%s", out)
	}
	if strings.Contains(out, "config set --local") {
		t.Fatalf("unconfigured llm status should not send new users to raw config set:\n%s", out)
	}
}

func TestLLMSetupCLIPresetsIncludeNoKeyLocalProviders(t *testing.T) {
	providers := presetProviders()
	found := map[string]string{}
	for _, provider := range providers {
		found[provider.Name] = provider.AuthType
	}
	for _, name := range []string{"Ollama Local", "LM Studio Local"} {
		authType, ok := found[name]
		if !ok {
			t.Fatalf("CLI LLM setup providers missing %q", name)
		}
		if authType != "none" {
			t.Fatalf("%s auth type = %q, want none", name, authType)
		}
	}
	if llmURLUsuallyNeedsKey("192.168.1.20:11434/v1") {
		t.Fatal("raw LAN LLM endpoint should not require an API key")
	}
}

func TestLLMStatusJSONIncludesNextTUICommand(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	out, err := captureLLMStdout(t, func() error {
		return llmStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("llmStatus --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("parse json %q: %v", out, err)
	}
	if info["next_tui_command"] != "maclaw-tui llm setup" {
		t.Fatalf("next_tui_command = %#v", info["next_tui_command"])
	}
	if next, ok := info["next_action"].(string); !ok || !strings.Contains(next, "maclaw-tui status") {
		t.Fatalf("next_action = %#v", info["next_action"])
	}
}

func TestLLMStatusHubReadyGuidesToServiceRedeem(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureLLMStdout(t, func() error {
		return llmStatus(nil)
	})
	if err != nil {
		t.Fatalf("llmStatus error = %v", err)
	}
	if !strings.Contains(out, "maclaw-tui redeem") || !strings.Contains(out, "官方服务") {
		t.Fatalf("Hub-ready LLM status should guide to service redeem:\n%s", out)
	}
	if strings.Contains(out, "llm setup cli") {
		t.Fatalf("Hub-ready LLM status should not prioritize the text setup wizard:\n%s", out)
	}

	jsonOut, err := captureLLMStdout(t, func() error {
		return llmStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("llmStatus --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
		t.Fatalf("parse json %q: %v", jsonOut, err)
	}
	if info["hub_service_ready"] != true {
		t.Fatalf("hub_service_ready = %#v", info["hub_service_ready"])
	}
	if info["next_tui_command"] != "maclaw-tui redeem" {
		t.Fatalf("next_tui_command = %#v", info["next_tui_command"])
	}
}

func TestLLMStatusLocalizesEnglishOutput(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{Language: "en"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureLLMStdout(t, func() error {
		return llmStatus(nil)
	})
	if err != nil {
		t.Fatalf("llmStatus error = %v", err)
	}
	for _, want := range []string{
		"LLM status: not configured",
		"Run maclaw-tui llm setup",
		"scripted text wizard",
		"Next: Run maclaw-tui llm setup",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("English LLM status missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "LLM 状态") || strings.Contains(out, "下一步") {
		t.Fatalf("English LLM status should not mix Chinese labels:\n%s", out)
	}
}

func TestLLMStatusConfiguredGuidesOptionalMCPTemplate(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:       "en",
		MaclawLLMUrl:   "http://localhost:11434/v1",
		MaclawLLMModel: "qwen2.5-coder:32b",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureLLMStdout(t, func() error {
		return llmStatus(nil)
	})
	if err != nil {
		t.Fatalf("llmStatus error = %v", err)
	}
	if !strings.Contains(out, "maclaw-tui mcp") || !strings.Contains(out, "tool templates") {
		t.Fatalf("configured LLM without MCP should guide to MCP templates:\n%s", out)
	}

	jsonOut, err := captureLLMStdout(t, func() error {
		return llmStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("llmStatus --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
		t.Fatalf("parse json %q: %v", jsonOut, err)
	}
	if info["mcp_count"] != float64(0) || info["next_tui_command"] != "maclaw-tui mcp" {
		t.Fatalf("MCP JSON guidance = count %#v next %#v", info["mcp_count"], info["next_tui_command"])
	}
}

func TestLLMStatusKeyedProviderWithoutKeyIsNotConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  corelib.AppConfig
	}{
		{
			name: "preset",
			cfg: corelib.AppConfig{
				Language:                 "en",
				MaclawLLMUrl:             "https://api.openai.com/v1",
				MaclawLLMModel:           "gpt-4o",
				MaclawLLMCurrentProvider: "OpenAI API Key",
			},
		},
		{
			name: "saved provider missing auth type",
			cfg: corelib.AppConfig{
				Language:                 "en",
				MaclawLLMUrl:             "https://llm.example/v1",
				MaclawLLMModel:           "cloud-model",
				MaclawLLMCurrentProvider: "Corp Gateway",
				MaclawLLMProviders: []corelib.MaclawLLMProvider{{
					Name:  "Corp Gateway",
					URL:   "https://llm.example/v1",
					Model: "cloud-model",
				}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			t.Setenv("MACLAW_DATA_DIR", dataDir)
			if err := NewFileConfigStore(dataDir).SaveConfig(tc.cfg); err != nil {
				t.Fatalf("save config: %v", err)
			}

			out, err := captureLLMStdout(t, func() error {
				return llmStatus(nil)
			})
			if err != nil {
				t.Fatalf("llmStatus error = %v", err)
			}
			if !strings.Contains(out, "LLM status: not configured") || !strings.Contains(out, "needs an API key") {
				t.Fatalf("missing-key LLM status should not be reported as configured:\n%s", out)
			}

			jsonOut, err := captureLLMStdout(t, func() error {
				return llmStatus([]string{"--json"})
			})
			if err != nil {
				t.Fatalf("llmStatus --json error = %v", err)
			}
			var info map[string]any
			if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
				t.Fatalf("parse json %q: %v", jsonOut, err)
			}
			if info["configured"] != false || info["missing_key"] != true || info["next_tui_command"] != "maclaw-tui llm setup" {
				t.Fatalf("missing-key JSON = configured %#v missing_key %#v next %#v", info["configured"], info["missing_key"], info["next_tui_command"])
			}
		})
	}
}

func TestLLMStatusLocalNetworkLLMWithoutKeyIsConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  corelib.AppConfig
	}{
		{
			name: "custom lan provider",
			cfg: corelib.AppConfig{
				Language:                 "en",
				MaclawLLMUrl:             "http://192.168.1.20:11434/v1",
				MaclawLLMModel:           "qwen2.5-coder:32b",
				MaclawLLMCurrentProvider: "Custom",
			},
		},
		{
			name: "saved docker host provider missing auth type",
			cfg: corelib.AppConfig{
				Language:                 "en",
				MaclawLLMUrl:             "host.docker.internal:1234/v1",
				MaclawLLMModel:           "local-model",
				MaclawLLMCurrentProvider: "Local Gateway",
				MaclawLLMProviders: []corelib.MaclawLLMProvider{{
					Name:  "Local Gateway",
					URL:   "host.docker.internal:1234/v1",
					Model: "local-model",
				}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			t.Setenv("MACLAW_DATA_DIR", dataDir)
			if err := NewFileConfigStore(dataDir).SaveConfig(tc.cfg); err != nil {
				t.Fatalf("save config: %v", err)
			}

			out, err := captureLLMStdout(t, func() error {
				return llmStatus(nil)
			})
			if err != nil {
				t.Fatalf("llmStatus error = %v", err)
			}
			if strings.Contains(out, "needs an API key") {
				t.Fatalf("local-network LLM status should not require an API key:\n%s", out)
			}
			if !strings.Contains(out, "maclaw-tui mcp") {
				t.Fatalf("local-network LLM should be configured enough to guide to MCP templates:\n%s", out)
			}

			jsonOut, err := captureLLMStdout(t, func() error {
				return llmStatus([]string{"--json"})
			})
			if err != nil {
				t.Fatalf("llmStatus --json error = %v", err)
			}
			var info map[string]any
			if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
				t.Fatalf("parse json %q: %v", jsonOut, err)
			}
			if info["configured"] != true || info["missing_key"] != false || info["next_tui_command"] != "maclaw-tui mcp" {
				t.Fatalf("local-network JSON = configured %#v missing_key %#v next %#v", info["configured"], info["missing_key"], info["next_tui_command"])
			}
		})
	}
}

func TestLoadLLMConfigUsesViewerTokenForOfficialService(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "auto",
		MaclawLLMCurrentProvider: "MaClaw官方",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         "MaClaw官方",
			IsHubService: true,
		}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	llm, err := LoadLLMConfig()
	if err != nil {
		t.Fatalf("LoadLLMConfig() error = %v", err)
	}
	if llm.Key != "viewer-token" {
		t.Fatalf("LLM key = %q, want viewer token fallback", llm.Key)
	}
}

func TestLoadLLMConfigUsesCurrentProviderKeyFallback(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:                 "en",
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMModel:           "cloud-model",
		MaclawLLMCurrentProvider: "Corp Gateway",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "Corp Gateway",
			URL:      "https://llm.example/v1",
			Key:      "sk-provider",
			Model:    "cloud-model",
			AuthType: "apikey",
		}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	llm, err := LoadLLMConfig()
	if err != nil {
		t.Fatalf("LoadLLMConfig() error = %v", err)
	}
	if llm.Key != "sk-provider" {
		t.Fatalf("LLM key = %q, want provider key fallback", llm.Key)
	}

	jsonOut, err := captureLLMStdout(t, func() error {
		return llmStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("llmStatus --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
		t.Fatalf("parse json %q: %v", jsonOut, err)
	}
	if info["configured"] != true || info["missing_key"] != false {
		t.Fatalf("provider-key JSON = configured %#v missing_key %#v", info["configured"], info["missing_key"])
	}
}

func TestLLMStatusConfiguredWithMCPKeepsStatusOverviewNext(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		Language:        "en",
		MaclawLLMUrl:    "http://localhost:11434/v1",
		MaclawLLMModel:  "qwen2.5-coder:32b",
		LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	jsonOut, err := captureLLMStdout(t, func() error {
		return llmStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("llmStatus --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
		t.Fatalf("parse json %q: %v", jsonOut, err)
	}
	if info["mcp_count"] != float64(1) || info["next_tui_command"] != "maclaw-tui status" {
		t.Fatalf("MCP-ready JSON guidance = count %#v next %#v", info["mcp_count"], info["next_tui_command"])
	}
}

func TestLLMValidationCommandsGuideToTUISetup(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	for name, fn := range map[string]func([]string) error{
		"test": llmTest,
		"ping": llmPing,
	} {
		t.Run(name, func(t *testing.T) {
			err := fn(nil)
			if err == nil {
				t.Fatal("expected unconfigured LLM error")
			}
			msg := err.Error()
			if !strings.Contains(msg, "llm setup") || !strings.Contains(msg, "llm setup cli") {
				t.Fatalf("%s error should guide to TUI setup: %s", name, msg)
			}
		})
	}
}

func captureLLMStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout
	outBytes, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(outBytes), runErr
}
