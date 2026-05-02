package tenant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudHeartbeatMonitorSendsImmediateServiceHeartbeat(t *testing.T) {
	seen := make(chan CenterHeartbeatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-1/heartbeat" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var req CenterHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		seen <- req
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	monitor := NewCloudHeartbeatMonitor(client, func(context.Context) (string, string, error) {
		return "center-1", "secret-abc", nil
	}, time.Hour)
	monitor.SetReadinessResolver(func(context.Context) *CloudIWorkerReadiness {
		return &CloudIWorkerReadiness{Ready: true, Status: "ready", AgentInstanceCount: 1}
	})
	monitor.Start()
	defer monitor.Stop()

	select {
	case req := <-seen:
		if req.Secret != "secret-abc" || req.RuntimeType != "service" || req.ProductKind != "iworkercenter" || req.AdminConsole != "web_console" {
			t.Fatalf("heartbeat request = %+v", req)
		}
		if req.IWorkerReadiness == nil || req.IWorkerReadiness.Status != "ready" {
			t.Fatalf("IWorkerReadiness = %+v", req.IWorkerReadiness)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat was not sent immediately")
	}
}

func TestCloudHeartbeatMonitorSkipsMissingCredentials(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	monitor := NewCloudHeartbeatMonitor(client, func(context.Context) (string, string, error) {
		return "", "", nil
	}, time.Hour)
	monitor.Start()
	time.Sleep(50 * time.Millisecond)
	monitor.Stop()
	if calls.Load() != 0 {
		t.Fatalf("cloud received %d heartbeat calls, want 0", calls.Load())
	}
}

func TestCloudHeartbeatMonitorStopBeforeStartDoesNotBlock(t *testing.T) {
	monitor := NewCloudHeartbeatMonitor(NewCloudClient(CloudConfig{BaseURL: "https://cloud.example.com"}), func(context.Context) (string, string, error) {
		return "center-1", "secret", nil
	}, time.Hour)
	done := make(chan struct{})
	go func() {
		monitor.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked before Start")
	}
}

func TestCloudHeartbeatMonitorSnapshotTracksSuccessAndFailure(t *testing.T) {
	client := NewCloudClient(CloudConfig{BaseURL: "https://cloud.example.com"})
	monitor := NewCloudHeartbeatMonitor(client, func(context.Context) (string, string, error) {
		return "center-1", "secret", nil
	}, time.Hour)

	initial := monitor.Snapshot()
	if initial.Status != "waiting_for_credentials" || !initial.Configured {
		t.Fatalf("initial snapshot = %+v", initial)
	}

	monitor.recordAttempt("center-1", time.Now().UTC())
	monitor.recordFailure("network down")
	failed := monitor.Snapshot()
	if failed.Status != "error" || failed.LastError != "network down" || failed.ConsecutiveFailures != 1 {
		t.Fatalf("failed snapshot = %+v", failed)
	}

	monitor.recordSuccess(time.Now().UTC())
	succeeded := monitor.Snapshot()
	if succeeded.Status != "online" || succeeded.LastError != "" || succeeded.ConsecutiveFailures != 0 || succeeded.CenterID != "center-1" {
		t.Fatalf("success snapshot = %+v", succeeded)
	}
}

func TestCloudHeartbeatMonitorTriggerNowSendsReadiness(t *testing.T) {
	seen := make(chan CenterHeartbeatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req CenterHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		seen <- req
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	monitor := NewCloudHeartbeatMonitor(NewCloudClient(CloudConfig{BaseURL: srv.URL}), func(context.Context) (string, string, error) {
		return "center-1", "secret-abc", nil
	}, time.Hour)
	monitor.SetReadinessResolver(func(context.Context) *CloudIWorkerReadiness {
		return &CloudIWorkerReadiness{Ready: true, Status: "ready", AgentInstanceCount: 2}
	})
	monitor.TriggerNow()

	select {
	case req := <-seen:
		if req.IWorkerReadiness == nil || req.IWorkerReadiness.AgentInstanceCount != 2 {
			t.Fatalf("readiness = %+v", req.IWorkerReadiness)
		}
	case <-time.After(time.Second):
		t.Fatal("triggered heartbeat was not sent")
	}
}
