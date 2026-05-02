package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/agentruntime"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/goalwatch"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func newReadinessTestCenter(t *testing.T) *Center {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Center{DB: provider}
}

func TestRuntimeStatusSnapshotIncludesProviderConfigMetadata(t *testing.T) {
	center := newReadinessTestCenter(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	settingsPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"providers":[{"id":"p1"},{"id":"p2"}]}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	status := center.RuntimeStatusSnapshot()
	if status.ConfigPath != settingsPath {
		t.Fatalf("ConfigPath = %q, want %q", status.ConfigPath, settingsPath)
	}
	if status.ProviderCount != 2 {
		t.Fatalf("ProviderCount = %d, want 2", status.ProviderCount)
	}
}

func TestIWorkerReadinessSnapshotReportsMissingBootstrapData(t *testing.T) {
	center := newReadinessTestCenter(t)
	status := center.IWorkerReadinessSnapshot()

	if status.Ready || status.Status != "needs_bootstrap" {
		t.Fatalf("readiness = %+v, want needs_bootstrap", status)
	}
	if status.TenantCount != 0 || status.RoleCount != 0 || status.ColleagueCount != 0 || status.LocalAccountCount != 0 || status.AgentInstanceCount != 0 {
		t.Fatalf("counts = tenants:%d roles:%d colleagues:%d accounts:%d agents:%d, want all zero", status.TenantCount, status.RoleCount, status.ColleagueCount, status.LocalAccountCount, status.AgentInstanceCount)
	}
	if len(status.RequiredClientPaths) == 0 || len(status.AuthMethods) != 3 {
		t.Fatalf("paths/auth methods missing: %+v", status)
	}
	if !containsString(status.RequiredClientPaths, "/client/iworker/instances") || containsString(status.RequiredClientPaths, "/client/iworker/agent-instances") {
		t.Fatalf("iWorker runtime path mismatch: %+v", status.RequiredClientPaths)
	}
	if status.AuthMethods[0].Method != "local" || status.AuthMethods[0].Ready {
		t.Fatalf("local auth readiness = %+v, want not ready", status.AuthMethods[0])
	}
}

func TestIWorkerReadinessSnapshotReportsReadyCenter(t *testing.T) {
	center := newReadinessTestCenter(t)
	center.Mux = newTestMux()
	center.AgentRuntime = nonNilAgentRuntime()
	center.GoalWatch = nonNilGoalWatch()
	center.GoalMonitor = nonNilGoalMonitor()

	_, err := center.DB.Write.Exec(`
		INSERT INTO tenants (id, company_name, email, status) VALUES ('tenant-a', 'Acme', 'admin@example.com', 'active');
		INSERT INTO roles (id, name, code, tenant_id, status) VALUES ('role-ops', 'Ops', 'ops', 'tenant-a', 'active');
		INSERT INTO colleagues (id, name, role_id, tenant_id, status) VALUES ('worker-ops', 'Ops iWorker', 'role-ops', 'tenant-a', 'active');
		INSERT INTO diworker_accounts (id, username, password_hash, salt, identifier, tenant_id, disabled) VALUES ('acct-1', 'alice', 'hash', 'salt', 'worker-ops', 'tenant-a', 0);
		INSERT INTO iworker_agent_instances (tenant_id, worker_id, instance_id, role, status, started_at, last_heartbeat_at, updated_at) VALUES ('tenant-a', 'worker-ops', 'worker-ops:executor', 'executor', 'online', datetime('now'), datetime('now'), datetime('now'));
	`)
	if err != nil {
		t.Fatalf("seed readiness data: %v", err)
	}

	status := center.IWorkerReadinessSnapshot()
	if !status.Ready || status.Status != "ready" {
		t.Fatalf("readiness = %+v, want ready", status)
	}
	if status.TenantCount != 1 || status.RoleCount != 1 || status.ColleagueCount != 1 || status.LocalAccountCount != 1 || status.AgentInstanceCount != 1 {
		t.Fatalf("counts = tenants:%d roles:%d colleagues:%d accounts:%d agents:%d, want all one", status.TenantCount, status.RoleCount, status.ColleagueCount, status.LocalAccountCount, status.AgentInstanceCount)
	}
	if !status.AgentRuntimeReady || !status.GoalWatchReady {
		t.Fatalf("runtime readiness = agent:%t goal:%t", status.AgentRuntimeReady, status.GoalWatchReady)
	}
	if !hasReadyCheck(status.Checks, "agent_instances") {
		t.Fatalf("agent_instances readiness check missing or not ready: %+v", status.Checks)
	}
	if !status.AuthMethods[0].Ready || status.AuthMethods[0].Status != "ready" {
		t.Fatalf("local auth readiness = %+v, want ready", status.AuthMethods[0])
	}
}

func TestCloudIWorkerReadinessSnapshotIncludesOnlyAggregateWorkload(t *testing.T) {
	center := newReadinessTestCenter(t)
	center.Mux = newTestMux()
	center.AgentRuntime = nonNilAgentRuntime()
	center.GoalWatch = nonNilGoalWatch()
	center.GoalMonitor = nonNilGoalMonitor()

	_, err := center.DB.Write.Exec(`
		INSERT INTO tenants (id, company_name, email, status) VALUES ('tenant-a', 'Acme', 'admin@example.com', 'active');
		INSERT INTO roles (id, name, code, tenant_id, status) VALUES ('role-ops', 'Ops', 'ops', 'tenant-a', 'active');
		INSERT INTO colleagues (id, name, role_id, tenant_id, status) VALUES ('worker-ops', 'Ops iWorker', 'role-ops', 'tenant-a', 'active');
		INSERT INTO iworker_agent_instances (tenant_id, worker_id, instance_id, role, status, work_status_json, started_at, last_heartbeat_at, updated_at)
		VALUES ('tenant-a', 'worker-ops', 'worker-ops:executor', 'executor', 'online', '{"current_task":"Prepare confidential board report","current_detail":"Do not leave Center","active_count":2,"completed_count":3,"review_count":1,"blocked_count":1,"updated_at":"2026-05-02T04:00:00Z"}', datetime('now'), datetime('now'), datetime('now'));
	`)
	if err != nil {
		t.Fatalf("seed workload data: %v", err)
	}

	cloud := center.CloudIWorkerReadinessSnapshot()
	if cloud.WorkloadSummary == nil {
		t.Fatalf("WorkloadSummary missing: %+v", cloud)
	}
	if cloud.WorkloadSummary.AgentInstanceCount != 1 || cloud.WorkloadSummary.ActiveCount != 2 || cloud.WorkloadSummary.CompletedCount != 3 || cloud.WorkloadSummary.ReviewCount != 1 || cloud.WorkloadSummary.BlockedCount != 1 {
		t.Fatalf("WorkloadSummary = %+v", cloud.WorkloadSummary)
	}
	data, err := json.Marshal(cloud)
	if err != nil {
		t.Fatalf("marshal cloud readiness: %v", err)
	}
	for _, forbidden := range []string{"confidential board report", "current_task", "current_detail", "required_client_paths", "checks", "auth_methods", "/client/iworker/instances"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("cloud readiness leaked %q: %s", forbidden, string(data))
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func hasReadyCheck(checks []ReadinessCheck, name string) bool {
	for _, check := range checks {
		if check.Name == name {
			return check.Ready
		}
	}
	return false
}

func newTestMux() *http.ServeMux                { return http.NewServeMux() }
func nonNilAgentRuntime() *agentruntime.Service { return &agentruntime.Service{} }
func nonNilGoalWatch() *goalwatch.Service       { return &goalwatch.Service{} }
func nonNilGoalMonitor() *goalwatch.Monitor     { return &goalwatch.Monitor{} }
