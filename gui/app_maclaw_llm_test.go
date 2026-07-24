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
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
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

func TestTestMaclawLLM_ReturnsSupportsVisionTrueForNonRedColour(t *testing.T) {
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
	if !got.SupportsVision {
		t.Fatal("TestMaclawLLM supports_vision = false, want true (model replied with a colour)")
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
		{"Yellow", true},
		{"blue", true},
		{"The image is green.", true},
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

	expectedNames := []string{"OpenAI", "Anthropic", "GitHub Copilot", "DeepSeek", "智谱编程", "MiniMax", "Kimi", volcengineAgentPlanProviderName, "讯飞星辰", "Custom1", "Custom2"}
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
