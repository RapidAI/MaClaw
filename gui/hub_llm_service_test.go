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
