package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestWriteJSONFileDurableReplacesExistingJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "snapshot.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := writeJSONFileDurable(path, map[string]any{"new": true, "count": 2}); err != nil {
		t.Fatalf("writeJSONFileDurable returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("cache file is not valid JSON: %v", err)
	}
	if got["new"] != true || got["old"] != nil || got["count"].(float64) != 2 {
		t.Fatalf("cache JSON = %+v", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "snapshot.json.*.tmp"))
	if err != nil {
		t.Fatalf("Glob returned error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
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
			ID:                     "task-1",
			Title:                  "daily report",
			Owner:                  "xiao-di",
			Status:                 "completed",
			UpdatedAt:              "04-07 09:30",
			Description:            "prepare today report",
			Draft:                  "prepare a report from raw notes",
			ExpectedOutput:         "summary",
			Result:                 "report generated",
			Model:                  "test-model",
			SourceType:             "workflow_handoff",
			CenterHandoffID:        "collab-submit",
			WorkflowStepInstanceID: "wf-step-submit",
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

func TestSaveTaskHistorySendsWorkStatusHeartbeat(t *testing.T) {
	setTestHome(t)
	seenHeartbeat := make(chan CenterAgentInstanceHeartbeatRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/iworker/instances/heartbeat" {
			http.NotFound(w, r)
			return
		}
		var req CenterAgentInstanceHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode heartbeat: %v", err)
		}
		seenHeartbeat <- req
		_ = json.NewEncoder(w).Encode(CenterAgentInstanceHeartbeatResult{Instance: CenterAgentInstance{TenantID: r.Header.Get("X-Tenant-ID"), WorkerID: req.WorkerID, InstanceID: req.InstanceID, Role: req.Role, Status: req.Status, WorkStatus: req.WorkStatus}})
	}))
	defer server.Close()

	if err := writeDiWorkerSettings(DiWorkerSettings{
		RoleProfile: RoleProfile{Name: "Ops iWorker", Description: "Operations"},
		Center:      CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "ops", WorkerID: "worker-a", TimeoutSec: 5},
	}); err != nil {
		t.Fatalf("writeDiWorkerSettings: %v", err)
	}

	app := NewApp()
	if err := app.SaveTaskHistory([]HistoryTaskItem{{ID: "task-1", Title: "Review invoice exception", Owner: "Ops iWorker", Status: "needs_review", UpdatedAt: "now"}}); err != nil {
		t.Fatalf("SaveTaskHistory returned error: %v", err)
	}

	select {
	case heartbeat := <-seenHeartbeat:
		if heartbeat.WorkerID != "worker-a" || heartbeat.WorkStatus == nil || heartbeat.WorkStatus.ReviewCount != 1 || heartbeat.WorkStatus.ActiveCount != 0 || heartbeat.WorkStatus.CurrentTask != "Review invoice exception" {
			t.Fatalf("heartbeat = %+v", heartbeat)
		}
	case <-time.After(time.Second):
		t.Fatal("SaveTaskHistory did not send work status heartbeat")
	}
}

func TestHeartbeatAgentRuntimeSurfacesRuntimeSkillError(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []any{}})
		case "/client/mcp-servers":
			_ = json.NewEncoder(w).Encode(map[string]any{"mcp_servers": []any{}})
		case "/runtime/iworker/instances/heartbeat":
			var req CenterAgentInstanceHeartbeatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			_ = json.NewEncoder(w).Encode(CenterAgentInstanceHeartbeatResult{
				Instance:          CenterAgentInstance{TenantID: r.Header.Get("X-Tenant-ID"), WorkerID: req.WorkerID, InstanceID: req.InstanceID, Role: req.Role, Status: req.Status},
				RuntimeSkillError: "decode installed runtime entry for cap-bad: invalid character",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{
		RoleProfile: RoleProfile{Name: "Ops iWorker", Description: "Operations"},
		Center:      CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "ops", WorkerID: "worker-a", TimeoutSec: 5},
	}); err != nil {
		t.Fatalf("writeDiWorkerSettings: %v", err)
	}

	instances, err := NewApp().HeartbeatAgentRuntime()
	if err == nil || !strings.Contains(err.Error(), "runtime skill sync failed") {
		t.Fatalf("HeartbeatAgentRuntime err = %v, want runtime skill sync failure", err)
	}
	if len(instances) == 0 || !strings.Contains(instances[0].RuntimeSkillError, "cap-bad") {
		t.Fatalf("instances = %+v, want runtime skill error on returned instance", instances)
	}
	cached, ok := readAgentInstancesCache("tenant-a", "ops", "worker-a")
	if !ok || len(cached) == 0 || !strings.Contains(cached[0].RuntimeSkillError, "cap-bad") {
		t.Fatalf("cached instances = %+v ok=%v, want runtime skill error cached", cached, ok)
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
		_, _ = w.Write([]byte(`{"status":"ok","provider_count":3,"config_path":"/tmp/center.json","iworker_readiness":{"ready":true,"status":"ready","tenant_count":1,"role_count":2,"colleague_count":3,"local_account_count":4,"agent_runtime_ready":true,"goalwatch_ready":true,"required_client_paths":["/client/roles"],"checks":[{"name":"tenant","ready":true,"status":"ready","count":1}],"auth_methods":[{"method":"local","label":"Local account","ready":true,"implemented":true,"status":"ready"}]}}`))
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
	if status.IWorkerReadiness == nil || !status.IWorkerReadiness.Ready {
		t.Fatalf("IWorkerReadiness = %+v, want ready", status.IWorkerReadiness)
	}
	if status.IWorkerReadiness.TenantCount != 1 || status.IWorkerReadiness.RoleCount != 2 || status.IWorkerReadiness.ColleagueCount != 3 || status.IWorkerReadiness.LocalAccountCount != 4 {
		t.Fatalf("IWorkerReadiness counts = %+v", status.IWorkerReadiness)
	}
	if len(status.IWorkerReadiness.AuthMethods) != 1 || status.IWorkerReadiness.AuthMethods[0].Method != "local" || !status.IWorkerReadiness.AuthMethods[0].Ready {
		t.Fatalf("IWorkerReadiness auth = %+v", status.IWorkerReadiness.AuthMethods)
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

func TestSaveWorkerMemoryRejectsTrailingJSONWithoutCaching(t *testing.T) {
	home := setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"mem-live","content":"live memory","scope":"personal"} {"id":"mem-injected"}`))
	}))
	defer server.Close()

	_, err := saveWorkerMemory(server.URL, "tenant-a", "sales", "worker-a", SaveWorkerMemoryRequest{Content: "must stay remote first"}, 5)
	if !errors.Is(err, errCenterJSONTrailing) {
		t.Fatalf("saveWorkerMemory error = %v, want errCenterJSONTrailing", err)
	}
	cachePath := filepath.Join(home, ".iworker", "cache", "memories", "tenant-a__sales__worker-a.json")
	if _, statErr := os.Stat(cachePath); !os.IsNotExist(statErr) {
		t.Fatalf("cache should not exist after invalid save response, statErr=%v", statErr)
	}
}

func TestSaveWorkerMemoryReportsCacheWriteFailureAfterCenterSuccess(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write home blocker: %v", err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("USERPROFILE", homeFile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"mem-1","tenant_id":"tenant-a","department_id":"sales","worker_id":"worker-a","scope":"personal","content":"Remember Alpha","category":"project_knowledge","tags":["alpha"]}`))
	}))
	defer server.Close()

	saved, err := saveWorkerMemory(server.URL, "tenant-a", "sales", "worker-a", SaveWorkerMemoryRequest{Content: "Remember Alpha"}, 5)
	if err == nil || !strings.Contains(err.Error(), "local cache update failed") {
		t.Fatalf("saveWorkerMemory err = %v, want cache update failure", err)
	}
	if saved.ID != "mem-1" {
		t.Fatalf("saved = %+v, want authoritative Center memory returned", saved)
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

func TestFetchWorkerMemoriesReportsCacheWriteFailureAfterCenterSuccess(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write home blocker: %v", err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("USERPROFILE", homeFile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"memories":[{"id":"mem-live","tenant_id":"tenant-a","department_id":"sales","worker_id":"worker-a","scope":"personal","content":"remote memory","category":"project_knowledge","tags":[]}]}`))
	}))
	defer server.Close()

	memories, err := fetchWorkerMemoriesResult(server.URL, "tenant-a", "sales", "worker-a", "remote", 10, 5)
	if err == nil || !strings.Contains(err.Error(), "local cache update failed") {
		t.Fatalf("fetchWorkerMemoriesResult err = %v, want cache update failure", err)
	}
	if len(memories) != 1 || memories[0].ID != "mem-live" {
		t.Fatalf("memories = %+v, want authoritative Center memories returned", memories)
	}
}

func TestFetchWorkerMemoriesRejectsTrailingJSONAndKeepsCache(t *testing.T) {
	setTestHome(t)
	if err := writeWorkerMemoryCache("tenant-a", "sales", "worker-a", []WorkerMemoryEntry{{ID: "mem-cache", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", Content: "cached memory"}}); err != nil {
		t.Fatalf("write memory cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"memories":[{"id":"mem-live","content":"live memory"}]} {"memories":[]}`))
	}))
	defer server.Close()

	memories := fetchWorkerMemories(server.URL, "tenant-a", "sales", "worker-a", "", 10, 5)
	if len(memories) != 1 || memories[0].ID != "mem-cache" {
		t.Fatalf("memories = %+v, want cached memory after invalid Center response", memories)
	}
	cached := readWorkerMemoryCache("tenant-a", "sales", "worker-a")
	if len(cached) != 1 || cached[0].ID != "mem-cache" {
		t.Fatalf("cache was overwritten by invalid response: %+v", cached)
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

func TestFetchWorkerMemoryStatsRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tenant_id":"tenant-a","worker_id":"worker-a","total":1} {"tenant_id":"tenant-b"}`))
	}))
	defer server.Close()

	_, err := fetchWorkerMemoryStats(server.URL, "tenant-a", "sales", "worker-a", 5)
	if !errors.Is(err, errCenterJSONTrailing) {
		t.Fatalf("fetchWorkerMemoryStats error = %v, want errCenterJSONTrailing", err)
	}
}

func TestFetchWorkerMemoryStatsFallsBackToLocalCache(t *testing.T) {
	setTestHome(t)
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 1}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := writeWorkerMemoryCache("tenant-a", "sales", "worker-a", []WorkerMemoryEntry{
		{ID: "mem-company", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", Scope: "company", Category: "policy", Content: "company rule"},
		{ID: "mem-personal", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", Scope: "personal", Category: "preference", Content: "personal note"},
	}); err != nil {
		t.Fatalf("write memory cache: %v", err)
	}

	stats, err := (&App{}).FetchWorkerMemoryStats()
	if err != nil {
		t.Fatalf("FetchWorkerMemoryStats returned error: %v", err)
	}
	if stats.Source != "cache" || !stats.Stale || stats.Total != 2 || stats.ByScope["company"] != 1 || stats.ByScope["personal"] != 1 || stats.ByCategory["policy"] != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.CachedAt == "" {
		t.Fatalf("CachedAt should be set for cache stats: %+v", stats)
	}
}

func TestFetchWorkerMemoryStatsUnavailableWhenCenterAndCacheMissing(t *testing.T) {
	setTestHome(t)
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 1}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	stats, err := (&App{}).FetchWorkerMemoryStats()
	if err != nil {
		t.Fatalf("FetchWorkerMemoryStats returned error: %v", err)
	}
	if stats.Source != "unavailable" || !stats.Stale || stats.Total != 0 || stats.TenantID != "tenant-a" || stats.DepartmentID != "sales" || stats.WorkerID != "worker-a" {
		t.Fatalf("stats = %+v", stats)
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

func TestDeleteWorkerMemoryReportsCacheWriteFailureAfterCenterSuccess(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write home blocker: %v", err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("USERPROFILE", homeFile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"deleted"}`))
	}))
	defer server.Close()

	err := deleteWorkerMemory(server.URL, "tenant-a", "sales", "worker-a", "mem-1", 5)
	if err == nil || !strings.Contains(err.Error(), "local cache update failed") {
		t.Fatalf("deleteWorkerMemory err = %v, want cache update failure", err)
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

func TestFetchSharedMemoriesResultRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"memories":[{"id":"mem-live","title":"Live","content":"ok"}]} {"memories":[]}`))
	}))
	defer server.Close()

	_, err := fetchSharedMemoriesResult(server.URL, "tenant-a", "ops", 5)
	if !errors.Is(err, errCenterJSONTrailing) {
		t.Fatalf("fetchSharedMemoriesResult err = %v, want errCenterJSONTrailing", err)
	}
}

func TestSubmitTaskFailsWhenSharedMemoriesUnavailable(t *testing.T) {
	setTestHome(t)
	original := doSimpleLLMRequest
	defer func() { doSimpleLLMRequest = original }()
	llmCalled := false
	doSimpleLLMRequest = func(corelib.MaclawLLMConfig, []interface{}, *http.Client, time.Duration) (*agent.LLMSimpleResponse, error) {
		llmCalled = true
		return &agent.LLMSimpleResponse{Content: "should not run"}, nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/colleagues":
			_ = json.NewEncoder(w).Encode(map[string]any{"colleagues": []map[string]any{{"id": "worker-a", "name": "Worker A", "role_code": "ops"}}})
		case "/client/recommend":
			_ = json.NewEncoder(w).Encode(map[string]any{"recommendations": []map[string]any{{"colleague_id": "worker-a", "name": "Worker A", "role_code": "ops", "score": 0.9}}})
		case "/client/memories":
			http.Error(w, `{"error":"memory db unavailable"}`, http.StatusServiceUnavailable)
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

	_, err := NewApp().SubmitTask(SubmitTaskRequest{TaskType: "brief", Draft: "Prepare daily operating brief", ExpectedOutput: "summary"})
	if err == nil || !strings.Contains(err.Error(), "shared memories failed") {
		t.Fatalf("SubmitTask err = %v, want shared memories failure", err)
	}
	if llmCalled {
		t.Fatal("LLM was called after shared memory dependency failed")
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
			if got := r.URL.Query().Get("colleague_id"); got != "" && got != "worker-a" {
				t.Fatalf("workflow colleague_id = %q, want empty or worker-a", got)
			}
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
	if got, err := fetchCenterWorkflowInstancesResult(context.Background(), server.URL, "tenant-a", "", 5); err != nil || len(got) != 1 {
		t.Fatalf("workflow instances result = %+v err=%v", got, err)
	}
	if got, err := fetchCenterWorkflowInstancesResult(context.Background(), server.URL, "tenant-a", "worker-a", 5); err != nil || len(got) != 1 {
		t.Fatalf("filtered workflow instances result = %+v err=%v", got, err)
	}
	if got := fetchRecommendations(server.URL, "tenant-a", "assign task", 1, 5); len(got) != 1 {
		t.Fatalf("recommendations = %+v", got)
	}
	if got, err := fetchRecommendationsResult(context.Background(), server.URL, "tenant-a", "assign task", 1, 5); err != nil || len(got) != 1 {
		t.Fatalf("recommendations result = %+v err=%v", got, err)
	}
	for _, path := range []string{"/client/colleagues", "/client/roles", "/client/capabilities", "/client/collaborations", "/client/workflow-instances", "/client/recommend"} {
		if !seen[path] {
			t.Fatalf("path %s was not requested", path)
		}
	}
}

func TestFetchCenterWorkflowInstancesResultReturnsCenterErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "workflow store unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := fetchCenterWorkflowInstancesResult(context.Background(), server.URL, "tenant-a", "", 5)
	if err == nil || !strings.Contains(err.Error(), "workflow instances failed") || !strings.Contains(err.Error(), "workflow store unavailable") {
		t.Fatalf("workflow instances error = %v, want Center failure", err)
	}
}

func TestFetchRecommendationsResultReturnsInvalidJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"recommendations":[{"colleague_id":"worker-a"}]} {"recommendations":[]}`))
	}))
	defer server.Close()

	_, err := fetchRecommendationsResult(context.Background(), server.URL, "tenant-a", "assign task", 1, 5)
	if !errors.Is(err, errCenterJSONTrailing) {
		t.Fatalf("recommendations error = %v, want errCenterJSONTrailing", err)
	}
}

func TestAppWorkflowAndRecommendationMethodsReturnErrors(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/workflow-instances":
			http.Error(w, "workflow db down", http.StatusInternalServerError)
		case "/client/recommend":
			http.Error(w, "recommend db down", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := (&App{}).FetchWorkflowInstances(); err == nil || !strings.Contains(err.Error(), "workflow db down") {
		t.Fatalf("FetchWorkflowInstances err = %v, want Center workflow error", err)
	}
	if _, err := (&App{}).RecommendColleague("assign task"); err == nil || !strings.Contains(err.Error(), "recommend db down") {
		t.Fatalf("RecommendColleague err = %v, want Center recommend error", err)
	}
}

func TestFetchWorkflowInstancesFallsBackToCache(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/workflow-instances" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		http.Error(w, "workflow db down", http.StatusInternalServerError)
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := writeWorkflowInstancesCache("tenant-a", "sales", "worker-a", []CenterWorkflowInstance{{
		ID:                             "wf-cache",
		DefinitionID:                   "def-cache",
		Title:                          "Cached workflow",
		InitiatorID:                    "planner",
		CurrentStepID:                  "review",
		CurrentStepAssigneeColleagueID: "worker-a",
		Status:                         "running",
	}, {
		ID:                             "wf-other",
		DefinitionID:                   "def-other",
		Title:                          "Other worker workflow",
		InitiatorID:                    "planner",
		CurrentStepID:                  "archive",
		CurrentStepAssigneeColleagueID: "worker-b",
		Status:                         "running",
	}}); err != nil {
		t.Fatalf("writeWorkflowInstancesCache returned error: %v", err)
	}

	instances, err := (&App{}).FetchWorkflowInstances()
	if err != nil {
		t.Fatalf("FetchWorkflowInstances returned error: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != "wf-cache" || instances[0].Source != "cache" || !instances[0].Stale || instances[0].CachedAt == "" {
		t.Fatalf("workflow instances = %+v, want stale cached workflow", instances)
	}
}

func TestTransitionWorkflowStepCallsCenterAndUpdatesCache(t *testing.T) {
	setTestHome(t)
	var gotActor string
	var gotResult string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/workflows/steps/step-a/complete" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-Tenant-ID") != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", r.Header.Get("X-Tenant-ID"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotActor, _ = body["actor_id"].(string)
		gotResult, _ = body["result"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","step":{"id":"step-a","instance_id":"wf-a","assignee_colleague_id":"worker-a","status":"completed","result":"done","created_at":"2026-05-01T00:00:00Z","updated_at":"2026-05-01T00:02:00Z"},"instance":{"id":"wf-a","definition_id":"def-a","title":"Approve invoice","initiator_id":"planner","current_step_id":"step-b","current_step_assignee_colleague_id":"worker-a","status":"running","created_at":"2026-05-01T00:00:00Z","updated_at":"2026-05-01T00:02:00Z"}}`))
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "finance", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	transition, err := (&App{}).TransitionWorkflowStep("step-a", "complete", "done", "from test")
	if err != nil {
		t.Fatalf("TransitionWorkflowStep returned error: %v", err)
	}
	if gotActor != "worker-a" || gotResult != "done" {
		t.Fatalf("transition body actor/result = %q/%q", gotActor, gotResult)
	}
	if transition.Step.ID != "step-a" || transition.Instance.ID != "wf-a" || transition.Instance.CurrentStepID != "step-b" {
		t.Fatalf("transition = %+v", transition)
	}
	cached, ok := readWorkflowInstancesCache("tenant-a", "finance", "worker-a")
	if !ok || len(cached) != 1 || cached[0].ID != "wf-a" || cached[0].CurrentStepID != "step-b" {
		t.Fatalf("cached workflow instances = %+v, ok=%v", cached, ok)
	}
}

func TestTransitionWorkflowStepReturnsStructuredCenterError(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/workflows/steps/step-a/resume" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"STEP_ACTOR_FORBIDDEN","message":"actor worker-a cannot operate workflow step step-a assigned to worker-b"}}`))
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "finance", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	_, err := (&App{}).TransitionWorkflowStep("step-a", "resume", "", "from test")
	if err == nil {
		t.Fatalf("expected transition error")
	}
	errText := err.Error()
	if !strings.Contains(errText, "status=403") || !strings.Contains(errText, "STEP_ACTOR_FORBIDDEN") || strings.Contains(errText, `{"error"`) {
		t.Fatalf("error = %q, want structured forbidden detail without raw JSON", errText)
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
	var firstSystemPrompt string
	llmCalls := 0
	doSimpleLLMRequest = func(cfg corelib.MaclawLLMConfig, messages []interface{}, _ *http.Client, _ time.Duration) (*agent.LLMSimpleResponse, error) {
		llmCalls++
		if cfg.Model != "office-openai" {
			t.Fatalf("model = %q, want office-openai", cfg.Model)
		}
		if len(messages) != 2 {
			t.Fatalf("messages len = %d, want 2", len(messages))
		}
		if llmCalls == 1 {
			firstSystemPrompt = fmt.Sprint(messages[0])
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
		case "/client/config/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "cfgb-submit",
				"version":      12,
				"content_type": "work_mode",
				"payload":      `{"tone":"operational","human_intervention":"ask before external send"}`,
				"status":       "published",
				"note":         "Use operations-safe working mode",
				"published_at": "2026-05-01T00:01:00Z",
			})
		case "/client/config/apply-result":
			if r.Method != http.MethodPost {
				t.Fatalf("apply-result method = %s, want POST", r.Method)
			}
			var applyResult map[string]any
			if err := json.NewDecoder(r.Body).Decode(&applyResult); err != nil {
				t.Fatalf("decode apply result: %v", err)
			}
			if applyResult["worker_id"] != "worker-a" || applyResult["department_id"] != "ops" || applyResult["status"] != "success" {
				t.Fatalf("unexpected apply result: %+v", applyResult)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case "/client/capabilities":
			if r.URL.Query().Get("runtime") != "1" || r.URL.Query().Get("colleague_id") != "worker-a" {
				t.Fatalf("capability query = %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []map[string]any{{
				"capability_id": "skill-brief",
				"name":          "Brief Writer",
				"version":       "1.0.0",
				"risk_level":    "low",
				"entry": map[string]any{
					"name":        "Brief Writer",
					"description": "Writes operating briefs",
					"triggers":    []string{"brief"},
				},
			}}})
		case "/client/mcp-servers":
			if r.URL.Query().Get("department_id") != "ops" {
				t.Fatalf("department_id = %q", r.URL.Query().Get("department_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"mcp_servers": []map[string]any{{
				"id":            "mcp-crm",
				"name":          "CRM MCP",
				"server_type":   "http",
				"endpoint":      "https://mcp.example/crm",
				"department_id": "ops",
				"risk_level":    "medium",
				"status":        "enabled",
				"env_keys":      []string{"CRM_TOKEN"},
			}}})
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
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "task": map[string]any{"id": "task-1", "title": created.Title, "to_colleague_id": created.ToColleagueID, "status": "completed", "result": completed.Result}})
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
	if !strings.Contains(firstSystemPrompt, "Center-installed tools available to this iWorker") || !strings.Contains(firstSystemPrompt, "Skill Brief Writer") || !strings.Contains(firstSystemPrompt, "MCP CRM MCP") || !strings.Contains(firstSystemPrompt, "env_keys=CRM_TOKEN") {
		t.Fatalf("system prompt missing installed tools context: %s", firstSystemPrompt)
	}
	if !strings.Contains(firstSystemPrompt, "iWorkerCenter configuration bundle") || !strings.Contains(firstSystemPrompt, "Config snapshot source=center / version=12") || !strings.Contains(firstSystemPrompt, "human_intervention") {
		t.Fatalf("system prompt missing config bundle context: %s", firstSystemPrompt)
	}
	if strings.Contains(firstSystemPrompt, "CRM_TOKEN=") {
		t.Fatalf("system prompt leaked env secret shape: %s", firstSystemPrompt)
	}
	if created.ToColleagueID != "worker-a" || created.FromColleagueID != "human_operator" || created.SourceType != "human_iworker_interaction" {
		t.Fatalf("unexpected created task: %+v", created)
	}
	if completed.ActorID != "worker-a" || completed.Result != "completed operating brief" || completed.Note != "completed_by_iworker_submit_task" {
		t.Fatalf("unexpected completion: %+v", completed)
	}
}

func TestGenerateSubmitTaskTitleUsesLLMTitle(t *testing.T) {
	original := doSimpleLLMRequest
	defer func() { doSimpleLLMRequest = original }()

	calls := 0
	doSimpleLLMRequest = func(cfg corelib.MaclawLLMConfig, messages []interface{}, _ *http.Client, _ time.Duration) (*agent.LLMSimpleResponse, error) {
		calls++
		if len(messages) != 2 {
			t.Fatalf("messages len = %d, want 2", len(messages))
		}
		return &agent.LLMSimpleResponse{Content: "Title: Prepare Operating Brief\nextra"}, nil
	}

	title := generateSubmitTaskTitle("free input", "Please prepare a daily operating brief from these notes", "summary", "brief body", corelib.MaclawLLMConfig{URL: "http://llm", Model: "test", TimeoutSec: 5}, nil)
	if title != "Prepare Operating Brief" {
		t.Fatalf("title = %q, want LLM title", title)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestGenerateSubmitTaskTitleFallsBackFromGenericLLMTitle(t *testing.T) {
	original := doSimpleLLMRequest
	defer func() { doSimpleLLMRequest = original }()

	doSimpleLLMRequest = func(corelib.MaclawLLMConfig, []interface{}, *http.Client, time.Duration) (*agent.LLMSimpleResponse, error) {
		return &agent.LLMSimpleResponse{Content: "free input"}, nil
	}

	title := generateSubmitTaskTitle("free input", "Please prepare a daily operating brief. Include risks and next steps.", "summary", "brief body", corelib.MaclawLLMConfig{URL: "http://llm", Model: "test", TimeoutSec: 5}, nil)
	if title != "prepare a daily operating brief" {
		t.Fatalf("title = %q, want local fallback", title)
	}
}

func TestInstalledToolsEnrichExecutorHeartbeatCapabilities(t *testing.T) {
	settings := normalizeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, TenantID: "tenant-a", DepartmentID: "finance", WorkerID: "worker-a"}})
	snapshot := BuildDefaultAgentRuntimeSnapshot(settings)
	enriched := enrichAgentRuntimeSnapshotWithInstalledTools(snapshot, CenterInstalledTools{
		Skills:     []CenterRuntimeCapability{{CapabilityID: "cap-finance-close", Name: "Finance Close"}},
		MCPServers: []CenterMCPServer{{ID: "mcp-ledger", Name: "Ledger MCP"}},
	})

	var executor AgentInstance
	for _, instance := range enriched.Instances {
		if instance.Role == AgentRoleExecutor {
			executor = instance
		}
		if instance.Role != AgentRoleExecutor && (testContainsString(instance.Capabilities, "skill:cap-finance-close") || testContainsString(instance.Capabilities, "mcp:mcp-ledger")) {
			t.Fatalf("non-executor instance got runtime tools: %+v", instance)
		}
	}
	if !testContainsString(executor.Capabilities, "business_task_execution") || !testContainsString(executor.Capabilities, "skill:cap-finance-close") || !testContainsString(executor.Capabilities, "mcp:mcp-ledger") {
		t.Fatalf("executor capabilities = %+v", executor.Capabilities)
	}
}

func TestInstalledToolCapabilityLabelsSanitizeAndDedupe(t *testing.T) {
	labels := installedToolCapabilityLabels(CenterInstalledTools{
		Skills:     []CenterRuntimeCapability{{CapabilityID: "Finance Close"}, {CapabilityID: "Finance Close"}},
		MCPServers: []CenterMCPServer{{Name: "Ledger MCP"}},
	})
	if len(labels) != 2 || labels[0] != "skill:finance-close" || labels[1] != "mcp:ledger-mcp" {
		t.Fatalf("labels = %+v", labels)
	}
}

func TestInstalledToolCapabilityLabelsForHeartbeatOnlyUsesLiveSections(t *testing.T) {
	labels := installedToolCapabilityLabelsForHeartbeat(CenterInstalledTools{
		Source:     "partial-cache",
		Skills:     []CenterRuntimeCapability{{CapabilityID: "fresh-skill"}},
		MCPServers: []CenterMCPServer{{ID: "cached-mcp"}},
		MCPError:   "mcp temporarily unavailable",
	})
	if len(labels) != 1 || labels[0] != "skill:fresh-skill" {
		t.Fatalf("labels = %+v, want only live skills", labels)
	}

	labels = installedToolCapabilityLabelsForHeartbeat(CenterInstalledTools{
		Source:     "partial-cache",
		Skills:     []CenterRuntimeCapability{{CapabilityID: "cached-skill"}},
		MCPServers: []CenterMCPServer{{ID: "fresh-mcp"}},
		SkillError: "skills temporarily unavailable",
	})
	if len(labels) != 1 || labels[0] != "mcp:fresh-mcp" {
		t.Fatalf("labels = %+v, want only live MCP", labels)
	}

	labels = installedToolCapabilityLabelsForHeartbeat(CenterInstalledTools{
		Source:     "cache",
		Skills:     []CenterRuntimeCapability{{CapabilityID: "cached-skill"}},
		MCPServers: []CenterMCPServer{{ID: "cached-mcp"}},
	})
	if len(labels) != 0 {
		t.Fatalf("labels = %+v, want no cached capabilities in heartbeat", labels)
	}
}

func testContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestFetchConfigBundleFallsBackToScopedCache(t *testing.T) {
	setTestHome(t)
	applyReported := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		switch {
		case r.URL.Path == "/client/config/latest" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "cfgb-1", "version": 7, "content_type": "full", "payload": `{"local_continuity":true}`, "status": "published", "note": "rollout"})
		case r.URL.Path == "/client/config/apply-result" && r.Method == http.MethodPost:
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode apply result: %v", err)
			}
			if req["bundle_id"] != "cfgb-1" || req["worker_id"] != "worker-a" || req["department_id"] != "sales" {
				t.Fatalf("unexpected apply result: %+v", req)
			}
			applyReported = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	settings := DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}
	fresh := fetchConfigBundleForSettings(settings, 5)
	if fresh.Source != "center" || fresh.Stale || fresh.Version != 7 || fresh.Note != "rollout" {
		t.Fatalf("fresh bundle = %+v", fresh)
	}
	if !applyReported {
		t.Fatal("expected config apply result to be reported")
	}

	settings.Center.BaseURL = "http://127.0.0.1:1"
	cached := fetchConfigBundleForSettings(settings, 1)
	if cached.Source != "cache" || !cached.Stale || cached.Version != 7 || cached.ID != "cfgb-1" {
		t.Fatalf("cached bundle = %+v", cached)
	}
}

func TestFetchConfigBundleReportsFailedApplyWhenLocalCacheWriteFails(t *testing.T) {
	home := setTestHome(t)
	cacheRoot := filepath.Join(home, ".iworker", "cache")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatalf("mkdir cache root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheRoot, "config_bundles"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocking cache path: %v", err)
	}

	applyResult := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		switch {
		case r.URL.Path == "/client/config/latest" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "cfgb-cache-fail", "version": 8, "content_type": "full", "payload": `{"local_continuity":true}`, "status": "published", "note": "rollout"})
		case r.URL.Path == "/client/config/apply-result" && r.Method == http.MethodPost:
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode apply result: %v", err)
			}
			applyResult <- req
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	settings := DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}
	bundle := fetchConfigBundleForSettings(settings, 5)
	if bundle.Source != "center" || bundle.Version != 8 || bundle.ID != "cfgb-cache-fail" {
		t.Fatalf("bundle = %+v", bundle)
	}
	if bundle.ApplyStatus != "failed" || !strings.Contains(bundle.ApplyMessage, "local cache write failed") {
		t.Fatalf("bundle apply fields = status %q message %q", bundle.ApplyStatus, bundle.ApplyMessage)
	}
	select {
	case req := <-applyResult:
		if req["status"] != "failed" || !strings.Contains(fmt.Sprint(req["message"]), "local cache write failed") {
			t.Fatalf("apply result = %+v, want failed cache-write report", req)
		}
	case <-time.After(time.Second):
		t.Fatal("expected config apply result report")
	}
}

func TestFetchConfigBundleTreatsMissingPublishedBundleAsReadyEmptyState(t *testing.T) {
	setTestHome(t)
	applyReported := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/config/apply-result" {
			applyReported = true
		}
		if r.URL.Path != "/client/config/latest" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		response := map[string]any{"error": map[string]string{"code": "NOT_FOUND", "message": "no published config bundle"}}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}
	bundle := fetchConfigBundleForSettings(settings, 5)
	if bundle.Source != "none" || bundle.Status != "not_published" || bundle.Version != 0 || bundle.Stale {
		t.Fatalf("bundle = %+v", bundle)
	}
	if applyReported {
		t.Fatal("missing published bundle should not report apply result")
	}
	if _, ok := readConfigBundleCache("tenant-a", "sales", "worker-a"); ok {
		t.Fatal("missing published bundle should not overwrite cache")
	}
}

func TestConfigBundleCacheIsScopedByDepartmentAndWorkerWithLegacyFallback(t *testing.T) {
	home := setTestHome(t)
	if err := writeConfigBundleCache("tenant-a", "sales", "worker-a", CenterConfigBundle{ID: "cfgb-sales", Version: 1, ApplyStatus: "success"}); err != nil {
		t.Fatalf("write sales cache: %v", err)
	}
	if err := writeConfigBundleCache("tenant-a", "quality", "worker-b", CenterConfigBundle{ID: "cfgb-quality", Version: 2, ApplyStatus: "failed"}); err != nil {
		t.Fatalf("write quality cache: %v", err)
	}

	sales, ok := readConfigBundleCache("tenant-a", "sales", "worker-a")
	if !ok || sales.ID != "cfgb-sales" || sales.ApplyStatus != "success" {
		t.Fatalf("sales cache = %+v ok=%v", sales, ok)
	}
	quality, ok := readConfigBundleCache("tenant-a", "quality", "worker-b")
	if !ok || quality.ID != "cfgb-quality" || quality.ApplyStatus != "failed" {
		t.Fatalf("quality cache = %+v ok=%v", quality, ok)
	}
	if _, ok := readConfigBundleCache("tenant-a", "sales", "worker-b"); ok {
		t.Fatal("different worker should not read another worker scoped config cache")
	}

	legacyDir := filepath.Join(home, ".iworker", "cache", "config_bundles")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy cache dir: %v", err)
	}
	legacy := CenterConfigBundle{ID: "cfgb-legacy", Version: 3, ApplyStatus: "success"}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(legacyDir, "tenant-b.json"), data, 0o644); err != nil {
		t.Fatalf("write legacy cache: %v", err)
	}
	migrated, ok := readConfigBundleCache("tenant-b", "default", "worker-c")
	if !ok || migrated.ID != "cfgb-legacy" {
		t.Fatalf("legacy cache fallback = %+v ok=%v", migrated, ok)
	}
}

func TestFetchInstalledToolsFetchesSkillsAndMCPConcurrently(t *testing.T) {
	setTestHome(t)
	started := make(chan string, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		started <- r.URL.Path
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		switch r.URL.Path {
		case "/client/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []map[string]any{{"capability_id": "cap-fast", "name": "Fast Skill"}}})
		case "/client/mcp-servers":
			_ = json.NewEncoder(w).Encode(map[string]any{"mcp_servers": []map[string]any{{"id": "mcp-fast", "name": "Fast MCP", "server_type": "http", "endpoint": "https://mcp.example"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	resultCh := make(chan CenterInstalledTools, 1)
	go func() {
		resultCh <- fetchInstalledToolsForSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}, 5)
	}()

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case path := <-started:
			seen[path] = true
		case <-time.After(750 * time.Millisecond):
			close(release)
			t.Fatalf("installed tools fetch did not start both requests concurrently; started=%v", seen)
		}
	}
	close(release)
	tools := <-resultCh

	if tools.Source != "center" || tools.Stale || len(tools.Skills) != 1 || len(tools.MCPServers) != 1 {
		t.Fatalf("tools = %+v", tools)
	}
	if !seen["/client/capabilities"] || !seen["/client/mcp-servers"] {
		t.Fatalf("installed tools fetch started unexpected paths: %v", seen)
	}
}
func TestFetchInstalledToolsFallsBackToScopedCache(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/capabilities":
			if r.URL.Query().Get("runtime") != "1" || r.URL.Query().Get("colleague_id") != "worker-a" {
				t.Fatalf("capability query = %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []map[string]any{{"capability_id": "cap-a", "name": "Skill A"}}})
		case "/client/mcp-servers":
			if r.URL.Query().Get("department_id") != "sales" {
				t.Fatalf("department_id = %q", r.URL.Query().Get("department_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"mcp_servers": []map[string]any{{"id": "mcp-a", "name": "MCP A", "server_type": "http", "endpoint": "https://mcp.example"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	settings := DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}
	fresh := fetchInstalledToolsForSettings(settings, 5)
	if fresh.Source != "center" || fresh.Stale || len(fresh.Skills) != 1 || len(fresh.MCPServers) != 1 {
		t.Fatalf("fresh tools = %+v", fresh)
	}

	settings.Center.BaseURL = "http://127.0.0.1:1"
	cached := fetchInstalledToolsForSettings(settings, 1)
	if cached.Source != "cache" || !cached.Stale || len(cached.Skills) != 1 || cached.Skills[0].CapabilityID != "cap-a" || len(cached.MCPServers) != 1 || cached.MCPServers[0].ID != "mcp-a" {
		t.Fatalf("cached tools = %+v", cached)
	}
}

func TestFetchInstalledToolsMergesFreshSkillsWithCachedMCP(t *testing.T) {
	setTestHome(t)
	if err := writeInstalledToolsCache("tenant-a", "sales", "worker-a", CenterInstalledTools{
		Skills:     []CenterRuntimeCapability{{CapabilityID: "old-skill", Name: "Old Skill"}},
		MCPServers: []CenterMCPServer{{ID: "mcp-old", Name: "Old MCP", Status: "enabled"}},
		Source:     "center",
	}); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []map[string]any{{"capability_id": "fresh-skill", "name": "Fresh Skill"}}})
		case "/client/mcp-servers":
			http.Error(w, "mcp temporarily unavailable", http.StatusServiceUnavailable)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	tools := fetchInstalledToolsForSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}, 5)
	if tools.Source != "partial-cache" || !tools.Stale || tools.CachedAt == "" {
		t.Fatalf("tools source = %+v, want partial stale snapshot", tools)
	}
	if len(tools.Skills) != 1 || tools.Skills[0].CapabilityID != "fresh-skill" {
		t.Fatalf("skills = %+v, want fresh skill", tools.Skills)
	}
	if len(tools.MCPServers) != 1 || tools.MCPServers[0].ID != "mcp-old" {
		t.Fatalf("mcp = %+v, want cached MCP", tools.MCPServers)
	}
	if tools.MCPError == "" || tools.SkillError != "" {
		t.Fatalf("sync errors = skill:%q mcp:%q, want MCP error only", tools.SkillError, tools.MCPError)
	}
	prompt := buildInstalledToolsSystemPrompt(tools)
	if !strings.Contains(prompt, "Tool snapshot source=partial-cache") || !strings.Contains(prompt, "context only until iWorkerCenter reconnects") || !strings.Contains(prompt, "MCP sync issue=") {
		t.Fatalf("prompt missing partial-cache guardrails: %s", prompt)
	}
	if !strings.Contains(prompt, "Skill Fresh Skill") || !strings.Contains(prompt, "MCP Old MCP") {
		t.Fatalf("prompt missing merged tool entries: %s", prompt)
	}
}

func TestFetchInstalledToolsMergesFreshMCPWithCachedSkills(t *testing.T) {
	setTestHome(t)
	if err := writeInstalledToolsCache("tenant-a", "sales", "worker-a", CenterInstalledTools{
		Skills:     []CenterRuntimeCapability{{CapabilityID: "old-skill", Name: "Old Skill"}},
		MCPServers: []CenterMCPServer{{ID: "mcp-old", Name: "Old MCP", Status: "enabled"}},
		Source:     "center",
	}); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/capabilities":
			http.Error(w, "skills temporarily unavailable", http.StatusServiceUnavailable)
		case "/client/mcp-servers":
			_ = json.NewEncoder(w).Encode(map[string]any{"mcp_servers": []map[string]any{{"id": "mcp-fresh", "name": "Fresh MCP", "server_type": "http", "endpoint": "https://mcp.example"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	tools := fetchInstalledToolsForSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}, 5)
	if tools.Source != "partial-cache" || !tools.Stale || tools.CachedAt == "" {
		t.Fatalf("tools source = %+v, want partial stale snapshot", tools)
	}
	if len(tools.Skills) != 1 || tools.Skills[0].CapabilityID != "old-skill" {
		t.Fatalf("skills = %+v, want cached skill", tools.Skills)
	}
	if len(tools.MCPServers) != 1 || tools.MCPServers[0].ID != "mcp-fresh" {
		t.Fatalf("mcp = %+v, want fresh MCP", tools.MCPServers)
	}
	if tools.SkillError == "" || tools.MCPError != "" {
		t.Fatalf("sync errors = skill:%q mcp:%q, want skill error only", tools.SkillError, tools.MCPError)
	}
}

func TestFetchInstalledToolsCenterEmptyOverridesStaleCache(t *testing.T) {
	setTestHome(t)
	if err := writeInstalledToolsCache("tenant-a", "sales", "worker-a", CenterInstalledTools{Skills: []CenterRuntimeCapability{{CapabilityID: "old-skill"}}, Source: "center"}); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []any{}})
		case "/client/mcp-servers":
			_ = json.NewEncoder(w).Encode(map[string]any{"mcp_servers": []any{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	tools := fetchInstalledToolsForSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}, 5)
	if tools.Source != "center" || tools.Stale || len(tools.Skills) != 0 || len(tools.MCPServers) != 0 {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestFetchInstalledToolsReportsCacheWriteFailureAfterCenterSuccess(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write home blocker: %v", err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("USERPROFILE", homeFile)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []map[string]any{{"capability_id": "cap-a", "name": "Skill A"}}})
		case "/client/mcp-servers":
			_ = json.NewEncoder(w).Encode(map[string]any{"mcp_servers": []map[string]any{{"id": "mcp-a", "name": "MCP A", "server_type": "http", "endpoint": "https://mcp.example"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	tools := fetchInstalledToolsForSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}, 5)
	if tools.Source != "center-cache-error" || !tools.Stale {
		t.Fatalf("tools source = %+v, want cache error snapshot", tools)
	}
	if len(tools.Skills) != 1 || len(tools.MCPServers) != 1 {
		t.Fatalf("fresh tools were not preserved: %+v", tools)
	}
	if tools.SkillError != "" || tools.MCPError != "" || !strings.Contains(tools.CacheError, "cache update failed") {
		t.Fatalf("cache errors not surfaced cleanly: skill=%q mcp=%q cache=%q", tools.SkillError, tools.MCPError, tools.CacheError)
	}
}

func TestFetchInstalledToolsReportsPartialCacheWriteFailure(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write home blocker: %v", err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("USERPROFILE", homeFile)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{"runtime_entries": []map[string]any{{"capability_id": "cap-a", "name": "Skill A"}}})
		case "/client/mcp-servers":
			http.Error(w, "mcp unavailable", http.StatusServiceUnavailable)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	tools := fetchInstalledToolsForSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}, 5)
	if tools.Source != "partial-cache" || !tools.Stale {
		t.Fatalf("tools source = %+v, want partial cache snapshot", tools)
	}
	if len(tools.Skills) != 1 || tools.Skills[0].CapabilityID != "cap-a" {
		t.Fatalf("fresh skill not preserved: %+v", tools.Skills)
	}
	if tools.SkillError != "" || !strings.Contains(tools.MCPError, "mcp unavailable") || !strings.Contains(tools.CacheError, "cache update failed") {
		t.Fatalf("errors not surfaced: skill=%q mcp=%q cache=%q", tools.SkillError, tools.MCPError, tools.CacheError)
	}
}

func TestFetchAgentInstancesFallsBackToCache(t *testing.T) {
	setTestHome(t)
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 1}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := writeAgentInstancesCache("tenant-a", "sales", "worker-a", []CenterAgentInstance{{TenantID: "tenant-a", WorkerID: "worker-a", InstanceID: "worker-a:executor", Role: "executor", Status: "online", EffectiveStatus: "online"}}); err != nil {
		t.Fatalf("write agent cache: %v", err)
	}

	instances, err := (&App{}).FetchAgentInstances()
	if err != nil {
		t.Fatalf("FetchAgentInstances returned error: %v", err)
	}
	if len(instances) != 1 || instances[0].Source != "cache" || !instances[0].Stale || instances[0].CachedAt == "" || instances[0].InstanceID != "worker-a:executor" {
		t.Fatalf("instances = %+v", instances)
	}
}

func TestFetchGoalPushesFallsBackToCache(t *testing.T) {
	setTestHome(t)
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 1}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := writeGoalPushesCache("tenant-a", "sales", "worker-a", []CenterGoalPush{{EventID: "event-a", TaskID: "task-a", Title: "Recover stalled task", Status: "open", RecommendedAction: "restart_executor", CreatedAt: "2026-05-02T00:00:00Z"}}); err != nil {
		t.Fatalf("write push cache: %v", err)
	}

	pushes, err := (&App{}).FetchGoalPushes(20)
	if err != nil {
		t.Fatalf("FetchGoalPushes returned error: %v", err)
	}
	if len(pushes) != 1 || pushes[0].Source != "cache" || !pushes[0].Stale || pushes[0].CachedAt == "" || pushes[0].EventID != "event-a" {
		t.Fatalf("pushes = %+v", pushes)
	}
}

func TestFetchCollaborationsFallsBackToCache(t *testing.T) {
	setTestHome(t)
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 1}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := writeCollaborationTasksCache("tenant-a", "sales", "worker-a", []CenterCollabTask{{ID: "collab-a", Title: "Cached handoff", ToColleagueID: "worker-a", Status: "pending", CreatedAt: "2026-05-02T00:00:00Z"}}); err != nil {
		t.Fatalf("write collaboration cache: %v", err)
	}

	tasks, err := (&App{}).FetchCollaborations("worker-a")
	if err != nil {
		t.Fatalf("FetchCollaborations returned error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Source != "cache" || !tasks[0].Stale || tasks[0].CachedAt == "" || tasks[0].ID != "collab-a" {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestTransitionCollaborationTaskRemovesTerminalCachedTask(t *testing.T) {
	setTestHome(t)
	if err := writeCollaborationTasksCache("tenant-a", "sales", "worker-a", []CenterCollabTask{
		{ID: "collab-a", Title: "A", Status: "in_progress", CreatedAt: "2026-05-02T00:00:00Z"},
		{ID: "collab-b", Title: "B", Status: "pending", CreatedAt: "2026-05-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write collaboration cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/collaboration/collab-a/complete" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","task":{"id":"collab-a","title":"A","status":"completed","result":"done","updated_at":"2026-05-02T00:01:00Z"}}`))
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := (&App{}).TransitionCollaborationTask("collab-a", "complete", "done", "test"); err != nil {
		t.Fatalf("TransitionCollaborationTask returned error: %v", err)
	}
	cached, ok := readCollaborationTasksCache("tenant-a", "sales", "worker-a")
	if !ok || len(cached) != 1 || cached[0].ID != "collab-b" {
		t.Fatalf("cached tasks = %+v ok=%v", cached, ok)
	}
}

func TestTransitionCollaborationTaskUpdatesNonTerminalCachedTask(t *testing.T) {
	setTestHome(t)
	if err := writeCollaborationTasksCache("tenant-a", "sales", "worker-a", []CenterCollabTask{
		{ID: "collab-a", Title: "A", Status: "pending", CreatedAt: "2026-05-02T00:00:00Z"},
		{ID: "collab-b", Title: "B", Status: "pending", CreatedAt: "2026-05-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write collaboration cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/collaboration/collab-a/accept" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","task":{"id":"collab-a","title":"A from Center","status":"accepted","updated_at":"2026-05-02T00:01:00Z"}}`))
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := (&App{}).TransitionCollaborationTask("collab-a", "accept", "", "test"); err != nil {
		t.Fatalf("TransitionCollaborationTask returned error: %v", err)
	}
	cached, ok := readCollaborationTasksCache("tenant-a", "sales", "worker-a")
	if !ok || len(cached) != 2 {
		t.Fatalf("cached tasks = %+v ok=%v", cached, ok)
	}
	byID := map[string]CenterCollabTask{}
	for _, task := range cached {
		byID[task.ID] = task
	}
	if byID["collab-a"].Status != "accepted" || byID["collab-a"].UpdatedAt == "" {
		t.Fatalf("collab-a cache = %+v", byID["collab-a"])
	}
	if byID["collab-b"].Status != "pending" {
		t.Fatalf("collab-b cache = %+v", byID["collab-b"])
	}
}

func TestTransitionCollaborationTaskUpdatesStartedCachedTask(t *testing.T) {
	setTestHome(t)
	if err := writeCollaborationTasksCache("tenant-a", "sales", "worker-a", []CenterCollabTask{
		{ID: "collab-a", Title: "A", Status: "accepted", CreatedAt: "2026-05-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write collaboration cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/collaboration/collab-a/start" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","task":{"id":"collab-a","title":"A","status":"in_progress","updated_at":"2026-05-02T00:01:00Z"}}`))
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := (&App{}).TransitionCollaborationTask("collab-a", "start", "", "test"); err != nil {
		t.Fatalf("TransitionCollaborationTask returned error: %v", err)
	}
	cached, ok := readCollaborationTasksCache("tenant-a", "sales", "worker-a")
	if !ok || len(cached) != 1 || cached[0].Status != "in_progress" || cached[0].UpdatedAt == "" {
		t.Fatalf("cached tasks = %+v ok=%v", cached, ok)
	}
}

func TestTransitionCollaborationTaskCreatesCacheFromAuthoritativeTask(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/collaboration/collab-a/accept" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","task":{"id":"collab-a","title":"Authoritative handoff","description":"Loaded from Center transition","from_colleague_id":"worker-office","to_colleague_id":"worker-a","to_role_code":"sales","status":"accepted","priority":5,"workflow_step_instance_id":"wf-step-a","created_at":"2026-05-02T00:00:00Z","updated_at":"2026-05-02T00:01:00Z"}}`))
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	updated, err := (&App{}).TransitionCollaborationTask("collab-a", "accept", "", "test")
	if err != nil {
		t.Fatalf("TransitionCollaborationTask returned error: %v", err)
	}
	if updated.Title != "Authoritative handoff" || updated.Status != "accepted" {
		t.Fatalf("updated task = %+v", updated)
	}
	cached, ok := readCollaborationTasksCache("tenant-a", "sales", "worker-a")
	if !ok || len(cached) != 1 {
		t.Fatalf("cached tasks = %+v ok=%v", cached, ok)
	}
	if cached[0].Title != "Authoritative handoff" || cached[0].Status != "accepted" || cached[0].WorkflowStepID != "wf-step-a" {
		t.Fatalf("cached task = %+v", cached[0])
	}
}

func TestTransitionCollaborationTaskUsesResolvedWorkerIDAsActor(t *testing.T) {
	setTestHome(t)
	if err := writeCollaborationTasksCache("tenant-a", "sales", "Xiao_Di", []CenterCollabTask{
		{ID: "collab-a", Title: "A", Status: "pending", CreatedAt: "2026-05-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write collaboration cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/collaboration/collab-a/accept" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			ActorID string `json:"actor_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		if req.ActorID != "Xiao_Di" {
			t.Fatalf("actor_id = %q, want Xiao_Di", req.ActorID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","task":{"id":"collab-a","title":"A","status":"accepted","updated_at":"2026-05-02T00:01:00Z"}}`))
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := (&App{}).TransitionCollaborationTask("collab-a", "accept", "", "test"); err != nil {
		t.Fatalf("TransitionCollaborationTask returned error: %v", err)
	}
	cached, ok := readCollaborationTasksCache("tenant-a", "sales", "Xiao_Di")
	if !ok || len(cached) != 1 || cached[0].Status != "accepted" {
		t.Fatalf("cached tasks = %+v ok=%v", cached, ok)
	}
}

func TestAutoHandleGoalPushRejectsCachedSnapshot(t *testing.T) {
	setTestHome(t)
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 1}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := writeGoalPushesCache("tenant-a", "sales", "worker-a", []CenterGoalPush{{EventID: "event-a", TaskID: "task-a", Title: "Recover stalled task", Status: "open", RecommendedAction: "restart_executor", CreatedAt: "2026-05-02T00:00:00Z"}}); err != nil {
		t.Fatalf("write push cache: %v", err)
	}

	_, err := (&App{}).AutoHandleGoalPush("event-a")
	if err == nil || !strings.Contains(err.Error(), "cached snapshot") {
		t.Fatalf("AutoHandleGoalPush error = %v, want cached snapshot rejection", err)
	}
}

func TestAckGoalPushRemovesCachedPush(t *testing.T) {
	setTestHome(t)
	if err := writeGoalPushesCache("tenant-a", "sales", "worker-a", []CenterGoalPush{
		{EventID: "event-a", TaskID: "task-a", Title: "A", Status: "open", CreatedAt: "2026-05-02T00:00:00Z"},
		{EventID: "event-b", TaskID: "task-b", Title: "B", Status: "open", CreatedAt: "2026-05-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write push cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/goalwatch/pushes/event-a/ack" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CenterGoalPushAckResult{EventID: "event-a", TaskID: "task-a", AckEventID: "ack-a", Status: "resumed", CreatedAt: "2026-05-02T00:00:00Z"})
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	result, err := (&App{}).AckGoalPush("event-a", "resumed", "ok")
	if err != nil {
		t.Fatalf("AckGoalPush returned error: %v", err)
	}
	if result.AckEventID != "ack-a" {
		t.Fatalf("result = %+v", result)
	}
	cached, ok := readGoalPushesCache("tenant-a", "sales", "worker-a")
	if !ok || len(cached) != 1 || cached[0].EventID != "event-b" {
		t.Fatalf("cached pushes = %+v ok=%v", cached, ok)
	}
}

func TestRecoverGoalPushRemovesCachedPush(t *testing.T) {
	setTestHome(t)
	if err := writeGoalPushesCache("tenant-a", "sales", "worker-a", []CenterGoalPush{
		{EventID: "event-a", TaskID: "task-a", Title: "A", Status: "open", WorkflowStepInstanceID: "wfsi-a", RecoveryAction: "resume_workflow_step", CreatedAt: "2026-05-02T00:00:00Z"},
		{EventID: "event-b", TaskID: "task-b", Title: "B", Status: "open", CreatedAt: "2026-05-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write push cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/goalwatch/pushes/event-a/recover" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CenterGoalPushRecoverResult{Push: CenterGoalPush{EventID: "event-a", TaskID: "task-a", WorkflowStepInstanceID: "wfsi-a", RecoveryAction: "resume_workflow_step"}, Ack: CenterGoalPushAckResult{EventID: "event-a", TaskID: "task-a", AckEventID: "ack-a", Status: "recovered", CreatedAt: "2026-05-02T00:00:00Z"}, RecoveryAction: "resume_workflow_step", RecoveryMethod: "POST", RecoveryPath: "/runtime/workflows/steps/wfsi-a/resume", Status: "recovered"})
	}))
	defer server.Close()
	if err := writeDiWorkerSettings(DiWorkerSettings{Center: CenterConfig{Enabled: true, BaseURL: server.URL, TenantID: "tenant-a", DepartmentID: "sales", WorkerID: "worker-a", TimeoutSec: 5}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	result, err := (&App{}).RecoverGoalPush("event-a", "approved")
	if err != nil {
		t.Fatalf("RecoverGoalPush returned error: %v", err)
	}
	if result.Ack.AckEventID != "ack-a" || result.Status != "recovered" {
		t.Fatalf("result = %+v", result)
	}
	cached, ok := readGoalPushesCache("tenant-a", "sales", "worker-a")
	if !ok || len(cached) != 1 || cached[0].EventID != "event-b" {
		t.Fatalf("cached pushes = %+v ok=%v", cached, ok)
	}
}
