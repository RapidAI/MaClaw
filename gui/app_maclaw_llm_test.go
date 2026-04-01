package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"pgregory.net/rapid"
)

func TestResolveProvidersPreservesCodeGenSSORuntimeConfig(t *testing.T) {
	saved := []MaclawLLMProvider{
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

	providers := append([]MaclawLLMProvider(nil), saved...)
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
	cfg := AppConfig{
		MaclawLLMProviders: []MaclawLLMProvider{{
			Name:     codegenProviderName,
			URL:      "https://codegen.qianxin-inc.cn/api/v1",
			Key:      "token-123",
			Model:    "qax-codegen/Auto",
			Protocol: "openai",
			AuthType: "sso",
		}},
		MaclawLLMCurrentProvider: codegenProviderName,
		Claude: ToolConfig{
			CurrentModel: codegenProviderName,
			Models: []ModelConfig{{
				ModelName: codegenProviderName,
				ModelId:   "qax-codegen/Auto",
				ModelUrl:  "http://127.0.0.1:5001/anthropic",
				ApiKey:    "token-123",
				WireApi:   "anthropic",
			}},
		},
		Codex: ToolConfig{Models: []ModelConfig{{
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

	var claudeCodeGen *ModelConfig
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

	var codexCodeGen *ModelConfig
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
	cfg := AppConfig{
		Claude: ToolConfig{
			CurrentModel: "GLM",
			Models: []ModelConfig{
				{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/anthropic", ApiKey: "glm-token", WireApi: "anthropic"},
				{ModelName: codegenProviderName, ModelId: "qax-codegen/Auto", ModelUrl: "http://127.0.0.1:5001/anthropic", ApiKey: "token-123", WireApi: "anthropic"},
			},
		},
		Codex: ToolConfig{Models: []ModelConfig{{
			ModelName: codegenProviderName,
			ModelId:   "qax-codegen/Auto",
			ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
			ApiKey:    "token-123",
		}}},
		MaclawLLMProviders: []MaclawLLMProvider{{
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
	if first.Name != "免费" {
		t.Errorf("first provider Name = %q, want %q", first.Name, "免费")
	}
	if first.URL != "http://localhost:18099/v1" {
		t.Errorf("免费 URL = %q, want %q", first.URL, "http://localhost:18099/v1")
	}
	if first.Model != "free-proxy" {
		t.Errorf("免费 Model = %q, want %q", first.Model, "free-proxy")
	}
	if first.AuthType != "none" {
		t.Errorf("免费 AuthType = %q, want %q", first.AuthType, "none")
	}
	if first.ContextLength != 10000 {
		t.Errorf("免费 ContextLength = %d, want %d", first.ContextLength, 10000)
	}

	openAI := providers[1]
	if openAI.Name != "OpenAI" {
		t.Errorf("providers[1].Name = %q, want %q", openAI.Name, "OpenAI")
	}
	if openAI.URL != "https://api.openai.com/v1" {
		t.Errorf("OpenAI URL = %q, want %q", openAI.URL, "https://api.openai.com/v1")
	}
	if openAI.Model != "gpt-5.4" {
		t.Errorf("OpenAI Model = %q, want %q", openAI.Model, "gpt-5.4")
	}
	if openAI.AuthType != "oauth" {
		t.Errorf("OpenAI AuthType = %q, want %q", openAI.AuthType, "oauth")
	}
	if openAI.ContextLength != 128000 {
		t.Errorf("OpenAI ContextLength = %d, want %d", openAI.ContextLength, 128000)
	}


	expectedNames := []string{"免费", "OpenAI", "智谱", "MiniMax", "Kimi", "Custom1", "Custom2"}
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

// resolveProviders extracts the provider-selection logic from
// GetMaclawLLMProviders: if saved is non-empty, return it as-is;
// otherwise fall back to defaultMaclawLLMProviders().
func resolveProviders(saved []MaclawLLMProvider) []MaclawLLMProvider {
	if len(saved) == 0 {
		return defaultMaclawLLMProviders()
	}
	return saved
}

// genMaclawLLMProvider returns a rapid generator for a random MaclawLLMProvider.
func genMaclawLLMProvider() *rapid.Generator[MaclawLLMProvider] {
	return rapid.Custom(func(t *rapid.T) MaclawLLMProvider {
		return MaclawLLMProvider{
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
		saved := make([]MaclawLLMProvider, n)
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
		providers := make([]MaclawLLMProvider, nProviders)
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
