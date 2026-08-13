package agent

import (
	"encoding/json"
	"errors"
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

func TestPersistentMemoryPromotesFreshCheckpointOnLoad(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.PersistInFlightCheckpoint(
		"desktop-user",
		[]ConversationEntry{
			{Role: "user", Content: "implement recovery"},
			{Role: "assistant", Content: "working", ToolCalls: []map[string]string{{"id": "call-1"}}},
			{Role: "tool", Content: "done", ToolCallID: "call-1", ToolName: "write_file"},
		},
		"implement recovery", "/project", "run-1",
		InFlightCheckpoint{Sequence: 1, LastToolName: "write_file", SideEffectState: "local_committed"},
	)
	cm.Stop()

	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	if task, _ := reloaded.ConsumeInFlightTask("desktop-user"); task != "" {
		t.Fatalf("marker remained after startup promotion: %q", task)
	}
	slot := reloaded.GetUnfinishedSlot("desktop-user")
	if slot == nil {
		t.Fatal("expected fresh checkpoint to become a recovery slot immediately")
	}
	if slot.Source != UnfinishedTaskSlotSourceInFlightRecovery || slot.EvidenceScopeKey != inFlightRunScopeKey("run-1") {
		t.Fatalf("unexpected recovery slot: %#v", slot)
	}
	if slot.LastToolName != "write_file" || slot.SideEffectState != "local_committed" || slot.RecoveryMode != "requires_review" {
		t.Fatalf("checkpoint evidence not preserved: %#v", slot)
	}
	if got := reloaded.Load("desktop-user"); len(got) != 3 || got[2].Role != "tool" {
		t.Fatalf("recovery history incomplete: %#v", got)
	}
}

func TestRecoveryModeForSideEffectRequiresReviewUnlessReadOnly(t *testing.T) {
	for _, sideEffect := range []string{"", "local_committed", "external_uncertain", "unknown"} {
		if got := recoveryModeForSideEffect(sideEffect); got != "requires_review" {
			t.Fatalf("recoveryModeForSideEffect(%q) = %q, want requires_review", sideEffect, got)
		}
	}
	if got := recoveryModeForSideEffect("none"); got != "resume_context" {
		t.Fatalf("recoveryModeForSideEffect(none) = %q, want resume_context", got)
	}
}

func TestSaveAndCompleteInFlightCheckpointForRunPersistsPairedInteractiveHistory(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	entries := []ConversationEntry{
		{Role: "user", Content: "need a decision"},
		{Role: "assistant", Content: "", ToolCalls: []map[string]string{{"id": "ask-1"}}},
		{Role: "tool", Content: "Asked user: continue?", ToolCallID: "ask-1", ToolName: "ask_user", ToolOutcome: "paused"},
	}
	if err := cm.PersistInFlightCheckpoint("desktop-user", entries[:1], "need a decision", "/project", "run-1", InFlightCheckpoint{
		Sequence: 1, LastToolName: "ask_user", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatalf("PersistInFlightCheckpoint() error = %v", err)
	}
	if err := cm.SaveAndCompleteInFlightCheckpointForRun("desktop-user", "run-1", entries); err != nil {
		t.Fatalf("SaveAndCompleteInFlightCheckpointForRun() error = %v", err)
	}
	cm.Stop()

	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	if slot := reloaded.GetUnfinishedSlot("desktop-user"); slot != nil {
		t.Fatalf("interactive pause must not promote a recovery slot: %#v", slot)
	}
	if got := reloaded.Load("desktop-user"); len(got) != len(entries) || got[2].ToolCallID != "ask-1" {
		t.Fatalf("paired interactive history was not persisted: %#v", got)
	}
}

func TestSaveAndCompleteInFlightCheckpointForRunRejectsDifferentRun(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	cm.SetInFlightTaskForRun("user", "active", "/project", "run-owner")
	if err := cm.SaveAndCompleteInFlightCheckpointForRun("user", "run-stale", []ConversationEntry{{Role: "user", Content: "stale"}}); !errors.Is(err, ErrInFlightCheckpointRunConflict) {
		t.Fatalf("SaveAndCompleteInFlightCheckpointForRun() error = %v, want run conflict", err)
	}
	if task, _ := cm.ConsumeInFlightTask("user"); task != "active" {
		t.Fatalf("different run overwrote active marker: %q", task)
	}
}

func TestSaveAndCompleteInFlightCheckpointForRunPreservesPendingRecoverySlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{SlotID: "pending-recovery", Status: UnfinishedTaskSlotStatusInterrupted, LastTask: "prior task"})
	if err := cm.SaveAndCompleteInFlightCheckpointForRun("user", "run-stale", []ConversationEntry{{Role: "user", Content: "stale"}}); !errors.Is(err, ErrInFlightCheckpointRunConflict) {
		t.Fatalf("SaveAndCompleteInFlightCheckpointForRun() error = %v, want pending-slot conflict", err)
	}
	if slot := cm.GetUnfinishedSlot("user"); slot == nil || slot.SlotID != "pending-recovery" {
		t.Fatalf("pending recovery slot was overwritten: %#v", slot)
	}
}

func TestCheckpointFlushFailureRollsBackInMemoryCandidate(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	defer cm.Stop()
	const userID = "desktop-user"
	original := []ConversationEntry{{Role: "user", Content: "durable original"}}
	cm.Save(userID, original)
	if err := cm.FlushNow(); err != nil {
		t.Fatalf("initial FlushNow() error = %v", err)
	}
	blockedPath := filepath.Join(dir, "blocked-store")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("make blocked store path a directory: %v", err)
	}
	cm.storePath = blockedPath

	if err := cm.PersistInFlightCheckpoint(userID, []ConversationEntry{{Role: "user", Content: "unsafe candidate"}}, "unsafe candidate", "/project", "run-1", InFlightCheckpoint{Sequence: 1}); err == nil {
		t.Fatal("PersistInFlightCheckpoint() succeeded despite failed disk write")
	}
	if got := cm.Load(userID); len(got) != 1 || got[0].Content != "durable original" {
		t.Fatalf("failed checkpoint leaked into live history: %#v", got)
	}
	if task, _ := cm.ConsumeInFlightTask(userID); task != "" {
		t.Fatalf("failed checkpoint leaked marker: %q", task)
	}
}

func TestInteractiveCheckpointFlushFailureRestoresPriorMarker(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	defer cm.Stop()
	const userID, runID = "desktop-user", "run-1"
	original := []ConversationEntry{{Role: "user", Content: "awaiting tool"}}
	if err := cm.PersistInFlightCheckpoint(userID, original, "awaiting tool", "/project", runID, InFlightCheckpoint{Sequence: 1}); err != nil {
		t.Fatalf("PersistInFlightCheckpoint() error = %v", err)
	}
	blockedPath := filepath.Join(dir, "blocked-store")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("make blocked store path a directory: %v", err)
	}
	cm.storePath = blockedPath

	paired := append(append([]ConversationEntry(nil), original...), ConversationEntry{Role: "assistant", ToolCalls: []map[string]string{{"id": "ask-1"}}}, ConversationEntry{Role: "tool", Content: "Asked user", ToolCallID: "ask-1", ToolName: "ask_user"})
	if err := cm.SaveAndCompleteInFlightCheckpointForRun(userID, runID, paired); err == nil {
		t.Fatal("SaveAndCompleteInFlightCheckpointForRun() succeeded despite failed disk write")
	}
	if got := cm.Load(userID); len(got) != len(original) || got[0].Content != "awaiting tool" {
		t.Fatalf("failed interactive checkpoint leaked paired history: %#v", got)
	}
	if task, _ := cm.ConsumeInFlightTask(userID); task != "awaiting tool" {
		t.Fatalf("failed interactive checkpoint cleared prior marker: %q", task)
	}
}

func TestCheckpointCompletionFlushFailureRestoresMarker(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	defer cm.Stop()
	const userID, runID = "desktop-user", "run-1"
	if err := cm.PersistInFlightCheckpoint(userID, []ConversationEntry{{Role: "user", Content: "finish safely"}}, "finish safely", "/project", runID, InFlightCheckpoint{Sequence: 1}); err != nil {
		t.Fatalf("PersistInFlightCheckpoint() error = %v", err)
	}
	blockedPath := filepath.Join(dir, "blocked-store")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("make blocked store path a directory: %v", err)
	}
	cm.storePath = blockedPath

	if err := cm.CompleteInFlightCheckpointForRun(userID, runID); err == nil {
		t.Fatal("CompleteInFlightCheckpointForRun() succeeded despite failed disk write")
	}
	if task, _ := cm.ConsumeInFlightTask(userID); task != "finish safely" {
		t.Fatalf("failed completion cleared recoverable marker: %q", task)
	}
}

func TestPromoteCheckpointPreservesExistingPendingSlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{SlotID: "existing", Status: UnfinishedTaskSlotStatusInterrupted, LastTask: "older task"})
	cm.SetInFlightTaskForRun("user", "newer task", "/project", "run-2")
	if promoted := cm.PromoteRecoverableCheckpoints(time.Now()); promoted != 0 {
		t.Fatalf("promoted = %d, want 0 with an existing pending slot", promoted)
	}
	if slot := cm.GetUnfinishedSlot("user"); slot == nil || slot.SlotID != "existing" {
		t.Fatalf("existing pending slot was replaced: %#v", slot)
	}
	if task, _ := cm.ConsumeInFlightTask("user"); task != "" {
		t.Fatalf("marker not cleared after duplicate suppression: %q", task)
	}
}

func TestPromoteCheckpointReplacesResolvedSlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{SlotID: "completed", Status: UnfinishedTaskSlotStatusCompleted, LastTask: "old task"})
	cm.SetInFlightTaskForRun("user", "new task", "/project", "run-3")
	if promoted := cm.PromoteRecoverableCheckpoints(time.Now()); promoted != 1 {
		t.Fatalf("promoted = %d, want 1 after resolved slot", promoted)
	}
	slot := cm.GetUnfinishedSlot("user")
	if slot == nil || slot.LastTask != "new task" || slot.Source != UnfinishedTaskSlotSourceInFlightRecovery {
		t.Fatalf("resolved slot prevented recovery promotion: %#v", slot)
	}
}

func TestExpireStaleInFlightReplacesResolvedSlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	now := time.Now()
	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{
		SlotID:   "completed-slot",
		Status:   UnfinishedTaskSlotStatusCompleted,
		LastTask: "old task",
	})
	cm.SetInFlightTaskForRun("user", "stalled task", "/project", "run-stalled")
	sh := cm.shard("user")
	sh.mu.Lock()
	sh.sessions["user"].inFlightSetAt = now.Add(-InFlightTaskLease - time.Second)
	sh.mu.Unlock()

	if expired := cm.ExpireStaleInFlightTasks(now, InFlightTaskLease); expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}
	slot := cm.GetUnfinishedSlot("user")
	if slot == nil || slot.LastTask != "stalled task" || slot.Source != UnfinishedTaskSlotSourceInFlightLeaseExpired {
		t.Fatalf("resolved slot blocked lease-expiry recovery: %#v", slot)
	}
}

func TestExpireStaleInFlightPreservesPendingSlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	now := time.Now()
	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{
		SlotID:   "pending-slot",
		Status:   UnfinishedTaskSlotStatusInterrupted,
		LastTask: "awaiting decision",
	})
	cm.SetInFlightTaskForRun("user", "stalled task", "/project", "run-stalled")
	sh := cm.shard("user")
	sh.mu.Lock()
	sh.sessions["user"].inFlightSetAt = now.Add(-InFlightTaskLease - time.Second)
	sh.mu.Unlock()

	if expired := cm.ExpireStaleInFlightTasks(now, InFlightTaskLease); expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}
	slot := cm.GetUnfinishedSlot("user")
	if slot == nil || slot.SlotID != "pending-slot" || slot.LastTask != "awaiting decision" {
		t.Fatalf("pending slot was overwritten: %#v", slot)
	}
}

func TestCompleteInFlightCheckpointForRunDoesNotClearNewerRun(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	cm.SetInFlightTaskForRun("user", "newer task", "/project", "run-new")
	if err := cm.CompleteInFlightCheckpointForRun("user", "run-old"); err != nil {
		t.Fatalf("CompleteInFlightCheckpointForRun() error = %v", err)
	}
	task, projectPath := cm.ConsumeInFlightTask("user")
	if task != "newer task" || projectPath != "/project" {
		t.Fatalf("old completion cleared newer marker: task=%q project=%q", task, projectPath)
	}
}

func TestPersistInFlightCheckpointDoesNotOverwriteNewerRun(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	cm.SetInFlightTaskForRun("user", "new task", "/new-project", "run-new")
	err := cm.PersistInFlightCheckpoint(
		"user",
		[]ConversationEntry{{Role: "user", Content: "old task"}},
		"old task", "/old-project", "run-old",
		InFlightCheckpoint{Sequence: 1, LastToolName: "write_file", SideEffectState: "local_committed"},
	)
	if !errors.Is(err, ErrInFlightCheckpointRunConflict) {
		t.Fatalf("PersistInFlightCheckpoint() error = %v, want run conflict", err)
	}
	task, projectPath := cm.ConsumeInFlightTask("user")
	if task != "new task" || projectPath != "/new-project" {
		t.Fatalf("old checkpoint overwrote newer marker: task=%q project=%q", task, projectPath)
	}
	if got := cm.Load("user"); len(got) != 0 {
		t.Fatalf("old checkpoint overwrote newer history: %#v", got)
	}
}

func TestPersistInFlightCheckpointRequiresRunID(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	err := cm.PersistInFlightCheckpoint(
		"user", []ConversationEntry{{Role: "user", Content: "task"}}, "task", "/project", "",
		InFlightCheckpoint{Sequence: 1},
	)
	if !errors.Is(err, ErrInFlightCheckpointRunConflict) {
		t.Fatalf("PersistInFlightCheckpoint() error = %v, want missing-run conflict", err)
	}
}

func TestPersistInFlightCheckpointDoesNotOverwritePendingSlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{
		SlotID:   "pending-recovery",
		Status:   UnfinishedTaskSlotStatusInterrupted,
		LastTask: "recover before starting new work",
	})
	err := cm.PersistInFlightCheckpoint(
		"user",
		[]ConversationEntry{{Role: "user", Content: "new task"}},
		"new task", "/project", "run-new", InFlightCheckpoint{Sequence: 1},
	)
	if !errors.Is(err, ErrInFlightCheckpointRunConflict) {
		t.Fatalf("PersistInFlightCheckpoint() error = %v, want pending-slot conflict", err)
	}
	slot := cm.GetUnfinishedSlot("user")
	if slot == nil || slot.SlotID != "pending-recovery" {
		t.Fatalf("pending slot was overwritten: %#v", slot)
	}
	if got := cm.Load("user"); len(got) != 0 {
		t.Fatalf("checkpoint changed history despite pending slot: %#v", got)
	}
}

func TestNewRecoverySlotIDIsUniqueWithinSameMillisecond(t *testing.T) {
	now := time.UnixMilli(1234)
	first := newRecoverySlotID("inflight-recovery", now)
	second := newRecoverySlotID("inflight-recovery", now)
	if first == second {
		t.Fatalf("same-millisecond recovery IDs collided: %q", first)
	}
}

func TestReplaceInFlightWithUnfinishedSlotReplacesResolvedSlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{
		SlotID:   "completed-slot",
		Status:   UnfinishedTaskSlotStatusCompleted,
		LastTask: "old task",
	})
	cm.SetInFlightTaskForRun("user", "active task", "/project", "run-4")
	if !cm.ReplaceInFlightWithUnfinishedSlot("user", &UnfinishedTaskSlot{
		SlotID:   "shutdown-slot",
		Status:   UnfinishedTaskSlotStatusInterrupted,
		LastTask: "active task",
	}) {
		t.Fatal("resolved slot must not block a newer shutdown snapshot")
	}
	slot := cm.GetUnfinishedSlot("user")
	if slot == nil || slot.SlotID != "shutdown-slot" || slot.LastTask != "active task" {
		t.Fatalf("replacement slot = %#v", slot)
	}
	if task, _ := cm.ConsumeInFlightTask("user"); task != "" {
		t.Fatalf("marker remained after replacement: %q", task)
	}
}
