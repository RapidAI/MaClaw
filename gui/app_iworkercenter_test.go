package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNewIWorkerCenterClientFromAppConfigUsesIndependentCenterFields(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteHubURL:             "https://hub.example.test",
		IWorkerCenterURL:         "https://center.example.test/",
		IWorkerCenterTenantID:    "tenant-a",
		IWorkerCenterColleagueID: "worker-a",
	}
	client, err := NewIWorkerCenterClientFromAppConfig(cfg, nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if client.baseURL != "https://center.example.test" || client.tenantID != "tenant-a" || client.colleagueID != "worker-a" {
		t.Fatalf("client = %+v", client)
	}
}

func TestNewIWorkerCenterClientFromAppConfigRequiresCenterFields(t *testing.T) {
	_, err := NewIWorkerCenterClientFromAppConfig(corelib.AppConfig{RemoteHubURL: "https://hub.example.test"}, nil)
	if err == nil {
		t.Fatal("expected missing iWorkerCenter config error")
	}
}

func TestNewIWorkerGoalWatchServiceFromAppConfigUsesInterval(t *testing.T) {
	cfg := corelib.AppConfig{
		IWorkerCenterURL:                  "https://center.example.test",
		IWorkerCenterTenantID:             "tenant-a",
		IWorkerCenterColleagueID:          "worker-a",
		IWorkerCenterGoalWatchIntervalSec: 7,
	}
	service, err := NewIWorkerGoalWatchServiceFromAppConfig(cfg, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if service.interval != 7*time.Second {
		t.Fatalf("interval = %s, want 7s", service.interval)
	}
	if service.heartbeater == nil {
		t.Fatal("expected iWorkerCenter client to be wired as heartbeat sender")
	}
}

func TestIWorkerCenterConfigHelpers(t *testing.T) {
	cfg := corelib.AppConfig{IWorkerCenterURL: "https://center", IWorkerCenterTenantID: "tenant", IWorkerCenterColleagueID: "worker"}
	if !isIWorkerCenterConfigured(cfg) {
		t.Fatal("expected config to be complete")
	}
	if got := normalizedIWorkerGoalWatchIntervalSec(cfg); got != 60 {
		t.Fatalf("default interval = %d, want 60", got)
	}
	cfg.IWorkerCenterGoalWatchIntervalSec = 15
	if got := normalizedIWorkerGoalWatchIntervalSec(cfg); got != 15 {
		t.Fatalf("interval = %d, want 15", got)
	}
}

func TestSaveIWorkerCenterConfigPersistsIndependentFields(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	result := app.SaveIWorkerCenterConfig(IWorkerCenterConfigRequest{URL: " https://center.example.test/ ", TenantID: " tenant-a ", ColleagueID: " worker-a ", GoalWatchIntervalSec: 17})
	if result["ok"] != true || result["configured"] != true {
		t.Fatalf("result = %+v", result)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.IWorkerCenterURL != "https://center.example.test" || cfg.IWorkerCenterTenantID != "tenant-a" || cfg.IWorkerCenterColleagueID != "worker-a" || cfg.IWorkerCenterGoalWatchIntervalSec != 17 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestSaveIWorkerCenterConfigAutoStartsGoalWatch(t *testing.T) {
	requests := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		switch r.URL.Path {
		case "/runtime/iworker/instances/heartbeat":
			_ = json.NewEncoder(w).Encode(IWorkerCenterHeartbeatResult{Instance: IWorkerCenterInstance{WorkerID: "worker-a", Role: "watcher", Status: "online"}})
		case "/client/goalwatch/pushes":
			_ = json.NewEncoder(w).Encode(map[string]any{"pushes": []IWorkerCenterPush{}})
		default:
			t.Fatalf("unexpected path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	app := NewApp()
	app.testHomeDir = t.TempDir()
	result := app.SaveIWorkerCenterConfig(IWorkerCenterConfigRequest{URL: server.URL, TenantID: "tenant-a", ColleagueID: "worker-a", GoalWatchIntervalSec: 60, AutoStart: true})
	defer app.stopIWorkerGoalWatch()
	if result["ok"] != true || result["configured"] != true {
		t.Fatalf("result = %+v", result)
	}
	seenHeartbeat := false
	seenPushPoll := false
	deadline := time.After(time.Second)
	for !seenHeartbeat || !seenPushPoll {
		select {
		case path := <-requests:
			if path == "/runtime/iworker/instances/heartbeat" {
				seenHeartbeat = true
			}
			if path == "/client/goalwatch/pushes" {
				seenPushPoll = true
			}
		case <-deadline:
			t.Fatalf("goalwatch service requests heartbeat=%v push_poll=%v", seenHeartbeat, seenPushPoll)
		}
	}
	if status := app.IWorkerGoalWatchStatus(); status.SkippedReason != "" && status.SkippedReason == "goalwatch_service_not_started" {
		t.Fatalf("status = %+v", status)
	}
}

func TestIWorkerCenterConfigStatusReportsServiceIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/center/status" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(IWorkerCenterServiceStatus{Status: "ok", RuntimeType: "service", ProductKind: "iworkercenter", AdminConsole: "web_console"})
	}))
	defer server.Close()

	app := NewApp()
	app.testHomeDir = t.TempDir()
	result := app.SaveIWorkerCenterConfig(IWorkerCenterConfigRequest{URL: server.URL, TenantID: "tenant-a", ColleagueID: "worker-a", GoalWatchIntervalSec: 60})
	if result["ok"] != true {
		t.Fatalf("save result = %+v", result)
	}
	status := app.IWorkerCenterConfigStatus()
	service, ok := status["center_service"].(map[string]any)
	if !ok || service["ok"] != true {
		t.Fatalf("center_service = %+v", status["center_service"])
	}
}
