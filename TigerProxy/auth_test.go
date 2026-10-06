package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

func TestNeedsOnboardingForFreshSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	s, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !needsOnboarding(s) {
		t.Fatalf("fresh settings should need onboarding, got mode=%q token=%q", s.ActiveAuthMode, s.AccessToken)
	}
}

func TestMigrateLegacyTigerProxySettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	legacyDir := filepath.Join(tmpHome, ".tigerproxy")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"listen_address":"127.0.0.1:18086","api_key":"tigerproxy-local-key","access_token":"sso-token","base_url":"https://codegen.example/api/v1","email":"user@corp","model_id":"gpt-5.5"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "settings.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveAuthMode != AuthModeSSO {
		t.Fatalf("active mode = %q, want sso", s.ActiveAuthMode)
	}
	if s.AccessToken != "sso-token" || s.Email != "user@corp" {
		t.Fatalf("migrated flattened fields = %+v", s)
	}
	if !profileHasCredential(s.AuthProfiles[AuthModeSSO]) {
		t.Fatal("sso profile was not snapshotted")
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".codexproxy", "settings.json")); err != nil {
		t.Fatalf("new settings file missing: %v", err)
	}
}

func TestMigrateLegacyTigerProxyLogs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	legacyLogs := filepath.Join(tmpHome, ".tigerproxy", "logs")
	if err := os.MkdirAll(legacyLogs, 0o755); err != nil {
		t.Fatal(err)
	}
	oldLog := []byte("old tigerproxy log line\n")
	if err := os.WriteFile(filepath.Join(legacyLogs, "tigerproxy_2026-09-20.log"), oldLog, 0o644); err != nil {
		t.Fatal(err)
	}
	// Settings already live in the new dir, which used to skip the rest of
	// migration. Logs should still move.
	newDir := filepath.Join(tmpHome, ".codexproxy")
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "settings.json"), []byte(`{"api_key":"tigerproxy-local-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != newDir {
		t.Fatalf("configDir = %q, want %q", dir, newDir)
	}
	got, err := os.ReadFile(filepath.Join(newDir, "logs", "tigerproxy_2026-09-20.log"))
	if err != nil {
		t.Fatalf("migrated log missing: %v", err)
	}
	if string(got) != string(oldLog) {
		t.Fatalf("migrated log = %q, want %q", got, oldLog)
	}
}

func TestSelectAndSwitchAuthModeKeepsProfiles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeSSO,
		AccessToken:    "sso-token",
		BaseURL:        "https://codegen.example/v1",
		Email:          "user@corp",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SwitchAuthMode(AuthModeCustomOpenAI); err != nil {
		t.Fatalf("SwitchAuthMode custom: %v", err)
	}
	after, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if after.ActiveAuthMode != AuthModeCustomOpenAI {
		t.Fatalf("mode = %q, want custom_openai", after.ActiveAuthMode)
	}
	if after.AuthProfiles[AuthModeSSO].AccessToken != "sso-token" {
		t.Fatalf("sso profile lost: %+v", after.AuthProfiles[AuthModeSSO])
	}
	if after.AccessToken != "" {
		t.Fatalf("custom mode should start without a key, got %q", after.AccessToken)
	}

	if _, err := app.SaveUpstreamCredential("https://api.example.com/v1", "sk-custom", "gpt-4.1"); err != nil {
		t.Fatalf("SaveUpstreamCredential: %v", err)
	}
	if _, err := app.SwitchAuthMode(AuthModeSSO); err != nil {
		t.Fatalf("switch back to sso: %v", err)
	}
	back, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if back.AccessToken != "sso-token" || back.Email != "user@corp" {
		t.Fatalf("did not restore sso profile: %+v", back)
	}
	if back.AuthProfiles[AuthModeCustomOpenAI].AccessToken != "sk-custom" {
		t.Fatalf("custom profile lost: %+v", back.AuthProfiles[AuthModeCustomOpenAI])
	}
}

func TestSwitchAuthModeUsesCanonicalBaseURL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeXAIOAuth,
		AccessToken:    "xai-token",
		BaseURL:        xaiOfficialURL,
		AuthProfiles: map[string]AuthProfile{
			AuthModeAnthropicOAuth: {BaseURL: xaiOfficialURL},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SwitchAuthMode(AuthModeAnthropicOAuth); err != nil {
		t.Fatalf("SwitchAuthMode anthropic: %v", err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimRight(anthropicOfficialURL, "/")
	if got.BaseURL != want {
		t.Fatalf("claude base url = %q, want %q", got.BaseURL, want)
	}
	if got.AuthProfiles[AuthModeXAIOAuth].AccessToken != "xai-token" {
		t.Fatalf("xAI profile lost: %+v", got.AuthProfiles[AuthModeXAIOAuth])
	}
	if got.ModelID != "claude-sonnet-4-5" {
		t.Fatalf("claude default model = %q", got.ModelID)
	}
}

func TestApplyOAuthResultIgnoresStaleLoginAfterSwitch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeAnthropicOAuth,
		BaseURL:        anthropicOfficialURL,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.applyOAuthResult(AuthModeOpenAIOAuth, openaiOfficialURL, &oauth.TokenResult{AccessToken: "sk-openai"}); err != nil {
		t.Fatalf("applyOAuthResult: %v", err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveAuthMode != AuthModeAnthropicOAuth {
		t.Fatalf("active mode = %q, want anthropic after stale OpenAI callback", got.ActiveAuthMode)
	}
	if got.AccessToken == "sk-openai" {
		t.Fatal("stale OpenAI token was applied after switching away")
	}
}

func TestNormalizeSettingsForcesCanonicalURL(t *testing.T) {
	got := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeAnthropicOAuth,
		BaseURL:        xaiOfficialURL,
		ModelID:        "grok-4.6",
		AccessToken:    "",
	})
	if got.BaseURL != strings.TrimRight(anthropicOfficialURL, "/") {
		t.Fatalf("normalized claude url = %q", got.BaseURL)
	}
}

func TestFetchOpenAICompatibleModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "gpt-4.1", "name": "GPT-4.1"}},
		})
	}))
	defer upstream.Close()
	models, err := fetchOpenAICompatibleModels(upstream.URL+"/v1", "sk-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4.1" {
		t.Fatalf("models = %+v", models)
	}
}

func TestFetchAnthropicModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "sk-ant-test" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "claude-sonnet-4-5", "display_name": "Claude Sonnet 4.5"}},
		})
	}))
	defer upstream.Close()
	models, err := fetchAnthropicModels(upstream.URL+"/v1", "sk-ant-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "claude-sonnet-4-5" {
		t.Fatalf("models = %+v", models)
	}
}

func TestFetchOpenAICompatibleModelsSendsExtraHeaders(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-XAI-Token-Auth")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "grok-4.6"}},
		})
	}))
	defer upstream.Close()
	if _, err := fetchOpenAICompatibleModels(upstream.URL, "xai-token", map[string]string{"X-XAI-Token-Auth": "xai-grok-cli"}); err != nil {
		t.Fatal(err)
	}
	if got != "xai-grok-cli" {
		t.Fatalf("X-XAI-Token-Auth = %q", got)
	}
}

func TestScrubSettingsHidesUpstreamSecrets(t *testing.T) {
	s := scrubSettings(Settings{
		AccessToken: "secret",
		AuthProfiles: map[string]AuthProfile{
			AuthModeCustomOpenAI: {AccessToken: "sk-live", APIKey: "sk-live", RefreshToken: "rt"},
		},
	})
	if s.AccessToken != "已保存" {
		t.Fatalf("access token not scrubbed: %q", s.AccessToken)
	}
	profile := s.AuthProfiles[AuthModeCustomOpenAI]
	if profile.AccessToken != "已保存" || profile.APIKey != "已保存" || profile.RefreshToken != "" {
		t.Fatalf("profile secrets not scrubbed: %+v", profile)
	}
}
