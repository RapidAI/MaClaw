package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func findProviderByName(providers []corelib.MaclawLLMProvider, name string) (corelib.MaclawLLMProvider, bool) {
	for _, provider := range providers {
		if provider.Name == name {
			return provider, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func TestEnsureViewerTokenThrottlesRecoveryWhenHubOmitsToken(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	// Isolate from public HubCenter defaults so ActivateRemote cannot re-enroll
	// against real hubs and silently obtain a viewer token.
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var enrollCalls atomic.Int32
	var hub *httptest.Server
	hub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{hub.URL}})
		case "/api/entry/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"email": "user@example.com",
				"mode":  "direct",
				"hubs": []map[string]any{{
					"hub_id":   "hub_test",
					"base_url": hub.URL,
					"status":   "online",
					"name":     "test-hub",
				}},
			})
		case "/api/enroll/start":
			enrollCalls.Add(1)
			// Intentionally omit viewer_token — recovery should fail and throttle.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_123",
				"email":         "user@example.com",
				"machine_id":    "m_123",
				"machine_token": "mt_123",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	t.Cleanup(func() {
		// Release any SQLite/memory handles so Windows TempDir cleanup succeeds.
		if app.memoryStore != nil {
			app.memoryStore.Stop()
			app.memoryStore = nil
		}
		app.shutdown(context.Background())
	})
	cfg := corelib.AppConfig{
		RemoteHubURL:        hub.URL,
		RemoteHubCenterURL:  hub.URL,
		RemoteHubCenterURLs: []string{hub.URL},
		RemoteEmail:         "user@example.com",
		RemoteMachineID:     "m_existing",
		RemoteMachineToken:  "mt_existing",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, firstErr := app.ensureViewerToken(cfg)
	_, secondErr := app.ensureViewerToken(cfg)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("ensureViewerToken errors = %v, %v; want both errors", firstErr, secondErr)
	}
	if !strings.Contains(secondErr.Error(), "recovery throttled") {
		t.Fatalf("second error = %v, want throttled", secondErr)
	}
	if got := enrollCalls.Load(); got != 1 {
		t.Fatalf("enroll calls = %d, want throttled to 1", got)
	}
	if next, ok := app.hubViewerTokenRecoveryNextAttempt.Load().(time.Time); !ok || time.Until(next) <= 0 {
		t.Fatalf("expected future retry time, got %v ok=%v", next, ok)
	}
}

func TestApplyHubLLMServiceStatusToConfig_RemovesProviderWhenUnauthorized(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{
				Name:          hubServiceProviderName,
				URL:           "https://hub.example.com/api/llm/v1",
				Key:           "viewer-token",
				Model:         hubServiceAutoModel,
				Protocol:      "openai",
				ContextLength: corelib.DefaultContextTokens,
				TimeoutSec:    corelib.DefaultLLMTimeoutSec,
			},
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}

	// Unauthorized / no entitlement should remove the hub service provider and
	// clear it as the current provider while keeping other providers.
	changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{
		Active: false,
	})
	if !changed {
		t.Fatal("expected config change when removing unauthorized hub provider")
	}
	if _, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatal("hub service provider should be removed when unauthorized")
	}
	if _, ok := findProviderByName(cfg.MaclawLLMProviders, "Custom1"); !ok {
		t.Fatal("non-hub providers must be preserved")
	}
	if cfg.MaclawLLMCurrentProvider != "" {
		t.Fatalf("current provider = %q, want empty after hub removal", cfg.MaclawLLMCurrentProvider)
	}
}

func TestApplyHubLLMServiceStatusToConfigRepairsProfilesReferencingRemovedHub(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: hubServiceProviderName, URL: "https://hub.example.com/v1", Model: "hub-model"},
			{ID: "custom", Name: "Custom1", URL: "https://custom.example.com/v1", Model: "custom-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "custom", Model: "custom-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "hub", Model: "hub-model"},
		},
	}
	if !app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{Active: false}) {
		t.Fatal("expected removing referenced hub provider to change config")
	}
	if cfg.MaclawLLMProfiles == nil || !cfg.MaclawLLMProfiles.Coding.InheritAssistant {
		t.Fatalf("coding profile = %#v, want follow assistant", cfg.MaclawLLMProfiles)
	}
	if cfg.MaclawLLMProfiles.Coding.ProviderID != "" || cfg.MaclawLLMProfiles.Coding.Model != "" {
		t.Fatalf("following recovery draft = %#v, want cleared", cfg.MaclawLLMProfiles.Coding)
	}
}

func TestApplyHubLLMServiceStatusToConfigKeepsAssistantHubProfileIntact(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: hubServiceProviderName, URL: "https://hub.example.com/v1", Model: "hub-model"},
			{ID: "custom", Name: "Custom1", URL: "https://custom.example.com/v1", Model: "custom-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "hub-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}
	// Normalization may still materialize defaults in a synthetic test config;
	// the invariant is that it cannot delete or alter the active assignment.
	app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{Active: false})
	if _, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName); !ok {
		t.Fatal("Hub provider referenced by assistant profile must remain")
	}
	if got := cfg.MaclawLLMProfiles.Assistant; got.ProviderID != "hub" || got.Model != "hub-model" {
		t.Fatalf("assistant profile = %#v, want unchanged Hub selection", got)
	}
}

func TestApplyHubLLMServiceStatusToConfigKeepsNormalizedAssistantHubCatalog(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			// The legacy alias has surrounding whitespace which normalization trims.
			{ID: "hub", Name: " " + hubServiceProviderName + " ", URL: "https://hub.example.com/v1", Model: "hub-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "hub-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}
	if !app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{Active: false}) {
		t.Fatal("expected legacy Hub provider normalization")
	}
	if len(cfg.MaclawLLMProviders) != 1 || !isHubServiceProviderName(cfg.MaclawLLMProviders[0].Name) {
		t.Fatalf("normalized Hub provider catalog = %#v", cfg.MaclawLLMProviders)
	}
	if cfg.MaclawLLMProfiles.Assistant.ProviderID != "hub" {
		t.Fatalf("assistant profile changed: %#v", cfg.MaclawLLMProfiles.Assistant)
	}
}

func TestApplyHubLLMServiceStatusToConfigPreservesHubProviderID(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: hubServiceProviderName, URL: "https://old.example.com/v1", Model: "hub-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "hub-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}
	if !app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{Active: true, HubLLMBaseURL: "https://hub.example.com/v1"}) {
		t.Fatal("expected Hub connection refresh to update the provider")
	}
	if got := cfg.MaclawLLMProviders[0].ID; got != "hub" {
		t.Fatalf("Hub provider ID = %q, want stable ID hub", got)
	}
}

func TestApplyHubLLMServiceStatusKeepsAssistantCompatibilityProjection(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "custom", Name: "Custom", URL: "https://custom.example.com/v1", Key: "custom-key", Model: "custom-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "custom", Model: "custom-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}
	if !app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{Active: true, HubLLMBaseURL: "https://hub.example.com/v1"}) {
		t.Fatal("expected Hub provider to be added")
	}
	if cfg.MaclawLLMCurrentProvider != "Custom" || cfg.MaclawLLMModel != "custom-model" || cfg.MaclawLLMUrl != "https://custom.example.com/v1" {
		t.Fatalf("Hub sync overwrote assistant compatibility projection: current=%q model=%q url=%q", cfg.MaclawLLMCurrentProvider, cfg.MaclawLLMModel, cfg.MaclawLLMUrl)
	}
}

func TestApplyHubLLMServiceStatusForceCurrentDoesNotOverrideProfiles(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: "Custom",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "custom", Name: "Custom", URL: "https://custom.example.com/v1", Model: "custom-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "custom", Model: "custom-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}
	if !app.applyHubLLMServiceStatusPatchToConfig(cfg, HubLLMServiceStatus{Active: true, HubLLMBaseURL: "https://hub.example.com/v1"}, true) {
		t.Fatal("expected Hub catalog change")
	}
	if cfg.MaclawLLMCurrentProvider != "Custom" {
		t.Fatalf("force sync changed profile-backed current provider to %q", cfg.MaclawLLMCurrentProvider)
	}
}

func TestApplyHubLLMServiceStatusRemovalRestoresAssistantCompatibilityProjection(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName, // stale legacy mirror
		MaclawLLMUrl:             "https://hub.example.com/v1",
		MaclawLLMModel:           "hub-model",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: hubServiceProviderName, URL: "https://hub.example.com/v1", Model: "hub-model"},
			{ID: "custom", Name: "Custom", URL: "https://custom.example.com/v1", Key: "custom-key", Model: "custom-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{Version: 1,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "custom", Model: "custom-model"},
			Coding:    corelib.MaclawLLMProfile{ProviderID: "hub", Model: "hub-model"},
		},
	}
	if !app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{Active: false}) {
		t.Fatal("expected Hub provider removal")
	}
	if cfg.MaclawLLMCurrentProvider != "Custom" || cfg.MaclawLLMModel != "custom-model" || cfg.MaclawLLMUrl != "https://custom.example.com/v1" {
		t.Fatalf("Hub removal did not restore assistant compatibility projection: current=%q model=%q url=%q", cfg.MaclawLLMCurrentProvider, cfg.MaclawLLMModel, cfg.MaclawLLMUrl)
	}
	if !cfg.MaclawLLMProfiles.Coding.InheritAssistant {
		t.Fatalf("coding profile = %#v, want follow assistant after Hub removal", cfg.MaclawLLMProfiles.Coding)
	}
}

func TestSyncHubLLMServiceStatusPatchesWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example.com",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	status := HubLLMServiceStatus{
		Active:        true,
		HubLLMBaseURL: "https://hub.example.com/api/llm/v1",
		ActiveGrants: []HubLLMActiveGrant{{
			ServiceGroupID: "sg1",
			Source:         "card",
			Active:         true,
			Status:         "active",
		}},
	}
	changed, err := app.syncHubLLMServiceStatusToConfig(status, false)
	if err != nil {
		t.Fatalf("syncHubLLMServiceStatusToConfig: %v", err)
	}
	if !changed {
		t.Fatal("expected hub provider to be added")
	}

	// Concurrent user change of current provider should not be clobbered when
	// forceCurrentProvider is false.
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.MaclawLLMCurrentProvider = "Custom1"
	}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}

	changed, err = app.syncHubLLMServiceStatusToConfig(status, false)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	// Second sync may be no-op if provider already present and unchanged.
	_ = changed

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MaclawLLMCurrentProvider != "Custom1" {
		t.Fatalf("current provider = %q, want Custom1 (not force-switched to hub)", cfg.MaclawLLMCurrentProvider)
	}
	if _, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName); !ok {
		t.Fatal("hub service provider should remain after sync")
	}
}
