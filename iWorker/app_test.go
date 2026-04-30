package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func setTestHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestLoadTaskHistoryReturnsEmptyWhenFileMissing(t *testing.T) {
	setTestHome(t)

	app := NewApp()
	items, err := app.LoadTaskHistory()
	if err != nil {
		t.Fatalf("LoadTaskHistory returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("LoadTaskHistory returned %d items, want 0", len(items))
	}
}

func TestSaveTaskHistoryRoundTrip(t *testing.T) {
	home := setTestHome(t)

	app := NewApp()
	want := []HistoryTaskItem{
		{
			ID:             "task-1",
			Title:          "daily report",
			Owner:          "xiao-di",
			Status:         "completed",
			UpdatedAt:      "04-07 09:30",
			Description:    "prepare today report",
			Draft:          "prepare a report from raw notes",
			ExpectedOutput: "summary",
			Result:         "report generated",
			Model:          "test-model",
		},
	}

	if err := app.SaveTaskHistory(want); err != nil {
		t.Fatalf("SaveTaskHistory returned error: %v", err)
	}

	path := filepath.Join(home, ".iworker", "task_history.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("task history file not written: %v", err)
	}

	got, err := app.LoadTaskHistory()
	if err != nil {
		t.Fatalf("LoadTaskHistory returned error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("LoadTaskHistory returned %d items, want %d", len(got), len(want))
	}

	if got[0] != want[0] {
		t.Fatalf("LoadTaskHistory returned %+v, want %+v", got[0], want[0])
	}
}

func TestLoadDiWorkerSettingsReturnsDefaultsWhenFileMissing(t *testing.T) {
	setTestHome(t)

	app := NewApp()
	got, err := app.LoadDiWorkerSettings()
	if err != nil {
		t.Fatalf("LoadDiWorkerSettings returned error: %v", err)
	}

	want := defaultDiWorkerSettings()
	if got.RoleProfile != want.RoleProfile {
		t.Fatalf("RoleProfile = %+v, want %+v", got.RoleProfile, want.RoleProfile)
	}
	if got.Center != want.Center {
		t.Fatalf("Center = %+v, want %+v", got.Center, want.Center)
	}
	if got.Routing != want.Routing {
		t.Fatalf("Routing = %+v, want %+v", got.Routing, want.Routing)
	}
	if len(got.Providers) != len(want.Providers) {
		t.Fatalf("Providers len = %d, want %d", len(got.Providers), len(want.Providers))
	}
}

func TestSaveDiWorkerSettingsRoundTrip(t *testing.T) {
	home := setTestHome(t)

	app := NewApp()
	want := DiWorkerSettings{
		RoleProfile: RoleProfile{
			Name:        "A Ning Assistant",
			Description: "Responsible for data cleaning, summaries, and analysis.",
		},
		Center: CenterConfig{
			Enabled:                    true,
			Host:                       "10.0.0.8",
			Port:                       9377,
			BaseURL:                    "http://10.0.0.8:9377",
			TenantID:                   "default",
			DepartmentID:               "default",
			WorkerID:                   "local-iworker",
			TimeoutSec:                 45,
			GoalWatchAutoHandleEnabled: true,
			GoalWatchIntervalSec:       30,
			GoalWatchMaxDurationSec:    120,
		},
		Routing: RoutingPolicy{
			Mode:            "priority",
			DefaultProvider: "analysis-anthropic",
			AllowFallback:   true,
		},
		Providers: []UpstreamProvider{
			{
				ID:          "analysis-anthropic",
				Name:        "Analysis service",
				Enabled:     true,
				Protocol:    "anthropic",
				BaseURL:     "https://analysis.example.com",
				APIKey:      "token-a",
				Model:       "claude-sonnet-4-6",
				Priority:    90,
				Features:    []string{"analysis", "root-cause"},
				Description: "For exception analysis",
				Capabilities: ProviderCapabilities{
					SupportsStream: true,
					SupportsVision: false,
					MaxContext:     110000,
				},
			},
		},
	}

	if err := app.SaveDiWorkerSettings(want); err != nil {
		t.Fatalf("SaveDiWorkerSettings returned error: %v", err)
	}

	path := filepath.Join(home, ".iworker", "settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}
	centerPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if _, err := os.Stat(centerPath); err != nil {
		t.Fatalf("center settings file not written: %v", err)
	}

	got, err := app.LoadDiWorkerSettings()
	if err != nil {
		t.Fatalf("LoadDiWorkerSettings returned error: %v", err)
	}

	if got.RoleProfile != want.RoleProfile {
		t.Fatalf("RoleProfile = %+v, want %+v", got.RoleProfile, want.RoleProfile)
	}
	if got.Center != want.Center {
		t.Fatalf("Center = %+v, want %+v", got.Center, want.Center)
	}
	if got.Routing != want.Routing {
		t.Fatalf("Routing = %+v, want %+v", got.Routing, want.Routing)
	}
	if len(got.Providers) != 1 {
		t.Fatalf("Providers len = %d, want 1", len(got.Providers))
	}
	if got.Providers[0].ID != want.Providers[0].ID || got.Providers[0].APIKey != want.Providers[0].APIKey {
		t.Fatalf("Provider = %+v, want %+v", got.Providers[0], want.Providers[0])
	}

	centerData, err := os.ReadFile(centerPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var centerSettings struct {
		Providers []struct {
			ID         string `json:"id"`
			Protocol   string `json:"protocol"`
			BaseURL    string `json:"base_url"`
			TimeoutSec int    `json:"timeout_sec"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(centerData, &centerSettings); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(centerSettings.Providers) != 1 {
		t.Fatalf("center providers len = %d, want 1", len(centerSettings.Providers))
	}
	if centerSettings.Providers[0].ID != "analysis-anthropic" || centerSettings.Providers[0].Protocol != "anthropic" {
		t.Fatalf("center provider = %+v, want synced provider", centerSettings.Providers[0])
	}
	if centerSettings.Providers[0].TimeoutSec != 45 {
		t.Fatalf("center timeout = %d, want 45", centerSettings.Providers[0].TimeoutSec)
	}
}

func TestLoadDiWorkerSettingsNormalizesMissingFields(t *testing.T) {
	home := setTestHome(t)
	path := filepath.Join(home, ".iworker", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	data := []byte(`{
  "role_profile": {},
  "center": {
    "enabled": true
  },
  "routing": {},
  "providers": [
    {
      "id": "custom",
      "name": "custom service",
      "enabled": true,
      "base_url": "http://127.0.0.1:9000",
      "api_key": "",
      "model": "custom-model",
      "priority": 10,
      "features": null,
      "description": "",
      "capabilities": {}
    }
  ]
}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	app := NewApp()
	got, err := app.LoadDiWorkerSettings()
	if err != nil {
		t.Fatalf("LoadDiWorkerSettings returned error: %v", err)
	}

	if got.RoleProfile.Name == "" || got.Center.Host == "" || got.Center.BaseURL == "" {
		t.Fatalf("normalized settings missing defaults: %+v", got)
	}
	if got.Routing.Mode == "" || got.Routing.DefaultProvider == "" {
		t.Fatalf("normalized routing missing defaults: %+v", got.Routing)
	}
	if got.Providers[0].Protocol != "openai" {
		t.Fatalf("Protocol = %q, want openai", got.Providers[0].Protocol)
	}
	if got.Providers[0].Features == nil {
		t.Fatalf("Features should be normalized to empty slice")
	}
	if got.Providers[0].Capabilities.MaxContext <= 0 {
		t.Fatalf("MaxContext should be normalized, got %d", got.Providers[0].Capabilities.MaxContext)
	}
}

func TestCenterLLMConfigUsesCenterSettings(t *testing.T) {
	settings := DiWorkerSettings{
		Center: CenterConfig{
			Enabled:    true,
			BaseURL:    "http://127.0.0.1:9377",
			TimeoutSec: 33,
		},
		Routing: RoutingPolicy{
			DefaultProvider: "analysis-anthropic",
		},
		Providers: []UpstreamProvider{
			{ID: "analysis-anthropic", Enabled: true, APIKey: "center-token"},
		},
	}

	cfg, ok := centerLLMConfig(settings)
	if !ok {
		t.Fatalf("centerLLMConfig returned ok=false")
	}
	if cfg.URL != "http://127.0.0.1:9377" {
		t.Fatalf("URL = %q, want http://127.0.0.1:9377", cfg.URL)
	}
	if cfg.Model != "analysis-anthropic" {
		t.Fatalf("Model = %q, want analysis-anthropic", cfg.Model)
	}
	if cfg.Protocol != "openai" {
		t.Fatalf("Protocol = %q, want openai", cfg.Protocol)
	}
	if cfg.Key != "center-token" {
		t.Fatalf("Key = %q, want center-token", cfg.Key)
	}
	if cfg.TimeoutSec != 33 {
		t.Fatalf("TimeoutSec = %d, want 33", cfg.TimeoutSec)
	}
}

func TestCenterLLMConfigBuildsBaseURLFromHostPort(t *testing.T) {
	settings := DiWorkerSettings{
		Center: CenterConfig{
			Enabled: true,
			Host:    "10.0.0.8",
			Port:    9001,
		},
		Providers: []UpstreamProvider{
			{ID: "office-openai", Enabled: true},
		},
	}

	cfg, ok := centerLLMConfig(settings)
	if !ok {
		t.Fatalf("centerLLMConfig returned ok=false")
	}
	if cfg.URL != "http://10.0.0.8:9001" {
		t.Fatalf("URL = %q, want http://10.0.0.8:9001", cfg.URL)
	}
	if cfg.Model != "office-openai" {
		t.Fatalf("Model = %q, want office-openai", cfg.Model)
	}
}

func TestCenterLLMConfigDisabledReturnsFalse(t *testing.T) {
	cfg, ok := centerLLMConfig(defaultDiWorkerSettings())
	if ok {
		t.Fatalf("centerLLMConfig returned cfg=%+v, want disabled", cfg)
	}
}

func TestLoadSubmitLLMConfigsReturnsCenterAndFallback(t *testing.T) {
	home := setTestHome(t)
	settings := DiWorkerSettings{
		Center: CenterConfig{
			Enabled:    true,
			BaseURL:    "http://127.0.0.1:9377",
			TimeoutSec: 40,
		},
		Routing: RoutingPolicy{
			DefaultProvider: "analysis-anthropic",
		},
		Providers: []UpstreamProvider{{ID: "analysis-anthropic", Enabled: true, APIKey: "center-token"}},
	}
	if err := writeDiWorkerSettings(settings); err != nil {
		t.Fatalf("writeDiWorkerSettings returned error: %v", err)
	}

	configPath := filepath.Join(home, ".maclaw", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	configData := []byte(`{"maclaw_llm_url":"http://127.0.0.1:9000","maclaw_llm_key":"fallback-key","maclaw_llm_model":"fallback-model","maclaw_llm_protocol":"openai","maclaw_llm_timeout_sec":25}`)
	if err := os.WriteFile(configPath, configData, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	primaryCfg, fallbackCfg, err := loadSubmitLLMConfigs()
	if err != nil {
		t.Fatalf("loadSubmitLLMConfigs returned error: %v", err)
	}
	if primaryCfg.URL != "http://127.0.0.1:9377" || primaryCfg.Model != "analysis-anthropic" {
		t.Fatalf("primaryCfg = %+v, want center config", primaryCfg)
	}
	if fallbackCfg == nil {
		t.Fatalf("fallbackCfg is nil")
	}
	if fallbackCfg.URL != "http://127.0.0.1:9000" || fallbackCfg.Model != "fallback-model" {
		t.Fatalf("fallbackCfg = %+v, want maclaw config", *fallbackCfg)
	}
}

func TestSubmitTaskWithFallbackUsesSecondConfig(t *testing.T) {
	original := doSimpleLLMRequest
	defer func() { doSimpleLLMRequest = original }()

	calls := make([]string, 0, 2)
	doSimpleLLMRequest = func(cfg corelib.MaclawLLMConfig, _ []interface{}, _ *http.Client, _ time.Duration) (*agent.LLMSimpleResponse, error) {
		calls = append(calls, cfg.Model)
		if cfg.Model == "center-model" {
			return nil, errors.New("center unavailable")
		}
		return &agent.LLMSimpleResponse{Content: "fallback result"}, nil
	}

	resp, usedCfg, err := submitTaskWithFallback(
		[]interface{}{map[string]string{"role": "user", "content": "test"}},
		corelib.MaclawLLMConfig{URL: "http://127.0.0.1:9377", Model: "center-model", TimeoutSec: 10},
		&corelib.MaclawLLMConfig{URL: "http://127.0.0.1:9000", Model: "fallback-model", TimeoutSec: 10},
	)
	if err != nil {
		t.Fatalf("submitTaskWithFallback returned error: %v", err)
	}
	if resp == nil || resp.Content != "fallback result" {
		t.Fatalf("resp = %+v, want fallback result", resp)
	}
	if usedCfg.Model != "fallback-model" {
		t.Fatalf("usedCfg.Model = %q, want fallback-model", usedCfg.Model)
	}
	if len(calls) != 2 || calls[0] != "center-model" || calls[1] != "fallback-model" {
		t.Fatalf("calls = %v, want [center-model fallback-model]", calls)
	}
}

func TestSubmitTaskWithFallbackReturnsCombinedError(t *testing.T) {
	original := doSimpleLLMRequest
	defer func() { doSimpleLLMRequest = original }()

	doSimpleLLMRequest = func(cfg corelib.MaclawLLMConfig, _ []interface{}, _ *http.Client, _ time.Duration) (*agent.LLMSimpleResponse, error) {
		return nil, errors.New(cfg.Model + " failed")
	}

	_, _, err := submitTaskWithFallback(
		[]interface{}{map[string]string{"role": "user", "content": "test"}},
		corelib.MaclawLLMConfig{URL: "http://127.0.0.1:9377", Model: "center-model", TimeoutSec: 10},
		&corelib.MaclawLLMConfig{URL: "http://127.0.0.1:9000", Model: "fallback-model", TimeoutSec: 10},
	)
	if err == nil {
		t.Fatalf("submitTaskWithFallback returned nil error")
	}
	if !strings.Contains(err.Error(), "center failed") || !strings.Contains(err.Error(), "fallback failed") {
		t.Fatalf("error = %v, want combined failure", err)
	}
}

func TestCheckCenterHealthReturnsSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","provider_count":3,"config_path":"/tmp/center.json"}`))
	}))
	defer server.Close()

	status, err := checkCenterHealth(DiWorkerSettings{
		Center: CenterConfig{
			Enabled:    true,
			BaseURL:    server.URL,
			TimeoutSec: 5,
		},
	})
	if err != nil {
		t.Fatalf("checkCenterHealth returned error: %v", err)
	}
	if !status.Reachable {
		t.Fatalf("Reachable = false, want true")
	}
	if status.Status != "ok" || status.ProviderCount != 3 {
		t.Fatalf("status = %+v, want health snapshot", status)
	}
	if status.ConfigPath != "/tmp/center.json" {
		t.Fatalf("ConfigPath = %q, want /tmp/center.json", status.ConfigPath)
	}
	if status.ResolvedBaseURL != server.URL {
		t.Fatalf("ResolvedBaseURL = %q, want %q", status.ResolvedBaseURL, server.URL)
	}
}

func TestCheckCenterHealthReturnsUnreachableState(t *testing.T) {
	status, err := checkCenterHealth(DiWorkerSettings{
		Center: CenterConfig{
			Enabled:    true,
			BaseURL:    "http://127.0.0.1:1",
			TimeoutSec: 1,
		},
	})
	if err != nil {
		t.Fatalf("checkCenterHealth returned error: %v", err)
	}
	if status.Reachable {
		t.Fatalf("Reachable = true, want false")
	}
	if status.ResolvedBaseURL != "http://127.0.0.1:1" {
		t.Fatalf("ResolvedBaseURL = %q, want http://127.0.0.1:1", status.ResolvedBaseURL)
	}
	if strings.TrimSpace(status.Message) == "" {
		t.Fatalf("Message should not be empty")
	}
}

func TestSaveWorkerMemoryPersistsToCenterBeforeCache(t *testing.T) {
	home := setTestHome(t)
	var received struct {
		WorkerID string   `json:"worker_id"`
		Content  string   `json:"content"`
		Tags     []string `json:"tags"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/iworker/memories" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"mem-1","worker_id":"worker-a","content":"Remember customer Alpha","category":"project_knowledge","tags":["alpha"],"source_type":"iworker"}`))
	}))
	defer server.Close()

	saved, err := saveWorkerMemory(server.URL, "tenant-a", "sales", "worker-a", SaveWorkerMemoryRequest{
		Content:  "Remember customer Alpha",
		Category: "project_knowledge",
		Tags:     []string{"alpha"},
	}, 5)
	if err != nil {
		t.Fatalf("saveWorkerMemory returned error: %v", err)
	}
	if saved.ID != "mem-1" || received.WorkerID != "worker-a" {
		t.Fatalf("saved=%+v received=%+v", saved, received)
	}
	cachePath := filepath.Join(home, ".iworker", "cache", "memories", "tenant-a__sales__worker-a.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache not written after center success: %v", err)
	}
	if !strings.Contains(string(data), "Remember customer Alpha") {
		t.Fatalf("cache data = %s", string(data))
	}
}

func TestSaveWorkerMemoryDoesNotCacheWhenCenterFails(t *testing.T) {
	home := setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer server.Close()

	_, err := saveWorkerMemory(server.URL, "tenant-a", "sales", "worker-a", SaveWorkerMemoryRequest{Content: "must stay remote first"}, 5)
	if err == nil {
		t.Fatalf("saveWorkerMemory returned nil error")
	}
	cachePath := filepath.Join(home, ".iworker", "cache", "memories", "tenant-a__sales__worker-a.json")
	if _, statErr := os.Stat(cachePath); !os.IsNotExist(statErr) {
		t.Fatalf("cache should not exist after failed center save, statErr=%v", statErr)
	}
}

func TestFetchWorkerMemoriesUsesTenantDepartmentCache(t *testing.T) {
	setTestHome(t)
	if err := writeWorkerMemoryCache("tenant-a", "sales", "worker-a", []WorkerMemoryEntry{{
		ID:       "mem-sales",
		TenantID: "tenant-a",
		Scope:    "personal",
		WorkerID: "worker-a",
		Content:  "sales cache",
	}}); err != nil {
		t.Fatalf("write sales cache: %v", err)
	}
	if err := writeWorkerMemoryCache("tenant-b", "sales", "worker-a", []WorkerMemoryEntry{{
		ID:       "mem-other-tenant",
		TenantID: "tenant-b",
		Scope:    "personal",
		WorkerID: "worker-a",
		Content:  "other tenant cache",
	}}); err != nil {
		t.Fatalf("write other tenant cache: %v", err)
	}

	memories := fetchWorkerMemories("http://127.0.0.1:1", "tenant-a", "sales", "worker-a", "anything", 10, 1)
	if len(memories) != 1 {
		t.Fatalf("memories len = %d, want 1", len(memories))
	}
	if memories[0].ID != "mem-sales" || strings.Contains(memories[0].Content, "other tenant") {
		t.Fatalf("unexpected cache memory: %+v", memories[0])
	}
}

func TestFetchWorkerMemoriesSendsTenantHeader(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/iworker/memories" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant-a" {
			t.Fatalf("tenant_id = %q, want tenant-a", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"memories":[{"id":"mem-1","tenant_id":"tenant-a","department_id":"sales","worker_id":"worker-a","scope":"personal","content":"remote memory","category":"project_knowledge","tags":[]}]}`))
	}))
	defer server.Close()

	memories := fetchWorkerMemories(server.URL, "tenant-a", "sales", "worker-a", "remote", 10, 5)
	if len(memories) != 1 || memories[0].ID != "mem-1" {
		t.Fatalf("unexpected memories: %+v", memories)
	}
}

func TestFetchWorkerMemoryStatsUsesCenterContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/iworker/memory-stats" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant-a" {
			t.Fatalf("tenant_id = %q, want tenant-a", got)
		}
		if got := r.URL.Query().Get("department_id"); got != "sales" {
			t.Fatalf("department_id = %q, want sales", got)
		}
		if got := r.URL.Query().Get("worker_id"); got != "worker-a" {
			t.Fatalf("worker_id = %q, want worker-a", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_id":"tenant-a","department_id":"sales","worker_id":"worker-a","total":3,"by_scope":{"company":1,"department":1,"personal":1},"by_category":{"project_knowledge":3},"visible_scopes":["company","department","personal"]}`))
	}))
	defer server.Close()

	stats, err := fetchWorkerMemoryStats(server.URL, "tenant-a", "sales", "worker-a", 5)
	if err != nil {
		t.Fatalf("fetchWorkerMemoryStats returned error: %v", err)
	}
	if stats.Total != 3 || stats.ByScope["company"] != 1 || stats.ByScope["department"] != 1 || stats.ByScope["personal"] != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestDeleteWorkerMemorySendsCenterContextAndClearsCache(t *testing.T) {
	setTestHome(t)

	var gotMethod, gotPath, gotTenant, gotTenantHeader, gotDepartment, gotWorker string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotTenant = r.URL.Query().Get("tenant_id")
		gotTenantHeader = r.Header.Get("X-Tenant-ID")
		gotDepartment = r.URL.Query().Get("department_id")
		gotWorker = r.URL.Query().Get("worker_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"deleted"}`))
	}))
	defer server.Close()

	if err := writeWorkerMemoryCache("acme", "ops", "worker-1", []WorkerMemoryEntry{
		{ID: "mem-1", Scope: "personal", Content: "remove me"},
		{ID: "mem-2", Scope: "personal", Content: "keep me"},
	}); err != nil {
		t.Fatalf("writeWorkerMemoryCache returned error: %v", err)
	}

	if err := deleteWorkerMemory(server.URL, "acme", "ops", "worker-1", "mem-1", 5); err != nil {
		t.Fatalf("deleteWorkerMemory returned error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/client/iworker/memories/mem-1" {
		t.Fatalf("path = %q, want memory delete path", gotPath)
	}
	if gotTenant != "acme" || gotTenantHeader != "acme" || gotDepartment != "ops" || gotWorker != "worker-1" {
		t.Fatalf("context = %q header=%q/%q/%q, want acme/acme/ops/worker-1", gotTenant, gotTenantHeader, gotDepartment, gotWorker)
	}
	cached := readWorkerMemoryCache("acme", "ops", "worker-1")
	if len(cached) != 1 || cached[0].ID != "mem-2" {
		t.Fatalf("cache = %+v, want only mem-2", cached)
	}
}

func TestFetchSharedMemoriesSendsTenantHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/memories" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		if got := r.URL.Query().Get("role_code"); got != "quality" {
			t.Fatalf("role_code = %q, want quality", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"memories":[{"id":"mem-1","title":"Quality rule","content":"Review defects weekly","level":"role","scope":"quality","tags":[]}]}`))
	}))
	defer server.Close()

	memories := fetchSharedMemories(server.URL, "tenant-a", "quality", 5)
	if len(memories) != 1 || memories[0].ID != "mem-1" {
		t.Fatalf("unexpected memories: %+v", memories)
	}
}

func TestCenterOrganizationClientsSendTenantHeader(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("%s X-Tenant-ID = %q, want tenant-a", r.URL.Path, got)
		}
		seen[r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/colleagues":
			_, _ = w.Write([]byte(`{"colleagues":[{"id":"worker-a","name":"Worker A"}]}`))
		case "/client/roles":
			_, _ = w.Write([]byte(`{"roles":[{"id":"role-a","name":"Role A","code":"role_a"}]}`))
		case "/client/capabilities":
			if got := r.URL.Query().Get("colleague_id"); got != "worker-a" {
				t.Fatalf("colleague_id = %q, want worker-a", got)
			}
			_, _ = w.Write([]byte(`{"capabilities":[{"id":"cap-a","name":"Capability A"}]}`))
		case "/client/collaborations":
			_, _ = w.Write([]byte(`{"tasks":[{"id":"task-a","title":"Task A"}]}`))
		case "/client/workflow-instances":
			_, _ = w.Write([]byte(`{"instances":[{"id":"wf-a","title":"Workflow A"}]}`))
		case "/client/recommend":
			if r.Method != http.MethodPost {
				t.Fatalf("recommend method = %s, want POST", r.Method)
			}
			_, _ = w.Write([]byte(`{"recommendations":[{"colleague_id":"worker-a","name":"Worker A","role_code":"role_a","score":0.9,"reason":"fit"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	if got := fetchCenterColleagues(server.URL, "tenant-a", 5); len(got) != 1 {
		t.Fatalf("colleagues = %+v", got)
	}
	if got := fetchCenterRoles(server.URL, "tenant-a", 5); len(got) != 1 {
		t.Fatalf("roles = %+v", got)
	}
	if got := fetchCenterCapabilities(server.URL, "tenant-a", "worker-a", 5); len(got) != 1 {
		t.Fatalf("capabilities = %+v", got)
	}
	if got := fetchCenterCollaborations(server.URL, "tenant-a", "worker-a", 5); len(got) != 1 {
		t.Fatalf("collaborations = %+v", got)
	}
	if got := fetchCenterWorkflowInstances(server.URL, "tenant-a", 5); len(got) != 1 {
		t.Fatalf("workflow instances = %+v", got)
	}
	if got := fetchRecommendations(server.URL, "tenant-a", "assign task", 1, 5); len(got) != 1 {
		t.Fatalf("recommendations = %+v", got)
	}
	for _, path := range []string{"/client/colleagues", "/client/roles", "/client/capabilities", "/client/collaborations", "/client/workflow-instances", "/client/recommend"} {
		if !seen[path] {
			t.Fatalf("path %s was not requested", path)
		}
	}
}

func TestStartAgentRuntimeHeartbeatStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	beats := make(chan struct{}, 4)
	startAgentRuntimeHeartbeat(ctx, 10*time.Millisecond, func() { beats <- struct{}{} })

	select {
	case <-beats:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("heartbeat did not run")
	}
	cancel()
	countAfterCancel := len(beats)
	time.Sleep(40 * time.Millisecond)
	if len(beats) > countAfterCancel+1 {
		t.Fatalf("heartbeat continued after cancel")
	}
}

func TestNormalizeGoalWatchAutoHandleSettings(t *testing.T) {
	settings := normalizeDiWorkerSettings(DiWorkerSettings{})
	if !settings.Center.GoalWatchAutoHandleEnabled {
		t.Fatalf("GoalWatchAutoHandleEnabled should default to true")
	}
	if settings.Center.GoalWatchIntervalSec != 30 || settings.Center.GoalWatchMaxDurationSec != 120 {
		t.Fatalf("goalwatch timing = %d/%d, want 30/120", settings.Center.GoalWatchIntervalSec, settings.Center.GoalWatchMaxDurationSec)
	}

	settings = normalizeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{GoalWatchAutoHandleEnabled: false, GoalWatchIntervalSec: 45, GoalWatchMaxDurationSec: 10}})
	if settings.Center.GoalWatchAutoHandleEnabled {
		t.Fatalf("explicit disabled auto handle should be preserved")
	}
	if settings.Center.GoalWatchMaxDurationSec != 45 {
		t.Fatalf("max duration = %d, want clamped to interval 45", settings.Center.GoalWatchMaxDurationSec)
	}
}

func TestGoalWatchAutoHandleConfigRequiresCenterAndSetting(t *testing.T) {
	enabled, interval, maxDuration := goalWatchAutoHandleConfig(DiWorkerSettings{Center: CenterConfig{Enabled: true, GoalWatchAutoHandleEnabled: true, GoalWatchIntervalSec: 15, GoalWatchMaxDurationSec: 40}})
	if !enabled || interval != 15*time.Second || maxDuration != 40*time.Second {
		t.Fatalf("config = %v %s %s, want enabled 15s 40s", enabled, interval, maxDuration)
	}

	enabled, _, _ = goalWatchAutoHandleConfig(DiWorkerSettings{Center: CenterConfig{Enabled: true, GoalWatchAutoHandleEnabled: false, GoalWatchIntervalSec: 15, GoalWatchMaxDurationSec: 40}})
	if enabled {
		t.Fatalf("disabled auto handle setting should stop watcher")
	}
}

func TestSubmitTaskSyncsCompletedTaskToCenter(t *testing.T) {
	setTestHome(t)
	original := doSimpleLLMRequest
	defer func() { doSimpleLLMRequest = original }()
	doSimpleLLMRequest = func(cfg corelib.MaclawLLMConfig, messages []interface{}, _ *http.Client, _ time.Duration) (*agent.LLMSimpleResponse, error) {
		if cfg.Model != "office-openai" {
			t.Fatalf("model = %q, want office-openai", cfg.Model)
		}
		if len(messages) != 2 {
			t.Fatalf("messages len = %d, want 2", len(messages))
		}
		return &agent.LLMSimpleResponse{Content: "completed operating brief"}, nil
	}

	var created struct {
		Title           string `json:"title"`
		FromColleagueID string `json:"from_colleague_id"`
		ToColleagueID   string `json:"to_colleague_id"`
		SourceType      string `json:"source_type"`
	}
	var completed struct {
		ActorID string `json:"actor_id"`
		Result  string `json:"result"`
		Note    string `json:"note"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("%s X-Tenant-ID = %q, want tenant-a", r.URL.Path, got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/colleagues":
			_ = json.NewEncoder(w).Encode(map[string]any{"colleagues": []map[string]any{{"id": "worker-a", "name": "Worker A", "role_code": "ops"}}})
		case "/client/recommend":
			_ = json.NewEncoder(w).Encode(map[string]any{"recommendations": []map[string]any{{"colleague_id": "worker-a", "name": "Worker A", "role_code": "ops", "score": 0.9}}})
		case "/client/memories":
			_ = json.NewEncoder(w).Encode(map[string]any{"memories": []any{}})
		case "/client/iworker/memories":
			_ = json.NewEncoder(w).Encode(map[string]any{"memories": []any{}})
		case "/runtime/collaboration/create":
			if r.Method != http.MethodPost {
				t.Fatalf("create method = %s, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-1", "title": created.Title, "to_colleague_id": created.ToColleagueID, "status": "pending"})
		case "/runtime/collaboration/task-1/complete":
			if r.Method != http.MethodPost {
				t.Fatalf("complete method = %s, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&completed); err != nil {
				t.Fatalf("decode complete: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	if err := writeDiWorkerSettings(DiWorkerSettings{
		RoleProfile: RoleProfile{Name: "Worker A", Description: "Ops worker"},
		Center:      CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "ops", WorkerID: "worker-a", TimeoutSec: 5},
		Routing:     RoutingPolicy{DefaultProvider: "office-openai"},
		Providers:   []UpstreamProvider{{ID: "office-openai", Enabled: true, APIKey: "token"}},
	}); err != nil {
		t.Fatalf("writeDiWorkerSettings: %v", err)
	}

	result, err := NewApp().SubmitTask(SubmitTaskRequest{TaskType: "brief", Draft: "Prepare daily operating brief", ExpectedOutput: "summary"})
	if err != nil {
		t.Fatalf("SubmitTask returned error: %v", err)
	}
	if result.Content != "completed operating brief" || result.CenterTaskID != "task-1" || result.CenterTaskStatus != "completed" || result.CenterTaskSyncError != "" {
		t.Fatalf("unexpected submit result: %+v", result)
	}
	if created.ToColleagueID != "worker-a" || created.FromColleagueID != "human_operator" || created.SourceType != "human_iworker_interaction" {
		t.Fatalf("unexpected created task: %+v", created)
	}
	if completed.ActorID != "worker-a" || completed.Result != "completed operating brief" || completed.Note != "completed_by_iworker_submit_task" {
		t.Fatalf("unexpected completion: %+v", completed)
	}
}
