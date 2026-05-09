package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpireStaleInFlightTaskCreatesRecoverySlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	runID := "loop-1"
	now := time.Now()
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "check maclaw logs"}})
	cm.SetInFlightTaskForRun(userID, "check maclaw logs", "/project", runID)

	sh := cm.shard(userID)
	sh.mu.Lock()
	sh.sessions[userID].inFlightSetAt = now.Add(-InFlightTaskLease - time.Second)
	sh.mu.Unlock()

	if expired := cm.ExpireStaleInFlightTasks(now, InFlightTaskLease); expired != 1 {
		t.Fatalf("expected 1 expired in-flight task, got %d", expired)
	}
	if task, _ := cm.ConsumeInFlightTask(userID); task != "" {
		t.Fatalf("expected in-flight task to be cleared, got %q", task)
	}
	slot := cm.GetUnfinishedSlot(userID)
	if slot == nil {
		t.Fatal("expected stale in-flight task to create recovery slot")
	}
	if slot.LastTask != "check maclaw logs" {
		t.Fatalf("expected LastTask to be preserved, got %q", slot.LastTask)
	}
	if slot.ProjectPath != "/project" {
		t.Fatalf("expected project path to be preserved, got %q", slot.ProjectPath)
	}
	if slot.Source != "in_flight_lease_expired" {
		t.Fatalf("expected lease-expired source, got %q", slot.Source)
	}
	if slot.EvidenceScopeKey != inFlightRunScopeKey(runID) {
		t.Fatalf("expected slot to be tied to run %q, got %q", runID, slot.EvidenceScopeKey)
	}
	if active := cm.ActiveUnfinishedSlot(userID); active != nil {
		t.Fatalf("expected expired in-flight slot to require explicit resume, got active=%#v", active)
	}
}

func TestFreshInFlightTaskDoesNotExpire(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	now := time.Now()
	cm.SetInFlightTask(userID, "fresh task", "/project")

	if expired := cm.ExpireStaleInFlightTasks(now, InFlightTaskLease); expired != 0 {
		t.Fatalf("expected no expiration, got %d", expired)
	}
	task, projectPath := cm.ConsumeInFlightTask(userID)
	if task != "fresh task" || projectPath != "/project" {
		t.Fatalf("expected fresh in-flight task to remain consumable, got task=%q project=%q", task, projectPath)
	}
}

func TestRefreshInFlightTaskRenewsAfterInterval(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	now := time.Now()
	cm.SetInFlightTask(userID, "long task", "/project")

	sh := cm.shard(userID)
	sh.mu.Lock()
	sh.sessions[userID].inFlightSetAt = now.Add(-InFlightTaskRenewInterval - time.Second)
	sh.mu.Unlock()

	if !cm.RefreshInFlightTask(userID) {
		t.Fatal("expected refresh to find active in-flight task")
	}
	if expired := cm.ExpireStaleInFlightTasks(now.Add(InFlightTaskLease/2), InFlightTaskLease); expired != 0 {
		t.Fatalf("expected renewed in-flight task to stay active, got %d expirations", expired)
	}
	task, projectPath := cm.ConsumeInFlightTask(userID)
	if task != "long task" || projectPath != "/project" {
		t.Fatalf("expected renewed in-flight task to remain consumable, got task=%q project=%q", task, projectPath)
	}
}

func TestClearInFlightTaskForRunRemovesMatchingExpiredRecoverySlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	runID := "loop-1"
	now := time.Now()
	cm.SetInFlightTaskForRun(userID, "long task", "/project", runID)

	sh := cm.shard(userID)
	sh.mu.Lock()
	sh.sessions[userID].inFlightSetAt = now.Add(-InFlightTaskLease - time.Second)
	sh.mu.Unlock()

	if expired := cm.ExpireStaleInFlightTasks(now, InFlightTaskLease); expired != 1 {
		t.Fatalf("expected 1 expired in-flight task, got %d", expired)
	}
	cm.ClearInFlightTaskForRun(userID, runID)

	if slot := cm.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("expected matching synthetic recovery slot to be cleared, got source=%q", slot.Source)
	}
}

func TestClearInFlightTaskForRunKeepsMismatchedExpiredRecoverySlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	now := time.Now()
	cm.SetInFlightTaskForRun(userID, "long task", "/project", "loop-1")

	sh := cm.shard(userID)
	sh.mu.Lock()
	sh.sessions[userID].inFlightSetAt = now.Add(-InFlightTaskLease - time.Second)
	sh.mu.Unlock()

	if expired := cm.ExpireStaleInFlightTasks(now, InFlightTaskLease); expired != 1 {
		t.Fatalf("expected 1 expired in-flight task, got %d", expired)
	}
	cm.ClearInFlightTaskForRun(userID, "loop-2")

	if slot := cm.GetUnfinishedSlot(userID); slot == nil {
		t.Fatal("expected mismatched synthetic recovery slot to remain")
	}
}

func TestClearInFlightTaskForRunDoesNotClearMismatchedActiveMarker(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	cm.SetInFlightTaskForRun(userID, "active task", "/project", "loop-1")
	cm.ClearInFlightTaskForRun(userID, "loop-2")

	task, projectPath := cm.ConsumeInFlightTask(userID)
	if task != "active task" || projectPath != "/project" {
		t.Fatalf("expected mismatched run clear to keep active marker, got task=%q project=%q", task, projectPath)
	}
}

func TestClearInFlightTaskForRunDoesNotClearLegacyActiveMarkerForNamedRun(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	cm.SetInFlightTask(userID, "legacy active task", "/project")
	cm.ClearInFlightTaskForRun(userID, "new-loop")

	task, projectPath := cm.ConsumeInFlightTask(userID)
	if task != "legacy active task" || projectPath != "/project" {
		t.Fatalf("expected named run clear to keep legacy active marker, got task=%q project=%q", task, projectPath)
	}
}

func TestClearInFlightTaskForRunKeepsLegacyExpiredRecoverySlotForNamedRun(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "desktop-user"
	now := time.Now()
	cm.SetInFlightTaskForRun(userID, "legacy task", "/project", "")

	sh := cm.shard(userID)
	sh.mu.Lock()
	sh.sessions[userID].inFlightSetAt = now.Add(-InFlightTaskLease - time.Second)
	sh.mu.Unlock()

	if expired := cm.ExpireStaleInFlightTasks(now, InFlightTaskLease); expired != 1 {
		t.Fatalf("expected 1 expired in-flight task, got %d", expired)
	}
	cm.ClearInFlightTaskForRun(userID, "new-loop")

	if slot := cm.GetUnfinishedSlot(userID); slot == nil {
		t.Fatal("expected named run clear to keep legacy recovery slot")
	} else if slot.EvidenceScopeKey != "" {
		t.Fatalf("expected legacy recovery slot to have empty scope, got %q", slot.EvidenceScopeKey)
	}
}

func TestLegacyInFlightTaskExpiresOnLoad(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "conversation.json")
	old := time.Now().Add(-InFlightTaskLease - time.Hour)
	snapshot := memorySnapshot{Sessions: map[string]persistedSession{
		"desktop-user": {
			Entries:             []ConversationEntry{{Role: "user", Content: "legacy task"}},
			LastAccess:          old,
			InFlightTask:        "legacy task",
			InFlightProjectPath: "/project",
		},
	}}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := os.WriteFile(storePath, data, 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	cm := NewPersistentConversationMemory(storePath)
	defer cm.Stop()

	if task, _ := cm.ConsumeInFlightTask("desktop-user"); task != "" {
		t.Fatalf("expected legacy stale in-flight task to be converted, got %q", task)
	}
	slot := cm.GetUnfinishedSlot("desktop-user")
	if slot == nil {
		t.Fatal("expected legacy stale in-flight task to create recovery slot on load")
	}
	if slot.LastTask != "legacy task" {
		t.Fatalf("expected legacy task in recovery slot, got %q", slot.LastTask)
	}
}
