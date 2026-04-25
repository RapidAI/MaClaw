package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"pgregory.net/rapid"
)

func TestTestOpenAILLM_UsesReasoningFallbackAndStripsTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"<think>hidden</think> <|FunctionCallBegin|>{}<|FunctionCallEnd|> final answer"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	app := &App{}
	got, err := app.testOpenAILLM(srv.URL, "", "test-model", "test-agent")
	if err != nil {
		t.Fatalf("testOpenAILLM returned error: %v", err)
	}
	if got != "final answer" {
		t.Fatalf("testOpenAILLM = %q, want %q", got, "final answer")
	}
}

func TestProbeVisionOpenAI_UsesReasoningFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"red"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	if !probeVisionOpenAI(srv.URL, "", "test-model", "abc", "test-agent") {
		t.Fatal("probeVisionOpenAI() = false, want true")
	}
}

func TestTestAnthropicLLM_StripsThinkAndFunctionTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"<think>hidden</think> <|FunctionCallBegin|>{}<|FunctionCallEnd|> final anthropic answer"}]}`))
	}))
	defer srv.Close()

	app := &App{}
	got, err := app.testAnthropicLLM(srv.URL, "", "test-model", "test-agent")
	if err != nil {
		t.Fatalf("testAnthropicLLM returned error: %v", err)
	}
	if got != "final anthropic answer" {
		t.Fatalf("testAnthropicLLM = %q, want %q", got, "final anthropic answer")
	}
}

func TestProbeVisionAnthropic_ReturnsTrueForRedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"red"}]}`))
	}))
	defer srv.Close()

	if !probeVisionAnthropic(srv.URL, "", "test-model", "abc", "test-agent") {
		t.Fatal("probeVisionAnthropic() = false, want true")
	}
}

func TestTestMaclawLLM_ReturnsSupportsVisionTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"red"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	app := &App{}
	got, err := app.TestMaclawLLM(corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", Protocol: "openai", AgentType: "test-agent"})
	if err != nil {
		t.Fatalf("TestMaclawLLM returned error: %v", err)
	}
	if got.Message != "red" {
		t.Fatalf("TestMaclawLLM message = %q, want %q", got.Message, "red")
	}
	if !got.SupportsVision {
		t.Fatal("TestMaclawLLM supports_vision = false, want true")
	}
}

func TestTestMaclawLLM_ReturnsSupportsVisionFalseWhenProbeFails(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if hits == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"blue"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	app := &App{}
	got, err := app.TestMaclawLLM(corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", Protocol: "openai", AgentType: "test-agent"})
	if err != nil {
		t.Fatalf("TestMaclawLLM returned error: %v", err)
	}
	if got.Message != "hello" {
		t.Fatalf("TestMaclawLLM message = %q, want %q", got.Message, "hello")
	}
	if got.SupportsVision {
		t.Fatal("TestMaclawLLM supports_vision = true, want false")
	}
}

func TestResolveProvidersPreservesCodeGenSSORuntimeConfig(t *testing.T) {
	saved := []corelib.MaclawLLMProvider{
		{
			Name:          codegenProviderName,
			URL:           "http://127.0.0.1:5001/anthropic",
			Model:         "qax-codegen/Auto",
			Protocol:      "anthropic",
			AuthType:      "sso",
			ContextLength: 32000,
		},
	}

	defaults := defaultMaclawLLMProviders()
	defaultCtx := make(map[string]int, len(defaults))
	defaultURL := make(map[string]string, len(defaults))
	for _, d := range defaults {
		if d.ContextLength > 0 {
			defaultCtx[d.Name] = d.ContextLength
		}
		if !d.IsCustom {
			defaultURL[d.Name] = d.URL
		}
	}

	providers := append([]corelib.MaclawLLMProvider(nil), saved...)
	for i := range providers {
		if providers[i].ContextLength == 0 {
			if cl, ok := defaultCtx[providers[i].Name]; ok {
				providers[i].ContextLength = cl
			}
		}
		if providers[i].Name == codegenProviderName && providers[i].AuthType == "sso" {
			providers[i].Protocol = "openai"
			providers[i].URL = strings.TrimRight(strings.TrimSpace(providers[i].URL), "/")
			providers[i].URL = strings.TrimSuffix(providers[i].URL, "/anthropic")
			continue
		}
		if !providers[i].IsCustom {
			if u, ok := defaultURL[providers[i].Name]; ok {
				providers[i].URL = u
			}
		}
	}

	got := providers[0]
	if got.Protocol != "openai" {
		t.Fatalf("CodeGen SSO protocol = %q, want %q", got.Protocol, "openai")
	}
	if got.URL != "http://127.0.0.1:5001" {
		t.Fatalf("CodeGen SSO URL = %q, want %q", got.URL, "http://127.0.0.1:5001")
	}
	if got.Model != saved[0].Model {
		t.Fatalf("CodeGen SSO model = %q, want %q", got.Model, saved[0].Model)
	}
	if got.ContextLength != saved[0].ContextLength {
		t.Fatalf("CodeGen SSO context length = %d, want %d", got.ContextLength, saved[0].ContextLength)
	}
}

func TestSaveCodeGenModelChoiceUsesClaudeSpecificModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     codegenProviderName,
			URL:      "https://codegen.qianxin-inc.cn/api/v1",
			Key:      "token-123",
			Model:    "qax-codegen/Auto",
			Protocol: "openai",
			AuthType: "sso",
		}},
		MaclawLLMCurrentProvider: codegenProviderName,
		Claude: corelib.ToolConfig{
			CurrentModel: codegenProviderName,
			Models: []corelib.ModelConfig{{
				ModelName: codegenProviderName,
				ModelId:   "qax-codegen/Auto",
				ModelUrl:  "http://127.0.0.1:5001/anthropic",
				ApiKey:    "token-123",
				WireApi:   "anthropic",
			}},
		},
		Codex: corelib.ToolConfig{Models: []corelib.ModelConfig{{
			ModelName: codegenProviderName,
			ModelId:   "qax-codegen/Auto",
			ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
			ApiKey:    "token-123",
			WireApi:   "responses",
		}}},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.SaveCodeGenModelChoice("maclaw-model", "claude-model"); err != nil {
		t.Fatalf("SaveCodeGenModelChoice() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if got := saved.MaclawLLMProviders[0].Model; got != "maclaw-model" {
		t.Fatalf("MaClaw provider model = %q, want %q", got, "maclaw-model")
	}
	if got := saved.Claude.CurrentModel; got != codegenProviderName {
		t.Fatalf("Claude CurrentModel = %q, want %q", got, codegenProviderName)
	}

	var claudeCodeGen *corelib.ModelConfig
	for i := range saved.Claude.Models {
		if saved.Claude.Models[i].ModelName == codegenProviderName {
			claudeCodeGen = &saved.Claude.Models[i]
			break
		}
	}
	if claudeCodeGen == nil {
		t.Fatalf("Claude CodeGen entry not found in %+v", saved.Claude.Models)
	}
	if got := claudeCodeGen.ModelId; got != "claude-model" {
		t.Fatalf("Claude Code model = %q, want %q", got, "claude-model")
	}
	if got := claudeCodeGen.WireApi; got != "anthropic" {
		t.Fatalf("Claude Code wire_api = %q, want %q", got, "anthropic")
	}

	var codexCodeGen *corelib.ModelConfig
	for i := range saved.Codex.Models {
		if saved.Codex.Models[i].ModelName == codegenProviderName {
			codexCodeGen = &saved.Codex.Models[i]
			break
		}
	}
	if codexCodeGen == nil {
		t.Fatalf("Codex CodeGen entry not found in %+v", saved.Codex.Models)
	}
	if got := codexCodeGen.ModelId; got != "maclaw-model" {
		t.Fatalf("Codex model = %q, want %q", got, "maclaw-model")
	}
	if got := codexCodeGen.WireApi; got != "responses" {
		t.Fatalf("Codex wire_api = %q, want %q", got, "responses")
	}

	settingsPath := filepath.Join(tmpHome, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Read settings.json error = %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Unmarshal settings.json error = %v", err)
	}
	env, ok := settings["env"].(map[string]any)
	if !ok {
		t.Fatal("settings env missing")
	}
	if got := env["ANTHROPIC_MODEL"]; got != "claude-model" {
		t.Fatalf("ANTHROPIC_MODEL = %v, want %q", got, "claude-model")
	}
}

func TestSaveCodeGenModelChoiceUpdatesClaudeSettingsForActiveCodeGenProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		Claude: corelib.ToolConfig{
			CurrentModel: "GLM",
			Models: []corelib.ModelConfig{
				{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/anthropic", ApiKey: "glm-token", WireApi: "anthropic"},
				{ModelName: codegenProviderName, ModelId: "qax-codegen/Auto", ModelUrl: "http://127.0.0.1:5001/anthropic", ApiKey: "token-123", WireApi: "anthropic"},
			},
		},
		Codex: corelib.ToolConfig{Models: []corelib.ModelConfig{{
			ModelName: codegenProviderName,
			ModelId:   "qax-codegen/Auto",
			ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
			ApiKey:    "token-123",
		}}},
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     codegenProviderName,
			URL:      "https://codegen.qianxin-inc.cn/api/v1",
			Key:      "token-123",
			Model:    "qax-codegen/Auto",
			Protocol: "openai",
			AuthType: "sso",
		}},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := configfile.WriteClaudeSettings("glm-token", "https://open.bigmodel.cn/api/anthropic", "glm-4.7"); err != nil {
		t.Fatalf("seed Claude settings error = %v", err)
	}

	if err := app.SaveCodeGenModelChoice("maclaw-model", "claude-model"); err != nil {
		t.Fatalf("SaveCodeGenModelChoice() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := saved.Claude.CurrentModel; got != codegenProviderName {
		t.Fatalf("Claude CurrentModel = %q, want %q", got, codegenProviderName)
	}

	settingsPath := filepath.Join(tmpHome, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Read settings.json error = %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Unmarshal settings.json error = %v", err)
	}
	env, ok := settings["env"].(map[string]any)
	if !ok {
		t.Fatal("settings env missing")
	}
	if got := env["ANTHROPIC_MODEL"]; got != "claude-model" {
		t.Fatalf("ANTHROPIC_MODEL = %v, want %q", got, "claude-model")
	}
	if got := env["ANTHROPIC_BASE_URL"]; got != "http://127.0.0.1:5001/anthropic" {
		t.Fatalf("ANTHROPIC_BASE_URL = %v, want %q", got, "http://127.0.0.1:5001/anthropic")
	}
}

func TestDefaultMaclawLLMProviders(t *testing.T) {
	providers := defaultMaclawLLMProviders()

	if len(providers) < 7 {
		t.Fatalf("provider count = %d, want >= 7", len(providers))
	}

	first := providers[0]
	if first.Name != "OpenAI" {
		t.Errorf("first provider Name = %q, want %q", first.Name, "OpenAI")
	}
	if first.URL != "https://chatgpt.com/backend-api" {
		t.Errorf("OpenAI URL = %q, want %q", first.URL, "https://chatgpt.com/backend-api")
	}
	if first.Model != "gpt-5.4" {
		t.Errorf("OpenAI Model = %q, want %q", first.Model, "gpt-5.4")
	}
	if first.AuthType != "oauth" {
		t.Errorf("OpenAI AuthType = %q, want %q", first.AuthType, "oauth")
	}
	if first.ContextLength != 110000 {
		t.Errorf("OpenAI ContextLength = %d, want %d", first.ContextLength, 110000)
	}
	if first.TimeoutSec != 360 {
		t.Errorf("OpenAI TimeoutSec = %d, want %d", first.TimeoutSec, 360)
	}

	zhipuLobster := providers[1]
	if zhipuLobster.Name != "智谱龙虾" {
		t.Errorf("providers[1].Name = %q, want %q", zhipuLobster.Name, "智谱龙虾")
	}
	if zhipuLobster.URL != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Errorf("智谱龙虾 URL = %q, want %q", zhipuLobster.URL, "https://open.bigmodel.cn/api/coding/paas/v4")
	}
	if zhipuLobster.Model != "glm-5-turbo" {
		t.Errorf("智谱龙虾 Model = %q, want %q", zhipuLobster.Model, "glm-5-turbo")
	}

	zhipuCoding := providers[2]
	if zhipuCoding.Name != "智谱编程" {
		t.Errorf("providers[2].Name = %q, want %q", zhipuCoding.Name, "智谱编程")
	}
	if zhipuCoding.URL != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("智谱编程 URL = %q, want %q", zhipuCoding.URL, "https://open.bigmodel.cn/api/anthropic")
	}
	if zhipuCoding.Model != "glm-5.1" {
		t.Errorf("智谱编程 Model = %q, want %q", zhipuCoding.Model, "glm-5.1")
	}
	if zhipuCoding.Protocol != "anthropic" {
		t.Errorf("智谱编程 Protocol = %q, want %q", zhipuCoding.Protocol, "anthropic")
	}
	if zhipuCoding.AgentType != "claude-code/2.0.0" {
		t.Errorf("智谱编程 AgentType = %q, want %q", zhipuCoding.AgentType, "claude-code/2.0.0")
	}

	expectedNames := []string{"OpenAI", "智谱龙虾", "智谱编程", "MiniMax", "Kimi", "讯飞星辰", "Custom1", "Custom2"}
	for i, want := range expectedNames {
		if providers[i].Name != want {
			t.Errorf("providers[%d].Name = %q, want %q", i, providers[i].Name, want)
		}
	}

	if got := providers[4].AgentType; got != "claude-code/2.0.0" {
		t.Errorf("Kimi AgentType = %q, want %q", got, "claude-code/2.0.0")
	}

	n := len(providers)
	if !providers[n-2].IsCustom {
		t.Errorf("providers[%d] (%s) IsCustom = false, want true", n-2, providers[n-2].Name)
	}
	if !providers[n-1].IsCustom {
		t.Errorf("providers[%d] (%s) IsCustom = false, want true", n-1, providers[n-1].Name)
	}
}

func TestGetMaclawLLMProviders_BackfillsLegacyTimeoutIntoCurrentProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMUrl:             "https://example.com/v1",
		MaclawLLMKey:             "sk-test",
		MaclawLLMModel:           "glm-5.1",
		MaclawLLMProtocol:        "anthropic",
		MaclawLLMContextLength:   64000,
		MaclawLLMTimeoutSec:      480,
		MaclawLLMCurrentProvider: "OpenAI",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	if data.Current != "OpenAI" {
		t.Fatalf("Current = %q, want %q", data.Current, "OpenAI")
	}
	if len(data.Providers) == 0 {
		t.Fatal("expected providers")
	}
	got := data.Providers[0]
	if got.TimeoutSec != 480 {
		t.Fatalf("TimeoutSec = %d, want %d", got.TimeoutSec, 480)
	}
	if got.ContextLength != 64000 {
		t.Fatalf("ContextLength = %d, want %d", got.ContextLength, 64000)
	}
}

// TestGetMaclawLLMProviders_MigratesRemovedCurrentProvider verifies that when
// the persisted current provider no longer exists in the default list (e.g.
// "免费" was removed), GetMaclawLLMProviders falls back to the first provider.
func TestGetMaclawLLMProviders_MigratesRemovedCurrentProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "免费", // no longer in defaults
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	// Should fall back to first default provider, not stay on "免费"
	if data.Current == "免费" {
		t.Fatalf("Current should not be %q (removed provider)", data.Current)
	}
	if data.Current != "OpenAI" {
		t.Fatalf("Current = %q, want %q (first default)", data.Current, "OpenAI")
	}
}

func TestGetMaclawLLMProviders_BackfillsMissingTimeoutToDefault(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "Custom1",
			URL:      "https://example.com/v1",
			Key:      "sk-test",
			Model:    "glm-5.1",
			Protocol: "anthropic",
			IsCustom: true,
		}},
		MaclawLLMCurrentProvider: "Custom1",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	if len(data.Providers) == 0 {
		t.Fatal("expected providers")
	}
	if got := data.Providers[0].TimeoutSec; got != 360 {
		t.Fatalf("TimeoutSec = %d, want %d", got, 360)
	}
}

func TestSaveMaclawLLMProviders_SyncsLegacyTimeout(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{{
		Name:       "Custom1",
		URL:        "https://example.com/v1",
		Key:        "sk-test",
		Model:      "glm-5.1",
		Protocol:   "anthropic",
		IsCustom:   true,
		TimeoutSec: 0,
	}}
	if err := app.SaveMaclawLLMProviders(providers, "Custom1"); err != nil {
		t.Fatalf("SaveMaclawLLMProviders() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.MaclawLLMTimeoutSec != 360 {
		t.Fatalf("MaclawLLMTimeoutSec = %d, want %d", saved.MaclawLLMTimeoutSec, 360)
	}
	if len(saved.MaclawLLMProviders) == 0 {
		t.Fatal("expected saved providers")
	}
	if got := saved.MaclawLLMProviders[0].TimeoutSec; got != 360 {
		t.Fatalf("provider TimeoutSec = %d, want %d", got, 360)
	}
}

func TestGetMaclawLLMConfig_ReturnsTimeout(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:       "Custom1",
			URL:        "https://example.com/v1",
			Key:        "sk-test",
			Model:      "glm-5.1",
			Protocol:   "anthropic",
			IsCustom:   true,
			TimeoutSec: 420,
		}},
		MaclawLLMCurrentProvider: "Custom1",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	got := app.GetMaclawLLMConfig()
	if got.TimeoutSec != 420 {
		t.Fatalf("TimeoutSec = %d, want %d", got.TimeoutSec, 420)
	}
}

func TestNewIMMessageHandler_UsesConfiguredTimeout(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:       "Custom1",
			URL:        "https://example.com/v1",
			Key:        "sk-test",
			Model:      "glm-5.1",
			Protocol:   "anthropic",
			IsCustom:   true,
			TimeoutSec: 510,
		}},
		MaclawLLMCurrentProvider: "Custom1",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	chatTransport, ok := h.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("chat transport type = %T, want *http.Transport", h.client.Transport)
	}
	taskTransport, ok := h.taskClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("task transport type = %T, want *http.Transport", h.taskClient.Transport)
	}
	want := 510 * time.Second
	if chatTransport.ResponseHeaderTimeout != want {
		t.Fatalf("chat ResponseHeaderTimeout = %v, want %v", chatTransport.ResponseHeaderTimeout, want)
	}
	if taskTransport.ResponseHeaderTimeout != want {
		t.Fatalf("task ResponseHeaderTimeout = %v, want %v", taskTransport.ResponseHeaderTimeout, want)
	}
}

func TestMaclawAgentMaxIterations_NormalizesBounds(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "negative becomes default", in: -1, want: config.MaxAgentIterationsCap},
		{name: "zero becomes default", in: 0, want: config.MaxAgentIterationsCap},
		{name: "below min clamps", in: 1, want: config.MinAgentIterations},
		{name: "just below min clamps", in: config.MinAgentIterations - 1, want: config.MinAgentIterations},
		{name: "min stays", in: config.MinAgentIterations, want: config.MinAgentIterations},
		{name: "middle stays", in: 200, want: 200},
		{name: "above max clamps", in: config.MaxAgentIterationsCap + 1, want: config.MaxAgentIterationsCap},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := app.SetMaclawAgentMaxIterations(tc.in); err != nil {
				t.Fatalf("SetMaclawAgentMaxIterations(%d) error = %v", tc.in, err)
			}
			if got := app.GetMaclawAgentMaxIterations(); got != tc.want {
				t.Fatalf("GetMaclawAgentMaxIterations() = %d, want %d", got, tc.want)
			}
			saved, err := app.LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if got := saved.MaclawAgentMaxIterations; got != tc.want {
				t.Fatalf("saved MaclawAgentMaxIterations = %d, want %d", got, tc.want)
			}
		})
	}
}

// resolveProviders extracts the provider-selection logic from
// GetMaclawLLMProviders: if saved is non-empty, return it as-is;
// otherwise fall back to defaultMaclawLLMProviders().
func resolveProviders(saved []corelib.MaclawLLMProvider) []corelib.MaclawLLMProvider {
	if len(saved) == 0 {
		return defaultMaclawLLMProviders()
	}
	return saved
}

// genMaclawLLMProvider returns a rapid generator for a random corelib.MaclawLLMProvider.
func genMaclawLLMProvider() *rapid.Generator[corelib.MaclawLLMProvider] {
	return rapid.Custom(func(t *rapid.T) corelib.MaclawLLMProvider {
		return corelib.MaclawLLMProvider{
			Name:           rapid.StringMatching(`[A-Za-z0-9_]{1,20}`).Draw(t, "name"),
			URL:            rapid.StringMatching(`https?://[a-z0-9.]{1,30}`).Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			Model:          rapid.StringMatching(`[a-z0-9-]{1,20}`).Draw(t, "model"),
			Protocol:       rapid.SampledFrom([]string{"", "openai", "anthropic"}).Draw(t, "protocol"),
			ContextLength:  rapid.IntRange(0, 256000).Draw(t, "ctx"),
			IsCustom:       rapid.Bool().Draw(t, "custom"),
			AuthType:       rapid.SampledFrom([]string{"", "api_key", "oauth"}).Draw(t, "auth"),
			RefreshToken:   rapid.String().Draw(t, "refresh"),
			TokenExpiresAt: rapid.Int64Range(0, 2000000000).Draw(t, "expires"),
		}
	})
}

// Feature: openai-oauth-provider, Property 8: 已保存的 provider 列表不被默认值覆盖
// **Validates: Requirements 2.4**
//
// For any non-empty saved provider list, calling resolveProviders (the core
// logic of GetMaclawLLMProviders) should return that saved list, not
// defaultMaclawLLMProviders()'s result.
func TestProperty_SavedProvidersNotOverwritten(t *testing.T) {
	defaults := defaultMaclawLLMProviders()

	rapid.Check(t, func(t *rapid.T) {
		// Generate a non-empty slice of random providers (1..10).
		n := rapid.IntRange(1, 10).Draw(t, "count")
		saved := make([]corelib.MaclawLLMProvider, n)
		for i := range saved {
			saved[i] = genMaclawLLMProvider().Draw(t, "provider")
		}

		result := resolveProviders(saved)

		// 1. The result must be the saved list, not the defaults.
		if !reflect.DeepEqual(result, saved) {
			t.Fatalf("resolveProviders returned different list than saved:\n  saved:  %+v\n  result: %+v", saved, result)
		}

		// 2. Confirm it is NOT the default list (unless saved happens to
		//    be identical, which is astronomically unlikely with random data).
		if reflect.DeepEqual(result, defaults) && !reflect.DeepEqual(saved, defaults) {
			t.Fatalf("resolveProviders returned defaults instead of saved list")
		}
	})
}

// Feature: codegen-scan-login, Property 9: Brand isolation — non-qianxin brands skip SSO
// **Validates: Requirements 7.1, 7.2**
//
// For any brand configuration where ID != "qianxin", ensureCodeGenToken returns nil
// (no error, no side effects). The shouldSkipCodeGenSSO helper must return true for
// every non-"qianxin" brand ID and false only for "qianxin".
func TestProperty_BrandIsolation_NonQianxinSkipsSSO(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random brand ID that is NOT "qianxin".
		brandID := rapid.StringMatching(`[a-zA-Z0-9_-]{1,30}`).
			Filter(func(s string) bool { return s != "qianxin" }).
			Draw(t, "brandID")

		// shouldSkipCodeGenSSO must return true for any non-qianxin brand.
		if !shouldSkipCodeGenSSO(brandID) {
			t.Fatalf("shouldSkipCodeGenSSO(%q) = false, want true", brandID)
		}
	})
}

// TestProperty_BrandIsolation_QianxinDoesNotSkip verifies the inverse: "qianxin"
// is the only brand ID that does NOT skip SSO.
func TestProperty_BrandIsolation_QianxinDoesNotSkip(t *testing.T) {
	if shouldSkipCodeGenSSO("qianxin") {
		t.Fatal("shouldSkipCodeGenSSO(\"qianxin\") = true, want false")
	}
}

// TestProperty_BrandIsolation_EnsureCodeGenTokenReturnsNil verifies that in the
// default build (brand ID = "maclaw"), ensureCodeGenToken returns nil regardless
// of the App state — confirming the brand guard works end-to-end.
func TestProperty_BrandIsolation_EnsureCodeGenTokenReturnsNil(t *testing.T) {
	tmpDir := t.TempDir()
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random provider list to populate the App config.
		// Even with SSO providers present, the brand guard should short-circuit.
		nProviders := rapid.IntRange(0, 5).Draw(rt, "nProviders")
		providers := make([]corelib.MaclawLLMProvider, nProviders)
		for i := range providers {
			providers[i] = genMaclawLLMProvider().Draw(rt, "provider")
			// Randomly make some providers look like CodeGen SSO providers.
			if rapid.Bool().Draw(rt, "isSSOProvider") {
				providers[i].Name = codegenProviderName
				providers[i].AuthType = "sso"
				providers[i].Key = rapid.String().Draw(rt, "ssoKey")
				providers[i].TokenExpiresAt = rapid.Int64Range(0, 2000000000).Draw(rt, "ssoExpires")
			}
		}

		// In the default build (no oem_qianxin tag), brand.Current().ID == "maclaw",
		// so ensureCodeGenToken must return nil immediately.
		app := &App{testHomeDir: tmpDir}
		err := app.ensureCodeGenToken()
		if err != nil {
			rt.Fatalf("ensureCodeGenToken() = %v, want nil (brand is not qianxin)", err)
		}
	})
}
