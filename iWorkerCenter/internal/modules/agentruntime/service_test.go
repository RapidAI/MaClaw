package agentruntime

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE iworker_agent_instances (
		tenant_id TEXT NOT NULL,
		worker_id TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		role TEXT NOT NULL,
		status TEXT NOT NULL,
		org_unit_id TEXT NOT NULL DEFAULT '',
		capabilities_json TEXT NOT NULL DEFAULT '[]',
		memory_authority TEXT NOT NULL DEFAULT 'iWorkerCenter',
		local_cache_mode TEXT NOT NULL DEFAULT 'cache_only',
		host_id TEXT NOT NULL DEFAULT '',
		process_id INTEGER NOT NULL DEFAULT 0,
		started_at TEXT NOT NULL,
		last_heartbeat_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (tenant_id, instance_id)
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return NewRepo(db, db)
}

func TestHeartbeatIncludesRuntimeSkillsWhenProviderConfigured(t *testing.T) {
	svc := NewService(newTestRepo(t))
	svc.SetRuntimeSkillProvider(fakeRuntimeSkillProvider{skills: []RuntimeSkill{{CapabilityID: "cap-1", Name: "Skill One"}}})
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	result, err := svc.Heartbeat("tenant-a", HeartbeatRequest{WorkerID: "worker-a", InstanceID: "worker-a:executor", Role: "executor"}, now)
	if err != nil {
		t.Fatalf("Heartbeat returned error: %v", err)
	}
	if len(result.RuntimeSkills) != 1 || result.RuntimeSkills[0].CapabilityID != "cap-1" {
		t.Fatalf("runtime skills = %+v", result.RuntimeSkills)
	}
}

func TestHeartbeatKeepsAliveWhenRuntimeSkillProviderFails(t *testing.T) {
	svc := NewService(newTestRepo(t))
	svc.SetRuntimeSkillProvider(fakeRuntimeSkillProvider{err: fmt.Errorf("skill db unavailable")})
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	result, err := svc.Heartbeat("tenant-a", HeartbeatRequest{WorkerID: "worker-a", InstanceID: "worker-a:executor", Role: "executor"}, now)
	if err != nil {
		t.Fatalf("Heartbeat returned error: %v", err)
	}
	if result.RuntimeSkillError == "" || result.Instance.WorkerID != "worker-a" {
		t.Fatalf("result = %+v", result)
	}
}

type fakeRuntimeSkillProvider struct {
	skills []RuntimeSkill
	err    error
}

func (f fakeRuntimeSkillProvider) RuntimeSkillsForWorker(context.Context, string, string) ([]RuntimeSkill, error) {
	return f.skills, f.err
}

func TestHeartbeatUpsertsInstance(t *testing.T) {
	svc := NewService(newTestRepo(t))
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	result, err := svc.Heartbeat("tenant-a", HeartbeatRequest{
		WorkerID:        "worker-a",
		InstanceID:      "worker-a:interaction",
		Role:            "interaction",
		Status:          "online",
		OrgUnitID:       "sales",
		Capabilities:    []string{"human_im", "voice_dialogue"},
		MemoryAuthority: "iWorkerCenter",
		LocalCacheMode:  "cache_only",
		HostID:          "host-a",
		ProcessID:       42,
		StartedAt:       now.Add(-time.Hour).Format(time.RFC3339),
	}, now)
	if err != nil {
		t.Fatalf("Heartbeat returned error: %v", err)
	}
	if result.Instance.TenantID != "tenant-a" || result.Instance.WorkerID != "worker-a" || result.Instance.Role != "interaction" {
		t.Fatalf("unexpected instance: %+v", result.Instance)
	}
	if len(result.Instance.Capabilities) != 2 {
		t.Fatalf("capabilities = %+v", result.Instance.Capabilities)
	}

	_, err = svc.Heartbeat("tenant-a", HeartbeatRequest{
		WorkerID:   "worker-a",
		InstanceID: "worker-a:interaction",
		Role:       "interaction",
		Status:     "busy",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second Heartbeat returned error: %v", err)
	}
	items, err := svc.List("tenant-a", "worker-a")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 1 || items[0].Status != "busy" {
		t.Fatalf("unexpected list after upsert: %+v", items)
	}
}

func TestHeartbeatRequiresWorkerID(t *testing.T) {
	svc := NewService(newTestRepo(t))
	_, err := svc.Heartbeat("tenant-a", HeartbeatRequest{Role: "executor"}, time.Now().UTC())
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestListWithHealthMarksStaleInstancesOffline(t *testing.T) {
	svc := NewService(newTestRepo(t))
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	_, err := svc.Heartbeat("tenant-a", HeartbeatRequest{WorkerID: "worker-a", InstanceID: "worker-a:executor", Role: "executor", Status: "online"}, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("Heartbeat returned error: %v", err)
	}
	items, err := svc.ListWithHealth("tenant-a", "worker-a", now, 90*time.Second)
	if err != nil {
		t.Fatalf("ListWithHealth returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Status != "online" || items[0].EffectiveStatus != "offline" {
		t.Fatalf("unexpected status fields: %+v", items[0])
	}
	if items[0].HeartbeatAgeSeconds != 120 {
		t.Fatalf("HeartbeatAgeSeconds = %d, want 120", items[0].HeartbeatAgeSeconds)
	}
}
