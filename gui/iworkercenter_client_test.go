package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIWorkerCenterClientListsAndRecoversEligibleGoalWatchPushes(t *testing.T) {
	recovered := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/client/goalwatch/pushes":
			if got := r.URL.Query().Get("colleague_id"); got != "worker-a" {
				t.Fatalf("colleague_id = %q, want worker-a", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"pushes": []IWorkerCenterPush{
				{EventID: "evt-recover", TaskID: "task-1", WorkflowStepInstanceID: "wfsi-1", RecoveryAction: "resume_workflow_step", RecoveryPath: "/runtime/workflows/steps/wfsi-1/resume"},
				{EventID: "evt-skip", TaskID: "task-2", RecoveryAction: "manual_review"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/client/goalwatch/pushes/evt-recover/recover":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["colleague_id"] != "worker-a" || body["note"] != "watcher_tick" {
				t.Fatalf("recover body = %+v", body)
			}
			recovered = append(recovered, "evt-recover")
			_ = json.NewEncoder(w).Encode(IWorkerCenterRecoverResult{Status: "recovered", Ack: IWorkerCenterAckResult{Status: "recovered"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client, err := NewIWorkerCenterClient(IWorkerCenterClientConfig{BaseURL: server.URL, TenantID: "tenant-a", ColleagueID: "worker-a", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	summary := client.RecoverEligibleGoalWatchPushes(10, "watcher_tick")
	if summary.Checked != 2 || summary.Recovered != 1 || summary.Skipped != 1 || len(summary.Errors) != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(recovered) != 1 || recovered[0] != "evt-recover" {
		t.Fatalf("recovered = %+v", recovered)
	}
}

func TestIWorkerCenterClientAckGoalWatchPush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/client/goalwatch/pushes/evt-ack/ack" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["colleague_id"] != "worker-a" || body["status"] != "blocked" || body["note"] != "need human" {
			t.Fatalf("ack body = %+v", body)
		}
		_ = json.NewEncoder(w).Encode(IWorkerCenterAckResult{EventID: "evt-ack", Status: "blocked", Note: "need human"})
	}))
	defer server.Close()

	client, err := NewIWorkerCenterClient(IWorkerCenterClientConfig{BaseURL: server.URL, TenantID: "tenant-a", ColleagueID: "worker-a", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ack, err := client.AckGoalWatchPush("evt-ack", "blocked", "need human")
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if ack.Status != "blocked" || ack.Note != "need human" {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestIsIWorkerCenterRecoverablePushRequiresWorkflowContext(t *testing.T) {
	if IsIWorkerCenterRecoverablePush(IWorkerCenterPush{EventID: "evt", RecoveryAction: "resume_workflow_step"}) {
		t.Fatal("push without workflow step should not be recoverable")
	}
	if IsIWorkerCenterRecoverablePush(IWorkerCenterPush{EventID: "evt", WorkflowStepInstanceID: "wfsi", RecoveryAction: "manual_review"}) {
		t.Fatal("manual review push should not be auto recoverable")
	}
	if !IsIWorkerCenterRecoverablePush(IWorkerCenterPush{EventID: "evt", WorkflowStepInstanceID: "wfsi", RecoveryAction: "start_workflow_step"}) {
		t.Fatal("workflow start push should be recoverable")
	}
}

func TestIWorkerCenterClientSendHeartbeatFillsDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/runtime/iworker/instances/heartbeat" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		var body IWorkerCenterHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.WorkerID != "worker-a" || body.Role != "watcher" || body.Status != "online" {
			t.Fatalf("identity defaults = %+v", body)
		}
		if body.InstanceID != "worker-a:watcher" {
			t.Fatalf("instance_id = %q", body.InstanceID)
		}
		if body.MemoryAuthority != "iWorkerCenter" || body.LocalCacheMode != "cache_only" {
			t.Fatalf("memory/cache defaults = %+v", body)
		}
		_ = json.NewEncoder(w).Encode(IWorkerCenterHeartbeatResult{Instance: IWorkerCenterInstance{TenantID: "tenant-a", WorkerID: body.WorkerID, InstanceID: body.InstanceID, Role: body.Role, Status: body.Status, EffectiveStatus: "online"}})
	}))
	defer server.Close()

	client, err := NewIWorkerCenterClient(IWorkerCenterClientConfig{BaseURL: server.URL, TenantID: "tenant-a", ColleagueID: "worker-a", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	result, err := client.SendHeartbeat(IWorkerCenterHeartbeatRequest{})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if result.Instance.WorkerID != "worker-a" || result.Instance.InstanceID != "worker-a:watcher" || result.Instance.EffectiveStatus != "online" {
		t.Fatalf("result = %+v", result)
	}
}

func TestIWorkerCenterClientValidatesServiceIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/center/status" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("X-Tenant-ID = %q, want tenant-a", got)
		}
		_ = json.NewEncoder(w).Encode(IWorkerCenterServiceStatus{Status: "ok", RuntimeType: "service", ProductKind: "iworkercenter", AdminConsole: "web_console", ProviderCount: 2, CloudHeartbeat: &IWorkerCenterCloudHeartbeatStatus{Configured: true, Status: "online", CenterID: "ctr-1", RuntimeType: "service", ProductKind: "iworkercenter", AdminConsole: "web_console"}})
	}))
	defer server.Close()

	client, err := NewIWorkerCenterClient(IWorkerCenterClientConfig{BaseURL: server.URL, TenantID: "tenant-a", ColleagueID: "worker-a", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	status, err := client.ValidateServiceIdentity(nil)
	if err != nil {
		t.Fatalf("validate service: %v", err)
	}
	if !status.IsIWorkerCenterService() || status.ProviderCount != 2 {
		t.Fatalf("status = %+v", status)
	}
	if status.CloudHeartbeat == nil || status.CloudHeartbeat.Status != "online" || status.CloudHeartbeat.CenterID != "ctr-1" {
		t.Fatalf("cloud heartbeat = %+v", status.CloudHeartbeat)
	}
}

func TestIWorkerCenterClientRejectsNonCenterServiceIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(IWorkerCenterServiceStatus{Status: "ok", RuntimeType: "desktop", ProductKind: "iworker", AdminConsole: "desktop_gui"})
	}))
	defer server.Close()

	client, err := NewIWorkerCenterClient(IWorkerCenterClientConfig{BaseURL: server.URL, TenantID: "tenant-a", ColleagueID: "worker-a", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	status, err := client.ValidateServiceIdentity(nil)
	if err == nil {
		t.Fatalf("expected non-center identity error, status=%+v", status)
	}
}
