package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"pgregory.net/rapid"
)

type appLLMRoundTripFunc func(*http.Request) (*http.Response, error)

func (f appLLMRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoFetchModelsRequestDefaultsCodeGenUserAgentToTigerclaw(t *testing.T) {
	app := &App{}
	client := &http.Client{Transport: appLLMRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("User-Agent"); got != corelib.CodeGenClientName {
			t.Fatalf("User-Agent = %q, want %q", got, corelib.CodeGenClientName)
		}
		if got := req.Header.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
			t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"data":[]}`))),
			Request:    req,
		}, nil
	})}

	resp, err := app.doFetchModelsRequest(client, "https://codegen.qianxin-inc.cn/api/v1/models", "token", "openai", "openclaw")
	if err != nil {
		t.Fatalf("doFetchModelsRequest() error = %v", err)
	}
	_ = resp.Body.Close()
}

func TestDoFetchModelsRequestSetsXAIOAuthHeader(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "xAI-Grok", URL: "https://api.x.ai/v1", AuthType: "oauth", Key: "xai-oauth-token"},
		},
		MaclawLLMCurrentProvider: "xAI-Grok",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := &http.Client{Transport: appLLMRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer xai-oauth-token" {
			t.Fatalf("Authorization = %q, want OAuth bearer token", got)
		}
		if got := req.Header.Get("X-XAI-Token-Auth"); got != "xai-grok-cli" {
			t.Fatalf("X-XAI-Token-Auth = %q, want xai-grok-cli", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: req}, nil
	})}
	resp, err := app.doFetchModelsRequest(client, "https://api.x.ai/v1/models", "xai-oauth-token", "openai", "openclaw")
	if err != nil {
		t.Fatalf("doFetchModelsRequest() error = %v", err)
	}
	_ = resp.Body.Close()
}

func TestMaclawLLMProbeSetsXAIOAuthHeader(t *testing.T) {
	oldClient := maclawLLMPingClient
	defer func() { maclawLLMPingClient = oldClient }()

	maclawLLMPingClient = &http.Client{Transport: appLLMRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer xai-oauth-token" {
			t.Fatalf("Authorization = %q, want OAuth bearer token", got)
		}
		if got := req.Header.Get("X-XAI-Token-Auth"); got != "xai-grok-cli" {
			t.Fatalf("X-XAI-Token-Auth = %q, want xai-grok-cli", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
	})}

	online, err := maclawLLMProbe("https://api.x.ai/v1/models", corelib.MaclawLLMConfig{
		URL: "https://api.x.ai/v1", Key: "xai-oauth-token", Model: "grok-4.5",
		ProviderName: "xAI-Grok", AuthType: "oauth",
	})
	if err != nil || !online {
		t.Fatalf("maclawLLMProbe() = (%v, %v), want (true, nil)", online, err)
	}
}

func TestPreserveManagedAuthSecretsKeepsOAuthKey(t *testing.T) {
	existing := []corelib.MaclawLLMProvider{
		{Name: "GitHub Copilot", AuthType: "oauth", Key: "copilot-token", RefreshToken: "gh-token", TokenExpiresAt: 123},
		{Name: "DeepSeek", AuthType: "api_key", Key: "sk-deepseek"},
	}
	incoming := []corelib.MaclawLLMProvider{
		{Name: "GitHub Copilot", AuthType: "oauth", Key: "", Model: "claude-sonnet-4"},
		{Name: "DeepSeek", AuthType: "api_key", Key: ""},
	}
	got := preserveManagedAuthSecrets(incoming, existing)
	if got[0].Key != "copilot-token" {
		t.Fatalf("oauth key = %q, want preserved copilot-token", got[0].Key)
	}
	if got[0].RefreshToken != "gh-token" {
		t.Fatalf("oauth refresh = %q, want preserved gh-token", got[0].RefreshToken)
	}
	if got[0].TokenExpiresAt != 123 {
		t.Fatalf("oauth expires = %d, want 123", got[0].TokenExpiresAt)
	}
	// Non-managed providers should not be backfilled from empty UI fields.
	if got[1].Key != "" {
		t.Fatalf("api_key provider key = %q, want empty (user cleared)", got[1].Key)
	}
}

func TestUrlsMatchForModelFetch(t *testing.T) {
	if !urlsMatchForModelFetch("https://api.githubcopilot.com", "https://api.githubcopilot.com/") {
		t.Fatal("expected trailing slash match")
	}
	if !urlsMatchForModelFetch("https://api.anthropic.com", "https://api.anthropic.com/v1") {
		t.Fatal("expected parent/child path match")
	}
	if urlsMatchForModelFetch("https://api.githubcopilot.com", "https://api.anthropic.com") {
		t.Fatal("expected different hosts not to match")
	}
}

func TestResolveAPIKeyForModelFetchUsesProviderKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "GitHub Copilot", URL: "https://api.githubcopilot.com", AuthType: "oauth", Key: "internal-copilot-key"},
		},
		MaclawLLMCurrentProvider: "GitHub Copilot",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	got := app.resolveAPIKeyForModelFetch("https://api.githubcopilot.com")
	if got != "internal-copilot-key" {
		t.Fatalf("resolveAPIKeyForModelFetch = %q, want internal-copilot-key", got)
	}
}

func TestResolveAPIKeyForModelFetchPrefersExactManagedMatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			// Prefix match only — should lose to exact OAuth match below.
			{Name: "Custom1", URL: "https://api.example.com", AuthType: "api_key", Key: "wrong-prefix-key", IsCustom: true},
			{Name: "GitHub Copilot", URL: "https://api.example.com/v1", AuthType: "oauth", Key: "exact-oauth-key"},
		},
		MaclawLLMCurrentProvider: "GitHub Copilot",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	got := app.resolveAPIKeyForModelFetch("https://api.example.com/v1")
	if got != "exact-oauth-key" {
		t.Fatalf("resolveAPIKeyForModelFetch = %q, want exact-oauth-key", got)
	}
}

func TestResolveAPIKeyForModelFetchDoesNotUseGitHubTokenAsAPIKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			// Key empty: must NOT fall back to OAuthAccessToken (GitHub token).
			{
				Name:             "GitHub Copilot",
				URL:              "https://api.githubcopilot.com",
				AuthType:         "oauth",
				Key:              "",
				OAuthAccessToken: "gho_github_token_not_copilot_api_key",
			},
		},
		MaclawLLMCurrentProvider: "GitHub Copilot",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	got := app.resolveAPIKeyForModelFetch("https://api.githubcopilot.com")
	if got != "" {
		t.Fatalf("resolveAPIKeyForModelFetch = %q, want empty (must not use GitHub OAuthAccessToken as API key)", got)
	}
}

func TestResolveAPIKeyForModelFetchUsesCopilotKeyNotOAuthAccessToken(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{
				Name:             "GitHub Copilot",
				URL:              "https://api.githubcopilot.com",
				AuthType:         "oauth",
				Key:              "copilot-api-token",
				OAuthAccessToken: "gho_github_token_should_not_win",
			},
		},
		MaclawLLMCurrentProvider: "GitHub Copilot",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	got := app.resolveAPIKeyForModelFetch("https://api.githubcopilot.com")
	if got != "copilot-api-token" {
		t.Fatalf("resolveAPIKeyForModelFetch = %q, want copilot-api-token", got)
	}
}

func TestFetchProviderModelsResolvesEmptyOAuthKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer internal-copilot-key" {
			t.Fatalf("Authorization = %q, want Bearer internal-copilot-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4","name":"Claude Sonnet 4"}]}`))
	}))
	defer srv.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "GitHub Copilot", URL: srv.URL, AuthType: "oauth", Key: "internal-copilot-key"},
		},
		MaclawLLMCurrentProvider: "GitHub Copilot",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	// Empty apiKey: must resolve from managed OAuth provider config.
	items, err := app.fetchProviderModels(srv.URL, "", "openai", "openclaw", false)
	if err != nil {
		t.Fatalf("fetchProviderModels() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "claude-sonnet-4" {
		t.Fatalf("items = %+v, want claude-sonnet-4", items)
	}
}

func TestGetMaclawLLMProvidersHydratesOAuthKeyFromCredentialStore(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	storePath := filepath.Join(tmpHome, "credentials.json")
	store := oauth.NewFileCredentialStore(storePath)
	if err := store.Modify("github-copilot", func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{
			Type:           "oauth",
			AccessToken:    "gh-long-lived",
			RawAccessToken: "copilot-short-lived",
			ExpiresAt:      time.Now().Add(time.Hour).Unix(),
		}, nil
	}); err != nil {
		t.Fatalf("store.Modify: %v", err)
	}

	app := &App{testHomeDir: tmpHome, credentialStore: store}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			// Key intentionally empty in config — should hydrate from store.
			{Name: "GitHub Copilot", URL: "https://api.githubcopilot.com", AuthType: "oauth", Key: ""},
		},
		MaclawLLMCurrentProvider: "GitHub Copilot",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	var got string
	for _, p := range data.Providers {
		if p.Name == "GitHub Copilot" {
			got = p.Key
			break
		}
	}
	if got != "copilot-short-lived" {
		t.Fatalf("GitHub Copilot Key = %q, want copilot-short-lived from credential store", got)
	}
}

func TestResolveProviderKeyFromStoreUsesRawOpenAIJWT(t *testing.T) {
	store := oauth.NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.json"))
	if err := store.Modify("openai", func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{
			Type:           "oauth",
			AccessToken:    "sk-legacy-platform-key",
			RawAccessToken: "eyJhbGciOiJub25lIn0.payload.sig",
		}, nil
	}); err != nil {
		t.Fatalf("store.Modify: %v", err)
	}

	app := &App{credentialStore: store}
	got := app.resolveProviderKeyFromStore(corelib.MaclawLLMProvider{
		Name:     "OpenAI",
		URL:      "https://chatgpt.com/backend-api/codex",
		AuthType: "oauth",
	})
	if got != "eyJhbGciOiJub25lIn0.payload.sig" {
		t.Fatalf("resolved OpenAI credential = %q, want raw OAuth JWT", got)
	}
}

func TestGetMaclawLLMConfigUsesRawJWTFromLegacyOpenAIConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:             "OpenAI",
			URL:              "https://chatgpt.com/backend-api/codex",
			Model:            oauth.CodexSubscriptionDefaultModel,
			AuthType:         "oauth",
			Key:              "sk-legacy-platform-key",
			OAuthAccessToken: "eyJhbGciOiJub25lIn0.payload.sig",
			WireAPI:          "responses-ws",
		}},
		MaclawLLMCurrentProvider: "OpenAI",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if got := app.GetMaclawLLMConfig().Key; got != "eyJhbGciOiJub25lIn0.payload.sig" {
		t.Fatalf("GetMaclawLLMConfig().Key = %q, want raw OAuth JWT", got)
	}
}

func TestGetMaclawLLMConfigPrefersRefreshedCredentialStoreJWT(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	store := oauth.NewFileCredentialStore(filepath.Join(tmpHome, "credentials.json"))
	if err := store.Modify("openai", func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{
			Type:           "oauth",
			AccessToken:    "eyJhbGciOiJub25lIn0.fresh.sig",
			RawAccessToken: "eyJhbGciOiJub25lIn0.fresh.sig",
		}, nil
	}); err != nil {
		t.Fatalf("store.Modify: %v", err)
	}

	app := &App{testHomeDir: tmpHome, credentialStore: store}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:             "OpenAI",
			URL:              "https://chatgpt.com/backend-api/codex",
			Model:            oauth.CodexSubscriptionDefaultModel,
			AuthType:         "oauth",
			Key:              "sk-legacy-platform-key",
			OAuthAccessToken: "eyJhbGciOiJub25lIn0.stale.sig",
			WireAPI:          "responses-ws",
		}},
		MaclawLLMCurrentProvider: "OpenAI",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if got := app.GetMaclawLLMConfig().Key; got != "eyJhbGciOiJub25lIn0.fresh.sig" {
		t.Fatalf("GetMaclawLLMConfig().Key = %q, want refreshed credential-store JWT", got)
	}
}

func TestDualLLMProfilesResolveIndependentCodingModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{
		{ID: "provider-assistant", Name: "Assistant Provider", URL: "https://assistant.example.test/v1", Key: "assistant-key", Model: "assistant-default"},
		{ID: "provider-coding", Name: "Coding Provider", URL: "https://coding.example.test/v1", Key: "coding-key", Model: "coding-default"},
	}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       providers,
		MaclawLLMCurrentProvider: "Assistant Provider",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "provider-coding", Model: "coding-model"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	assistantCfg := app.GetMaclawLLMConfig()
	if assistantCfg.ProviderName != "Assistant Provider" || assistantCfg.Model != "assistant-model" || assistantCfg.Profile != maclawLLMProfileAssistant {
		t.Fatalf("assistant config = %#v", assistantCfg)
	}
	codingCfg := app.GetCodingLLMConfig()
	if codingCfg.ProviderName != "Coding Provider" || codingCfg.Model != "coding-model" || codingCfg.Profile != maclawLLMProfileCoding {
		t.Fatalf("coding config = %#v", codingCfg)
	}
}

func TestDualLLMProfilesCodingFollowsAssistant(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "provider-assistant", Name: "Assistant Provider", URL: "https://assistant.example.test/v1", Key: "assistant-key", Model: "assistant-default"},
			{ID: "provider-next", Name: "Next Provider", URL: "https://next.example.test/v1", Key: "next-key", Model: "next-default"},
		},
		MaclawLLMCurrentProvider: "Assistant Provider",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	codingCfg := app.GetCodingLLMConfig()
	if codingCfg.ProviderName != "Assistant Provider" || codingCfg.Model != "assistant-model" || codingCfg.Profile != maclawLLMProfileCoding {
		t.Fatalf("coding follow config = %#v", codingCfg)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	updated := state.Profiles
	updated.Assistant.ProviderID = "provider-next"
	updated.Assistant.Model = "next-model"
	if err := app.SaveMaclawLLMProfiles(updated, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles: %v", err)
	}

	if codingCfg = app.GetCodingLLMConfig(); codingCfg.ProviderName != "Next Provider" || codingCfg.Model != "next-model" || codingCfg.Profile != maclawLLMProfileCoding {
		t.Fatalf("coding follow config after assistant change = %#v", codingCfg)
	}
	state, err = app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState after save: %v", err)
	}
	if state.Coding.ProviderName != "Next Provider" || state.Coding.Model != "next-model" || !state.Coding.InheritAssistant {
		t.Fatalf("coding follow panel summary after assistant change = %#v", state.Coding)
	}
}

func TestSaveMaclawLLMProfilesPreservesIndependentCodingModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "assistant-provider", Name: "Assistant", URL: "https://assistant.example.test/v1", Key: "assistant-key", Model: "assistant-old"},
			{ID: "coding-provider", Name: "Coding", URL: "https://coding.example.test/v1", Key: "coding-key", Model: "coding-old"},
		},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant-provider", Model: "assistant-old"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "coding-provider", Model: "coding-old"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	updated := state.Profiles
	updated.Coding.Model = "coding-new"
	if err := app.SaveMaclawLLMProfiles(updated, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles: %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if saved.MaclawLLMProfiles == nil || saved.MaclawLLMProfiles.Coding.Model != "coding-new" {
		t.Fatalf("saved coding profile = %#v", saved.MaclawLLMProfiles)
	}
	if saved.MaclawLLMProviders[0].Model != "assistant-old" {
		t.Fatalf("coding save changed assistant compatibility projection: %#v", saved.MaclawLLMProviders)
	}
	if got := app.GetCodingLLMConfig().Model; got != "coding-new" {
		t.Fatalf("coding model = %q, want coding-new", got)
	}
}

func TestSaveMaclawLLMProfilesRejectsStaleRevision(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://example.test/v1", Key: "key", Model: "model-a"}},
		MaclawLLMCurrentProvider: "Provider",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	first, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("first panel state: %v", err)
	}
	changed := first.Profiles
	changed.Assistant.Model = "model-b"
	if err := app.SaveMaclawLLMProfiles(changed, first.Revision); err != nil {
		t.Fatalf("first SaveMaclawLLMProfiles: %v", err)
	}

	stale := first.Profiles
	stale.Assistant.Model = "model-c"
	if err := app.SaveMaclawLLMProfiles(stale, first.Revision); err == nil || !strings.Contains(err.Error(), "changed elsewhere") {
		t.Fatalf("stale SaveMaclawLLMProfiles error = %v, want conflict", err)
	}
	if got := app.GetMaclawLLMConfig().Model; got != "model-b" {
		t.Fatalf("stale save overwrote model: %q", got)
	}
}

func TestQuickSaveMaclawLLMProfileScopesWritesAndRejectsFollowingCoding(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "assistant", Name: "Assistant", URL: "https://assistant.example.test/v1", Key: "assistant-key", Model: "assistant-a"},
			{ID: "coding", Name: "Coding", URL: "https://coding.example.test/v1", Key: "coding-key", Model: "coding-a"},
		},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-a"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "coding", Model: "coding-a"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	next, err := app.QuickSaveMaclawLLMProfile(maclawLLMProfileCoding, "coding", "coding-b", state.Revision)
	if err != nil {
		t.Fatalf("QuickSaveMaclawLLMProfile(coding): %v", err)
	}
	if next.Coding.Model != "coding-b" || next.Assistant.Model != "assistant-a" {
		t.Fatalf("quick save state = %#v", next)
	}
	if got := app.GetMaclawLLMConfig().Model; got != "assistant-a" {
		t.Fatalf("coding quick save changed assistant projection: %q", got)
	}

	profiles := next.Profiles
	profiles.Coding.InheritAssistant = true
	if err := app.SaveMaclawLLMProfiles(profiles, next.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles(follow): %v", err)
	}
	following, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState(following): %v", err)
	}
	if _, err := app.QuickSaveMaclawLLMProfile(maclawLLMProfileCoding, "coding", "coding-c", following.Revision); err == nil || !strings.Contains(err.Error(), "follows assistant") {
		t.Fatalf("following coding quick save error = %v, want follows-assistant rejection", err)
	}
}

func TestProfilePanelStateReportsVisionOnlyForTheConfirmedModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "vision-provider", Name: "Vision Provider", URL: "https://example.test/v1", Key: "key",
			Model: "vision-model", SupportsVision: true,
		}},
		MaclawLLMCurrentProvider: "Vision Provider",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "vision-provider", Model: "text-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if state.Assistant.SupportsVision || state.Coding.SupportsVision {
		t.Fatalf("other profile model inherited provider vision capability: %#v", state)
	}

	profiles := state.Profiles
	profiles.Assistant.Model = "vision-model"
	if err := app.SaveMaclawLLMProfiles(profiles, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles: %v", err)
	}
	state, err = app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState after save: %v", err)
	}
	if !state.Assistant.SupportsVision || !state.Coding.SupportsVision {
		t.Fatalf("confirmed vision model did not enable capability: %#v", state)
	}
}

func TestProfilePanelStateUsesVisionCapabilityForEachConfirmedProviderModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "vision-provider", Name: "Vision Provider", URL: "https://example.test/v1", Key: "key",
			Model: "default-vision", SupportsVision: true, VisionModels: []string{"default-vision", "alternate-vision"},
		}},
		MaclawLLMCurrentProvider: "Vision Provider",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "vision-provider", Model: "alternate-vision"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "vision-provider", Model: "text-only"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if !state.Assistant.SupportsVision || state.Coding.SupportsVision {
		t.Fatalf("model-specific capability = assistant:%v coding:%v, want true/false", state.Assistant.SupportsVision, state.Coding.SupportsVision)
	}
	if got := app.GetMaclawLLMConfig(); !got.SupportsVision {
		t.Fatalf("assistant runtime config lost alternate model vision capability: %#v", got)
	}
	if got := app.GetCodingLLMConfig(); got.SupportsVision {
		t.Fatalf("coding runtime config inherited another model vision capability: %#v", got)
	}
}

func TestNormalizeMaclawLLMProviderDeduplicatesVisionModelIDs(t *testing.T) {
	provider := normalizeMaclawLLMProvider(corelib.MaclawLLMProvider{
		Model: "Vision-Default", SupportsVision: true,
		VisionModels: []string{" vision-default ", "alternate", "ALTERNATE", ""},
	})
	if !reflect.DeepEqual(provider.VisionModels, []string{"alternate", "Vision-Default"}) {
		t.Fatalf("VisionModels = %#v", provider.VisionModels)
	}
	if !provider.SupportsVision || !providerSupportsVisionForModel(provider, "ALTERNATE") || providerSupportsVisionForModel(provider, "text-only") {
		t.Fatalf("normalized capability lookup is incorrect: %#v", provider)
	}
}

func TestFetchMaclawLLMProfileModelsResolvesProviderByStableID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"selected-model"}]}`))
	}))
	defer srv.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "assistant", Name: "Assistant", URL: "https://assistant.example.test/v1", Key: "assistant-key", Model: "assistant-model"},
			{ID: "catalog-target", Name: "Catalog target", URL: srv.URL + "/v1", Key: "target-key", Model: "target-model"},
		},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "catalog-target", Model: "target-model"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	items, err := app.FetchMaclawLLMProfileModels("catalog-target")
	if err != nil {
		t.Fatalf("FetchMaclawLLMProfileModels: %v", err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer target-key" {
		t.Fatalf("authorization = %q, want target provider key", gotAuth)
	}
	if len(items) != 1 || items[0].ID != "selected-model" {
		t.Fatalf("items = %+v", items)
	}
	if _, err := app.FetchMaclawLLMProfileModels("removed-provider"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing provider error = %v, want not found", err)
	}
}

func TestLegacyModelWritesAreRejectedForProfileManagedConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "key", Model: "assistant-model"},
			{ID: "coding", Name: "Coding", URL: "https://coding.example/v1", Key: "key", Model: "coding-model"},
		},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "coding", Model: "coding-model"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := app.SetMaclawLLMCurrentModel("assistant-new"); err == nil || !strings.Contains(err.Error(), "LLM profiles are enabled") {
		t.Fatalf("SetMaclawLLMCurrentModel error = %v, want profile-managed rejection", err)
	}
	if err := app.SaveMaclawLLMConfig(corelib.MaclawLLMConfig{URL: "https://changed.example/v1", Key: "changed-key", Model: "assistant-new"}); err == nil || !strings.Contains(err.Error(), "LLM profiles are enabled") {
		t.Fatalf("SaveMaclawLLMConfig error = %v, want profile-managed rejection", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := saved.MaclawLLMProfiles; got == nil || got.Assistant.Model != "assistant-model" || got.Coding.Model != "coding-model" {
		t.Fatalf("legacy model write changed profile-managed assignments: %#v", got)
	}
	if saved.MaclawLLMUrl != "" || saved.MaclawLLMKey != "" {
		t.Fatalf("legacy flat config write changed profile-managed config: %#v", saved)
	}
}

func TestSaveMaclawLLMProvidersRejectsRemovingIndependentCodingProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{
		{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "key", Model: "assistant-model"},
		{ID: "coding", Name: "Coding", URL: "https://coding.example/v1", Key: "key", Model: "coding-model"},
	}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: providers, MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "coding", Model: "coding-model"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := app.SaveMaclawLLMProviders([]corelib.MaclawLLMProvider{providers[0]}, "Assistant"); err == nil || !strings.Contains(err.Error(), "independent coding profile") {
		t.Fatalf("SaveMaclawLLMProviders removal error = %v, want coding-profile reference rejection", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if _, found := resolveMaclawLLMProviderByID(saved.MaclawLLMProviders, "coding"); !found {
		t.Fatal("failed provider save removed coding provider")
	}
}

func TestSaveMaclawLLMProvidersPreservesProfilesWhenEditingProviderCatalog(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{
		{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "assistant-key", Model: "assistant-default"},
		{ID: "coding", Name: "Coding", URL: "https://coding.example/v1", Key: "coding-key", Model: "coding-default"},
	}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: providers, MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-selected"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "coding", Model: "coding-selected"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	// Provider management is allowed to change catalog/connection fields, but
	// its legacy current argument cannot overwrite either selected profile.
	updated := append([]corelib.MaclawLLMProvider(nil), providers...)
	updated[0].Models = []string{"assistant-selected", "assistant-new"}
	updated[1].Models = []string{"coding-selected", "coding-new"}
	updated[1].URL = "https://coding-updated.example/v1"
	if err := app.SaveMaclawLLMProviders(updated, "Coding"); err != nil {
		t.Fatalf("SaveMaclawLLMProviders: %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if saved.MaclawLLMProfiles == nil || saved.MaclawLLMProfiles.Assistant.Model != "assistant-selected" || saved.MaclawLLMProfiles.Coding.Model != "coding-selected" {
		t.Fatalf("provider save overwrote profile assignment: %#v", saved.MaclawLLMProfiles)
	}
	if got := app.GetMaclawLLMConfig(); got.ProviderID != "assistant" || got.Model != "assistant-selected" {
		t.Fatalf("assistant resolution after provider save = %#v", got)
	}
	if got := app.GetCodingLLMConfig(); got.ProviderID != "coding" || got.Model != "coding-selected" || got.URL != "https://coding-updated.example/v1" {
		t.Fatalf("coding resolution after provider save = %#v", got)
	}
}

func TestSaveMaclawLLMProvidersRejectsRemovingAssistantProfileProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{
		{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "key", Model: "assistant-model"},
		{ID: "other", Name: "Other", URL: "https://other.example/v1", Key: "key", Model: "other-model"},
	}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: providers, MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := app.SaveMaclawLLMProviders([]corelib.MaclawLLMProvider{providers[1]}, "Other"); err == nil || !strings.Contains(err.Error(), "assistant profile") {
		t.Fatalf("assistant provider removal error = %v, want assistant-profile reference rejection", err)
	}
}

func TestMaclawLLMProfileHealthSeparatesIndependentProfilesAndReusesFollow(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "key", Model: "assistant-model"},
			{ID: "coding", Name: "Coding", URL: "https://coding.example/v1", Key: "key", Model: "coding-model"},
		},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "coding", Model: "coding-model"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	app.setMaclawLLMProfileHealth(maclawLLMProfileAssistant, "assistant", "assistant-model", maclawLLMProfileHealthRecord{Health: "configured", CheckedAt: "2026-01-01T00:00:00Z"})
	app.setMaclawLLMProfileHealth(maclawLLMProfileCoding, "coding", "coding-model", maclawLLMProfileHealthRecord{Health: "unavailable", CheckedAt: "2026-01-01T00:00:00Z", ReasonCode: "authentication_failed"})
	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if state.Assistant.Health != "configured" || state.Coding.Health != "unavailable" {
		t.Fatalf("independent health = assistant:%q coding:%q, want configured/unavailable", state.Assistant.Health, state.Coding.Health)
	}
	if state.Coding.ReasonCode != "authentication_failed" {
		t.Fatalf("coding reason = %q, want authentication_failed", state.Coding.ReasonCode)
	}

	profiles := state.Profiles
	profiles.Coding.InheritAssistant = true
	if err := app.SaveMaclawLLMProfiles(profiles, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles(follow): %v", err)
	}
	// Saving invalidates stale results; a fresh assistant probe is then the
	// single source for the following coding profile.
	app.setMaclawLLMProfileHealth(maclawLLMProfileAssistant, "assistant", "assistant-model", maclawLLMProfileHealthRecord{Health: "configured", CheckedAt: "2026-01-02T00:00:00Z"})
	following, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState(follow): %v", err)
	}
	if following.Coding.Health != "configured" || following.Coding.CheckedAt != following.Assistant.CheckedAt {
		t.Fatalf("following coding health = %#v, assistant = %#v; want assistant health projection", following.Coding, following.Assistant)
	}
}

func TestProfilePanelStateListsOnlyConnectionTestedProviders(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "passed", Name: "Passed provider", URL: "https://passed.example/v1", Key: "key", Model: "passed-model", ConnectionTestPassed: true},
			{ID: "pending", Name: "Pending provider", URL: "https://pending.example/v1", Key: "key", Model: "pending-model"},
			{ID: "hub", Name: hubServiceProviderName, URL: "https://hub.example/v1", Model: "auto", IsHubService: true, ConnectionTestPassed: true},
		},
		MaclawLLMCurrentProvider: "Passed provider",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "passed", Model: "passed-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if len(state.Providers) != 2 {
		t.Fatalf("assignment providers = %#v, want passed provider and Hub service only", state.Providers)
	}
	if state.Providers[0].ID != "passed" || !state.Providers[0].ConnectionTestPassed {
		t.Fatalf("first assignment provider = %#v, want tested passed provider", state.Providers[0])
	}
	if state.Providers[1].ID != "hub" || !state.Providers[1].IsHubService {
		t.Fatalf("second assignment provider = %#v, want Hub service", state.Providers[1])
	}
}

func TestSaveMaclawLLMProfilesRejectsUntestedProviderSelection(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "tested", Name: "Tested provider", URL: "https://tested.example/v1", Key: "key", Model: "tested-model", ConnectionTestPassed: true},
			{ID: "untested", Name: "Untested provider", URL: "https://untested.example/v1", Key: "key", Model: "untested-model"},
		},
		MaclawLLMCurrentProvider: "Tested provider",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "tested", Model: "tested-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	profiles := state.Profiles
	profiles.Assistant = corelib.MaclawLLMProfile{ProviderID: "untested", Model: "untested-model"}
	if err := app.SaveMaclawLLMProfiles(profiles, state.Revision); err == nil || !strings.Contains(err.Error(), "has not passed a connection test") {
		t.Fatalf("SaveMaclawLLMProfiles untested error = %v, want connection-test rejection", err)
	}
	if got := app.GetMaclawLLMConfig().ProviderID; got != "tested" {
		t.Fatalf("untested save changed assistant provider to %q", got)
	}
}

func TestProfileRevisionChangesWhenProviderEligibilityChanges(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://example.test/v1", Key: "key", Model: "model"}},
		MaclawLLMCurrentProvider: "Provider",
		MaclawLLMProfiles:        &corelib.MaclawLLMProfiles{Version: maclawLLMProfilesVersion, Assistant: corelib.MaclawLLMProfile{ProviderID: "provider", Model: "model"}, Coding: corelib.MaclawLLMProfile{InheritAssistant: true}},
	}
	before := maclawLLMProfileRevision(cfg)
	cfg.MaclawLLMProviders[0].ConnectionTestPassed = true
	if after := maclawLLMProfileRevision(cfg); after == before {
		t.Fatal("profile revision did not change when connection-test eligibility changed")
	}
}

func TestMarkMaclawLLMProviderConnectionTestPassedRejectsChangedModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://example.test/v1", Key: "key", Model: "current-model"}},
		MaclawLLMCurrentProvider: "Provider",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	err := app.markMaclawLLMProviderConnectionTestPassed("Provider", corelib.MaclawLLMConfig{ProviderID: "provider", Model: "tested-model"})
	if err == nil || !strings.Contains(err.Error(), "changed while its connection test was running") {
		t.Fatalf("mark changed model error = %v, want stale-test rejection", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MaclawLLMProviders[0].ConnectionTestPassed {
		t.Fatal("stale probe marked the changed provider as tested")
	}
}

func TestMarkMaclawLLMProviderConnectionTestPassedRejectsChangedConnectionShape(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://new.example/v1", Key: "key", Model: "model", Protocol: "openai", WireAPI: "responses"}},
		MaclawLLMCurrentProvider: "Provider",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	err := app.markMaclawLLMProviderConnectionTestPassed("Provider", corelib.MaclawLLMConfig{ProviderID: "provider", URL: "https://old.example/v1", Model: "model", Protocol: "openai", WireAPI: "responses"})
	if err == nil || !strings.Contains(err.Error(), "changed while its connection test was running") {
		t.Fatalf("mark changed connection error = %v, want stale-test rejection", err)
	}
}

func TestSaveMaclawLLMProvidersDoesNotTrustClientConnectionTestFlag(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://example.test/v1", Key: "old-key", Model: "model", ConnectionTestPassed: true}},
		MaclawLLMCurrentProvider: "Provider",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SaveMaclawLLMProviders([]corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://example.test/v1", Key: "new-key", Model: "model", ConnectionTestPassed: true}}, "Provider"); err != nil {
		t.Fatalf("SaveMaclawLLMProviders: %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if saved.MaclawLLMProviders[0].ConnectionTestPassed {
		t.Fatal("public save retained forged connection-test result after credential change")
	}
}

func TestSaveMaclawLLMProvidersKeepsTestedProviderIDAcrossRename(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "provider", Name: "Old name", URL: "https://example.test/v1", Key: "key", Model: "model", ConnectionTestPassed: true,
		}},
		MaclawLLMCurrentProvider: "Old name",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider", Model: "model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SaveMaclawLLMProviders([]corelib.MaclawLLMProvider{{
		Name: "Renamed provider", URL: "https://example.test/v1", Key: "key", Model: "model", ConnectionTestPassed: true,
	}}, "Renamed provider"); err != nil {
		t.Fatalf("SaveMaclawLLMProviders: %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	provider := saved.MaclawLLMProviders[0]
	if provider.ID != "provider" || !provider.ConnectionTestPassed {
		t.Fatalf("renamed provider = %#v, want original ID and verified state", provider)
	}
	if saved.MaclawLLMProfiles == nil || saved.MaclawLLMProfiles.Assistant.ProviderID != "provider" {
		t.Fatalf("rename invalidated assistant profile: %#v", saved.MaclawLLMProfiles)
	}
}

func TestTestAndSaveMaclawLLMProvidersMarksSuccessfulConnectionTest(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://old.example/v1", Key: "old-key", Model: "model"}},
		MaclawLLMCurrentProvider: "Provider",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer srv.Close()
	providers := []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://new.example/v1", Key: "new-key", Model: "model"}}
	providers[0].URL = srv.URL
	if _, err := app.TestAndSaveMaclawLLMProviders(providers, "Provider", "Provider"); err != nil {
		t.Fatalf("TestAndSaveMaclawLLMProviders: %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	provider := saved.MaclawLLMProviders[0]
	if !provider.ConnectionTestPassed || provider.URL != srv.URL || provider.Key != "new-key" {
		t.Fatalf("saved provider = %#v, want tested new connection", provider)
	}
}

func TestTestAndSaveMaclawLLMProvidersRejectsAmbiguousProviderName(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "first", Name: "Shared name", URL: "https://first.example/v1", Key: "first-key", Model: "first-model"},
			{ID: "second", Name: "Shared name", URL: "https://second.example/v1", Key: "second-key", Model: "second-model"},
		},
		MaclawLLMCurrentProvider: "Shared name",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	_, err := app.TestAndSaveMaclawLLMProviders([]corelib.MaclawLLMProvider{
		{ID: "first", Name: "Shared name", URL: "https://first.example/v1", Key: "first-key", Model: "first-model"},
		{ID: "second", Name: "Shared name", URL: "https://second.example/v1", Key: "second-key", Model: "second-model"},
	}, "Shared name", "Shared name")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("TestAndSaveMaclawLLMProviders() error = %v, want ambiguous provider rejection", err)
	}
}

func TestTestAndSaveMaclawLLMProvidersRejectsConcurrentProviderChange(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://old.example/v1", Key: "old-key", Model: "model"}},
		MaclawLLMCurrentProvider: "Provider",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer srv.Close()

	result := make(chan error, 1)
	go func() {
		_, err := app.TestAndSaveMaclawLLMProviders([]corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: srv.URL, Key: "new-key", Model: "model"}}, "Provider", "Provider")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("connection test did not start")
	}
	if err := app.SaveMaclawLLMProviders([]corelib.MaclawLLMProvider{{ID: "provider", Name: "Provider", URL: "https://concurrent.example/v1", Key: "other-key", Model: "model"}}, "Provider"); err != nil {
		t.Fatalf("concurrent SaveMaclawLLMProviders: %v", err)
	}
	close(release)
	if err := <-result; err == nil || !strings.Contains(err.Error(), "providers changed while the connection test was running") {
		t.Fatalf("TestAndSaveMaclawLLMProviders error = %v, want stale provider rejection", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if provider := saved.MaclawLLMProviders[0]; provider.URL != "https://concurrent.example/v1" || provider.ConnectionTestPassed {
		t.Fatalf("concurrent provider was overwritten or marked tested: %#v", provider)
	}
}

func TestMaclawLLMProfileHealthFromPingDoesNotMakeTransientFailuresUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   MaclawLLMStatus
		want string
	}{
		{name: "online", in: MaclawLLMStatus{Online: true, Configured: true}, want: "configured"},
		{name: "auth", in: MaclawLLMStatus{Configured: true, Error: "HTTP 401 unauthorized"}, want: "unavailable"},
		{name: "timeout", in: MaclawLLMStatus{Configured: true, Error: "request failed: timeout"}, want: "unverified"},
		{name: "invalid", in: MaclawLLMStatus{Configured: false}, want: "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := maclawLLMProfileHealthFromPing(tc.in).Health; got != tc.want {
				t.Fatalf("health = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSuccessfulLLMRequestMarksCurrentProfileHealthy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "key", Model: "assistant-model"}},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles:        &corelib.MaclawLLMProfiles{Version: 1, Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"}, Coding: corelib.MaclawLLMProfile{InheritAssistant: true}},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	resolved, err := app.ResolveMaclawLLMProfile(maclawLLMProfileAssistant)
	if err != nil {
		t.Fatalf("ResolveMaclawLLMProfile: %v", err)
	}
	app.markMaclawLLMProfileHealthyIfCurrent(resolved)
	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if state.Assistant.Health != "configured" || state.Coding.Health != "configured" {
		t.Fatalf("health after successful request = assistant:%q coding:%q, want configured/configured", state.Assistant.Health, state.Coding.Health)
	}

	stale := resolved
	stale.Model = "different-model"
	app.markMaclawLLMProfileHealthyIfCurrent(stale)
	if _, ok := app.maclawLLMProfileHealthFor(maclawLLMProfileAssistant, stale.ProviderID, stale.Model); ok {
		t.Fatal("a stale successful request created health for a non-current model")
	}
}

func TestTransientProbeDoesNotOverrideSuccessfulLLMRequestHealth(t *testing.T) {
	app := &App{}
	app.setMaclawLLMProfileHealth(maclawLLMProfileAssistant, "assistant", "assistant-model", maclawLLMProfileHealthRecord{Health: "configured", CheckedAt: "2026-08-10T10:00:00Z"})
	app.setMaclawLLMProfileHealth(maclawLLMProfileAssistant, "assistant", "assistant-model", maclawLLMProfileHealthRecord{Health: "unverified", CheckedAt: "2026-08-10T10:01:00Z", ReasonCode: "probe_retryable"})
	record, ok := app.maclawLLMProfileHealthFor(maclawLLMProfileAssistant, "assistant", "assistant-model")
	if !ok || record.Health != "configured" || record.CheckedAt != "2026-08-10T10:00:00Z" {
		t.Fatalf("transient probe overwrote successful request health: %#v", record)
	}

	app.setMaclawLLMProfileHealth(maclawLLMProfileAssistant, "assistant", "assistant-model", maclawLLMProfileHealthRecord{Health: "unavailable", ReasonCode: "authentication_failed"})
	record, _ = app.maclawLLMProfileHealthFor(maclawLLMProfileAssistant, "assistant", "assistant-model")
	if record.Health != "unavailable" {
		t.Fatalf("authentication failure must override previous success, got %#v", record)
	}
}

func TestMarkMaclawLLMProfileHealthyDoesNotEmitForUnchangedHealth(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "key", Model: "assistant-model"}},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles:        &corelib.MaclawLLMProfiles{Version: 1, Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"}, Coding: corelib.MaclawLLMProfile{InheritAssistant: true}},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	resolved, err := app.ResolveMaclawLLMProfile(maclawLLMProfileAssistant)
	if err != nil {
		t.Fatalf("ResolveMaclawLLMProfile: %v", err)
	}
	if !app.setMaclawLLMProfileHealthIfCurrent(maclawLLMProfileAssistant, resolved, maclawLLMProfileHealthRecord{Health: "configured", CheckedAt: "2026-08-10T10:00:00Z"}) {
		t.Fatal("initial health write was unexpectedly suppressed")
	}
	if app.setMaclawLLMProfileHealthIfCurrent(maclawLLMProfileAssistant, resolved, maclawLLMProfileHealthRecord{Health: "unverified", ReasonCode: "probe_retryable"}) {
		t.Fatal("a transient probe should not replace successful request health")
	}
	if app.setMaclawLLMProfileHealthIfCurrent(maclawLLMProfileAssistant, resolved, maclawLLMProfileHealthRecord{Health: "configured", CheckedAt: "2026-08-10T10:01:00Z"}) {
		t.Fatal("a repeated successful request should not create a redundant health update")
	}
}

func TestRefreshMaclawLLMProfileHealthProbesAssistantAndIndependentCodingSeparately(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	oldClient := maclawLLMPingClient
	defer func() { maclawLLMPingClient = oldClient }()

	var assistantRequests, codingRequests atomic.Int32
	maclawLLMPingClient = &http.Client{Transport: appLLMRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "assistant.health.test":
			assistantRequests.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
		case "coding.health.test":
			codingRequests.Add(1)
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
		default:
			return nil, fmt.Errorf("unexpected host %q", req.URL.Host)
		}
	})}
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "assistant", Name: "Assistant", URL: "https://assistant.health.test/v1", Key: "key", Model: "assistant-model"},
			{ID: "coding", Name: "Coding", URL: "https://coding.health.test/v1", Key: "key", Model: "coding-model"},
		},
		MaclawLLMCurrentProvider: "Assistant",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "coding", Model: "coding-model"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	state, err := app.RefreshMaclawLLMProfileHealth()
	if err != nil {
		t.Fatalf("RefreshMaclawLLMProfileHealth: %v", err)
	}
	if state.Assistant.Health != "configured" || state.Coding.Health != "unavailable" {
		t.Fatalf("health = assistant:%q coding:%q, want configured/unavailable", state.Assistant.Health, state.Coding.Health)
	}
	if got := assistantRequests.Load(); got == 0 {
		t.Fatal("assistant was not probed")
	}
	if got := codingRequests.Load(); got == 0 {
		t.Fatal("independent coding was not probed")
	}

	profiles := state.Profiles
	profiles.Coding.InheritAssistant = true
	if err := app.SaveMaclawLLMProfiles(profiles, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles(follow): %v", err)
	}
	assistantRequests.Store(0)
	codingRequests.Store(0)
	following, err := app.RefreshMaclawLLMProfileHealth()
	if err != nil {
		t.Fatalf("RefreshMaclawLLMProfileHealth(follow): %v", err)
	}
	if following.Coding.Health != following.Assistant.Health {
		t.Fatalf("following coding health = %q, assistant = %q", following.Coding.Health, following.Assistant.Health)
	}
	if codingRequests.Load() != 0 {
		t.Fatalf("following coding sent %d duplicate probes", codingRequests.Load())
	}
}

func TestFetchProviderModelsCodexSubscriptionUsesCodexEndpoint(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var gotPath, gotAuth, gotAccount, gotBeta, gotOriginator string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		gotBeta = r.Header.Get("OpenAI-Beta")
		gotOriginator = r.Header.Get("originator")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4","object":"model"},{"id":"gpt-5.3-codex","object":"model"}]}`))
	}))
	defer srv.Close()

	// Minimal JWT with auth.chatgpt_account_id for header extraction.
	// payload: {"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}
	jwt := "eyJhbGciOiJub25lIn0." +
		base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}`)) +
		".sig"

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:             "OpenAI",
			URL:              srv.URL + "/backend-api/codex",
			AuthType:         "oauth",
			Key:              "sk-should-not-be-used",
			OAuthAccessToken: jwt,
			WireAPI:          "responses-ws",
		}},
		MaclawLLMCurrentProvider: "OpenAI",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Frontend may pass sk- key; backend must use JWT against codex/models.
	items, err := app.fetchProviderModels(srv.URL+"/backend-api/codex", "sk-should-not-be-used", "openai", "openclaw", false)
	if err != nil {
		t.Fatalf("fetchProviderModels: %v", err)
	}
	if gotPath != "/backend-api/codex/models" {
		t.Fatalf("path = %q, want /backend-api/codex/models", gotPath)
	}
	if gotAuth != "Bearer "+jwt {
		t.Fatalf("Authorization = %q, want Bearer JWT", gotAuth)
	}
	if gotAccount != "acct-123" {
		t.Fatalf("chatgpt-account-id = %q, want acct-123", gotAccount)
	}
	if gotBeta != "responses=experimental" {
		t.Fatalf("OpenAI-Beta = %q", gotBeta)
	}
	if gotOriginator != "codex_cli_rs" {
		t.Fatalf("originator = %q", gotOriginator)
	}
	if len(items) != 2 || items[0].ID != "gpt-5.4" {
		t.Fatalf("items = %+v", items)
	}
}

func TestFetchProviderModelsCodexSubscriptionFallsBackToCatalogOn403(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate the broken /models path returning 403; codex/models also 403.
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"Forbidden"}`))
	}))
	defer srv.Close()

	app := &App{testHomeDir: tmpHome}
	items, err := app.fetchProviderModels(srv.URL+"/backend-api/codex", "eyJhbGciOiJub25lIn0.e30.sig", "openai", "openclaw", true)
	if err != nil {
		t.Fatalf("fetchProviderModels: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected built-in catalog fallback")
	}
	wanted := map[string]bool{
		"gpt-5.6-luna":  true,
		"gpt-5.6-terra": true,
		"gpt-5.6-sol":   true,
	}
	for _, it := range items {
		delete(wanted, it.ID)
	}
	if len(wanted) != 0 {
		t.Fatalf("catalog missing GPT-5.6 variants %v: %+v", wanted, items)
	}
}

func TestResolveCodexAuthTokenPrefersJWTOverAPIKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:             "OpenAI",
			URL:              "https://chatgpt.com/backend-api",
			AuthType:         "oauth",
			Key:              "sk-api-key",
			OAuthAccessToken: "eyJhbGciOiJub25lIn0.payload.sig",
		}},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got := app.resolveCodexAuthToken("https://chatgpt.com/backend-api", "sk-from-frontend")
	if got != "eyJhbGciOiJub25lIn0.payload.sig" {
		t.Fatalf("resolveCodexAuthToken = %q, want JWT", got)
	}
}

func TestResolveCodexAuthTokenRejectsPlatformAPIKeyWithoutJWT(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if got := app.resolveCodexAuthToken("https://chatgpt.com/backend-api", "sk-platform-key"); got != "" {
		t.Fatalf("resolveCodexAuthToken = %q, want empty without a JWT", got)
	}
}

func TestFetchProviderModelsAnthropicUsesOfficialSDK(t *testing.T) {
	var gotPath, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		if r.Header.Get("x-api-key") != "token" {
			t.Fatalf("x-api-key = %q, want token", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-5.1","display_name":"GLM 5.1"}]}`))
	}))
	defer srv.Close()

	items, err := (&App{}).fetchProviderModels(srv.URL, "token", "anthropic", "claude code 2.0", false)
	if err != nil {
		t.Fatalf("fetchProviderModels() error = %v", err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotUA != "claude code 2.0" {
		t.Fatalf("User-Agent = %q, want claude code 2.0", gotUA)
	}
	if len(items) != 1 || items[0].ID != "glm-5.1" || items[0].Name != "GLM 5.1" {
		t.Fatalf("items = %+v, want glm-5.1/GLM 5.1", items)
	}
}

func TestOpenAIModelsEndpointCandidates(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		protocol string
		want     []string
	}{
		{
			name:    "openai bare base includes v1 fallback",
			baseURL: "https://example.test/api",
			want: []string{
				"https://example.test/api/models",
				"https://example.test/api/v1/models",
			},
		},
		{
			name:    "openai v1 base does not duplicate v1",
			baseURL: "https://example.test/api/v1",
			want:    []string{"https://example.test/api/v1/models"},
		},
		{
			name:    "models endpoint stays unchanged",
			baseURL: "https://example.test/api/v1/models",
			want:    []string{"https://example.test/api/v1/models"},
		},
		{
			name:     "anthropic bare base prefers v1 then legacy",
			baseURL:  "https://example.test/anthropic",
			protocol: "anthropic",
			want: []string{
				"https://example.test/anthropic/v1/models",
				"https://example.test/anthropic/models",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openAIModelsEndpointCandidates(tt.baseURL, tt.protocol)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("openAIModelsEndpointCandidates() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeOpenAIProbeBaseURLUsesGLMCodingPlanEndpoint(t *testing.T) {
	got := normalizeOpenAIProbeBaseURL("https://open.bigmodel.cn/api/paas/v4", "Kilo Code")
	want := "https://open.bigmodel.cn/api/coding/paas/v4"
	if got != want {
		t.Fatalf("normalizeOpenAIProbeBaseURL() = %q, want %q", got, want)
	}

	got = normalizeOpenAIProbeBaseURL("https://open.bigmodel.cn/api/paas/v4", "openclaw")
	want = "https://open.bigmodel.cn/api/paas/v4"
	if got != want {
		t.Fatalf("normalizeOpenAIProbeBaseURL() with openclaw = %q, want %q", got, want)
	}
}

func TestCodeGenClientNameForModelConfigPrefersModelAgentType(t *testing.T) {
	model := corelib.ModelConfig{
		ModelName: "CodeGen",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		AgentType: "custom-tool-agent",
	}
	cfg := corelib.AppConfig{MaclawLLMProviders: []corelib.MaclawLLMProvider{{
		Name:      "CodeGen",
		URL:       "https://codegen.qianxin-inc.cn/api/v1",
		AgentType: "provider-agent",
	}}}

	if got := codeGenClientNameForModelConfig(cfg, model); got != "custom-tool-agent" {
		t.Fatalf("codeGenClientNameForModelConfig() = %q, want custom-tool-agent", got)
	}
}

func TestShouldPatchRemoteEmailFromLogin(t *testing.T) {
	tests := []struct {
		name         string
		currentEmail string
		loginEmail   string
		want         bool
	}{
		{name: "empty login email", currentEmail: "", loginEmail: "", want: false},
		{name: "login phone placeholder", currentEmail: "", loginEmail: "phone:17090134628", want: false},
		{name: "empty current email", currentEmail: "", loginEmail: "user@example.com", want: true},
		{name: "phone placeholder", currentEmail: "phone:17090134628", loginEmail: "user@example.com", want: true},
		{name: "phone placeholder uppercase", currentEmail: " PHONE:17090134628 ", loginEmail: " user@example.com ", want: true},
		{name: "existing real email", currentEmail: "owner@example.com", loginEmail: "user@example.com", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPatchRemoteEmailFromLogin(tt.currentEmail, tt.loginEmail); got != tt.want {
				t.Fatalf("shouldPatchRemoteEmailFromLogin(%q, %q) = %v, want %v", tt.currentEmail, tt.loginEmail, got, tt.want)
			}
		})
	}
}

func TestTestOpenAILLM_UsesReasoningFallbackAndStripsTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"<think>hidden</think> <|FunctionCallBegin|>{}<|FunctionCallEnd|> final answer"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	app := &App{}
	got, err := app.testOpenAILLM(corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", AgentType: "test-agent"})
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

	if !probeVisionOpenAI(corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", AgentType: "test-agent"}, "abc") {
		t.Fatal("probeVisionOpenAI() = false, want true")
	}
}

func TestVisionProbeImage_IsValidStablePNG(t *testing.T) {
	if !validVisionProbeImage(visionProbeRedPNG) {
		t.Fatal("visionProbeRedPNG must remain a valid, known 64x64 red PNG payload")
	}
}

func TestVisionProbePrompt_DoesNotRevealExpectedColour(t *testing.T) {
	prompt := strings.ToLower(visionProbePrompt())
	if strings.Contains(prompt, "red") || strings.Contains(prompt, "ff0000") {
		t.Fatalf("vision probe prompt leaks the expected answer: %q", prompt)
	}
	if !strings.Contains(prompt, "no image") {
		t.Fatalf("vision probe prompt must give a deterministic no-image response: %q", prompt)
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
	got, err := app.testAnthropicLLM(corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", AgentType: "test-agent"})
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

	if !probeVisionAnthropic(corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", AgentType: "test-agent"}, "abc") {
		t.Fatal("probeVisionAnthropic() = false, want true")
	}
}

func TestProbeVisionAnthropicClassifiesExplicitImageRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"message":"This model does not support image inputs"}}`))
	}))
	defer srv.Close()

	got := probeVisionAnthropicResult(corelib.MaclawLLMConfig{
		URL: srv.URL, Model: "test-model", Protocol: "anthropic", AgentType: "test-agent",
	}, visionProbeRedPNG)
	if got != visionProbeUnsupported {
		t.Fatalf("probeVisionAnthropicResult() = %q, want unsupported", got)
	}
}

func TestProbeVisionResponsesAPI_SanitizesQwenStoreAndNormalizesEndpoint(t *testing.T) {
	var captured map[string]interface{}
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"red"}]}]}`))
	}))
	defer srv.Close()

	if !probeVisionResponsesAPI(srv.URL, "key", "qwen-vl-plus", "test-agent") {
		t.Fatal("probeVisionResponsesAPI() = false, want true")
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if _, ok := captured["store"]; ok {
		t.Fatalf("Qwen Responses vision probe leaked store: %#v", captured)
	}
}

func TestProbeVisionResponsesAPI_UsesSharedRequestBuilder(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"ruby red"}]}]}`))
	}))
	defer srv.Close()

	if !probeVisionResponsesAPIWithConfig(corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "oauth-token", Model: "grok-4.5", ProviderName: "xAI-Grok", AuthType: "oauth", WireAPI: "responses",
	}) {
		t.Fatal("probeVisionResponsesAPIWithConfig() = false, want true")
	}
	if got := captured["store"]; got != false {
		t.Fatalf("store = %#v, want false", got)
	}
	input := captured["input"].([]interface{})
	content := input[0].(map[string]interface{})["content"].([]interface{})
	if len(content) != 2 || content[0].(map[string]interface{})["type"] != "input_text" || content[1].(map[string]interface{})["type"] != "input_image" {
		t.Fatalf("vision content = %#v, want input_text followed by input_image", content)
	}
}

func TestProbeVisionResponsesAPI_AcceptsCompatibleResponseText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"text","content":"red"}]}]}`))
	}))
	defer srv.Close()

	if !probeVisionResponsesAPIWithConfig(corelib.MaclawLLMConfig{
		URL: srv.URL, Model: "grok-4.5", WireAPI: "responses",
	}) {
		t.Fatal("probeVisionResponsesAPIWithConfig() = false, want true for compatible text content")
	}
}

func TestTestMaclawLLMResponsesProbeRetainsProviderAuth(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("X-XAI-Token-Auth"); got != "xai-grok-cli" {
			t.Fatalf("X-XAI-Token-Auth = %q, want xai-grok-cli", got)
		}
		if requests.Load() == 2 {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode vision request: %v", err)
			}
			input := body["input"].([]interface{})
			content := input[0].(map[string]interface{})["content"].([]interface{})
			if len(content) != 2 || content[1].(map[string]interface{})["type"] != "input_image" {
				t.Fatalf("vision request content = %#v, want input_text plus input_image", content)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"red"}]}]}`))
	}))
	defer srv.Close()

	app := &App{}
	got, err := app.TestMaclawLLM(corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "oauth-token", Model: "grok-4.5", Protocol: "openai",
		WireAPI: "responses", ProviderName: "xAI-Grok", AuthType: "oauth",
	})
	if err != nil {
		t.Fatalf("TestMaclawLLM returned error: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2 (text test plus vision probe)", got)
	}
	if !got.SupportsVision {
		t.Fatal("TestMaclawLLM supports_vision = false, want true")
	}
}

func TestOAuthProviderCapabilityCheckPersistsVisionResult(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("Authorization = %q, want OAuth bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"red"}}]}`))
	}))
	defer srv.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{
				Name: "OAuth Test", URL: srv.URL, Key: "oauth-token", Model: "test-model",
				Protocol: "openai", AuthType: "oauth", WireAPI: "chat", IsCustom: true,
			},
			{Name: "Other", URL: "https://other.example/v1", Model: "other-model", IsCustom: true},
		},
		MaclawLLMCurrentProvider: "Other",
		MaclawLLMModel:           "other-model",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.testAndSaveOAuthProviderCapability("OAuth Test")
	if err != nil {
		t.Fatalf("testAndSaveOAuthProviderCapability() error = %v", err)
	}
	if !result.SupportsVision {
		t.Fatal("SupportsVision = false, want true")
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2 (text test plus image probe)", got)
	}

	providers := app.GetMaclawLLMProviders()
	if providers.Current != "Other" {
		t.Fatalf("current provider = %q, want Other", providers.Current)
	}
	for _, provider := range providers.Providers {
		if provider.Name == "OAuth Test" {
			if !provider.SupportsVision {
				t.Fatal("persisted SupportsVision = false, want true")
			}
			if provider.Key != "oauth-token" {
				t.Fatalf("OAuth Test key = %q, want preserved OAuth token", provider.Key)
			}
			return
		}
	}
	t.Fatal("OAuth Test provider missing after capability check")
}

func TestOAuthProviderCapabilityCheckKeepsExistingVisionOnInconclusiveProbe(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
			return
		}
		// A malformed response represents an inconclusive image request, not a
		// model-confirmed lack of image input support.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "OAuth Test", URL: srv.URL, Key: "oauth-token", Model: "test-model",
			Protocol: "openai", AuthType: "oauth", WireAPI: "chat", SupportsVision: true, IsCustom: true,
		}},
		MaclawLLMCurrentProvider: "OAuth Test",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.testAndSaveOAuthProviderCapability("OAuth Test")
	if err != nil {
		t.Fatalf("testAndSaveOAuthProviderCapability() error = %v", err)
	}
	if result.VisionProbeStatus != string(visionProbeInconclusive) {
		t.Fatalf("VisionProbeStatus = %q, want inconclusive", result.VisionProbeStatus)
	}
	if result.SupportsVision {
		t.Fatal("inconclusive probe must not report SupportsVision=true")
	}
	if got := app.GetMaclawLLMProviders().Providers[0].SupportsVision; !got {
		t.Fatal("inconclusive image probe overwrote a previously confirmed SupportsVision=true")
	}
}

func TestSaveVisionProbeResultForProviderDoesNotCreateUnneededWrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "OAuth Test", Model: "test-model", SupportsVision: true, IsCustom: true,
		}},
		MaclawLLMCurrentProvider: "OAuth Test",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.saveVisionProbeResultForProvider("OAuth Test", "test-model", true); err != nil {
		t.Fatalf("saveVisionProbeResultForProvider() error = %v", err)
	}
	if got := app.GetMaclawLLMProviders().Providers[0].SupportsVision; !got {
		t.Fatal("SupportsVision = false, want true")
	}
}

func TestSaveVisionProbeResultForProviderTracksOnlyTheTestedModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "Vision Provider", Model: "default-model", SupportsVision: true,
			VisionModels: []string{"default-model", "alternate-model"}, IsCustom: true,
		}},
		MaclawLLMCurrentProvider: "Vision Provider",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.saveVisionProbeResultForProvider("Vision Provider", "alternate-model", false); err != nil {
		t.Fatalf("save alternate-model result: %v", err)
	}
	provider := app.GetMaclawLLMProviders().Providers[0]
	if provider.SupportsVision != true || !reflect.DeepEqual(provider.VisionModels, []string{"default-model"}) {
		t.Fatalf("alternate result changed default capability or retained stale model: %#v", provider)
	}

	if err := app.saveVisionProbeResultForProvider("Vision Provider", "default-model", false); err != nil {
		t.Fatalf("save default-model result: %v", err)
	}
	provider = app.GetMaclawLLMProviders().Providers[0]
	if provider.SupportsVision || len(provider.VisionModels) != 0 {
		t.Fatalf("default result did not clear only default capability: %#v", provider)
	}
}

func TestMaterializeProviderByNameKeepsNonOpenAIOAuthOnConfiguredProtocol(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "Anthropic", URL: "https://api.anthropic.com", Key: "anthropic-token", Model: "claude-test", Protocol: "anthropic", AuthType: "oauth"},
			{Name: "GitHub Copilot", URL: "https://api.githubcopilot.com", Key: "copilot-token", Model: "copilot-test", Protocol: "openai", AuthType: "oauth"},
		},
		MaclawLLMCurrentProvider: "Anthropic",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	for _, providerName := range []string{"Anthropic", "GitHub Copilot"} {
		cfg, err := app.MaterializeProviderByName(providerName)
		if err != nil {
			t.Fatalf("MaterializeProviderByName(%q) error = %v", providerName, err)
		}
		if cfg.WireAPI != "" {
			t.Fatalf("MaterializeProviderByName(%q).WireAPI = %q, want empty/default transport", providerName, cfg.WireAPI)
		}
	}

	openAI := corelib.MaclawLLMProvider{Name: "OpenAI", URL: "https://chatgpt.com/backend-api/codex", AuthType: "oauth"}
	if !openAI.IsCodexSubscriptionOAuthProvider() {
		t.Fatal("OpenAI Codex provider must retain its Responses transport default")
	}
}

func TestMaterializeProviderByNameUsesImportedCodexAPIKeyWhenNoOAuthJWTExists(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	store := oauth.NewFileCredentialStore(filepath.Join(tmpHome, "credentials.json"))
	app := &App{testHomeDir: tmpHome, credentialStore: store}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "OpenAI", URL: "https://chatgpt.com/backend-api/codex",
			Model: "gpt-5", AuthType: "oauth",
		}},
		MaclawLLMCurrentProvider: "OpenAI",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := store.Modify("openai", func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "sk-imported-key"}, nil
	}); err != nil {
		t.Fatalf("credential store Modify() error = %v", err)
	}

	cfg, err := app.MaterializeProviderByName("OpenAI")
	if err != nil {
		t.Fatalf("MaterializeProviderByName() error = %v", err)
	}
	if cfg.Key != "sk-imported-key" {
		t.Fatalf("materialized key = %q, want imported API key", cfg.Key)
	}
}

func TestTestMaclawLLMRejectsUnsignedOAuthProbe(t *testing.T) {
	app := &App{}
	_, err := app.TestMaclawLLM(corelib.MaclawLLMConfig{
		URL: "https://api.x.ai/v1", Model: "grok-4.5", Protocol: "openai",
		WireAPI: "responses", ProviderName: "xAI-Grok", AuthType: "oauth",
	})
	if err == nil || !strings.Contains(err.Error(), "OAuth token is missing") {
		t.Fatalf("TestMaclawLLM unsigned OAuth error = %v, want missing-token error", err)
	}
}

func TestOAuthFlowReplacementKeepsNewestCancellationHandle(t *testing.T) {
	app := &App{}
	first, finishFirst, _ := app.beginOAuthFlow(time.Minute)
	second, finishSecond, claimSecond := app.beginOAuthFlow(time.Minute)
	defer finishSecond()

	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("starting a new OAuth flow did not cancel the previous flow")
	}
	finishFirst()
	if err := claimSecond(nil); err != nil {
		t.Fatal("finishing the replaced OAuth flow cleared the active flow")
	}

	app.CancelXAIOAuth()
	select {
	case <-second.Done():
		t.Fatal("cancelling a claimed OAuth result cancelled its completed flow")
	default:
	}

	third, finishThird, claimThird := app.beginOAuthFlow(time.Minute)
	defer finishThird()
	app.CancelXAIOAuth()
	select {
	case <-third.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelling OAuth did not cancel the active flow")
	}
	if err := claimThird(nil); err == nil {
		t.Fatal("cancelled OAuth flow claimed a result")
	}
}

func TestOAuthClaimSerializesProviderPersistence(t *testing.T) {
	app := &App{}
	_, finishFirst, claimFirst := app.beginOAuthFlow(time.Minute)
	defer finishFirst()

	commitStarted := make(chan struct{})
	releaseCommit := make(chan struct{})
	commitDone := make(chan error, 1)
	go func() {
		commitDone <- claimFirst(func() error {
			close(commitStarted)
			<-releaseCommit
			return nil
		})
	}()
	select {
	case <-commitStarted:
	case <-time.After(time.Second):
		t.Fatal("OAuth claim did not start its commit")
	}

	newFlowStarted := make(chan struct{})
	go func() {
		_, finish, _ := app.beginOAuthFlow(time.Minute)
		defer finish()
		close(newFlowStarted)
	}()
	select {
	case <-newFlowStarted:
		t.Fatal("new OAuth flow started before the accepted result committed")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseCommit)
	if err := <-commitDone; err != nil {
		t.Fatalf("claimFirst commit error = %v", err)
	}
	select {
	case <-newFlowStarted:
	case <-time.After(time.Second):
		t.Fatal("new OAuth flow remained blocked after commit")
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
		// Simulate a model that does NOT see the image — replies with "I don't see any image".
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"I don't see any image in your message."},"finish_reason":"stop"}]}`))
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

func TestClassifyVisionProbeHTTPFailure(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   visionProbeResult
	}{
		{
			name:   "explicit image rejection",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"This model does not support image inputs"}}`,
			want:   visionProbeUnsupported,
		},
		{
			name:   "vision disabled for model",
			status: http.StatusUnprocessableEntity,
			body:   `{"error":{"message":"Vision is not enabled for this model"}}`,
			want:   visionProbeUnsupported,
		},
		{
			name:   "ordinary malformed request",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"invalid request body"}}`,
			want:   visionProbeInconclusive,
		},
		{
			name:   "transient server error",
			status: http.StatusServiceUnavailable,
			body:   `{"error":{"message":"image service unavailable"}}`,
			want:   visionProbeInconclusive,
		},
		{
			name:   "image permission failure is not a capability result",
			status: http.StatusForbidden,
			body:   `{"error":{"message":"image input is unavailable for this account"}}`,
			want:   visionProbeInconclusive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyVisionProbeHTTPFailure(tt.status, []byte(tt.body)); got != tt.want {
				t.Fatalf("classifyVisionProbeHTTPFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyVisionProbeResponseRecognizesTextOnlyModel(t *testing.T) {
	if got := classifyVisionProbeResponse("I'm a text-only model and cannot view images."); got != visionProbeUnsupported {
		t.Fatalf("classifyVisionProbeResponse() = %q, want unsupported", got)
	}
}

func TestTestMaclawLLM_DoesNotTreatWrongColourAsVisionConfirmation(t *testing.T) {
	// Some models misidentify the tiny red PNG as "yellow" — the probe should
	// still detect vision support because the model named a colour.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if hits == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Yellow"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	app := &App{}
	got, err := app.TestMaclawLLM(corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model", Protocol: "openai", AgentType: "test-agent"})
	if err != nil {
		t.Fatalf("TestMaclawLLM returned error: %v", err)
	}
	if got.SupportsVision {
		t.Fatal("TestMaclawLLM supports_vision = true, want false for wrong probe colour")
	}
}

func TestLooksLikeVisionResponse(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"Red", true},
		{"red", true},
		{"红色", true},
		{"Yellow", false},
		{"blue", false},
		{"The image is green.", false},
		{"The image is #FF0000.", true},
		{"RGB(255, 0, 0)", true},
		{"It appears crimson.", true},
		{"The colour is ruby red.", true},
		{"The image looks maroon.", true},
		{"I don't see any image", false},
		{"no image", false},
		{"No image attached", false},
		{"I can't see any image in your message.", false},
		{"没有图片", false},
		{"hello", false},
		{"", false},
		{"I don't see any image, but red is my favourite colour", false}, // negative overrides positive
	}
	for _, tc := range tests {
		got := looksLikeVisionResponse(tc.content)
		if got != tc.want {
			t.Errorf("looksLikeVisionResponse(%q) = %v, want %v", tc.content, got, tc.want)
		}
	}
}

func TestResolveProvidersPreservesCodeGenSSORuntimeConfig(t *testing.T) {
	saved := []corelib.MaclawLLMProvider{
		{
			Name:          codegenProviderName,
			URL:           "https://codegen.qianxin-inc.cn/api/v1/anthropic",
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
	if got.URL != "https://codegen.qianxin-inc.cn/api/v1" {
		t.Fatalf("CodeGen SSO URL = %q, want %q", got.URL, "https://codegen.qianxin-inc.cn/api/v1")
	}
	if got.Model != saved[0].Model {
		t.Fatalf("CodeGen SSO model = %q, want %q", got.Model, saved[0].Model)
	}
	if got.ContextLength != saved[0].ContextLength {
		t.Fatalf("CodeGen SSO context length = %d, want %d", got.ContextLength, saved[0].ContextLength)
	}
}

func TestGetMaclawLLMProviders_NormalizesCodeGenAutoPlaceholder(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     codegenProviderName,
			URL:      "https://codegen.qianxin-inc.cn/api/v1/anthropic",
			Key:      "token-123",
			Model:    "auto",
			Protocol: "anthropic",
			AuthType: "sso",
		}},
		MaclawLLMCurrentProvider: codegenProviderName,
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	if data.Current != codegenProviderName {
		t.Fatalf("Current = %q, want %q", data.Current, codegenProviderName)
	}
	if len(data.Providers) == 0 {
		t.Fatal("expected providers")
	}
	got := data.Providers[0]
	if got.Model != corelib.CodeGenDefaultModelID {
		t.Fatalf("CodeGen model = %q, want %q", got.Model, corelib.CodeGenDefaultModelID)
	}
	if got.Protocol != "openai" {
		t.Fatalf("CodeGen protocol = %q, want openai", got.Protocol)
	}
	if got.URL != "https://codegen.qianxin-inc.cn/api/v1" {
		t.Fatalf("CodeGen URL = %q, want trimmed OpenAI base URL", got.URL)
	}
}

func TestSaveMaclawLLMProviders_NormalizesCodeGenAutoPlaceholder(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{{
		Name:     codegenProviderName,
		URL:      "https://codegen.qianxin-inc.cn/api/v1",
		Key:      "token-123",
		Model:    "auto",
		Protocol: "anthropic",
		AuthType: "sso",
	}}
	if err := app.SaveMaclawLLMProviders(providers, codegenProviderName); err != nil {
		t.Fatalf("SaveMaclawLLMProviders() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := saved.MaclawLLMModel; got != corelib.CodeGenDefaultModelID {
		t.Fatalf("legacy model = %q, want %q", got, corelib.CodeGenDefaultModelID)
	}
	if got := saved.MaclawLLMProviders[0].Model; got != corelib.CodeGenDefaultModelID {
		t.Fatalf("provider model = %q, want %q", got, corelib.CodeGenDefaultModelID)
	}
	if got := saved.MaclawLLMProtocol; got != "openai" {
		t.Fatalf("legacy protocol = %q, want openai", got)
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
				ModelUrl:  codegenClaudeRemoteBaseURL,
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

	// Saving model choice must not rewrite native CLI configs; those are only
	// written when the user launches the programming tool from the UI.
	settingsPath := filepath.Join(tmpHome, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("settings.json should not be created by SaveCodeGenModelChoice, stat err = %v", err)
	}
	codexPath := filepath.Join(tmpHome, ".codex", "config.toml")
	if _, err := os.Stat(codexPath); !os.IsNotExist(err) {
		t.Fatalf("codex config.toml should not be created by SaveCodeGenModelChoice, stat err = %v", err)
	}
}

func TestInjectCodeGenModelIntoToolConfigsUsesFirstModelAsToolModelName(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		Claude: corelib.ToolConfig{
			CurrentModel: codegenProviderName,
			Models: []corelib.ModelConfig{{
				ModelName: codegenProviderName,
				ModelId:   "old-model",
				ModelUrl:  codegenClaudeRemoteBaseURL,
				ApiKey:    "old-token",
				WireApi:   "anthropic",
			}},
		},
		Codex: corelib.ToolConfig{
			CurrentModel: "Original",
			Models: []corelib.ModelConfig{
				{
					ModelName: "Original",
					ModelId:   "gpt-4.1",
					ModelUrl:  "https://api.openai.com/v1",
					ApiKey:    "openai-token",
					WireApi:   "responses",
				},
				{
					ModelName: codegenProviderName,
					ModelId:   "old-model",
					ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
					ApiKey:    "old-token",
					WireApi:   "responses",
				},
			},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.injectCodeGenModelIntoToolConfigs(oauth.CodeGenSSOResult{
		AccessToken: "token-123",
		BaseURL:     "https://codegen.qianxin-inc.cn/api/v1",
		ModelID:     "first-usable-model",
	})

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := saved.Claude.CurrentModel; got != codegenProviderName {
		t.Fatalf("Claude CurrentModel = %q, want %q", got, codegenProviderName)
	}
	if got := saved.Claude.Models[0].ModelName; got != codegenProviderName {
		t.Fatalf("Claude model_name = %q, want %q", got, codegenProviderName)
	}
	if got := saved.Claude.Models[0].ModelId; got != "first-usable-model" {
		t.Fatalf("Claude model_id = %q, want %q", got, "first-usable-model")
	}
	if got := saved.Codex.CurrentModel; got != codegenProviderName {
		t.Fatalf("Codex CurrentModel = %q, want %q", got, codegenProviderName)
	}
	if got := saved.Codex.Models[0].ModelName; got != "Original" {
		t.Fatalf("Codex first model_name = %q, want %q", got, "Original")
	}
	if got := saved.Codex.Models[1].ModelName; got != codegenProviderName {
		t.Fatalf("Codex model_name = %q, want %q", got, codegenProviderName)
	}
	if got := saved.Codex.Models[1].ModelId; got != "first-usable-model" {
		t.Fatalf("Codex model_id = %q, want %q", got, "first-usable-model")
	}
}

func TestUpsertCodeGenProviderStoresAvailableModelIDs(t *testing.T) {
	providers := []corelib.MaclawLLMProvider{{
		Name:      codegenProviderName,
		URL:       "https://old.example/api/v1",
		Key:       "old-token",
		Model:     "old-model",
		AgentType: corelib.CodeGenClientName,
		Models:    []string{"old-model"},
	}, {
		Name:  "Other",
		Model: "other-model",
	}}

	updated := upsertCodeGenProvider(providers, oauth.CodeGenSSOResult{
		AccessToken: "token-123",
		BaseURL:     "https://codegen.qianxin-inc.cn/api/v1",
		ModelID:     "qax-codegen/Auto",
		Models: []oauth.CodeGenModel{
			{ID: "qax-codegen/Auto"},
			{ID: " qax-codegen/Claude "},
			{ID: "qax-codegen/Auto"},
			{ID: ""},
		},
	})

	if got := providers[0].Key; got != "old-token" {
		t.Fatalf("original providers mutated, key = %q", got)
	}
	if len(updated) != len(providers) {
		t.Fatalf("len(updated) = %d, want %d", len(updated), len(providers))
	}
	gotModels := updated[0].Models
	wantModels := []string{"qax-codegen/Auto", "qax-codegen/Claude"}
	if !reflect.DeepEqual(gotModels, wantModels) {
		t.Fatalf("CodeGen Models = %#v, want %#v", gotModels, wantModels)
	}
	if got := updated[0].Model; got != "qax-codegen/Auto" {
		t.Fatalf("CodeGen Model = %q, want selected SSO model", got)
	}
	if updated[0].ConnectionTestPassed {
		t.Fatal("SSO authentication incorrectly marked the provider's model connection as tested")
	}
	if got := updated[1].Name; got != "Other" {
		t.Fatalf("non-CodeGen provider was not preserved: %+v", updated[1])
	}
}

func TestFetchCodeGenModelsUsesSavedProviderEndpointAndCachesModels(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var gotPath, gotAuth, gotClientName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotClientName = r.Header.Get(corelib.CodeGenClientNameHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Auto","name":"Auto"},{"id":"qax-codegen/Claude","name":"Claude"}]}`))
	}))
	defer srv.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:      codegenProviderName,
			URL:       srv.URL + "/api/v1",
			Key:       "token-123",
			Model:     "qax-codegen/Auto",
			Protocol:  "openai",
			AuthType:  "sso",
			AgentType: corelib.CodeGenClientName,
		}},
		MaclawLLMCurrentProvider: codegenProviderName,
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items, err := app.FetchCodeGenModels()
	if err != nil {
		t.Fatalf("FetchCodeGenModels() error = %v", err)
	}
	if gotPath != "/api/v1/models" {
		t.Fatalf("path = %q, want /api/v1/models", gotPath)
	}
	if gotAuth != "Bearer token-123" {
		t.Fatalf("Authorization = %q, want Bearer token-123", gotAuth)
	}
	if gotClientName != corelib.CodeGenClientName {
		t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, gotClientName, corelib.CodeGenClientName)
	}
	want := []CodeGenModelItem{{ID: "qax-codegen/Auto", Name: "Auto"}, {ID: "qax-codegen/Claude", Name: "Claude"}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %#v, want %#v", items, want)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := saved.MaclawLLMProviders[0].Models; !reflect.DeepEqual(got, []string{"qax-codegen/Auto", "qax-codegen/Claude"}) {
		t.Fatalf("saved provider models = %#v", got)
	}
}

func TestFetchCodeGenModelsFallsBackToCachedProviderModels(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`upstream unavailable`))
	}))
	defer srv.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     codegenProviderName,
			URL:      srv.URL + "/api/v1",
			Key:      "token-123",
			Model:    "qax-codegen/Auto",
			Protocol: "openai",
			AuthType: "sso",
			Models:   []string{"qax-codegen/Auto", " qax-codegen/Claude ", "qax-codegen/Auto"},
		}},
		MaclawLLMCurrentProvider: codegenProviderName,
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items, err := app.FetchCodeGenModels()
	if err != nil {
		t.Fatalf("FetchCodeGenModels() error = %v", err)
	}
	want := []CodeGenModelItem{{ID: "qax-codegen/Auto", Name: "qax-codegen/Auto"}, {ID: "qax-codegen/Claude", Name: "qax-codegen/Claude"}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %#v, want cached %#v", items, want)
	}
}

func TestSaveCodeGenModelChoiceRenamesExistingCodeGenModelEntry(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     codegenProviderName,
			URL:      "https://codegen.qianxin-inc.cn/api/v1",
			Key:      "token-123",
			Model:    "first-model",
			Protocol: "openai",
			AuthType: "sso",
		}},
		MaclawLLMCurrentProvider: codegenProviderName,
		Codex: corelib.ToolConfig{
			CurrentModel: "first-model",
			Models: []corelib.ModelConfig{
				{
					ModelName: "first-model",
					ModelId:   "first-model",
					ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
					ApiKey:    "token-123",
					WireApi:   "responses",
				},
				{
					ModelName: codegenProviderName,
					ModelId:   "legacy-model",
					ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
					ApiKey:    "token-123",
					WireApi:   "responses",
				},
			},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.SaveCodeGenModelChoice("second-model", ""); err != nil {
		t.Fatalf("SaveCodeGenModelChoice() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := saved.Codex.CurrentModel; got != codegenProviderName {
		t.Fatalf("Codex CurrentModel = %q, want %q", got, codegenProviderName)
	}
	// Find the CodeGen entry (default "Original" and other builtin models may also be present)
	var codexCodeGen *corelib.ModelConfig
	codegenCount := 0
	for i := range saved.Codex.Models {
		if saved.Codex.Models[i].ModelName == codegenProviderName {
			codexCodeGen = &saved.Codex.Models[i]
			codegenCount++
		}
	}
	if codexCodeGen == nil {
		t.Fatalf("Codex CodeGen entry not found in %+v", saved.Codex.Models)
	}
	if codexCodeGen.ModelId != "second-model" {
		t.Fatalf("Codex model_id = %q, want %q", codexCodeGen.ModelId, "second-model")
	}
	if codegenCount != 1 {
		t.Fatalf("Codex CodeGen entries should be deduplicated to 1, got %d in %+v", codegenCount, saved.Codex.Models)
	}
}

func TestUpdateCodeGenToolAPIKeyMatchesRenamedCodeGenEntryWithoutSwitchingCurrent(t *testing.T) {
	tc := corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{
				ModelName: "Original",
				ModelId:   "gpt-4.1",
				ModelUrl:  "https://api.openai.com/v1",
				ApiKey:    "openai-token",
				WireApi:   "responses",
			},
			{
				ModelName: "first-usable-model",
				ModelId:   "first-usable-model",
				ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
				ApiKey:    "old-token",
				WireApi:   "responses",
			},
		},
	}

	changed := updateCodeGenToolAPIKey(&tc, corelib.ModelConfig{
		ModelName: "first-usable-model",
		ModelId:   "first-usable-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "new-token",
		WireApi:   "responses",
	})

	if !changed {
		t.Fatal("updateCodeGenToolAPIKey() changed = false, want true")
	}
	if got := tc.CurrentModel; got != "Original" {
		t.Fatalf("CurrentModel = %q, want %q", got, "Original")
	}
	if got := tc.Models[0].ApiKey; got != "openai-token" {
		t.Fatalf("custom ApiKey = %q, want %q", got, "openai-token")
	}
	if got := tc.Models[1].ApiKey; got != "new-token" {
		t.Fatalf("CodeGen ApiKey = %q, want %q", got, "new-token")
	}
}

func TestUpdateCodeGenToolAPIKeyPropagatesClientName(t *testing.T) {
	tc := corelib.ToolConfig{Models: []corelib.ModelConfig{{
		ModelName: "first-usable-model",
		ModelId:   "first-usable-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "old-token",
		WireApi:   "responses",
		AgentType: "old-agent",
	}}}

	changed := updateCodeGenToolAPIKey(&tc, corelib.ModelConfig{
		ModelName: "first-usable-model",
		ModelId:   "first-usable-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "new-token",
		WireApi:   "responses",
		AgentType: "custom-agent",
	})

	if !changed {
		t.Fatal("updateCodeGenToolAPIKey() changed = false, want true")
	}
	if got := tc.Models[0].AgentType; got != "custom-agent" {
		t.Fatalf("AgentType = %q, want custom-agent", got)
	}
}

func TestMaclawLLMTokenUsageIgnoresRemoteToolProviders(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	app.AccumulateLLMTokenUsageWithCache("codex:gpt-5.4", 1200, 80, 768, 128)
	app.AccumulateLLMTokenUsageWithCache("GLM (智谱)", 100, 20, 40, 12)

	all := app.GetAllLLMTokenUsage()
	if _, ok := all["codex:gpt-5.4"]; ok {
		t.Fatalf("remote tool usage should not be exposed as Maclaw usage: %+v", all)
	}
	stat := all["GLM (智谱)"]
	if stat == nil || stat.InputTokens != 100 || stat.OutputTokens != 20 || stat.CachedInputTokens != 40 || stat.CacheWriteTokens != 12 {
		t.Fatalf("Maclaw provider usage = %+v", stat)
	}
	if got := app.GetLLMTokenUsage("codex:gpt-5.4"); got.TotalTokens != 0 || got.CachedInputTokens != 0 {
		t.Fatalf("remote tool provider should read as zero usage, got %+v", got)
	}
}

func TestMaclawLLMTokenUsageIgnoresBlankProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	app.AccumulateLLMTokenUsageWithCache("  ", 100, 20, 0, 0)

	if all := app.GetAllLLMTokenUsage(); len(all) != 0 {
		t.Fatalf("blank provider usage should be ignored, got %+v", all)
	}
}

func TestMaclawLLMUsageProviderNameFallsBackToCurrent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "CurrentProvider", URL: "https://example.test/v1", Model: "model"}},
		MaclawLLMCurrentProvider: "CurrentProvider",
	}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	if got := maclawLLMUsageProviderName(app, corelib.MaclawLLMConfig{}); got != "CurrentProvider" {
		t.Fatalf("provider name = %q, want CurrentProvider", got)
	}
}

func TestMaclawLLMTokenUsageCalculatesCost(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:                     "PricedProvider",
			URL:                      "https://example.test/v1",
			Model:                    "model",
			InputPricePerMTokensRMB:  2,
			OutputPricePerMTokensRMB: 4,
		}},
		MaclawLLMCurrentProvider: "PricedProvider",
	}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	app.AccumulateLLMTokenUsageWithCache("PricedProvider", 1_000_000, 500_000, 0, 0)

	stat := app.GetLLMTokenUsage("PricedProvider")
	if stat.InputPricePerMTokensRMB != 2 || stat.OutputPricePerMTokensRMB != 4 {
		t.Fatalf("prices = input %.2f output %.2f", stat.InputPricePerMTokensRMB, stat.OutputPricePerMTokensRMB)
	}
	if stat.InputCostRMB != 2 || stat.OutputCostRMB != 2 || stat.TotalCostRMB != 4 {
		t.Fatalf("cost = input %.2f output %.2f total %.2f", stat.InputCostRMB, stat.OutputCostRMB, stat.TotalCostRMB)
	}
}

func TestMaclawLLMProfileTokenUsageSeparatesProfilesAndFinalModels(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{MaclawLLMProviders: []corelib.MaclawLLMProvider{{
		ID: "provider", Name: "Provider", InputPricePerMTokensRMB: 2, OutputPricePerMTokensRMB: 4,
	}}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.AccumulateLLMProfileTokenUsageWithCache(corelib.MaclawLLMConfig{
		Profile: "assistant", ProviderID: "provider", ProviderName: "Provider", Model: "assistant-model", RouteSource: "base",
	}, 100, 20, 0, 0)
	app.AccumulateLLMProfileTokenUsageWithCache(corelib.MaclawLLMConfig{
		Profile: "coding", ProviderID: "provider", ProviderName: "Provider", Model: "coding-route-model", RouteSource: "vision",
	}, 200, 40, 0, 0)
	app.flushPendingTokenUsage()
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(saved.LLMProfileTokenUsage) != 2 {
		t.Fatalf("profile usage = %#v, want two attributed rows", saved.LLMProfileTokenUsage)
	}
	assistant := saved.LLMProfileTokenUsage["profile:assistant|provider|assistant-model"]
	if assistant == nil || assistant.Profile != "assistant" || assistant.FinalModel != "assistant-model" || assistant.TotalTokens != 120 {
		t.Fatalf("assistant profile usage = %#v", assistant)
	}
	coding := saved.LLMProfileTokenUsage["profile:coding|provider|coding-route-model"]
	if coding == nil || coding.Profile != "coding" || coding.FinalModel != "coding-route-model" || coding.RouteSource != "vision" || coding.TotalTokens != 240 {
		t.Fatalf("coding profile usage = %#v", coding)
	}
	if len(saved.LLMTokenUsage) != 0 {
		t.Fatalf("profile-only recorder should not fabricate legacy attribution: %#v", saved.LLMTokenUsage)
	}
	all := app.GetAllLLMProfileTokenUsage()
	if len(all) != 2 || all["profile:coding|provider|coding-route-model"] == nil {
		t.Fatalf("profile usage API = %#v", all)
	}
}

func TestRecordLLMUsageSnapshotKeepsCapturedCodingProfile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{MaclawLLMProviders: []corelib.MaclawLLMProvider{
		{ID: "assistant", Name: "Assistant", URL: "https://assistant.example/v1", Key: "assistant-key", Model: "assistant-model"},
		{ID: "coding", Name: "Coding", URL: "https://coding.example/v1", Key: "coding-key", Model: "coding-model"},
	}, MaclawLLMCurrentProvider: "Assistant", MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
		Version:   maclawLLMProfilesVersion,
		Assistant: corelib.MaclawLLMProfile{ProviderID: "assistant", Model: "assistant-model"},
		Coding:    corelib.MaclawLLMProfile{ProviderID: "coding", Model: "coding-model"},
	}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := &IMMessageHandler{app: app}
	// This response belongs to an in-flight coding request. The persisted
	// assistant selection is deliberately different, proving the recorder must
	// use the request snapshot rather than resolving current settings again.
	codingCfg := corelib.MaclawLLMConfig{
		Profile: "coding", ProviderID: "coding", ProviderName: "Coding", Model: "coding-model", RouteSource: "base",
	}
	h.recordLLMUsageSnapshot("coding_round", codingCfg, &llm.Response{Usage: &llm.Usage{PromptTokens: 12, CompletionTokens: 4}}, nil)
	app.flushPendingTokenUsage()

	usage := app.GetAllLLMProfileTokenUsage()
	if usage["profile:coding|coding|coding-model"] == nil {
		t.Fatalf("coding usage missing or attributed to assistant: %#v", usage)
	}
	if usage["profile:assistant|assistant|assistant-model"] != nil {
		t.Fatalf("coding request was attributed to assistant: %#v", usage)
	}
}

func TestMaclawLLMTokenUsagePatchesWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	app.AccumulateLLMTokenUsageWithCache("MiniMax", 100, 20, 10, 5)
	app.AccumulateLLMLocalCacheRequest("MiniMax", true)
	if err := app.ResetLLMTokenUsage("Other"); err != nil {
		t.Fatalf("ResetLLMTokenUsage(other) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if saved.RemoteEmail != "owner@example.com" || !saved.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by token usage updates: %#v", saved)
	}
	stat := saved.LLMTokenUsage["MiniMax"]
	if stat == nil || stat.InputTokens != 100 || stat.OutputTokens != 20 || stat.LocalCacheRequests != 1 || stat.LocalCacheHits != 1 {
		t.Fatalf("token usage not patched: %+v", stat)
	}
}

func TestGetAllLLMTokenUsageFiltersPersistedRemoteToolPollution(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{LLMTokenUsage: map[string]*corelib.TokenUsageStat{
		"codex:gpt-5.4": {InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280},
		"remote:claude": {InputTokens: 200, OutputTokens: 20, TotalTokens: 220},
		"MiniMax":       {InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	all := app.GetAllLLMTokenUsage()
	if len(all) != 1 || all["MiniMax"] == nil {
		t.Fatalf("expected only Maclaw provider usage, got %+v", all)
	}
}

func TestRemoteToolTokenUsageProviderMatchingIsCaseAndSpaceInsensitive(t *testing.T) {
	for _, provider := range []string{
		" Codex:gpt-5.4 ",
		"CLAUDE:sonnet",
		"remote:opencode",
	} {
		if !isRemoteToolTokenUsageProvider(provider) {
			t.Fatalf("expected %q to be treated as remote-tool diagnostic usage", provider)
		}
	}
	for _, provider := range []string{"", "GLM (智谱)", "MaClaw 官方", "custom-codex-provider"} {
		if isRemoteToolTokenUsageProvider(provider) {
			t.Fatalf("expected %q to remain normal Maclaw provider usage", provider)
		}
	}
}

func TestGetAllLLMTokenUsageReturnsCopies(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{LLMTokenUsage: map[string]*corelib.TokenUsageStat{
		"MiniMax": {InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	all := app.GetAllLLMTokenUsage()
	all["MiniMax"].InputTokens = 999

	got := app.GetLLMTokenUsage("MiniMax")
	if got.InputTokens != 10 {
		t.Fatalf("GetAllLLMTokenUsage exposed mutable config stat, got %+v", got)
	}
}

func TestGetLLMTokenUsageReturnsCopy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{LLMTokenUsage: map[string]*corelib.TokenUsageStat{
		"MiniMax": {InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}}); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	stat := app.GetLLMTokenUsage("MiniMax")
	stat.InputTokens = 999

	got := app.GetLLMTokenUsage("MiniMax")
	if got.InputTokens != 10 {
		t.Fatalf("GetLLMTokenUsage exposed mutable config stat, got %+v", got)
	}
}

func TestUpdateCodeGenToolAPIKeySkipsCustomSameURLProvider(t *testing.T) {
	tc := corelib.ToolConfig{
		CurrentModel: "Custom CodeGen Mirror",
		Models: []corelib.ModelConfig{
			{
				ModelName: "Custom CodeGen Mirror",
				ModelId:   "custom-model",
				ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
				ApiKey:    "custom-token",
				WireApi:   "responses",
				IsCustom:  true,
			},
			{
				ModelName: "first-usable-model",
				ModelId:   "first-usable-model",
				ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
				ApiKey:    "old-token",
				WireApi:   "responses",
			},
		},
	}

	changed := updateCodeGenToolAPIKey(&tc, corelib.ModelConfig{
		ModelName: "first-usable-model",
		ModelId:   "first-usable-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "new-token",
		WireApi:   "responses",
	})

	if !changed {
		t.Fatal("updateCodeGenToolAPIKey() changed = false, want true")
	}
	if got := tc.Models[0].ApiKey; got != "custom-token" {
		t.Fatalf("custom ApiKey = %q, want %q", got, "custom-token")
	}
	if got := tc.Models[1].ApiKey; got != "new-token" {
		t.Fatalf("CodeGen ApiKey = %q, want %q", got, "new-token")
	}
}

func TestUpdateCodeGenToolAPIKeyDoesNotMatchByModelNameOnly(t *testing.T) {
	tc := corelib.ToolConfig{
		CurrentModel: "first-usable-model",
		Models: []corelib.ModelConfig{
			{
				ModelName: "first-usable-model",
				ModelId:   "first-usable-model",
				ModelUrl:  "https://example.invalid/v1",
				ApiKey:    "custom-token",
				WireApi:   "responses",
			},
		},
	}

	changed := updateCodeGenToolAPIKey(&tc, corelib.ModelConfig{
		ModelName: "first-usable-model",
		ModelId:   "first-usable-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "new-token",
		WireApi:   "responses",
	})

	if changed {
		t.Fatal("updateCodeGenToolAPIKey() changed = true, want false")
	}
	if got := tc.Models[0].ApiKey; got != "custom-token" {
		t.Fatalf("ApiKey = %q, want %q", got, "custom-token")
	}
}

func TestEnsureCodeGenToolModelAvailableFallsBackToFirstModel(t *testing.T) {
	tc := corelib.ToolConfig{
		CurrentModel: "missing-model",
		Models: []corelib.ModelConfig{
			{
				ModelName: "missing-model",
				ModelId:   "missing-model",
				ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
				ApiKey:    "old-token",
				WireApi:   "responses",
			},
		},
	}

	changed := ensureCodeGenToolModelAvailable(&tc, corelib.ModelConfig{
		ModelName: "first-available-model",
		ModelId:   "first-available-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "token-123",
		WireApi:   "responses",
	}, map[string]bool{"first-available-model": true})

	if !changed {
		t.Fatal("ensureCodeGenToolModelAvailable() changed = false, want true")
	}
	if got := tc.CurrentModel; got != "first-available-model" {
		t.Fatalf("CurrentModel = %q, want %q", got, "first-available-model")
	}
	if got := tc.Models[0].ModelName; got != "first-available-model" {
		t.Fatalf("ModelName = %q, want %q", got, "first-available-model")
	}
	if got := tc.Models[0].ModelId; got != "first-available-model" {
		t.Fatalf("ModelId = %q, want %q", got, "first-available-model")
	}
}

func TestEnsureCodeGenToolModelAvailablePropagatesClientName(t *testing.T) {
	tc := corelib.ToolConfig{Models: []corelib.ModelConfig{{
		ModelName: "missing-model",
		ModelId:   "missing-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "old-token",
		WireApi:   "responses",
		AgentType: "old-agent",
	}}}

	changed := ensureCodeGenToolModelAvailable(&tc, corelib.ModelConfig{
		ModelName: "first-available-model",
		ModelId:   "first-available-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "token-123",
		WireApi:   "responses",
		AgentType: "custom-agent",
	}, map[string]bool{"first-available-model": true})

	if !changed {
		t.Fatal("ensureCodeGenToolModelAvailable() changed = false, want true")
	}
	if got := tc.Models[0].AgentType; got != "custom-agent" {
		t.Fatalf("AgentType = %q, want custom-agent", got)
	}
}

func TestEnsureCodeGenToolModelAvailableDoesNotSwitchUnrelatedCurrentModel(t *testing.T) {
	tc := corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{
				ModelName: "Original",
				ModelId:   "gpt-4.1",
				ModelUrl:  "https://api.openai.com/v1",
				ApiKey:    "openai-token",
				WireApi:   "responses",
			},
			{
				ModelName: "missing-model",
				ModelId:   "missing-model",
				ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
				ApiKey:    "old-token",
				WireApi:   "responses",
			},
		},
	}

	changed := ensureCodeGenToolModelAvailable(&tc, corelib.ModelConfig{
		ModelName: "first-available-model",
		ModelId:   "first-available-model",
		ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
		ApiKey:    "token-123",
		WireApi:   "responses",
	}, map[string]bool{"first-available-model": true})

	if !changed {
		t.Fatal("ensureCodeGenToolModelAvailable() changed = false, want true")
	}
	if got := tc.CurrentModel; got != "Original" {
		t.Fatalf("CurrentModel = %q, want %q", got, "Original")
	}
	if got := tc.Models[1].ModelName; got != "first-available-model" {
		t.Fatalf("CodeGen ModelName = %q, want %q", got, "first-available-model")
	}
}

func TestProviderModelItemFromEntrySkipsExplicitlyUnavailableModels(t *testing.T) {
	unavailable := false

	if _, ok := providerModelItemFromEntry(providerModelEntry{ID: "disabled-model", Disabled: true}); ok {
		t.Fatal("disabled model should be skipped")
	}
	if _, ok := providerModelItemFromEntry(providerModelEntry{ID: "unavailable-model", Available: &unavailable}); ok {
		t.Fatal("unavailable model should be skipped")
	}
	if _, ok := providerModelItemFromEntry(providerModelEntry{ID: "inactive-model", Status: "inactive"}); ok {
		t.Fatal("inactive model should be skipped")
	}
}

func TestProviderModelItemFromEntryUsesDisplayNameWhenAvailable(t *testing.T) {
	item, ok := providerModelItemFromEntry(providerModelEntry{
		ID:          "model-id",
		Name:        "Model Name",
		DisplayName: "Display Name",
		Status:      "available",
	})
	if !ok {
		t.Fatal("available model should be included")
	}
	if item.ID != "model-id" {
		t.Fatalf("ID = %q, want %q", item.ID, "model-id")
	}
	if item.Name != "Display Name" {
		t.Fatalf("Name = %q, want %q", item.Name, "Display Name")
	}
}

func TestSyncCodeGenAPIKeysToToolConfigsDoesNotWriteNativeConfigs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		Claude: corelib.ToolConfig{Models: []corelib.ModelConfig{{
			ModelName: codegenProviderName,
			ModelId:   "claude-model",
			ModelUrl:  codegenClaudeRemoteBaseURL,
			ApiKey:    "old-token",
			WireApi:   "anthropic",
		}}},
		Codex: corelib.ToolConfig{Models: []corelib.ModelConfig{{
			ModelName: codegenProviderName,
			ModelId:   "maclaw-model",
			ModelUrl:  "https://codegen.qianxin-inc.cn/api/v1",
			ApiKey:    "old-token",
			WireApi:   "responses",
		}}},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.syncCodeGenAPIKeysToToolConfigs(corelib.MaclawLLMProvider{
		Name:      codegenProviderName,
		URL:       "https://codegen.qianxin-inc.cn/api/v1",
		Key:       "new-token",
		Model:     "maclaw-model",
		AgentType: corelib.CodeGenClientName,
		AuthType:  "sso",
	})

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got := saved.Codex.Models[0].ApiKey; got != "new-token" {
		t.Fatalf("Codex api key = %q, want new-token", got)
	}
	if got := saved.Claude.Models[0].ApiKey; got != "new-token" {
		t.Fatalf("Claude api key = %q, want new-token", got)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("token sync must not write ~/.codex/config.toml, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("token sync must not write ~/.claude/settings.json, stat err = %v", err)
	}
}

func TestSaveCodeGenModelChoiceDoesNotRewriteNativeClaudeSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		Claude: corelib.ToolConfig{
			CurrentModel: "GLM",
			Models: []corelib.ModelConfig{
				{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/anthropic", ApiKey: "glm-token", WireApi: "anthropic"},
				{ModelName: codegenProviderName, ModelId: "qax-codegen/Auto", ModelUrl: codegenClaudeRemoteBaseURL, ApiKey: "token-123", WireApi: "anthropic"},
			},
		},
		Codex: corelib.ToolConfig{CurrentModel: "Original", Models: []corelib.ModelConfig{{
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
	if got := saved.Codex.CurrentModel; got != codegenProviderName {
		t.Fatalf("Codex CurrentModel = %q, want %q", got, codegenProviderName)
	}

	// Existing native Claude settings must stay untouched until LaunchTool.
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
	if got := env["ANTHROPIC_MODEL"]; got != "glm-4.7" {
		t.Fatalf("ANTHROPIC_MODEL = %v, want seeded %q", got, "glm-4.7")
	}
	if got := env["ANTHROPIC_BASE_URL"]; got != "https://open.bigmodel.cn/api/anthropic" {
		t.Fatalf("ANTHROPIC_BASE_URL = %v, want seeded GLM URL", got)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("codex config.toml should not be written by SaveCodeGenModelChoice, stat err = %v", err)
	}
}

func TestCodeGenAnthropicBaseURLUsesRemoteEndpoint(t *testing.T) {
	if got := codegenAnthropicBaseURL("https://codegen.qianxin-inc.cn/api/v1"); got != codegenClaudeRemoteBaseURL {
		t.Fatalf("codegenAnthropicBaseURL() = %q, want %q", got, codegenClaudeRemoteBaseURL)
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
	if first.URL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("OpenAI URL = %q, want %q", first.URL, "https://chatgpt.com/backend-api/codex")
	}
	if first.Model != oauth.CodexSubscriptionDefaultModel {
		t.Errorf("OpenAI Model = %q, want %q", first.Model, oauth.CodexSubscriptionDefaultModel)
	}
	if first.AuthType != "oauth" {
		t.Errorf("OpenAI AuthType = %q, want %q", first.AuthType, "oauth")
	}
	if first.ContextLength != 110000 {
		t.Errorf("OpenAI ContextLength = %d, want %d", first.ContextLength, 110000)
	}
	if first.TimeoutSec != corelib.DefaultLLMTimeoutSec {
		t.Errorf("OpenAI TimeoutSec = %d, want %d", first.TimeoutSec, corelib.DefaultLLMTimeoutSec)
	}

	deepseek, ok := findProviderByName(providers, "DeepSeek")
	if !ok {
		t.Fatalf("providers missing DeepSeek: %+v", providers)
	}
	if deepseek.URL != "https://api.deepseek.com/v1" {
		t.Errorf("DeepSeek URL = %q, want %q", deepseek.URL, "https://api.deepseek.com/v1")
	}
	if deepseek.Model != "deepseek-v4-flash" {
		t.Errorf("DeepSeek Model = %q, want %q", deepseek.Model, "deepseek-v4-flash")
	}

	xaiGrok, ok := findProviderByName(providers, "xAI-Grok")
	if !ok {
		t.Fatalf("providers missing xAI-Grok: %+v", providers)
	}
	if xaiGrok.URL != "https://api.x.ai/v1" {
		t.Errorf("xAI-Grok URL = %q, want %q", xaiGrok.URL, "https://api.x.ai/v1")
	}
	if xaiGrok.Model != "grok-4.5" {
		t.Errorf("xAI-Grok Model = %q, want %q", xaiGrok.Model, "grok-4.5")
	}
	if xaiGrok.Protocol != "openai" {
		t.Errorf("xAI-Grok Protocol = %q, want %q", xaiGrok.Protocol, "openai")
	}
	if xaiGrok.AuthType != "oauth" {
		t.Errorf("xAI-Grok AuthType = %q, want oauth", xaiGrok.AuthType)
	}
	if xaiGrok.WireAPI != "responses" {
		t.Errorf("xAI-Grok WireAPI = %q, want responses", xaiGrok.WireAPI)
	}
	if xaiGrok.ContextLength != 400000 {
		t.Errorf("xAI-Grok ContextLength = %d, want %d", xaiGrok.ContextLength, 400000)
	}

	zhipuCoding, ok := findProviderByName(providers, "智谱编程")
	if !ok {
		t.Fatalf("providers missing 智谱编程: %+v", providers)
	}
	if zhipuCoding.URL != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("智谱编程 URL = %q, want %q", zhipuCoding.URL, "https://open.bigmodel.cn/api/anthropic")
	}
	if zhipuCoding.Model != "GLM-5.2" {
		t.Errorf("智谱编程 Model = %q, want %q", zhipuCoding.Model, "GLM-5.2")
	}
	if zhipuCoding.Protocol != "anthropic" {
		t.Errorf("智谱编程 Protocol = %q, want %q", zhipuCoding.Protocol, "anthropic")
	}
	if zhipuCoding.AgentType != "claude code 2.0" {
		t.Errorf("智谱编程 AgentType = %q, want %q", zhipuCoding.AgentType, "claude code 2.0")
	}

	tokenPlan, ok := findProviderByName(providers, volcengineAgentPlanProviderName)
	if !ok {
		t.Fatalf("providers missing %s: %+v", volcengineAgentPlanProviderName, providers)
	}
	if tokenPlan.URL != "https://ark.cn-beijing.volces.com/api/plan/v3" {
		t.Errorf("火山引擎 Agent Plan URL = %q, want %q", tokenPlan.URL, "https://ark.cn-beijing.volces.com/api/plan/v3")
	}
	if tokenPlan.Model != "glm-5.2" {
		t.Errorf("火山引擎 Agent Plan Model = %q, want %q", tokenPlan.Model, "glm-5.2")
	}
	if tokenPlan.Protocol != "openai" {
		t.Errorf("火山引擎 Agent Plan Protocol = %q, want %q", tokenPlan.Protocol, "openai")
	}
	if tokenPlan.WireAPI != "responses" {
		t.Errorf("火山引擎 Agent Plan WireAPI = %q, want %q", tokenPlan.WireAPI, "responses")
	}

	expectedNames := []string{"OpenAI", "Anthropic", "GitHub Copilot", "DeepSeek", "xAI-Grok", "智谱编程", "MiniMax", "Kimi", volcengineAgentPlanProviderName, "讯飞星辰", "Custom1", "Custom2"}
	if len(providers) < len(expectedNames) {
		t.Fatalf("provider count = %d, want >= %d", len(providers), len(expectedNames))
	}
	for i, want := range expectedNames {
		if providers[i].Name != want {
			t.Errorf("providers[%d].Name = %q, want %q", i, providers[i].Name, want)
		}
	}

	kimi, ok := findProviderByName(providers, "Kimi")
	if !ok {
		t.Fatalf("providers missing Kimi: %+v", providers)
	}
	if got := kimi.AgentType; got != "claude code 2.0" {
		t.Errorf("Kimi AgentType = %q, want %q", got, "claude code 2.0")
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

func TestGetMaclawLLMProviders_BackfillsVolcengineAgentPlanProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "OpenAI",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "OpenAI", URL: "https://chatgpt.com/backend-api/codex", Model: "gpt-5.4", AuthType: "oauth"},
			{Name: "Custom1", IsCustom: true},
			{Name: "Custom2", IsCustom: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	provider, ok := findProviderByName(data.Providers, volcengineAgentPlanProviderName)
	if !ok {
		t.Fatalf("providers missing 火山引擎 Agent Plan card: %+v", data.Providers)
	}
	if provider.URL != "https://ark.cn-beijing.volces.com/api/plan/v3" {
		t.Fatalf("火山引擎 Agent Plan URL = %q, want %q", provider.URL, "https://ark.cn-beijing.volces.com/api/plan/v3")
	}
	if provider.Model != "glm-5.2" || provider.Protocol != "openai" || provider.WireAPI != "responses" {
		t.Fatalf("火山引擎 Agent Plan config = model %q protocol %q wire %q, want glm-5.2/openai/responses", provider.Model, provider.Protocol, provider.WireAPI)
	}
	if provider.IsCustom {
		t.Fatal("火山引擎 Agent Plan IsCustom = true, want false")
	}
}

func TestGetMaclawLLMProviders_MigratesXAIGrokAPIKeyConfigToOAuth(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: "xAI-Grok",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "xAI-Grok", URL: "https://legacy.x.ai/v1", Key: "xai-api-key", Model: "legacy-grok"},
			{Name: "Custom1", IsCustom: true},
			{Name: "Custom2", IsCustom: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	provider, ok := findProviderByName(data.Providers, "xAI-Grok")
	if !ok {
		t.Fatalf("providers missing xAI-Grok: %+v", data.Providers)
	}
	if provider.AuthType != "oauth" || provider.Protocol != "openai" || provider.WireAPI != "responses" {
		t.Fatalf("xAI-Grok auth config = auth %q protocol %q wire %q, want oauth/openai/responses", provider.AuthType, provider.Protocol, provider.WireAPI)
	}
	if provider.URL != "https://api.x.ai/v1" {
		t.Fatalf("xAI-Grok URL = %q, want current endpoint", provider.URL)
	}
	if provider.Model != "grok-4.5" {
		t.Fatalf("xAI-Grok Model = %q, want current default grok-4.5", provider.Model)
	}
	if provider.Key != "" || provider.OAuthAccessToken != "" || provider.RefreshToken != "" || provider.TokenExpiresAt != 0 {
		t.Fatalf("legacy xAI API credentials were retained: %+v", provider)
	}

	if err := app.SaveMaclawLLMProviders(data.Providers, data.Current); err != nil {
		t.Fatalf("SaveMaclawLLMProviders() error = %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	persisted, ok := findProviderByName(saved.MaclawLLMProviders, "xAI-Grok")
	if !ok || persisted.Key != "" || persisted.AuthType != "oauth" {
		t.Fatalf("persisted xAI migration = %+v", persisted)
	}
}

func TestNormalizeXAIProviderMigratesFormerGrokBuildDefault(t *testing.T) {
	defaults, ok := findProviderByName(defaultMaclawLLMProviders(), "xAI-Grok")
	if !ok {
		t.Fatal("default xAI-Grok provider not found")
	}
	provider := normalizeXAIProvider(corelib.MaclawLLMProvider{
		Name: "xAI-Grok", URL: "https://api.x.ai/v1", Model: "grok-build", ContextLength: 256000,
		Protocol: "openai", AuthType: "oauth", WireAPI: "responses",
	}, defaults)
	if provider.Model != "grok-4.5" {
		t.Fatalf("legacy grok-build default was not migrated: %q", provider.Model)
	}
	if provider.ContextLength != 400000 {
		t.Fatalf("legacy 256K context window was not migrated: %d", provider.ContextLength)
	}

	provider.Model = "grok-4.1-fast"
	provider = normalizeXAIProvider(provider, defaults)
	if provider.Model != "grok-4.1-fast" {
		t.Fatalf("explicit OAuth model selection was overwritten: %q", provider.Model)
	}
}

func TestGetMaclawLLMProviders_MigratesVolcengineTokenPlanToAgentPlan(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: legacyVolcengineTokenPlanProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: legacyVolcengineTokenPlanProviderName, URL: "https://ark.cn-beijing.volces.com/api/plan", Model: "Auto", Protocol: "anthropic", AgentType: "claude code 2.0"},
			{Name: "Custom1", IsCustom: true},
			{Name: "Custom2", IsCustom: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	provider, ok := findProviderByName(data.Providers, volcengineAgentPlanProviderName)
	if !ok {
		t.Fatalf("providers missing 火山引擎 Agent Plan card: %+v", data.Providers)
	}
	if provider.URL != "https://ark.cn-beijing.volces.com/api/plan/v3" {
		t.Fatalf("火山引擎 Agent Plan URL = %q, want %q", provider.URL, "https://ark.cn-beijing.volces.com/api/plan/v3")
	}
	if provider.Protocol != "openai" || provider.WireAPI != "responses" || provider.AgentType != "" {
		t.Fatalf("火山引擎 Agent Plan migrated config = protocol %q wire %q agent %q, want openai/responses/empty", provider.Protocol, provider.WireAPI, provider.AgentType)
	}
}

func TestGetMaclawLLMProviders_MigratesLegacyVolcengineTokenPlanAnthropicNameToAgentPlan(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: legacyVolcengineTokenPlanAnthropicProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: legacyVolcengineTokenPlanAnthropicProviderName, URL: "https://ark.cn-beijing.volces.com/api/plan", Model: "Auto", Protocol: "anthropic", AgentType: "claude code 2.0"},
			{Name: "Custom1", IsCustom: true},
			{Name: "Custom2", IsCustom: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	if data.Current != volcengineAgentPlanProviderName {
		t.Fatalf("Current = %q, want %q", data.Current, volcengineAgentPlanProviderName)
	}
	if _, ok := findProviderByName(data.Providers, legacyVolcengineTokenPlanAnthropicProviderName); ok {
		t.Fatalf("providers still contain legacy Anthropic TokenPlan name: %+v", data.Providers)
	}
	provider, ok := findProviderByName(data.Providers, volcengineAgentPlanProviderName)
	if !ok {
		t.Fatalf("providers missing canonical 火山引擎 Agent Plan card: %+v", data.Providers)
	}
	if provider.URL != "https://ark.cn-beijing.volces.com/api/plan/v3" || provider.Protocol != "openai" || provider.WireAPI != "responses" {
		t.Fatalf("canonical 火山引擎 Agent Plan config = url %q protocol %q wire %q", provider.URL, provider.Protocol, provider.WireAPI)
	}
}

func TestGetMaclawLLMProviders_DedupesLegacyAndCanonicalVolcengineTokenPlanNames(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: legacyVolcengineTokenPlanAnthropicProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: legacyVolcengineTokenPlanAnthropicProviderName, URL: "https://old.example.com/api/plan", Model: "Old", Protocol: "anthropic", AgentType: "claude code 2.0"},
			{Name: volcengineAgentPlanProviderName, URL: "https://ark.cn-beijing.volces.com/api/plan/v3", Model: "glm-5.2", Protocol: "openai", WireAPI: "responses"},
			{Name: "Custom1", IsCustom: true},
			{Name: "Custom2", IsCustom: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	count := 0
	for _, provider := range data.Providers {
		if provider.Name == volcengineAgentPlanProviderName {
			count++
			if provider.URL != "https://ark.cn-beijing.volces.com/api/plan/v3" {
				t.Fatalf("canonical 火山引擎 Agent Plan URL = %q, want %q", provider.URL, "https://ark.cn-beijing.volces.com/api/plan/v3")
			}
		}
		if provider.Name == legacyVolcengineTokenPlanAnthropicProviderName {
			t.Fatalf("providers still contain legacy Anthropic TokenPlan name: %+v", data.Providers)
		}
	}
	if count != 1 {
		t.Fatalf("canonical 火山引擎 Agent Plan count = %d, want 1; providers=%+v", count, data.Providers)
	}
	if data.Current != volcengineAgentPlanProviderName {
		t.Fatalf("Current = %q, want %q", data.Current, volcengineAgentPlanProviderName)
	}
}

func TestSaveMaclawLLMProviders_CanonicalizesLegacyVolcengineTokenPlanCurrent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{
		{Name: legacyVolcengineTokenPlanAnthropicProviderName, URL: "https://ark.cn-beijing.volces.com/api/plan", Model: "Auto", Protocol: "anthropic", AgentType: "claude code 2.0"},
		{Name: "Custom1", IsCustom: true},
	}
	if err := app.SaveMaclawLLMProviders(providers, legacyVolcengineTokenPlanAnthropicProviderName); err != nil {
		t.Fatalf("SaveMaclawLLMProviders() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.MaclawLLMCurrentProvider != volcengineAgentPlanProviderName {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want %q", saved.MaclawLLMCurrentProvider, volcengineAgentPlanProviderName)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, legacyVolcengineTokenPlanAnthropicProviderName); ok {
		t.Fatalf("saved providers still contain legacy Anthropic TokenPlan name: %+v", saved.MaclawLLMProviders)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, volcengineAgentPlanProviderName); !ok {
		t.Fatalf("saved providers missing canonical 火山引擎 Agent Plan name: %+v", saved.MaclawLLMProviders)
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
	if got := data.Providers[0].TimeoutSec; got != corelib.DefaultLLMTimeoutSec {
		t.Fatalf("TimeoutSec = %d, want %d", got, corelib.DefaultLLMTimeoutSec)
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
	if saved.MaclawLLMTimeoutSec != corelib.DefaultLLMTimeoutSec {
		t.Fatalf("MaclawLLMTimeoutSec = %d, want %d", saved.MaclawLLMTimeoutSec, corelib.DefaultLLMTimeoutSec)
	}
	if len(saved.MaclawLLMProviders) == 0 {
		t.Fatal("expected saved providers")
	}
	if got := saved.MaclawLLMProviders[0].TimeoutSec; got != corelib.DefaultLLMTimeoutSec {
		t.Fatalf("provider TimeoutSec = %d, want %d", got, corelib.DefaultLLMTimeoutSec)
	}
}

func TestSaveMaclawLLMProviders_PersistsHubServiceFlag(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	providers := []corelib.MaclawLLMProvider{{
		Name:     hubServiceProviderName,
		URL:      "https://hub.example.com/api/llm/v1",
		Key:      "viewer-token",
		Model:    hubServiceAutoModel,
		Protocol: "openai",
	}}
	if err := app.SaveMaclawLLMProviders(providers, hubServiceProviderName); err != nil {
		t.Fatalf("SaveMaclawLLMProviders() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	provider, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("saved providers missing hub provider: %+v", saved.MaclawLLMProviders)
	}
	if !provider.IsHubService {
		t.Fatal("saved provider IsHubService = false, want true")
	}
}

func TestSaveMaclawLLMProviders_CanonicalizesDuplicateHubServiceAliases(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	mojibakeHubName := "MaClaw\u7039\u6a3b\u67df"
	providers := []corelib.MaclawLLMProvider{
		{Name: mojibakeHubName, URL: "https://old.example.com/api/llm/v1", Key: "old-token", Model: hubServiceAutoModel, Protocol: "openai"},
		{Name: hubServiceProviderName, URL: "https://hub.example.com/api/llm/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai"},
		{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test", IsCustom: true},
	}
	if err := app.SaveMaclawLLMProviders(providers, mojibakeHubName); err != nil {
		t.Fatalf("SaveMaclawLLMProviders() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.MaclawLLMCurrentProvider != hubServiceProviderName {
		t.Fatalf("current provider = %q, want %q", saved.MaclawLLMCurrentProvider, hubServiceProviderName)
	}
	hubCount := 0
	for _, provider := range saved.MaclawLLMProviders {
		if provider.Name == mojibakeHubName {
			t.Fatalf("saved providers still contain mojibake hub alias: %+v", saved.MaclawLLMProviders)
		}
		if provider.Name == hubServiceProviderName {
			hubCount++
			if !provider.IsHubService {
				t.Fatal("canonical hub provider IsHubService = false, want true")
			}
		}
	}
	if hubCount != 1 {
		t.Fatalf("hub provider count = %d, want 1; providers=%+v", hubCount, saved.MaclawLLMProviders)
	}
}

func TestGetMaclawLLMProviders_CanonicalizesLegacyHubCurrentAndBackfillsTimeout(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	mojibakeHubName := "MaClaw\u7039\u6a3b\u67df"
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: mojibakeHubName,
		MaclawLLMTimeoutSec:      77,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: mojibakeHubName, URL: "https://hub.example.com/api/llm/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetMaclawLLMProviders()
	if data.Current != hubServiceProviderName {
		t.Fatalf("Current = %q, want %q", data.Current, hubServiceProviderName)
	}
	provider, ok := findProviderByName(data.Providers, hubServiceProviderName)
	if !ok {
		t.Fatalf("providers missing canonical hub provider: %+v", data.Providers)
	}
	if provider.TimeoutSec != corelib.DefaultLLMTimeoutSec {
		t.Fatalf("TimeoutSec = %d, want normalized default timeout %d", provider.TimeoutSec, corelib.DefaultLLMTimeoutSec)
	}
	if !provider.IsHubService {
		t.Fatal("canonical hub provider IsHubService = false, want true")
	}
}

func TestSaveMaclawLLMProviders_SyncsMissingHubProviderWhenSelected(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/service/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false,"hub_llm_base_url":"` + hubURL + `/api/llm/v1","credit_grants":[{"service_group_id":"coding-basic","active":false,"status":"period_limited","retry_after_seconds":3600}]}`))
	}))
	hubURL = hub.URL
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	providers := []corelib.MaclawLLMProvider{{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test", IsCustom: true}}
	if err := app.SaveMaclawLLMProviders(providers, hubServiceProviderName); err != nil {
		t.Fatalf("SaveMaclawLLMProviders() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	provider, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("saved providers missing hub provider: %+v", saved.MaclawLLMProviders)
	}
	if provider.URL != hub.URL+"/api/llm/v1" || provider.Key != "viewer-token" || provider.Model != hubServiceAutoModel || !provider.IsHubService {
		t.Fatalf("unexpected synced hub provider: %+v", provider)
	}
	if saved.MaclawLLMUrl != provider.URL || saved.MaclawLLMCurrentProvider != hubServiceProviderName {
		t.Fatalf("legacy/current fields not synced: current=%q url=%q provider=%+v", saved.MaclawLLMCurrentProvider, saved.MaclawLLMUrl, provider)
	}
}

func TestGetHubLLMServiceStatus_PrefersAccountEndpointCredits(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hubURL := ""
	var hub *httptest.Server
	hub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			_, _ = w.Write([]byte(`{"status":{"active":true,"service_group_ids":["newbie"],"service_group_names":["充值服务组"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"` + hubURL + `/api/llm/v1","credits_total":50000,"credits_used":1017.432,"credits_remaining":48982.568,"credits_available":48982.568,"credit_grants":[{"service_group_id":"newbie","source":"card","active":true,"status":"active","credits_total":50000,"credits_used":1017.432,"credits_remaining":48982.568,"credits_available":48982.568}]}}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"` + hubURL + `/api/llm/v1","credits_total":0,"credits_used":0,"credits_remaining":0,"credits_available":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	hubURL = hub.URL
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should use account endpoint when available")
	}
	if status.CreditsAvailable != 48982.568 || status.CreditsTotal != 50000 {
		t.Fatalf("credits = total %v available %v, want paid account credits", status.CreditsTotal, status.CreditsAvailable)
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "充值服务组" {
		t.Fatalf("service group names = %#v, want paid group", status.ServiceGroupNames)
	}
}

func TestGetHubLLMServiceStatus_AcceptsDirectAccountStatusResponse(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["newbie"],"service_group_names":["充值服务组"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":55000,"credits_used":5536.136,"credits_remaining":48982.568,"credits_available":48982.568}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":0,"credits_available":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should accept direct account status without falling back")
	}
	if status.CreditsAvailable != 48982.568 || status.CreditsTotal != 55000 {
		t.Fatalf("credits = total %v available %v, want direct account credits", status.CreditsTotal, status.CreditsAvailable)
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "充值服务组" {
		t.Fatalf("service group names = %#v, want direct account group", status.ServiceGroupNames)
	}
}

func TestGetHubLLMServiceStatus_AcceptsServiceStatusAccountResponse(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			_, _ = w.Write([]byte(`{"service_status":{"active":true,"service_group_ids":["newbie"],"service_group_names":["充值服务组"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":55000,"credits_used":5536.136,"credits_remaining":48982.568,"credits_available":48982.568}}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":0,"credits_available":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should accept service_status account response without falling back")
	}
	if status.CreditsAvailable != 48982.568 || status.CreditsTotal != 55000 {
		t.Fatalf("credits = total %v available %v, want service_status account credits", status.CreditsTotal, status.CreditsAvailable)
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "充值服务组" {
		t.Fatalf("service group names = %#v, want service_status account group", status.ServiceGroupNames)
	}
}

func TestGetHubLLMServiceStatus_AcceptsCreditsOnlyAccountStatus(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			_, _ = w.Write([]byte(`{"status":{"active":true,"credits_total":55000,"credits_used":5536.136,"credits_remaining":48982.568,"credits_available":48982.568}}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":0,"credits_available":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if !sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should query status endpoint to fill route details")
	}
	if status.CreditsAvailable != 48982.568 || status.CreditsTotal != 55000 {
		t.Fatalf("credits = total %v available %v, want account credit totals", status.CreditsTotal, status.CreditsAvailable)
	}
	if status.HubLLMBaseURL != "https://hub.example.com/api/llm/v1" || status.DefaultModel != "auto" {
		t.Fatalf("route details = base %q default %q, want legacy status route details", status.HubLLMBaseURL, status.DefaultModel)
	}
	if len(status.ServiceGroupNames) != 0 {
		t.Fatalf("service group names = %#v, want account groups preserved instead of stale fallback free group", status.ServiceGroupNames)
	}
}

func TestGetHubLLMServiceStatus_MergesRouteDetailsWithoutOverwritingPaidAccount(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			_, _ = w.Write([]byte(`{"status":{"active":true,"service_group_ids":["newbie"],"service_group_names":["充值服务组"],"credits_total":55000,"credits_used":5536.136,"credits_remaining":48982.568,"credits_available":48982.568}}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":0,"credits_available":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if !sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should query status endpoint to fill route details")
	}
	if status.CreditsAvailable != 48982.568 || status.CreditsTotal != 55000 {
		t.Fatalf("credits = total %v available %v, want paid account credits", status.CreditsTotal, status.CreditsAvailable)
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "充值服务组" {
		t.Fatalf("service group names = %#v, want paid account group preserved", status.ServiceGroupNames)
	}
	if status.HubLLMBaseURL != "https://hub.example.com/api/llm/v1" || status.DefaultModel != "auto" || len(status.AvailableModels) != 1 || status.AvailableModels[0] != "auto" {
		t.Fatalf("route details = base %q default %q models %#v, want merged route details", status.HubLLMBaseURL, status.DefaultModel, status.AvailableModels)
	}
}

func TestGetHubLLMServiceStatus_KeepsPaidAccountWhenRouteDetailsFail(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			_, _ = w.Write([]byte(`{"status":{"active":true,"service_group_ids":["newbie"],"service_group_names":["充值服务组"],"credits_total":55000,"credits_used":5536.136,"credits_remaining":48982.568,"credits_available":48982.568}}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"legacy status unavailable"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if !sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should try status endpoint to fill missing route details")
	}
	if status.CreditsAvailable != 48982.568 || status.CreditsTotal != 55000 {
		t.Fatalf("credits = total %v available %v, want paid account credits", status.CreditsTotal, status.CreditsAvailable)
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "充值服务组" {
		t.Fatalf("service group names = %#v, want paid account group preserved", status.ServiceGroupNames)
	}
}

func TestGetHubLLMServiceStatus_FallsBackWhenAccountEndpointFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"usage report failed"}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if !sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should fall back to status endpoint")
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "企业免费服务" {
		t.Fatalf("service group names = %#v, want fallback status", status.ServiceGroupNames)
	}
}

func TestGetHubLLMServiceStatus_DoesNotFallbackWhenAccountEndpointUnauthorized(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"viewer token expired"}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":0,"credits_available":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "expired-viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := app.GetHubLLMServiceStatus()
	if err == nil {
		t.Fatal("GetHubLLMServiceStatus() error = nil, want unauthorized account error")
	}
	if sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() must not fall back to status endpoint on 401/403")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "viewer token expired") {
		t.Fatalf("error = %v, want 401 viewer token expired", err)
	}
}

func TestGetHubLLMServiceStatus_DoesNotFallbackWhenAccountEndpointRejectsRequest(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"invalid tenant"}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["Free"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":0,"credits_available":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := app.GetHubLLMServiceStatus()
	if err == nil {
		t.Fatal("GetHubLLMServiceStatus() error = nil, want bad account request error")
	}
	if sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() must not fall back to status endpoint on 400")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "invalid tenant") {
		t.Fatalf("error = %v, want 400 invalid tenant", err)
	}
}

func TestGetHubLLMServiceStatus_FallsBackWhenAccountEndpointReturnsEmptyStatus(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			_, _ = w.Write([]byte(`{"status":{"active":true}}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.GetHubLLMServiceStatus()
	if err != nil {
		t.Fatalf("GetHubLLMServiceStatus() error = %v", err)
	}
	if !sawStatusEndpoint {
		t.Fatal("GetHubLLMServiceStatus() should fall back when account status has no usable entitlement details")
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "企业免费服务" {
		t.Fatalf("service group names = %#v, want fallback status", status.ServiceGroupNames)
	}
}

func TestGetHubLLMServiceStatus_FallsBackWhenAccountEndpointTimesOut(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawStatusEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/account":
			time.Sleep(50 * time.Millisecond)
			_, _ = w.Write([]byte(`{"status":{"active":true}}`))
		case "/api/llm/service/status":
			sawStatusEndpoint = true
			_, _ = w.Write([]byte(`{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	status, err := app.fetchHubLLMServiceStatusWithTimeout(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchHubLLMServiceStatusWithTimeout() error = %v", err)
	}
	if !sawStatusEndpoint {
		t.Fatal("fetchHubLLMServiceStatusWithTimeout() should fall back to status endpoint")
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "企业免费服务" {
		t.Fatalf("service group names = %#v, want fallback status", status.ServiceGroupNames)
	}
}

func TestHubLLMServiceAccountStatusTimeoutCapsLongStatusTimeout(t *testing.T) {
	if got := hubLLMServiceAccountStatusTimeout(30 * time.Second); got != hubServiceAccountStatusMaxTimeout {
		t.Fatalf("hubLLMServiceAccountStatusTimeout(30s) = %v, want %v", got, hubServiceAccountStatusMaxTimeout)
	}
	if got := hubLLMServiceAccountStatusTimeout(500 * time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("hubLLMServiceAccountStatusTimeout(500ms) = %v, want 500ms", got)
	}
}

func TestRedeemHubLLMService_RefreshesAccountStatusAfterRedeem(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var sawAccountEndpoint bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/llm/service/redeem":
			_, _ = w.Write([]byte(`{"success":true,"service_status":{"active":true,"service_group_ids":["free"],"service_group_names":["企业免费服务"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":0,"credits_available":0}}`))
		case "/api/llm/service/account":
			sawAccountEndpoint = true
			_, _ = w.Write([]byte(`{"status":{"active":true,"service_group_ids":["newbie"],"service_group_names":["充值服务组"],"available_models":["auto"],"default_model":"auto","hub_llm_base_url":"https://hub.example.com/api/llm/v1","credits_total":50000,"credits_used":1017.432,"credits_remaining":48982.568,"credits_available":48982.568}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	status, err := app.RedeemHubLLMService("ABC123")
	if err != nil {
		t.Fatalf("RedeemHubLLMService() error = %v", err)
	}
	if !sawAccountEndpoint {
		t.Fatal("RedeemHubLLMService() should refresh account status after redeem")
	}
	if status.CreditsAvailable != 48982.568 || status.CreditsTotal != 50000 {
		t.Fatalf("credits = total %v available %v, want refreshed paid account credits", status.CreditsTotal, status.CreditsAvailable)
	}
	if len(status.ServiceGroupNames) != 1 || status.ServiceGroupNames[0] != "充值服务组" {
		t.Fatalf("service group names = %#v, want refreshed paid group", status.ServiceGroupNames)
	}
}

func TestSaveMaclawLLMProviders_RejectsMissingHubProviderWhenSyncUnavailable(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      "http://127.0.0.1:1",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	providers := []corelib.MaclawLLMProvider{{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test", IsCustom: true}}
	err := app.SaveMaclawLLMProviders(providers, hubServiceProviderName)
	if err == nil || !strings.Contains(err.Error(), "MaClaw 官方服务商暂不可用") {
		t.Fatalf("SaveMaclawLLMProviders() error = %v, want missing hub provider error", err)
	}

	saved, loadErr := app.LoadConfig()
	if loadErr != nil {
		t.Fatalf("LoadConfig() error = %v", loadErr)
	}
	if saved.MaclawLLMCurrentProvider == hubServiceProviderName {
		t.Fatalf("current provider changed to missing hub provider: %+v", saved)
	}
}

func TestSaveMaclawLLMProviders_ExplainsLimitedHubProviderWhenServiceEntryMissing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/service/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false,"credit_grants":[{"service_group_id":"coding-basic","active":false,"status":"period_limited","retry_after_seconds":3600}]}`))
	}))
	hubURL = hub.URL
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hubURL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	providers := []corelib.MaclawLLMProvider{{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test", IsCustom: true}}
	err := app.SaveMaclawLLMProviders(providers, hubServiceProviderName)
	if err == nil {
		t.Fatal("SaveMaclawLLMProviders() expected error")
	}
	if !strings.Contains(err.Error(), "周期限流") || !strings.Contains(err.Error(), "1 小时") {
		t.Fatalf("SaveMaclawLLMProviders() error = %v, want period-limit explanation", err)
	}
	if strings.Contains(err.Error(), "服务商暂不可用") {
		t.Fatalf("SaveMaclawLLMProviders() error = %v, should not hide period limit behind unavailable wording", err)
	}
}

func TestSaveMaclawLLMProviders_ExplainsInactiveHubProviderWhenServiceEntryMissing(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		retryAfter int
		want       string
	}{
		{name: "queued", status: "queued", retryAfter: 7200, want: "授权尚未生效"},
		{name: "exhausted", status: "exhausted", want: "额度已用尽"},
		{name: "expired", status: "expired", want: "授权已过期"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpHome := t.TempDir()
			t.Setenv("USERPROFILE", tmpHome)
			t.Setenv("HOME", tmpHome)

			app := &App{testHomeDir: tmpHome}
			hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/llm/service/status" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				grant := fmt.Sprintf(`{"service_group_id":"coding-basic","active":false,"status":%q`, tt.status)
				if tt.retryAfter > 0 {
					grant += fmt.Sprintf(`,"retry_after_seconds":%d`, tt.retryAfter)
				}
				grant += `}`
				_, _ = w.Write([]byte(`{"active":false,"credit_grants":[` + grant + `]}`))
			}))
			defer hub.Close()

			if err := app.SaveConfig(corelib.AppConfig{
				RemoteHubURL:      hub.URL,
				RemoteViewerToken: "viewer-token",
			}); err != nil {
				t.Fatalf("SaveConfig() error = %v", err)
			}

			providers := []corelib.MaclawLLMProvider{{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test", IsCustom: true}}
			err := app.SaveMaclawLLMProviders(providers, hubServiceProviderName)
			if err == nil {
				t.Fatal("SaveMaclawLLMProviders() expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("SaveMaclawLLMProviders() error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "MaClaw 官方服务商暂不可用") || strings.Contains(err.Error(), "服务商暂不可用") {
				t.Fatalf("SaveMaclawLLMProviders() error = %v, should explain inactive grant state", err)
			}
		})
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

func TestSubAgentConcurrencyNormalizesBounds(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "zero defaults", in: 0, want: corelib.DefaultSubAgentConcurrency},
		{name: "one stays", in: 1, want: 1},
		{name: "middle stays", in: 3, want: 3},
		{name: "above max clamps", in: 15, want: corelib.MaxSubAgentConcurrency},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := app.SetSubAgentConcurrency(tc.in); err != nil {
				t.Fatalf("SetSubAgentConcurrency(%d) error = %v", tc.in, err)
			}
			if got := app.GetSubAgentConcurrency(); got != tc.want {
				t.Fatalf("GetSubAgentConcurrency() = %d, want %d", got, tc.want)
			}
			saved, err := app.LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if got := saved.SubAgentConcurrency; got != tc.want {
				t.Fatalf("saved SubAgentConcurrency = %d, want %d", got, tc.want)
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

func TestMaclawLLMThinkingMode_GlobalSetting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}

	// Default: auto (empty).
	if got := app.GetMaclawLLMThinkingMode(); got != "" {
		t.Fatalf("default GetMaclawLLMThinkingMode() = %q, want auto(empty)", got)
	}

	for _, tc := range []struct{ in, want string }{
		{in: "enabled", want: "enabled"},
		{in: "ON", want: "enabled"},
		{in: "disabled", want: "disabled"},
		{in: "off", want: "disabled"},
		{in: "auto", want: ""},
		{in: "garbage", want: ""},
	} {
		if err := app.SetMaclawLLMThinkingMode(tc.in); err != nil {
			t.Fatalf("SetMaclawLLMThinkingMode(%q) error = %v", tc.in, err)
		}
		if got := app.GetMaclawLLMThinkingMode(); got != tc.want {
			t.Fatalf("GetMaclawLLMThinkingMode() after set %q = %q, want %q", tc.in, got, tc.want)
		}
		saved, err := app.LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
		if got := saved.MaclawLLMThinkingMode; got != tc.want {
			t.Fatalf("saved MaclawLLMThinkingMode = %q, want %q", got, tc.want)
		}
	}

	// Enabled propagates onto the materialized runtime config.
	if err := app.SetMaclawLLMThinkingMode("enabled"); err != nil {
		t.Fatalf("SetMaclawLLMThinkingMode(enabled) error = %v", err)
	}
	cfg := app.GetMaclawLLMConfig()
	if cfg.ThinkingMode != "enabled" {
		t.Fatalf("GetMaclawLLMConfig().ThinkingMode = %q, want enabled", cfg.ThinkingMode)
	}
	if err := app.SetMaclawLLMThinkingMode(""); err != nil {
		t.Fatalf("SetMaclawLLMThinkingMode(empty) error = %v", err)
	}
	if got := app.GetMaclawLLMConfig().ThinkingMode; got != "" {
		t.Fatalf("GetMaclawLLMConfig().ThinkingMode after reset = %q, want auto(empty)", got)
	}
}

func TestTestMaclawLLMHonorsGlobalThinkingMode(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	if err := app.SetMaclawLLMThinkingMode("disabled"); err != nil {
		t.Fatalf("SetMaclawLLMThinkingMode: %v", err)
	}

	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	_, err := app.TestMaclawLLM(corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "deepseek-reasoner", Protocol: "openai",
	})
	if err != nil {
		t.Fatalf("TestMaclawLLM: %v", err)
	}
	thinking, _ := got["thinking"].(map[string]interface{})
	if thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v, want disabled", got["thinking"])
	}
}
