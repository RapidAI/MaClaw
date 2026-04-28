package main

import (
	"testing"
	"time"
)

func TestBuildAgentRuntimeSnapshotCreatesParallelInstances(t *testing.T) {
	settings := defaultDiWorkerSettings()
	settings.Center.Enabled = true
	settings.Center.TenantID = "tenant-a"
	settings.Center.DepartmentID = "sales"
	settings.Center.WorkerID = "xiaodi"

	snapshot := buildAgentRuntimeSnapshot(settings, time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC))

	if snapshot.WorkerID != "xiaodi" || snapshot.TenantID != "tenant-a" || snapshot.OrgUnitID != "sales" {
		t.Fatalf("unexpected identity fields: %+v", snapshot)
	}
	if !snapshot.CenterRegistered {
		t.Fatalf("CenterRegistered = false, want true")
	}
	if snapshot.MemoryAuthority != "iWorkerCenter" || snapshot.LocalMemoryBehavior != "cache_only" {
		t.Fatalf("unexpected memory contract: %+v", snapshot)
	}
	if len(snapshot.Instances) < 2 {
		t.Fatalf("instances len = %d, want at least 2", len(snapshot.Instances))
	}

	roles := map[AgentInstanceRole]bool{}
	for _, instance := range snapshot.Instances {
		if instance.WorkerID != "xiaodi" {
			t.Fatalf("instance WorkerID = %q, want xiaodi", instance.WorkerID)
		}
		if instance.Status != AgentStatusOnline {
			t.Fatalf("instance status = %q, want online", instance.Status)
		}
		roles[instance.Role] = true
	}
	if !roles[AgentRoleInteraction] || !roles[AgentRoleExecutor] {
		t.Fatalf("runtime must include interaction and executor roles, got %+v", roles)
	}
}

func TestBuildAgentRuntimeSnapshotMarksUnregisteredMemoryAsDegraded(t *testing.T) {
	settings := defaultDiWorkerSettings()
	settings.Center.Enabled = false

	snapshot := buildAgentRuntimeSnapshot(settings, time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC))

	if snapshot.CenterRegistered {
		t.Fatalf("CenterRegistered = true, want false")
	}
	if snapshot.MemoryAuthority != "unregistered-iworkercenter" {
		t.Fatalf("MemoryAuthority = %q", snapshot.MemoryAuthority)
	}
	for _, instance := range snapshot.Instances {
		if instance.Status != AgentStatusDegraded {
			t.Fatalf("instance status = %q, want degraded", instance.Status)
		}
	}
}
