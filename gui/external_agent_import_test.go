package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

func TestStartOpenCodeZenLoginOpensAuthPage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "xdg"))
	var opened string
	setOpenURLForTest(func(rawURL string) error {
		opened = rawURL
		return nil
	})
	t.Cleanup(func() { setOpenURLForTest(nil) })

	app := &App{testHomeDir: tmp}
	result, err := app.StartOpenCodeZenLogin()
	if err != nil {
		t.Fatalf("StartOpenCodeZenLogin: %v", err)
	}
	if opened != configfile.OpenCodeZenAuthURL {
		t.Fatalf("opened %q, want %q", opened, configfile.OpenCodeZenAuthURL)
	}
	if result.Key != "" {
		t.Fatalf("unexpected local key: %q", result.Key)
	}
	if result.Message == "" {
		t.Fatal("expected a user-facing message")
	}
	if !strings.Contains(result.Message, "API Keys") {
		t.Fatalf("message should point at the API Keys page: %q", result.Message)
	}
}

func TestStartOpenCodeZenLoginReturnsLocalKey(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("OPENCODE_API_KEY", "zen-local-key")
	setOpenURLForTest(func(string) error { return nil })
	t.Cleanup(func() { setOpenURLForTest(nil) })

	app := &App{testHomeDir: tmp}
	result, err := app.StartOpenCodeZenLogin()
	if err != nil {
		t.Fatalf("StartOpenCodeZenLogin: %v", err)
	}
	if result.Key != "zen-local-key" {
		t.Fatalf("key = %q", result.Key)
	}
	if strings.Contains(result.Message, "已填入") {
		t.Fatalf("backend must not claim the key was filled: %q", result.Message)
	}
}

func TestImportExternalAgentsOnlyAddsTestedProvidersAndKeepsCurrent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{
			{Source: configfile.ExternalAgentSourceCodex, Name: configfile.ExternalAgentProviderCodex, URL: "https://codex.example/v1", Key: "codex-key", Model: "glm-5.3", WireAPI: "responses"},
			{Source: configfile.ExternalAgentSourceClaudeCode, Name: configfile.ExternalAgentProviderClaudeCode, URL: "https://claude.example", Key: "claude-key", Model: "glm-5.3", Protocol: "anthropic", AgentType: configfile.ExternalAgentTypeClaudeCode},
			{Source: configfile.ExternalAgentSourceOpenCode, Name: configfile.ExternalAgentProviderOpenCode, URL: configfile.OpenCodeZenBaseURL, Key: "bad-key", Model: "big-pickle"},
		}
	}
	testImportedAgentForTest = func(cfg corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		if strings.Contains(cfg.URL, "opencode.ai") {
			return corelib.MaclawLLMTestResult{}, errAuthFailed
		}
		if strings.Contains(cfg.URL, "claude") {
			return corelib.MaclawLLMTestResult{Message: "ok", SupportsVision: true, VisionProbeStatus: "supported"}, nil
		}
		return corelib.MaclawLLMTestResult{Message: "ok", VisionProbeStatus: "unsupported"}, nil
	}
	listImportedModelsForTest = func(cfg corelib.MaclawLLMConfig) []string {
		if strings.Contains(cfg.URL, "codex.example") {
			return []string{"glm-5.3", "glm-4.7"}
		}
		return []string{cfg.Model}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 2 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	if result.Current != "DeepSeek" {
		t.Fatalf("current = %q, want DeepSeek (must not auto-switch)", result.Current)
	}
	var sawOpenCode bool
	for _, skip := range result.Skipped {
		if skip.Source == configfile.ExternalAgentSourceOpenCode {
			sawOpenCode = true
		}
	}
	if !sawOpenCode {
		t.Fatalf("OpenCode auth failure should be skipped: %#v", result.Skipped)
	}

	saved := app.GetMaclawLLMProviders()
	if saved.Current != "DeepSeek" {
		t.Fatalf("saved current = %q", saved.Current)
	}
	codex, ok := findImportedProviderByName(saved.Providers, configfile.ExternalAgentProviderCodex)
	if !ok {
		t.Fatal("Codex provider missing")
	}
	if codex.ImportSource != configfile.ExternalAgentSourceCodex || codex.Model != "glm-5.3" {
		t.Fatalf("codex = %+v", codex)
	}
	if !containsStringFold(codex.Models, "glm-4.7") {
		t.Fatalf("codex models = %#v", codex.Models)
	}
	if codex.AgentType != configfile.ExternalAgentTypeCodex {
		t.Fatalf("codex AgentType = %q", codex.AgentType)
	}
	claude, ok := findImportedProviderByName(saved.Providers, configfile.ExternalAgentProviderClaudeCode)
	if !ok || !claude.SupportsVision {
		t.Fatalf("claude = %+v ok=%v", claude, ok)
	}
	if claude.AgentType != configfile.ExternalAgentTypeClaudeCode {
		t.Fatalf("claude AgentType = %q", claude.AgentType)
	}
	oc, ok := findImportedProviderByName(saved.Providers, configfile.ExternalAgentProviderOpenCode)
	if !ok {
		t.Fatal("builtin OpenCode missing")
	}
	if strings.TrimSpace(oc.Key) != "" || oc.ImportSource != "" || oc.ConnectionTestPassed {
		t.Fatalf("failed OpenCode auth must not fill the builtin provider: %+v", oc)
	}
}

func TestImportExternalAgentsOpenCodeKeepsCatalogModels(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source: configfile.ExternalAgentSourceOpenCode,
			Name:   configfile.ExternalAgentProviderOpenCode,
			URL:    configfile.OpenCodeZenBaseURL,
			Key:    "zen-key",
			Model:  "big-pickle",
		}}
	}
	testImportedAgentForTest = func(corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		return corelib.MaclawLLMTestResult{Message: "ok"}, nil
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string {
		return []string{"gpt-5.5", "big-pickle", "hy3-free", "claude-opus-5"}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	oc, ok := findImportedProviderByName(app.GetMaclawLLMProviders().Providers, configfile.ExternalAgentProviderOpenCode)
	if !ok {
		t.Fatal("OpenCode missing")
	}
	if !containsStringFold(oc.Models, "gpt-5.5") || !containsStringFold(oc.Models, "claude-opus-5") {
		t.Fatalf("paid catalog dropped: %#v", oc.Models)
	}
	if !containsStringFold(oc.Models, "big-pickle") || !containsStringFold(oc.Models, "hy3-free") {
		t.Fatalf("free models missing: %#v", oc.Models)
	}
	if oc.AgentType != configfile.ExternalAgentTypeOpenCode {
		t.Fatalf("OpenCode AgentType = %q", oc.AgentType)
	}
}

func TestShouldRetryImportedAgentAsChat(t *testing.T) {
	codex := configfile.ExternalAgentCandidate{
		Source:  configfile.ExternalAgentSourceCodex,
		URL:     "https://open.bigmodel.cn/api/coding/paas/v4",
		WireAPI: "responses",
	}
	if !shouldRetryImportedAgentAsChat(codex, externalAgentAuthError("HTTP 400: unknown route")) {
		t.Fatal("third-party Codex 400 should fall back to chat")
	}
	if shouldRetryImportedAgentAsChat(codex, externalAgentAuthError("context_length 400000 is too large")) {
		t.Fatal("token/context numbers must not look like HTTP 400")
	}
	if shouldRetryImportedAgentAsChat(codex, externalAgentAuthError("401 unauthorized")) {
		t.Fatal("auth failures must not be retried as chat")
	}
	if !isImportedAuthFailure(externalAgentAuthError("HTTP 401 unauthorized")) {
		t.Fatal("HTTP 401 should be treated as an auth failure")
	}
	if isImportedAuthFailure(externalAgentAuthError("HTTP 404: Not Found")) {
		t.Fatal("HTTP 404 is not an auth failure")
	}
	official := codex
	official.URL = "https://chatgpt.com/backend-api/codex"
	official.WireAPI = "responses-ws"
	if shouldRetryImportedAgentAsChat(official, externalAgentAuthError("HTTP 404")) {
		t.Fatal("official Codex subscription must not fall back to chat")
	}
	if shouldRetryImportedAgentAsChat(codex, externalAgentAuthError("model not found")) {
		t.Fatal("model-not-found must not fall back to chat")
	}
}

func TestMergeImportedVisionKeepsOtherModels(t *testing.T) {
	existing := corelib.MaclawLLMProvider{
		Name:         configfile.ExternalAgentProviderCodex,
		ImportSource: configfile.ExternalAgentSourceCodex,
		Model:        "glm-5.3",
		VisionModels: []string{"glm-4.7"},
	}
	incoming := corelib.MaclawLLMProvider{
		Name:         configfile.ExternalAgentProviderCodex,
		ImportSource: configfile.ExternalAgentSourceCodex,
		Model:        "glm-5.3",
	}
	got := mergeImportedVision(existing, incoming, string(visionProbeSupported))
	if !containsStringFold(got.VisionModels, "glm-4.7") || !containsStringFold(got.VisionModels, "glm-5.3") {
		t.Fatalf("vision models = %#v", got.VisionModels)
	}
	kept := mergeImportedVision(existing, incoming, string(visionProbeInconclusive))
	if !containsStringFold(kept.VisionModels, "glm-4.7") {
		t.Fatalf("inconclusive probe wiped previous vision models: %#v", kept.VisionModels)
	}
}

func TestImportedProviderNeedsWrite(t *testing.T) {
	existing := corelib.MaclawLLMProvider{
		Name:                 configfile.ExternalAgentProviderCodex,
		URL:                  "https://open.bigmodel.cn/api/coding/paas/v4",
		Key:                  "k",
		Model:                "glm-5.3",
		Models:               []string{"glm-5.3"},
		ImportSource:         configfile.ExternalAgentSourceCodex,
		ConnectionTestPassed: true,
	}
	same := existing
	if importedProviderNeedsWrite(existing, same) {
		t.Fatal("unchanged tested provider should not rewrite config")
	}
	existing.ConnectionTestPassed = false
	if !importedProviderNeedsWrite(existing, same) {
		t.Fatal("untested provider must be written")
	}
}

func TestImportedOpenCodePresetNeedsWrite(t *testing.T) {
	existing := corelib.MaclawLLMProvider{
		Name:                 configfile.ExternalAgentProviderOpenCode,
		URL:                  configfile.OpenCodeZenBaseURL,
		Key:                  "zen-key",
		Model:                "big-pickle",
		Protocol:             "openai",
		AgentType:            configfile.ExternalAgentTypeOpenCode,
		Models:               []string{"big-pickle", "hy3-free"},
		ConnectionTestPassed: true,
	}
	incoming := existing
	incoming.ImportSource = configfile.ExternalAgentSourceOpenCode
	if defaults, ok := findImportedProviderByName(defaultMaclawLLMProviders(), configfile.ExternalAgentProviderOpenCode); ok {
		incoming = normalizeOpenCodeProvider(incoming, defaults)
	}
	if importedProviderNeedsWrite(existing, incoming) {
		t.Fatalf("unchanged OpenCode preset should not rewrite config: %+v vs %+v", existing, incoming)
	}
}

func TestImportExternalAgentsFallsBackFromBrokenCodexResponsesAPI(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source:    configfile.ExternalAgentSourceCodex,
			Name:      configfile.ExternalAgentProviderCodex,
			URL:       "https://open.bigmodel.cn/api/coding/paas/v4",
			Key:       "glm-key",
			Model:     "glm-5.3",
			WireAPI:   "responses",
			AgentType: configfile.ExternalAgentTypeCodex,
		}}
	}
	var seen []string
	testImportedAgentForTest = func(cfg corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		seen = append(seen, cfg.WireAPI+"|"+cfg.AgentType)
		if cfg.WireAPI == "responses" {
			return corelib.MaclawLLMTestResult{}, externalAgentAuthError("Codex 接口或模型不存在 (HTTP 404)：Not Found")
		}
		if cfg.AgentType != configfile.ExternalAgentTypeCodex {
			t.Fatalf("retry AgentType = %q", cfg.AgentType)
		}
		return corelib.MaclawLLMTestResult{Message: "ok"}, nil
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string { return []string{"glm-5.3"} }
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported = %#v seen=%v", result.Imported, seen)
	}
	codex, ok := findImportedProviderByName(app.GetMaclawLLMProviders().Providers, configfile.ExternalAgentProviderCodex)
	if !ok {
		t.Fatal("Codex missing")
	}
	if strings.TrimSpace(codex.WireAPI) != "" {
		t.Fatalf("WireAPI should fall back to chat, got %q", codex.WireAPI)
	}
	if codex.AgentType != configfile.ExternalAgentTypeCodex {
		t.Fatalf("AgentType = %q", codex.AgentType)
	}
}

func TestImportExternalAgentsOpenCodeFallsBackToAnotherFreeModel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source:    configfile.ExternalAgentSourceOpenCode,
			Name:      configfile.ExternalAgentProviderOpenCode,
			URL:       configfile.OpenCodeZenBaseURL,
			Key:       "zen-key",
			Model:     "big-pickle",
			AgentType: configfile.ExternalAgentTypeOpenCode,
		}}
	}
	testImportedAgentForTest = func(cfg corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		if cfg.Model == "big-pickle" {
			return corelib.MaclawLLMTestResult{}, externalAgentAuthError("model unavailable")
		}
		if cfg.Model == "hy3-free" && cfg.AgentType == configfile.ExternalAgentTypeOpenCode {
			return corelib.MaclawLLMTestResult{Message: "ok", VisionProbeStatus: "unsupported"}, nil
		}
		return corelib.MaclawLLMTestResult{}, externalAgentAuthError("unexpected " + cfg.Model)
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string {
		return []string{"big-pickle", "hy3-free", "gpt-5.5"}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	oc, ok := findImportedProviderByName(app.GetMaclawLLMProviders().Providers, configfile.ExternalAgentProviderOpenCode)
	if !ok {
		t.Fatal("OpenCode missing")
	}
	if oc.Model != "hy3-free" {
		t.Fatalf("default model = %q, want hy3-free", oc.Model)
	}
	if oc.ContextLength != 128000 {
		t.Fatalf("ContextLength = %d", oc.ContextLength)
	}
}

func TestImportExternalAgentsOpenCodeDoesNotTryOtherModelsOnAuthFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	tried := 0
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source: configfile.ExternalAgentSourceOpenCode,
			Name:   configfile.ExternalAgentProviderOpenCode,
			URL:    configfile.OpenCodeZenBaseURL,
			Key:    "bad-key",
			Model:  "big-pickle",
		}}
	}
	testImportedAgentForTest = func(corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		tried++
		return corelib.MaclawLLMTestResult{}, externalAgentAuthError("HTTP 401 unauthorized")
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string {
		return []string{"big-pickle", "hy3-free", "mimo-v2.5-free"}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 0 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	if tried != 1 {
		t.Fatalf("tried %d models, want 1 on 401", tried)
	}
}

func TestImportExternalAgentsPrefersScannedDefaultBeforeOtherFreeModels(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
			{
				Name:         configfile.ExternalAgentProviderOpenCode,
				URL:          configfile.OpenCodeZenBaseURL,
				Key:          "zen-key",
				Model:        "hy3-free",
				ImportSource: configfile.ExternalAgentSourceOpenCode,
			},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var tested []string
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source: configfile.ExternalAgentSourceOpenCode,
			Name:   configfile.ExternalAgentProviderOpenCode,
			URL:    configfile.OpenCodeZenBaseURL,
			Key:    "zen-key",
			Model:  "big-pickle",
		}}
	}
	testImportedAgentForTest = func(cfg corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		tested = append(tested, cfg.Model)
		if cfg.Model == "hy3-free" {
			return corelib.MaclawLLMTestResult{}, externalAgentAuthError("model unavailable")
		}
		if cfg.Model == "big-pickle" {
			return corelib.MaclawLLMTestResult{Message: "ok", VisionProbeStatus: "unsupported"}, nil
		}
		return corelib.MaclawLLMTestResult{}, externalAgentAuthError("unexpected extra model " + cfg.Model)
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string {
		return []string{"mimo-v2.5-free", "hy3-free", "big-pickle"}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported = %#v tested=%v", result.Imported, tested)
	}
	if len(tested) < 2 || tested[0] != "hy3-free" || tested[1] != "big-pickle" {
		t.Fatalf("test order = %#v, want current then scanned default", tested)
	}
	oc, ok := findImportedProviderByName(app.GetMaclawLLMProviders().Providers, configfile.ExternalAgentProviderOpenCode)
	if !ok || oc.Model != "big-pickle" {
		t.Fatalf("fallback model = %+v", oc)
	}
}

func TestImportExternalAgentsKeepsSelectedModelOnRescan(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
			{
				Name:                 configfile.ExternalAgentProviderCodex,
				URL:                  "https://codex.example/v1",
				Key:                  "codex-key",
				Model:                "glm-4.7",
				Models:               []string{"glm-5.3", "glm-4.7"},
				ImportSource:         configfile.ExternalAgentSourceCodex,
				AgentType:            configfile.ExternalAgentTypeCodex,
				ConnectionTestPassed: true,
			},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	var tested []string
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source:    configfile.ExternalAgentSourceCodex,
			Name:      configfile.ExternalAgentProviderCodex,
			URL:       "https://codex.example/v1",
			Key:       "codex-key",
			Model:     "glm-5.3",
			AgentType: configfile.ExternalAgentTypeCodex,
		}}
	}
	testImportedAgentForTest = func(cfg corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		tested = append(tested, cfg.Model)
		return corelib.MaclawLLMTestResult{Message: "ok", VisionProbeStatus: "unsupported"}, nil
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string {
		return []string{"glm-5.3", "glm-4.7"}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	if len(tested) == 0 || tested[0] != "glm-4.7" {
		t.Fatalf("tested models = %#v, want current selection first", tested)
	}
	codex, ok := findImportedProviderByName(app.GetMaclawLLMProviders().Providers, configfile.ExternalAgentProviderCodex)
	if !ok || codex.Model != "glm-4.7" {
		t.Fatalf("rescan reset user model: %+v", codex)
	}
}

func TestImportExternalAgentsFillsBuiltinOpenCode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
			{Name: configfile.ExternalAgentProviderOpenCode, URL: configfile.OpenCodeZenBaseURL, Model: "big-pickle", AgentType: configfile.ExternalAgentTypeOpenCode},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source:    configfile.ExternalAgentSourceOpenCode,
			Name:      configfile.ExternalAgentProviderOpenCode,
			URL:       configfile.OpenCodeZenBaseURL,
			Key:       "zen-key",
			Model:     "big-pickle",
			AgentType: configfile.ExternalAgentTypeOpenCode,
		}}
	}
	testImportedAgentForTest = func(corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		return corelib.MaclawLLMTestResult{Message: "ok", VisionProbeStatus: "unsupported"}, nil
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string {
		return []string{"big-pickle", "hy3-free"}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	var openCodeCount int
	var filled corelib.MaclawLLMProvider
	for _, p := range app.GetMaclawLLMProviders().Providers {
		if p.Name == configfile.ExternalAgentProviderOpenCode {
			openCodeCount++
			filled = p
		}
	}
	if openCodeCount != 1 {
		t.Fatalf("OpenCode count = %d, want single builtin slot", openCodeCount)
	}
	if filled.Key != "zen-key" || filled.ImportSource != "" {
		t.Fatalf("builtin OpenCode not filled as preset: %+v", filled)
	}
}

func TestImportExternalAgentsMergesVisionOnPromotedOpenCode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
			{
				Name:                 configfile.ExternalAgentProviderOpenCode,
				URL:                  configfile.OpenCodeZenBaseURL,
				Key:                  "zen-key",
				Model:                "big-pickle",
				AgentType:            configfile.ExternalAgentTypeOpenCode,
				VisionModels:         []string{"hy3-free"},
				ConnectionTestPassed: true,
			},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source:    configfile.ExternalAgentSourceOpenCode,
			Name:      configfile.ExternalAgentProviderOpenCode,
			URL:       configfile.OpenCodeZenBaseURL,
			Key:       "zen-key",
			Model:     "big-pickle",
			AgentType: configfile.ExternalAgentTypeOpenCode,
		}}
	}
	testImportedAgentForTest = func(corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		return corelib.MaclawLLMTestResult{Message: "ok", VisionProbeStatus: "unsupported"}, nil
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string {
		return []string{"big-pickle", "hy3-free"}
	}
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	oc, ok := findImportedProviderByName(app.GetMaclawLLMProviders().Providers, configfile.ExternalAgentProviderOpenCode)
	if !ok {
		t.Fatal("OpenCode missing")
	}
	if oc.ImportSource != "" {
		t.Fatalf("promoted OpenCode still marked imported: %+v", oc)
	}
	if !containsStringFold(oc.VisionModels, "hy3-free") {
		t.Fatalf("previous vision models dropped on re-import: %#v", oc.VisionModels)
	}
}

func TestImportExternalAgentsDoesNotOverwriteUnrelatedSameName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "Codex", URL: "https://custom.example/v1", Key: "keep-me", Model: "mine"},
		},
		MaclawLLMCurrentProvider: "Codex",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		return []configfile.ExternalAgentCandidate{{
			Source: configfile.ExternalAgentSourceCodex,
			Name:   configfile.ExternalAgentProviderCodex,
			URL:    "https://codex.example/v1",
			Key:    "imported",
			Model:  "glm-5.3",
		}}
	}
	testImportedAgentForTest = func(corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
		return corelib.MaclawLLMTestResult{Message: "ok"}, nil
	}
	listImportedModelsForTest = func(corelib.MaclawLLMConfig) []string { return []string{"glm-5.3"} }
	t.Cleanup(func() {
		scanExternalAgentsForTest = nil
		testImportedAgentForTest = nil
		listImportedModelsForTest = nil
	})

	result, err := app.ImportExternalAgents()
	if err != nil {
		t.Fatalf("ImportExternalAgents: %v", err)
	}
	if len(result.Imported) != 0 {
		t.Fatalf("imported = %#v", result.Imported)
	}
	saved, ok := findImportedProviderByName(app.GetMaclawLLMProviders().Providers, "Codex")
	if !ok || saved.Key != "keep-me" || saved.ImportSource != "" {
		t.Fatalf("custom Codex was overwritten: %+v", saved)
	}
}

func TestMaybeImportExternalAgentsOnceDoesNotRepeat(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-keep", Model: "deepseek-chat"},
		},
		MaclawLLMCurrentProvider: "DeepSeek",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	calls := 0
	scanExternalAgentsForTest = func() []configfile.ExternalAgentCandidate {
		calls++
		return nil
	}
	t.Cleanup(func() { scanExternalAgentsForTest = nil })

	app.maybeImportExternalAgentsOnce()
	app.maybeImportExternalAgentsOnce()
	if calls != 1 {
		t.Fatalf("scan calls = %d, want 1", calls)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ExternalAgentImportAttempted {
		t.Fatal("expected first-start flag to be set")
	}
}

var errAuthFailed = externalAgentAuthError("401 unauthorized")

type externalAgentAuthError string

func (e externalAgentAuthError) Error() string { return string(e) }
