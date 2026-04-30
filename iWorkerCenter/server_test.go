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

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/app"
	centercompute "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/compute"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
)

func setCenterTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestNewCenterServerLoadsDefaultProviders(t *testing.T) {
	setCenterTestHome(t)

	server := newCenterServer(":0")
	if len(server.providers) != 2 {
		t.Fatalf("providers len = %d, want 2", len(server.providers))
	}
	if server.providers[0].ID != "office-openai" {
		t.Fatalf("first provider = %q, want office-openai", server.providers[0].ID)
	}
}

func TestLoadCenterSettingsReturnsDefaultsWhenFileMissing(t *testing.T) {
	setCenterTestHome(t)

	settings, err := readCenterSettings()
	if err != nil {
		t.Fatalf("readCenterSettings returned error: %v", err)
	}
	if len(settings.Providers) != 2 {
		t.Fatalf("providers len = %d, want 2", len(settings.Providers))
	}
}

func TestWriteCenterSettingsRoundTrip(t *testing.T) {
	home := setCenterTestHome(t)
	settings := centerSettingsFile{
		Providers: []centerProviderFile{{
			ID:          "custom-openai",
			Name:        "自定义服务",
			Protocol:    "",
			BaseURL:     "http://127.0.0.1:9000/",
			APIKey:      "token-a",
			Model:       "gpt-test",
			Priority:    77,
			Features:    nil,
			Description: "自定义 provider",
			Enabled:     true,
			TimeoutSec:  0,
		}},
	}

	if err := writeCenterSettings(settings); err != nil {
		t.Fatalf("writeCenterSettings returned error: %v", err)
	}

	path := filepath.Join(home, ".iworkercenter", "settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}

	got, err := readCenterSettings()
	if err != nil {
		t.Fatalf("readCenterSettings returned error: %v", err)
	}
	if len(got.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(got.Providers))
	}
	if got.Providers[0].Protocol != "openai" || got.Providers[0].BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("provider = %+v, want normalized values", got.Providers[0])
	}
	if got.Providers[0].TimeoutSec != 60 {
		t.Fatalf("TimeoutSec = %d, want 60", got.Providers[0].TimeoutSec)
	}
	if got.Providers[0].Features == nil {
		t.Fatalf("Features should be normalized to empty slice")
	}
}

func TestRefreshProvidersUsesCloudComputeSource(t *testing.T) {
	setCenterTestHome(t)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers": []map[string]any{{
				"id": "p1", "name": "Cloud GPT", "protocol": "openai", "base_url": "https://api.example.com/v1",
				"api_key": "sk-cloud", "model": "gpt-cloud", "enabled": true, "priority": 88, "compute_type": "coding",
			}},
			"compute_permission": true,
		})
	}))
	defer cloud.Close()

	syncMgr := centercompute.NewSyncManager(cloud.URL, "center-1", "secret-1")
	if err := syncMgr.SyncNow(); err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	sourceMgr := centercompute.NewSourceManager(syncMgr)
	server := newCenterServer(":0")
	server.center = &app.Center{ComputeSourceManager: sourceMgr}

	server.refreshProviders()
	if len(server.providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(server.providers))
	}
	if server.providers[0].ID != "cloud-p1" || server.providers[0].Model != "gpt-cloud" {
		t.Fatalf("provider = %+v, want cloud provider", server.providers[0])
	}
}

func TestLoadCenterProvidersReadsSettingsFile(t *testing.T) {
	home := setCenterTestHome(t)

	path := filepath.Join(home, ".iworkercenter", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	data := []byte(`{"providers":[{"id":"custom-openai","name":"自定义服务","protocol":"openai","base_url":"http://127.0.0.1:9000/","api_key":"token-a","model":"gpt-test","priority":77,"features":["表格"],"description":"自定义 provider","enabled":true,"timeout_sec":25}]}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	providers := loadCenterProviders()
	if len(providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(providers))
	}
	if providers[0].ID != "custom-openai" || providers[0].BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("provider = %+v, want normalized custom provider", providers[0])
	}
	if providers[0].TimeoutSec != 25 {
		t.Fatalf("TimeoutSec = %d, want 25", providers[0].TimeoutSec)
	}
}

func TestCenterStatusSnapshot(t *testing.T) {
	home := setCenterTestHome(t)
	status, err := centerStatusSnapshot()
	if err != nil {
		t.Fatalf("centerStatusSnapshot returned error: %v", err)
	}
	if status.Status != "ok" {
		t.Fatalf("Status = %q, want ok", status.Status)
	}
	if status.RuntimeType != "service" || status.ProductKind != "iworkercenter" || status.AdminConsole != "web_console" {
		t.Fatalf("service identity = runtime_type:%q product_kind:%q admin_console:%q", status.RuntimeType, status.ProductKind, status.AdminConsole)
	}
	if status.ProviderCount != 2 {
		t.Fatalf("ProviderCount = %d, want 2", status.ProviderCount)
	}
	wantPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if status.ConfigPath != wantPath {
		t.Fatalf("ConfigPath = %q, want %q", status.ConfigPath, wantPath)
	}
}

func TestHandleHealthReturnsStatusSnapshot(t *testing.T) {
	home := setCenterTestHome(t)
	server := newCenterServer(":0")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Status              string                           `json:"status"`
		RuntimeType         string                           `json:"runtime_type"`
		ProductKind         string                           `json:"product_kind"`
		AdminConsole        string                           `json:"admin_console"`
		ProviderCount       int                              `json:"provider_count"`
		ConfigPath          string                           `json:"config_path"`
		RuntimeProviderMode string                           `json:"runtime_provider_mode"`
		ComputeSource       string                           `json:"compute_source"`
		ComputeSyncStatus   *centercompute.ComputeSyncStatus `json:"compute_sync_status"`
		CloudProviderCount  int                              `json:"cloud_provider_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("Status = %q, want ok", body.Status)
	}
	if body.RuntimeType != "service" || body.ProductKind != "iworkercenter" || body.AdminConsole != "web_console" {
		t.Fatalf("service identity = runtime_type:%q product_kind:%q admin_console:%q", body.RuntimeType, body.ProductKind, body.AdminConsole)
	}
	if body.ProviderCount != 2 {
		t.Fatalf("ProviderCount = %d, want 2", body.ProviderCount)
	}
	wantPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if body.ConfigPath != wantPath {
		t.Fatalf("ConfigPath = %q, want %q", body.ConfigPath, wantPath)
	}
	if body.RuntimeProviderMode != "settings" {
		t.Fatalf("RuntimeProviderMode = %q, want settings", body.RuntimeProviderMode)
	}
}

func TestHandleHealthIncludesComputeRuntimeStatus(t *testing.T) {
	setCenterTestHome(t)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers": []map[string]any{{
				"id": "p1", "name": "Cloud GPT", "protocol": "openai", "base_url": "https://api.example.com/v1",
				"api_key": "sk-cloud", "model": "gpt-cloud", "enabled": true, "priority": 88, "compute_type": "coding",
			}},
			"compute_permission": true,
		})
	}))
	defer cloud.Close()

	syncMgr := centercompute.NewSyncManager(cloud.URL, "center-1", "secret-1")
	if err := syncMgr.SyncNow(); err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	server := newCenterServer(":0")
	server.center = &app.Center{
		ComputeSyncManager:   syncMgr,
		ComputeSourceManager: centercompute.NewSourceManager(syncMgr),
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		ProviderCount       int                              `json:"provider_count"`
		RuntimeProviderMode string                           `json:"runtime_provider_mode"`
		ComputeSource       string                           `json:"compute_source"`
		ComputePermission   bool                             `json:"compute_permission"`
		CloudProviderCount  int                              `json:"cloud_provider_count"`
		ComputeSyncStatus   *centercompute.ComputeSyncStatus `json:"compute_sync_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if body.ProviderCount != 1 || body.CloudProviderCount != 1 {
		t.Fatalf("provider counts = runtime:%d cloud:%d, want 1/1", body.ProviderCount, body.CloudProviderCount)
	}
	if body.RuntimeProviderMode != "cloud_sync" || body.ComputeSource != "cloud" {
		t.Fatalf("runtime mode/source = %q/%q, want cloud_sync/cloud", body.RuntimeProviderMode, body.ComputeSource)
	}
	if !body.ComputePermission {
		t.Fatal("compute_permission should be true")
	}
	if body.ComputeSyncStatus == nil || body.ComputeSyncStatus.Status != "success" {
		t.Fatalf("compute_sync_status = %+v, want success", body.ComputeSyncStatus)
	}
}

func TestPickProviderByModelHint(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	provider := server.pickProvider(openAIChatRequest{Model: "analysis-anthropic"})
	if provider == nil {
		t.Fatalf("pickProvider returned nil")
	}
	if provider.ID != "analysis-anthropic" {
		t.Fatalf("provider.ID = %q, want analysis-anthropic", provider.ID)
	}
}

func TestPickProviderByFeatureMatch(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	provider := server.pickProvider(openAIChatRequest{
		Messages: []openAIChatMessage{{Role: "user", Content: "请帮我整理会议纪要和正式通知"}},
	})
	if provider == nil {
		t.Fatalf("pickProvider returned nil")
	}
	if provider.ID != "office-openai" {
		t.Fatalf("provider.ID = %q, want office-openai", provider.ID)
	}
}

func TestHandleChatCompletionsFallsBackToNextProvider(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	attempted := make([]string, 0, 2)
	server.forward = func(_ context.Context, provider CenterProvider, _ openAIChatRequest) ([]byte, error) {
		attempted = append(attempted, provider.ID)
		if provider.ID == "office-openai" {
			return nil, errors.New("primary provider failed")
		}
		return []byte(`{"id":"ok","object":"chat.completion","created":1,"model":"analysis-anthropic","choices":[{"index":0,"message":{"role":"assistant","content":"分析结果"},"finish_reason":"stop"}]}`), nil
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"请帮我整理会议纪要和正式通知"}]}`))
	rec := httptest.NewRecorder()

	server.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(attempted) != 2 {
		t.Fatalf("attempted = %v, want 2 providers", attempted)
	}
	if attempted[0] != "office-openai" || attempted[1] != "analysis-anthropic" {
		t.Fatalf("attempted = %v, want [office-openai analysis-anthropic]", attempted)
	}
	if !strings.Contains(rec.Body.String(), "分析结果") {
		t.Fatalf("response body = %s, want fallback response", rec.Body.String())
	}
}

func TestBuildAnthropicFallbackBody(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"分析结果"}]}`)
	converted, err := convertAnthropicBodyDirect(body, "analysis-anthropic")
	if err != nil {
		t.Fatalf("convertAnthropicBodyDirect returned error: %v", err)
	}
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if parsed.Model != "analysis-anthropic" {
		t.Fatalf("Model = %q, want analysis-anthropic", parsed.Model)
	}
	if len(parsed.Choices) != 1 || parsed.Choices[0].Message.Content != "分析结果" {
		t.Fatalf("unexpected choices: %+v", parsed.Choices)
	}
}

func TestHandleHealthIncludesCloudHeartbeatSnapshot(t *testing.T) {
	setCenterTestHome(t)
	seen := make(chan struct{}, 1)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- struct{}{}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer cloud.Close()

	monitor := tenant.NewCloudHeartbeatMonitor(tenant.NewCloudClient(tenant.CloudConfig{BaseURL: cloud.URL}), func(context.Context) (string, string, error) {
		return "center-1", "secret", nil
	}, time.Hour)
	monitor.Start()
	defer monitor.Stop()
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("heartbeat was not sent")
	}
	deadline := time.Now().Add(time.Second)
	for monitor.Snapshot().Status != "online" {
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat snapshot did not become online: %+v", monitor.Snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}

	server := newCenterServer(":0")
	server.center = &app.Center{CloudHeartbeatMonitor: monitor}
	req := httptest.NewRequest(http.MethodGet, "/api/center/status", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		CloudHeartbeat *tenant.CloudHeartbeatSnapshot `json:"cloud_heartbeat"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if body.CloudHeartbeat == nil {
		t.Fatal("cloud_heartbeat is missing")
	}
	if body.CloudHeartbeat.Status != "online" || body.CloudHeartbeat.CenterID != "center-1" {
		t.Fatalf("cloud_heartbeat = %+v", body.CloudHeartbeat)
	}
}

func TestCloudComputeProvidersToCenterProviderFiles(t *testing.T) {
	providers := cloudComputeProvidersToCenterProviderFiles([]tenant.CloudComputeProvider{
		{
			ID:                  "p1",
			Name:                "Cloud GPT",
			BaseURL:             "https://llm.example/v1/",
			APIKey:              "sk-cloud",
			Protocol:            "openai",
			ComputeType:         "high",
			Model:               "gpt-4.1",
			Enabled:             true,
			Priority:            80,
			Description:         "cloud assigned provider",
			InputPricePerMToken: 1.25,
		},
		{ID: "disabled", BaseURL: "https://disabled.example", Protocol: "openai", Model: "gpt-4.1", Enabled: false},
		{ID: "bad-protocol", BaseURL: "https://bad.example", Protocol: "unknown", Model: "x", Enabled: true},
	})
	if len(providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(providers))
	}
	got := providers[0]
	if got.ID != "cloud-p1" || got.BaseURL != "https://llm.example/v1" || got.APIKey != "sk-cloud" || got.Protocol != "openai" || got.Model != "gpt-4.1" {
		t.Fatalf("provider = %+v", got)
	}
	if got.CostTier != "high" || got.Priority != 80 || got.TimeoutSec != 60 {
		t.Fatalf("provider tier/priority/timeout = %+v", got)
	}
	if len(got.Features) == 0 {
		t.Fatalf("features should be derived from cloud metadata")
	}
}

func TestCloudProviderCostTierFallsBackToPrice(t *testing.T) {
	low := cloudProviderCostTier(tenant.CloudComputeProvider{InputPricePerMToken: 0.2, OutputPricePerMToken: 0.3})
	medium := cloudProviderCostTier(tenant.CloudComputeProvider{InputPricePerMToken: 2, OutputPricePerMToken: 3})
	high := cloudProviderCostTier(tenant.CloudComputeProvider{InputPricePerMToken: 6, OutputPricePerMToken: 6})
	if low != "low" || medium != "medium" || high != "high" {
		t.Fatalf("tiers = %q/%q/%q, want low/medium/high", low, medium, high)
	}
}
