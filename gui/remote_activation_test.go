package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/gorilla/websocket"
)

func TestNormalizedRemotePlatform(t *testing.T) {
	original := remotePlatformGOOS
	defer func() {
		remotePlatformGOOS = original
	}()

	cases := map[string]string{
		"windows": "windows",
		"darwin":  "mac",
		"linux":   "linux",
		"freebsd": "linux",
	}

	for goos, want := range cases {
		remotePlatformGOOS = func() string { return goos }
		if got := normalizedRemotePlatform(); got != want {
			t.Fatalf("normalizedRemotePlatform() for %q = %q, want %q", goos, got, want)
		}
	}
}

func TestResolveProjectProxyURL_ProjectSpecificPreferred(t *testing.T) {
	app := &App{}
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:            "proj-1",
				Path:          filepath.Clean(`D:\workprj\proj`),
				ProxyHost:     "project-proxy.local",
				ProxyPort:     "7890",
				ProxyUsername: "alice",
				ProxyPassword: "secret",
			},
		},
		DefaultProxyHost:     "global-proxy.local",
		DefaultProxyPort:     "8080",
		DefaultProxyUsername: "global-user",
		DefaultProxyPassword: "global-pass",
	}

	got := app.resolveProjectProxyURL(cfg, filepath.Clean(`D:\workprj\proj`))
	want := "http://alice:secret@project-proxy.local:7890"
	if got != want {
		t.Fatalf("resolveProjectProxyURL() = %q, want %q", got, want)
	}
}

func TestResolveProjectProxyURL_FallsBackToDefault(t *testing.T) {
	app := &App{}
	cfg := corelib.AppConfig{
		CurrentProject:       "proj-1",
		Projects:             []corelib.ProjectConfig{{Id: "proj-1", Path: filepath.Clean(`D:\workprj\proj`)}},
		DefaultProxyHost:     "global-proxy.local",
		DefaultProxyPort:     "8080",
		DefaultProxyUsername: "global-user",
		DefaultProxyPassword: "global-pass",
	}

	got := app.resolveProjectProxyURL(cfg, filepath.Clean(`D:\workprj\proj`))
	want := "http://global-user:global-pass@global-proxy.local:8080"
	if got != want {
		t.Fatalf("resolveProjectProxyURL() = %q, want %q", got, want)
	}
}

func TestBuildClaudeLaunchEnv_SetsAnthropicFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	model := &corelib.ModelConfig{
		ModelName: "ChatFire",
		ModelId:   "claude-sonnet-4",
		ModelUrl:  "https://api.example.com/anthropic",
		ApiKey:    "sk-test",
		WireApi:   "anthropic",
	}

	env, err := app.buildClaudeLaunchEnv(corelib.AppConfig{}, model, filepath.Clean(`D:\workprj\proj`), false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchEnv() error = %v", err)
	}

	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-test" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["ANTHROPIC_BASE_URL"] != "https://api.example.com/anthropic" {
		t.Fatalf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_MODEL"] != "claude-sonnet-4" {
		t.Fatalf("ANTHROPIC_MODEL = %q", env["ANTHROPIC_MODEL"])
	}
	if env["CLAUDE_CODE_USE_COLORS"] != "true" {
		t.Fatalf("CLAUDE_CODE_USE_COLORS = %q", env["CLAUDE_CODE_USE_COLORS"])
	}
	if env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "128000" {
		t.Fatalf("CLAUDE_CODE_MAX_OUTPUT_TOKENS = %q", env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"])
	}
}

func TestBuildClaudeLaunchEnv_CodegenWritesDedicatedSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	model := &corelib.ModelConfig{
		ModelName: "codegen",
		ModelId:   "claude-codegen-1",
		ModelUrl:  "http://127.0.0.1:5001/anthropic",
		ApiKey:    "cg-test",
		WireApi:   "anthropic",
	}

	env, err := app.buildClaudeLaunchEnv(corelib.AppConfig{}, model, filepath.Clean(`D:\workprj\proj`), false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchEnv() error = %v", err)
	}

	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:5001/anthropic" {
		t.Fatalf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}

	codegenSettings, err := configfile.ReadCodeGenSettings()
	if err != nil {
		t.Fatalf("ReadCodeGenSettings() error = %v", err)
	}
	if codegenSettings == nil {
		t.Fatal("expected codegen settings to be written")
	}
	envMap, _ := codegenSettings["env"].(map[string]interface{})
	if got, _ := envMap["ANTHROPIC_AUTH_TOKEN"].(string); got != "cg-test" {
		t.Fatalf("codegen ANTHROPIC_AUTH_TOKEN = %q", got)
	}
	if got, _ := envMap["ANTHROPIC_MODEL"].(string); got != "claude-codegen-1" {
		t.Fatalf("codegen ANTHROPIC_MODEL = %q", got)
	}
}

func TestBuildClaudeLaunchEnv_EnablesTeamModeAndProxy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	projectPath := filepath.Clean(`D:\workprj\proj`)
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:       "proj-1",
				Path:     projectPath,
				TeamMode: true,
			},
		},
		DefaultProxyHost:     "proxy.local",
		DefaultProxyPort:     "8081",
		DefaultProxyUsername: "bob",
		DefaultProxyPassword: "pwd",
	}
	model := &corelib.ModelConfig{
		ModelName: "ChatFire",
		ModelId:   "claude-sonnet-4",
		ModelUrl:  "https://api.example.com/anthropic",
		ApiKey:    "sk-test",
		WireApi:   "anthropic",
	}

	env, err := app.buildClaudeLaunchEnv(cfg, model, projectPath, true)
	if err != nil {
		t.Fatalf("buildClaudeLaunchEnv() error = %v", err)
	}

	if env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] != "1" {
		t.Fatalf("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = %q", env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"])
	}
	wantProxy := "http://bob:pwd@proxy.local:8081"
	if env["HTTP_PROXY"] != wantProxy || env["HTTPS_PROXY"] != wantProxy {
		t.Fatalf("proxy env mismatch: HTTP_PROXY=%q HTTPS_PROXY=%q", env["HTTP_PROXY"], env["HTTPS_PROXY"])
	}
}

func TestBuildClaudeLaunchEnv_RejectsNonAnthropicWireAPI(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	model := &corelib.ModelConfig{
		ModelName: "ChatFire",
		ModelId:   "claude-sonnet-4",
		ModelUrl:  "https://api.example.com/v1",
		ApiKey:    "sk-test",
		WireApi:   "responses",
	}

	_, err := app.buildClaudeLaunchEnv(corelib.AppConfig{}, model, filepath.Clean(`D:\workprj\proj`), false)
	if err == nil {
		t.Fatal("expected error for non-anthropic wire_api, got nil")
	}
	if !strings.Contains(err.Error(), "must use anthropic wire_api") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildClaudeLaunchSpec_UsesCurrentProjectAndTitle(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	projectPath := filepath.Clean(`D:\workprj\proj`)
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:       "proj-1",
				Path:     projectPath,
				TeamMode: true,
			},
		},
		Claude: corelib.ToolConfig{
			CurrentModel: "ChatFire",
			Models: []corelib.ModelConfig{
				{
					ModelName: "ChatFire",
					ModelId:   "claude-sonnet-4",
					ModelUrl:  "https://api.example.com/anthropic",
					ApiKey:    "sk-test",
				},
			},
		},
	}

	spec, err := app.buildClaudeLaunchSpec(cfg, true, false, "", projectPath, false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec() error = %v", err)
	}

	if spec.Tool != "claude" {
		t.Fatalf("Tool = %q", spec.Tool)
	}
	if spec.ProjectPath != projectPath {
		t.Fatalf("ProjectPath = %q, want %q", spec.ProjectPath, projectPath)
	}
	if spec.Title != "proj" {
		t.Fatalf("Title = %q", spec.Title)
	}
	if !spec.TeamMode {
		t.Fatal("TeamMode = false, want true")
	}
	if !spec.YoloMode {
		t.Fatal("YoloMode = false, want true")
	}
	if spec.Env["ANTHROPIC_MODEL"] != "claude-sonnet-4" {
		t.Fatalf("ANTHROPIC_MODEL = %q", spec.Env["ANTHROPIC_MODEL"])
	}
}

func TestBuildClaudeLaunchSpec_UsesSavedCurrentProjectWhenProjectDirEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	projectPath := filepath.Clean(`D:\workprj\proj-saved`)
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:       "proj-1",
				Path:     projectPath,
				TeamMode: true,
			},
		},
		Claude: corelib.ToolConfig{
			CurrentModel: "ChatFire",
			Models: []corelib.ModelConfig{
				{
					ModelName: "ChatFire",
					ModelId:   "claude-sonnet-4",
					ModelUrl:  "https://api.example.com/anthropic",
					ApiKey:    "sk-test",
				},
			},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	spec, err := app.buildClaudeLaunchSpec(cfg, false, false, "", "", false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec() error = %v", err)
	}
	if spec.ProjectPath != projectPath {
		t.Fatalf("ProjectPath = %q, want %q", spec.ProjectPath, projectPath)
	}
}

// Note: Tests for resolveRemoteHubURL and buildCenterURLList have been removed
// because these functions are now delegated to corelib/remote.EnrollmentClient.
// The corresponding tests are in corelib/remote/enrollment_test.go.

func TestActivateRemote_ResolvesHubAndPersistsIdentity(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	remote.InvalidateCenterCache()

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_123",
				"tenant_id":     "tenant_123",
				"tenant_name":   "Acme Team",
				"email":         "user@example.com",
				"sn":            "SN-2026-000001",
				"machine_id":    "m_123",
				"machine_token": "mt_123",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/entry/resolve" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"email": "user@example.com",
			"mode":  "single",
			"default_hub": map[string]any{
				"hub_id":   "hub_1",
				"base_url": hub.URL,
				"pwa_url":  hub.URL + "/app?email=user@example.com&entry=app",
			},
			"hubs": []map[string]any{
				{
					"hub_id":   "hub_1",
					"base_url": hub.URL,
					"pwa_url":  hub.URL + "/app?email=user@example.com&entry=app",
				},
			},
		})
	}))
	defer center.Close()

	// Override defaults so the enrollment client doesn't probe real HubCenter URLs.
	origDefaults := remote.DefaultRemoteHubCenterURLs
	origDefault := remote.DefaultRemoteHubCenterURL
	origGUIDefault := defaultRemoteHubCenterURL
	remote.DefaultRemoteHubCenterURLs = []string{center.URL}
	remote.DefaultRemoteHubCenterURL = center.URL
	defaultRemoteHubCenterURL = center.URL
	defer func() {
		remote.DefaultRemoteHubCenterURLs = origDefaults
		remote.DefaultRemoteHubCenterURL = origDefault
		defaultRemoteHubCenterURL = origGUIDefault
	}()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubCenterURL: center.URL,
		RemoteNickname:     "Old Desk",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.ActivateRemote("user@example.com", "", "")
	if err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}
	if result.MachineID != "m_123" || result.MachineToken != "mt_123" {
		t.Fatalf("unexpected activation result: %+v", result)
	}
	if result.TenantID != "tenant_123" || result.TenantName != "Acme Team" {
		t.Fatalf("unexpected tenant result: %+v", result)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubURL != hub.URL {
		t.Fatalf("RemoteHubURL = %q, want %q", saved.RemoteHubURL, hub.URL)
	}
	if saved.RemoteHubID != "hub_1" {
		t.Fatalf("RemoteHubID = %q, want %q", saved.RemoteHubID, "hub_1")
	}
	if saved.RemoteEmail != "user@example.com" || saved.RemoteSN != "SN-2026-000001" {
		t.Fatalf("saved identity mismatch: %+v", saved)
	}
	if saved.RemoteMachineID != "m_123" || saved.RemoteMachineToken != "mt_123" {
		t.Fatalf("saved machine identity mismatch: %+v", saved)
	}
	if saved.RemoteTenantID != "tenant_123" || saved.RemoteTenantName != "Acme Team" {
		t.Fatalf("saved tenant identity mismatch: %+v", saved)
	}
	if saved.RemoteMachineName == "" {
		t.Fatal("RemoteMachineName should be saved after activation")
	}
	if saved.RemoteNickname != "" {
		t.Fatalf("RemoteNickname = %q, want cleared until Hub assigns current nickname", saved.RemoteNickname)
	}
	// Verify RemoteEnabled is set
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled should be true after activation")
	}
}

func TestActivateRemote_SwitchesToHubProviderWhenRegisteredAccountHasOfficialService(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_456",
				"email":         "user@example.com",
				"sn":            "SN-2026-000456",
				"machine_id":    "m_456",
				"machine_token": "mt_456",
				"viewer_token":  "viewer-token",
			})
		case "/api/llm/service/account":
			if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
				t.Errorf("Authorization = %q, want viewer token", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": map[string]any{
					"active":              true,
					"service_group_ids":   []string{"official"},
					"service_group_names": []string{"MaClaw官方服务"},
					"available_models":    []string{"auto"},
					"default_model":       "auto",
					"hub_llm_base_url":    hubURL + "/api/llm/v1",
					"credits_total":       100,
					"credits_available":   100,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	hubURL = hub.URL
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMUrl:             "https://custom.example.com/v1",
		MaclawLLMKey:             "custom-key",
		MaclawLLMModel:           "gpt-test",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "Custom1",
			URL:      "https://custom.example.com/v1",
			Key:      "custom-key",
			Model:    "gpt-test",
			Protocol: "openai",
			IsCustom: true,
		}},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.MaclawLLMCurrentProvider != hubServiceProviderName {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want %q", saved.MaclawLLMCurrentProvider, hubServiceProviderName)
	}
	provider, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("saved providers missing hub provider: %+v", saved.MaclawLLMProviders)
	}
	if provider.URL != hub.URL+"/api/llm/v1" || provider.Key != "viewer-token" || provider.Model != hubServiceAutoModel || !provider.IsHubService {
		t.Fatalf("unexpected hub provider: %+v", provider)
	}
	if saved.MaclawLLMUrl != provider.URL || saved.MaclawLLMKey != provider.Key || saved.MaclawLLMModel != provider.Model {
		t.Fatalf("legacy fields not synced to hub provider: url=%q key=%q model=%q provider=%+v", saved.MaclawLLMUrl, saved.MaclawLLMKey, saved.MaclawLLMModel, provider)
	}
}

func TestActivateRemote_RemovesStaleHubProviderWhenRegisteredAccountHasNoOfficialService(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_789",
				"email":         "user@example.com",
				"sn":            "SN-2026-000789",
				"machine_id":    "m_789",
				"machine_token": "mt_789",
				"viewer_token":  "viewer-token-new",
			})
		case "/api/llm/service/account":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": map[string]any{
					"active":           false,
					"available_models": []string{"auto"},
					"default_model":    "auto",
					"hub_llm_base_url": hubURL + "/api/llm/v1",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	hubURL = hub.URL
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token-old",
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: "https://old-hub.example.com/api/llm/v1", Key: "viewer-token-old", Model: hubServiceAutoModel, Protocol: "openai", IsHubService: true},
			{Name: "Custom1", URL: "https://custom.example.com/v1", Key: "custom-key", Model: "gpt-test", Protocol: "openai", IsCustom: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.MaclawLLMCurrentProvider != "Custom1" {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want Custom1", saved.MaclawLLMCurrentProvider)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("stale hub provider should be removed when account has no official service: %+v", saved.MaclawLLMProviders)
	}
}

func TestActivateRemote_RemovesStaleHubProviderWhenOfficialServiceAuthorizationFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_890",
				"email":         "user@example.com",
				"sn":            "SN-2026-000890",
				"machine_id":    "m_890",
				"machine_token": "mt_890",
				"viewer_token":  "viewer-token-new",
			})
		case "/api/llm/service/account":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "viewer token expired"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token-old",
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: "https://old-hub.example.com/api/llm/v1", Key: "viewer-token-old", Model: hubServiceAutoModel, Protocol: "openai", IsHubService: true},
			{Name: "Custom1", URL: "https://custom.example.com/v1", Key: "custom-key", Model: "gpt-test", Protocol: "openai", IsCustom: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteViewerToken != "viewer-token-new" {
		t.Fatalf("RemoteViewerToken = %q, want new viewer token", saved.RemoteViewerToken)
	}
	if saved.MaclawLLMCurrentProvider != "Custom1" {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want Custom1", saved.MaclawLLMCurrentProvider)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("stale hub provider should be removed after authorization failure: %+v", saved.MaclawLLMProviders)
	}
}

func TestActivateRemote_ClearsStaleViewerTokenAndHubProviderWhenEnrollOmitsViewerToken(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_987",
				"email":         "user@example.com",
				"sn":            "SN-2026-000987",
				"machine_id":    "m_987",
				"machine_token": "mt_987",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token-old",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: hubServiceProviderName, URL: hub.URL + "/api/llm/v1", Key: "viewer-token-old", Model: hubServiceAutoModel, Protocol: "openai", IsHubService: true}},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubURL != hub.URL {
		t.Fatalf("RemoteHubURL = %q, want %q", saved.RemoteHubURL, hub.URL)
	}
	if saved.RemoteViewerToken != "" {
		t.Fatalf("RemoteViewerToken = %q, want cleared", saved.RemoteViewerToken)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("stale hub provider should be removed after registering different hub without viewer token: %+v", saved.MaclawLLMProviders)
	}
	if saved.MaclawLLMCurrentProvider != "" {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want cleared with stale hub provider", saved.MaclawLLMCurrentProvider)
	}
}

func TestActivateRemote_ReturnsBeforeBackgroundHubConnect(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var authCount atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_234",
				"email":         "user@example.com",
				"sn":            "SN-2026-000234",
				"machine_id":    "m_234",
				"machine_token": "mt_234",
			})
		case "/ws":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade websocket: %v", err)
				return
			}
			defer conn.Close()
			time.Sleep(800 * time.Millisecond)

			for {
				var msg map[string]any
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}
				switch msg["type"] {
				case "auth.machine":
					authCount.Add(1)
					_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
				default:
					_ = conn.WriteJSON(map[string]any{"type": "ack", "payload": map[string]any{"ok": true}})
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	start := time.Now()
	result, err := app.ActivateRemote("user@example.com", "", "")
	if err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("ActivateRemote() returned too slowly: %s", elapsed)
	}
	if result.MachineID != "m_234" {
		t.Fatalf("MachineID = %q, want %q", result.MachineID, "m_234")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if app.remoteSessions != nil && app.remoteSessions.hubClient != nil && app.remoteSessions.hubClient.IsConnected() && authCount.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	if app.remoteSessions == nil || app.remoteSessions.hubClient == nil {
		t.Fatal("expected remote hub client to be initialized")
	}

	t.Fatalf("hub client did not connect after activation: connected=%v authCount=%d ai_ready=%v init_status=%q",
		app.remoteSessions.hubClient.IsConnected(), authCount.Load(), app.IsAIAssistantReady(), app.GetAIAssistantInitStatus())
}

func TestActivateRemote_SendsNormalizedPlatform(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	original := remotePlatformGOOS
	remotePlatformGOOS = func() string { return "darwin" }
	defer func() {
		remotePlatformGOOS = original
	}()

	var enrollPayload map[string]any
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&enrollPayload); err != nil {
			t.Fatalf("decode enroll body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "approved",
			"user_id":       "u_345",
			"email":         "user@example.com",
			"sn":            "SN-2026-000345",
			"machine_id":    "m_345",
			"machine_token": "mt_345",
		})
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	if got := enrollPayload["platform"]; got != "mac" {
		t.Fatalf("platform = %v, want mac", got)
	}
}

func TestActivateRemote_TimesOutSlowEnrollRequest(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	previousTimeout := remoteEnrollTimeout
	remoteEnrollTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		remoteEnrollTimeout = previousTimeout
	})

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(remoteEnrollTimeout + 50*time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "approved",
			"user_id":       "u_slow",
			"email":         "user@example.com",
			"sn":            "SN-slow",
			"machine_id":    "m_slow",
			"machine_token": "mt_slow",
		})
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	started := time.Now()
	_, err := app.ActivateRemote("user@example.com", "", "")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "registration timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("ActivateRemote() took too long: %s", elapsed)
	}
}

func TestSkillMarketAutoLoginThrottlesFailedMachineLogin(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/machine-login" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Too many requests, please slow down"})
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.acquireSkillMarketTokenAfterEnroll("user@example.com", "m_123", "viewer-token")
	app.acquireSkillMarketTokenAfterEnroll("user@example.com", "m_123", "viewer-token")

	if got := calls.Load(); got != 1 {
		t.Fatalf("machine-login calls = %d, want throttled to 1", got)
	}
	if next, ok := app.skillMarketAutoLoginNextAttempt.Load().(time.Time); !ok || time.Until(next) <= 0 {
		t.Fatalf("expected future retry time, got %v ok=%v", next, ok)
	}

	app.skillMarketAutoLoginNextAttempt.Store(time.Now().Add(-time.Second))
	app.acquireSkillMarketTokenAfterEnroll("user@example.com", "m_123", "viewer-token")
	if got := calls.Load(); got != 2 {
		t.Fatalf("machine-login calls after retry window = %d, want 2", got)
	}
}

func TestClearRemoteActivation_DisconnectsHubClient(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg["type"] {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
			default:
				_ = conn.WriteJSON(map[string]any{"type": "ack", "payload": map[string]any{"ok": true}})
			}
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteEmail:        "user@example.com",
		RemoteSN:           "SN-2026-000345",
		RemoteUserID:       "u_345",
		RemoteTenantID:     "tenant_345",
		RemoteTenantName:   "Old Team",
		RemoteMachineID:    "m_345",
		RemoteMachineName:  "old-machine",
		RemoteMachineToken: "mt_345",
		RemoteNickname:     "Old Desk",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(hubClient)
	if err := hubClient.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hubClient.IsConnected() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !hubClient.IsConnected() {
		t.Fatal("expected hub client to connect before clearing activation")
	}

	if err := app.ClearRemoteActivation(); err != nil {
		t.Fatalf("ClearRemoteActivation() error = %v", err)
	}
	if hubClient.IsConnected() {
		t.Fatal("expected hub client to disconnect after clearing activation")
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteMachineID != "" || saved.RemoteMachineToken != "" || saved.RemoteEmail != "" || saved.RemoteSN != "" {
		t.Fatalf("expected activation identity to be cleared, got %+v", saved)
	}
	if saved.RemoteTenantID != "" || saved.RemoteTenantName != "" || saved.RemoteMachineName != "" || saved.RemoteNickname != "" {
		t.Fatalf("expected hub identity metadata to be cleared, got %+v", saved)
	}
}
