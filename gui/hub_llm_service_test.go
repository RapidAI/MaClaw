package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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

	var enrollCalls atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		enrollCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "approved",
			"user_id":       "u_123",
			"email":         "user@example.com",
			"machine_id":    "m_123",
			"machine_token": "mt_123",
		})
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteEmail:        "user@example.com",
		RemoteMachineID:    "m_existing",
		RemoteMachineToken: "mt_existing",
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

	changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{Active: false})
	if !changed {
		t.Fatal("applyHubLLMServiceStatusToConfig() changed = false, want true")
	}
	if _, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("hub provider still present after unauthorized sync: %+v", cfg.MaclawLLMProviders)
	}
	if cfg.MaclawLLMCurrentProvider != "" {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want empty", cfg.MaclawLLMCurrentProvider)
	}
}

func TestApplyHubLLMServiceStatusToConfig_KeepsProviderWhenPeriodLimited(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: "https://old.example.com/api/llm/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai"},
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}

	changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{
		Active:        false,
		HubLLMBaseURL: "https://hub.example.com/api/llm/v1/",
		CreditGrants: []HubLLMActiveGrant{{
			ServiceGroupID:    "coding-basic",
			Active:            false,
			Status:            "period_limited",
			CreditsTotal:      100,
			CreditsUsed:       10,
			CreditsRemaining:  90,
			RetryAfterSeconds: 3600,
		}},
	})
	if !changed {
		t.Fatal("applyHubLLMServiceStatusToConfig() changed = false, want true because provider URL is refreshed")
	}
	provider, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("hub provider removed while period-limited: %+v", cfg.MaclawLLMProviders)
	}
	if !provider.IsHubService {
		t.Fatal("provider IsHubService = false, want true")
	}
	if provider.URL != "https://hub.example.com/api/llm/v1" {
		t.Fatalf("provider URL = %q, want refreshed hub URL", provider.URL)
	}
	if cfg.MaclawLLMCurrentProvider != hubServiceProviderName {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want %q", cfg.MaclawLLMCurrentProvider, hubServiceProviderName)
	}
}

func TestApplyHubLLMServiceStatusToConfig_CanonicalizesDuplicateHubAliases(t *testing.T) {
	app := &App{}
	mojibakeHubName := "MaClaw\u7039\u6a3b\u67df"
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: mojibakeHubName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: mojibakeHubName, URL: "https://old.example.com/api/llm/v1", Key: "old-token", Model: hubServiceAutoModel, Protocol: "openai"},
			{Name: hubServiceProviderName, URL: "https://hub.example.com/api/llm/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai", ContextLength: corelib.DefaultContextTokens, TimeoutSec: corelib.DefaultLLMTimeoutSec, IsHubService: true},
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}

	changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{
		Active:        true,
		HubLLMBaseURL: "https://hub.example.com/api/llm/v1/",
	})
	if !changed {
		t.Fatal("applyHubLLMServiceStatusToConfig() changed = false, want true because duplicate alias is canonicalized")
	}
	if cfg.MaclawLLMCurrentProvider != hubServiceProviderName {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want %q", cfg.MaclawLLMCurrentProvider, hubServiceProviderName)
	}
	hubCount := 0
	for _, provider := range cfg.MaclawLLMProviders {
		if provider.Name == mojibakeHubName {
			t.Fatalf("providers still contain mojibake hub alias: %+v", cfg.MaclawLLMProviders)
		}
		if provider.Name == hubServiceProviderName {
			hubCount++
		}
	}
	if hubCount != 1 {
		t.Fatalf("hub provider count = %d, want 1; providers=%+v", hubCount, cfg.MaclawLLMProviders)
	}
}

func TestIsMaclawLLMConfiguredWithConfigMatchesHubAliases(t *testing.T) {
	app := &App{}
	mojibakeHubName := "MaClaw\u7039\u6a3b\u67df"
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: mojibakeHubName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: "https://hub.example.com/api/llm/v1", Model: hubServiceAutoModel},
		},
	}

	if !app.isMaclawLLMConfiguredWithConfig(cfg) {
		t.Fatal("isMaclawLLMConfiguredWithConfig() = false, want true for alias current and canonical provider")
	}
}

func TestApplyHubLLMServiceStatusToConfig_KeepsProviderWhenExpired(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: "https://old.example.com/api/llm/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai"},
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}

	changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{
		Active:        false,
		HubLLMBaseURL: "https://hub.example.com/api/llm/v1/",
		CreditGrants: []HubLLMActiveGrant{{
			ServiceGroupID: "coding-basic",
			Active:         false,
			Status:         "expired",
			CreditsTotal:   100,
			CreditsUsed:    10,
		}},
	})
	if !changed {
		t.Fatal("applyHubLLMServiceStatusToConfig() changed = false, want true because provider URL is refreshed")
	}
	provider, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("hub provider removed while expired grant explains status: %+v", cfg.MaclawLLMProviders)
	}
	if !provider.IsHubService {
		t.Fatal("provider IsHubService = false, want true")
	}
}

func TestApplyHubLLMServiceStatusToConfig_AddsProviderWhenAuthorized(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}

	changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{
		Active:        true,
		HubLLMBaseURL: "https://hub.example.com/api/llm/v1/",
	})
	if !changed {
		t.Fatal("applyHubLLMServiceStatusToConfig() changed = false, want true")
	}
	provider, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("hub provider missing after authorized sync: %+v", cfg.MaclawLLMProviders)
	}
	if !provider.IsHubService {
		t.Fatal("provider IsHubService = false, want true")
	}
	if provider.URL != "https://hub.example.com/api/llm/v1" {
		t.Fatalf("provider URL = %q, want %q", provider.URL, "https://hub.example.com/api/llm/v1")
	}
	if provider.Key != "viewer-token" {
		t.Fatalf("provider Key = %q, want %q", provider.Key, "viewer-token")
	}
	if provider.Model != hubServiceAutoModel {
		t.Fatalf("provider Model = %q, want %q", provider.Model, hubServiceAutoModel)
	}
	if provider.Protocol != "openai" {
		t.Fatalf("provider Protocol = %q, want %q", provider.Protocol, "openai")
	}
	if provider.ContextLength != corelib.DefaultContextTokens {
		t.Fatalf("provider ContextLength = %d, want %d", provider.ContextLength, corelib.DefaultContextTokens)
	}
	if provider.TimeoutSec != corelib.DefaultLLMTimeoutSec {
		t.Fatalf("provider TimeoutSec = %d, want %d", provider.TimeoutSec, corelib.DefaultLLMTimeoutSec)
	}
	if cfg.MaclawLLMCurrentProvider != hubServiceProviderName {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want %q", cfg.MaclawLLMCurrentProvider, hubServiceProviderName)
	}
	if cfg.MaclawLLMModel != hubServiceAutoModel {
		t.Fatalf("MaclawLLMModel = %q, want %q", cfg.MaclawLLMModel, hubServiceAutoModel)
	}
}

func TestApplyHubLLMServiceStatusToConfig_PreservesExplicitModelOverride(t *testing.T) {
	app := &App{}
	cfg := &corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{
				Name:          hubServiceProviderName,
				URL:           "https://hub.example.com/api/llm/v1",
				Key:           "viewer-token",
				Model:         "gpt-debug",
				Protocol:      "openai",
				ContextLength: corelib.DefaultContextTokens,
				TimeoutSec:    corelib.DefaultLLMTimeoutSec,
			},
		},
	}

	changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{
		Active:        true,
		HubLLMBaseURL: "https://hub.example.com/api/llm/v1",
	})
	if !changed {
		t.Fatal("applyHubLLMServiceStatusToConfig() changed = false, want true because config mirrors selected provider fields")
	}
	provider, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatal("hub provider missing after authorized sync")
	}
	if provider.Model != "gpt-debug" {
		t.Fatalf("provider Model = %q, want %q", provider.Model, "gpt-debug")
	}
	if cfg.MaclawLLMModel != "gpt-debug" {
		t.Fatalf("MaclawLLMModel = %q, want %q", cfg.MaclawLLMModel, "gpt-debug")
	}
}

func TestApplyHubLLMServiceStatusToConfig_UsesAutoModelWhenExistingModelIsAutoOrEmpty(t *testing.T) {
	app := &App{}
	testCases := []struct {
		name          string
		existingModel string
	}{
		{name: "empty model", existingModel: ""},
		{name: "auto model", existingModel: hubServiceAutoModel},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &corelib.AppConfig{
				RemoteViewerToken: "viewer-token",
				MaclawLLMProviders: []corelib.MaclawLLMProvider{
					{
						Name:          hubServiceProviderName,
						URL:           "https://hub.example.com/api/llm/v1",
						Key:           "viewer-token",
						Model:         tc.existingModel,
						Protocol:      "openai",
						ContextLength: corelib.DefaultContextTokens,
						TimeoutSec:    corelib.DefaultLLMTimeoutSec,
					},
				},
			}

			changed := app.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{
				Active:        true,
				HubLLMBaseURL: "https://hub.example.com/api/llm/v1",
			})
			if !changed {
				t.Fatal("applyHubLLMServiceStatusToConfig() changed = false, want true")
			}
			provider, ok := findProviderByName(cfg.MaclawLLMProviders, hubServiceProviderName)
			if !ok {
				t.Fatal("hub provider missing after authorized sync")
			}
			if provider.Model != hubServiceAutoModel {
				t.Fatalf("provider Model = %q, want %q", provider.Model, hubServiceAutoModel)
			}
			if cfg.MaclawLLMModel != hubServiceAutoModel {
				t.Fatalf("MaclawLLMModel = %q, want %q", cfg.MaclawLLMModel, hubServiceAutoModel)
			}
		})
	}
}

func TestGetMaclawLLMPanelState_RemovesHubProviderWhenAuthorizationRevoked(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/service/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false}`))
	}))
	defer hub.Close()

	cfg := corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: hub.URL + "/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai"},
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	state := app.GetMaclawLLMPanelState()
	if _, ok := findProviderByName(state.Providers, hubServiceProviderName); ok {
		t.Fatalf("panel providers still contain hub provider after revoked auth: %+v", state.Providers)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("saved config still contains hub provider after revoked auth: %+v", saved.MaclawLLMProviders)
	}
}

func TestGetMaclawLLMPanelState_KeepsHubProviderWhenPeriodLimited(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/service/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false,"hub_llm_base_url":"` + hubURL + `/api/llm/v1","credit_grants":[{"service_group_id":"coding-basic","active":false,"status":"period_limited","credits_total":100,"credits_used":10,"credits_remaining":90,"retry_after_seconds":3600}]}`))
	}))
	hubURL = hub.URL
	defer hub.Close()

	cfg := corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: hub.URL + "/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai"},
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	state := app.GetMaclawLLMPanelState()
	provider, ok := findProviderByName(state.Providers, hubServiceProviderName)
	if !ok {
		t.Fatalf("panel providers missing hub provider while period-limited: %+v", state.Providers)
	}
	if provider.URL != hub.URL+"/api/llm/v1" {
		t.Fatalf("provider URL = %q, want %q", provider.URL, hub.URL+"/api/llm/v1")
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); !ok {
		t.Fatalf("saved config lost hub provider while period-limited: %+v", saved.MaclawLLMProviders)
	}
}

func TestSyncHubLLMServiceStatusPatchesWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteEmail = "owner@example.com"
		cfg.RemoteViewerToken = "viewer-token"
		cfg.LogDetailEnabled = true
		cfg.MaclawLLMCurrentProvider = "Custom1"
		cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"}}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	changed, err := app.syncHubLLMServiceStatusToConfig(HubLLMServiceStatus{
		Active:        true,
		HubLLMBaseURL: "https://hub.example.com/api/llm/v1/",
	}, false)
	if err != nil {
		t.Fatalf("syncHubLLMServiceStatusToConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("syncHubLLMServiceStatusToConfig() changed = false, want true")
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.RemoteEmail != "owner@example.com" || !reloaded.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by Hub LLM service sync: %#v", reloaded)
	}
	provider, ok := findProviderByName(reloaded.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("hub provider missing after service sync: %+v", reloaded.MaclawLLMProviders)
	}
	if provider.URL != "https://hub.example.com/api/llm/v1" || provider.Key != "viewer-token" || !provider.IsHubService {
		t.Fatalf("hub provider not normalized/applied: %+v", provider)
	}
	if _, ok := findProviderByName(reloaded.MaclawLLMProviders, "Custom1"); !ok {
		t.Fatalf("custom provider removed by Hub LLM service sync: %+v", reloaded.MaclawLLMProviders)
	}
}

func TestGetMaclawLLMPanelState_AddsHubProviderWhenAuthorized(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/service/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true,"hub_llm_base_url":"` + hubURL + `/api/llm/v1"}`))
	}))
	hubURL = hub.URL
	defer hub.Close()

	cfg := corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	state := app.GetMaclawLLMPanelState()
	provider, ok := findProviderByName(state.Providers, hubServiceProviderName)
	if !ok {
		t.Fatalf("panel providers missing hub provider after authorized status: %+v", state.Providers)
	}
	if !provider.IsHubService {
		t.Fatal("provider IsHubService = false, want true")
	}
	if provider.Model != hubServiceAutoModel {
		t.Fatalf("provider Model = %q, want %q", provider.Model, hubServiceAutoModel)
	}
	if provider.URL != hub.URL+"/api/llm/v1" {
		t.Fatalf("provider URL = %q, want %q", provider.URL, hub.URL+"/api/llm/v1")
	}
}

func TestGetMaclawLLMProviders_RemovesHubProviderWhenAuthorizationRevoked(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/service/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false}`))
	}))
	defer hub.Close()

	cfg := corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: hub.URL + "/v1", Key: "viewer-token", Model: hubServiceAutoModel, Protocol: "openai"},
			{Name: "Custom1", URL: "https://example.com/v1", Model: "gpt-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	state := app.GetMaclawLLMProviders()
	if _, ok := findProviderByName(state.Providers, hubServiceProviderName); ok {
		t.Fatalf("providers still contain hub provider after revoked auth: %+v", state.Providers)
	}
}

func TestGetMaclawLLMProviders_AddsHubProviderWhenAuthorized(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/service/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true,"hub_llm_base_url":"` + hubURL + `/api/llm/v1"}`))
	}))
	hubURL = hub.URL
	defer hub.Close()

	cfg := corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	state := app.GetMaclawLLMProviders()
	provider, ok := findProviderByName(state.Providers, hubServiceProviderName)
	if !ok {
		t.Fatalf("providers missing hub provider after authorized status: %+v", state.Providers)
	}
	if !provider.IsHubService {
		t.Fatal("provider IsHubService = false, want true")
	}
	if provider.Model != hubServiceAutoModel {
		t.Fatalf("provider Model = %q, want %q", provider.Model, hubServiceAutoModel)
	}
	if provider.URL != hub.URL+"/api/llm/v1" {
		t.Fatalf("provider URL = %q, want %q", provider.URL, hub.URL+"/api/llm/v1")
	}
}
