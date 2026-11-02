package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// A failed coding workbench turn often enters no runtime ledger and leaves no
// V2 workflow. The task list used to fall through to the has_output fallback
// and show the row as completed while the chat header said failed.
func TestTaskListShowsFailedWorkbenchTurnAsFailed(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	app.aiConversationMemory = memory

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	owner := projectSessionOwnerID(projectPath)
	handler := &IMMessageHandler{app: app}
	handler.stickyCodingWorkbenchMemory.Store(owner, stickyCodingWorkbenchMemory{
		Kind: "local", LastOutcome: "failed", LastOutcomeAtUnix: time.Now().Unix(),
	})
	app.imHandler = handler

	result := &ProjectSearchResult{
		ProjectPath: projectPath,
		HasOutput:   true,
		Tags:        []string{"coding_dev", "working_dir:" + workDir},
	}
	app.applyTaskListWorkflow(result, projectPath, nil, nil, nil, nil, nil)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != "failed" {
		t.Fatalf("failed turn row = %#v, want failed over the has_output fallback", result.ActiveWorkflow)
	}
	if result.ActiveWorkflow.UpdatedAt.IsZero() {
		t.Fatal("workbench outcome row has no time")
	}

	// While that task's session runs again, the live run is the row.
	live := map[string]struct{}{normalizeRecentTaskPathKey(projectPath): {}}
	app.applyTaskListWorkflow(result, projectPath, nil, nil, nil, nil, live)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != string(codingruntime.TaskRunning) {
		t.Fatalf("live run row = %#v, want running over the failed outcome", result.ActiveWorkflow)
	}
}

// A later successful turn is the newer fact: it replaces an older failed
// ledger row and restores the row to completed.
func TestLaterWorkbenchSuccessReplacesOlderLedgerFailure(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	ref := normalizeCodingRuntimeRef(workDir)
	tags := []string{"coding_dev", "working_dir:" + workDir}
	owner := projectSessionOwnerID(projectPath)
	handler := &IMMessageHandler{app: app}
	handler.stickyCodingWorkbenchMemory.Store(owner, stickyCodingWorkbenchMemory{
		Kind: "local", LastOutcome: "completed", LastOutcomeAtUnix: time.Now().Unix(),
	})
	app.imHandler = handler

	ledger := map[string]string{ref: string(codingruntime.TaskFailed)}
	ledgerAt := map[string]time.Time{ref: time.Now().Add(-time.Hour)}
	result := &ProjectSearchResult{ProjectPath: projectPath, HasOutput: true, Tags: tags}
	app.applyTaskListWorkflow(result, projectPath, nil, ledger, ledgerAt, nil, nil)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != string(codingruntime.TaskCompleted) {
		t.Fatalf("row = %#v, want the later completed turn over the older ledger failure", result.ActiveWorkflow)
	}
}

// An older workbench outcome must not hide a newer terminal snapshot, and a
// non-coding row never reads the workbench memory at all.
func TestWorkbenchOutcomeLosesToNewerSnapshotAndSkipsNonCodingRows(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	tags := []string{"coding_dev", "working_dir:" + workDir}
	owner := projectSessionOwnerID(projectPath)
	failedAt := time.Now()
	handler := &IMMessageHandler{app: app}
	handler.stickyCodingWorkbenchMemory.Store(owner, stickyCodingWorkbenchMemory{
		Kind: "local", LastOutcome: "completed", LastOutcomeAtUnix: failedAt.Add(-time.Hour).Unix(),
	})
	app.imHandler = handler

	// The snapshot is the newer fact and stays.
	snapshot := map[string]*workflow.WorkflowState{
		owner: {UserID: owner, Status: "failed", ProjectPath: projectPath, UpdatedAt: failedAt},
	}
	result := &ProjectSearchResult{ProjectPath: projectPath, HasOutput: true, Tags: tags}
	app.applyTaskListWorkflow(result, projectPath, snapshot, nil, nil, nil, nil)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != "failed" {
		t.Fatalf("row = %#v, want the newer failed snapshot over the older outcome", result.ActiveWorkflow)
	}

	// A row without coding tags never reads the workbench memory.
	plain := &ProjectSearchResult{ProjectPath: projectPath, HasOutput: false}
	app.applyTaskListWorkflow(plain, projectPath, nil, nil, nil, nil, nil)
	if plain.ActiveWorkflow != nil {
		t.Fatalf("non-coding row = %#v, want no workbench outcome projection", plain.ActiveWorkflow)
	}
}

// The recovery slot still competes by time: a newer interrupted slot outranks
// an older failed turn outcome.
func TestNewerRecoverySlotOutranksOlderWorkbenchOutcome(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	app.aiConversationMemory = memory

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	owner := projectSessionOwnerID(projectPath)
	memory.UpsertUnfinishedSlot(owner, &agent.UnfinishedTaskSlot{
		SlotID: "shutdown-1", Status: agent.UnfinishedTaskSlotStatusInterrupted,
	})
	slot := memory.GetUnfinishedSlot(owner)
	if slot == nil || slot.UpdatedAt.IsZero() {
		t.Fatal("recovery slot has no time")
	}
	handler := &IMMessageHandler{app: app}
	handler.stickyCodingWorkbenchMemory.Store(owner, stickyCodingWorkbenchMemory{
		Kind: "local", LastOutcome: "failed", LastOutcomeAtUnix: slot.UpdatedAt.Add(-time.Hour).Unix(),
	})
	app.imHandler = handler

	result := &ProjectSearchResult{
		ProjectPath: projectPath,
		HasOutput:   true,
		Tags:        []string{"coding_dev", "working_dir:" + workDir},
	}
	app.applyTaskListWorkflow(result, projectPath, nil, nil, nil, nil, nil)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != "interrupted" || result.ActiveWorkflow.ID != "shutdown-1" {
		t.Fatalf("row = %#v, want the newer recovery slot", result.ActiveWorkflow)
	}
}

// The turn recorders write the outcome their result describes, and an
// unmapped status leaves the previous outcome untouched.
func TestRecordStickyCodingTurnOutcome(t *testing.T) {
	projectPath := filepath.ToSlash(t.TempDir())
	owner := projectSessionOwnerID(projectPath)
	handler := &IMMessageHandler{}

	handler.recordStickyLocalCodingTurn(owner, projectPath, "fix the bug", &CodingSubAgentResult{Status: TaskExecFailed, Summary: "verify FAIL"}, "")
	mem := handler.getStickyCodingWorkbenchMemory(owner)
	if mem.LastOutcome != "failed" || mem.LastOutcomeAtUnix <= 0 {
		t.Fatalf("local failed turn outcome = %q/%d", mem.LastOutcome, mem.LastOutcomeAtUnix)
	}

	handler.recordStickyLocalCodingTurn(owner, projectPath, "fix the bug", &CodingSubAgentResult{Status: TaskExecPassed, Summary: "done"}, "")
	mem = handler.getStickyCodingWorkbenchMemory(owner)
	if mem.LastOutcome != "completed" {
		t.Fatalf("local passed turn outcome = %q, want completed", mem.LastOutcome)
	}

	// A skipped result is not an outcome: the completed fact stays.
	handler.recordStickyLocalCodingTurn(owner, projectPath, "fix the bug", &CodingSubAgentResult{Status: TaskExecSkipped, Summary: "skipped"}, "")
	mem = handler.getStickyCodingWorkbenchMemory(owner)
	if mem.LastOutcome != "completed" {
		t.Fatalf("skipped tail changed the outcome to %q", mem.LastOutcome)
	}

	handler.recordStickyLocalCodingTurn(owner, projectPath, "fix the bug", &CodingSubAgentResult{Status: TaskExecInterrupted, Summary: "cancelled"}, "")
	mem = handler.getStickyCodingWorkbenchMemory(owner)
	if mem.LastOutcome != "interrupted" {
		t.Fatalf("interrupted turn outcome = %q", mem.LastOutcome)
	}

	// The disk file carries the outcome, so the projection survives a restart.
	loaded, found, _ := loadStickyCodingWorkbenchMemoryFromDisk(owner)
	if !found || loaded.LastOutcome != "interrupted" {
		t.Fatalf("persisted outcome = %#v (found=%v)", loaded, found)
	}

	handler.recordStickyRemoteCodingTurn(owner, "fix remote", &RemoteCodingSubAgentResult{Status: "failed", Summary: "step failed"}, "")
	mem = handler.getStickyCodingWorkbenchMemory(owner)
	if mem.LastOutcome != "failed" {
		t.Fatalf("remote failed turn outcome = %q", mem.LastOutcome)
	}
	handler.recordStickyRemoteCodingTurn(owner, "fix remote", &RemoteCodingSubAgentResult{Status: "success", Summary: "ok"}, "")
	mem = handler.getStickyCodingWorkbenchMemory(owner)
	if mem.LastOutcome != "completed" {
		t.Fatalf("remote success outcome = %q", mem.LastOutcome)
	}

	handler.recordStickyLocalCodingTurn(owner, projectPath, "继续优化", &CodingSubAgentResult{
		Status: TaskExecFailed, Error: "max iterations reached", Summary: "任务运行错误 T1，300 轮迭代，322 次工具调用",
	}, "")
	mem = handler.getStickyCodingWorkbenchMemory(owner)
	if mem.LastOutcome != "completed" {
		t.Fatalf("iteration cap changed the task row to %q", mem.LastOutcome)
	}
	if strings.Contains(mem.LastSummary, "任务运行错误") || strings.Contains(mem.LastSummary, "max iterations") {
		t.Fatalf("iteration cap was carried as the next round's summary: %q", mem.LastSummary)
	}
}

// A workbench memory without an outcome (or without a time) projects nothing.
func TestWorkbenchOutcomeWithoutTimeProjectsNothing(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	owner := projectSessionOwnerID(projectPath)
	handler := &IMMessageHandler{app: app}
	handler.stickyCodingWorkbenchMemory.Store(owner, stickyCodingWorkbenchMemory{Kind: "local", LastOutcome: "failed"})
	app.imHandler = handler

	if outcome := app.stickyCodingTurnOutcomeForTask(projectPath, []string{"coding_dev", "working_dir:" + workDir}); outcome != nil {
		t.Fatalf("undated outcome = %#v, want nil", outcome)
	}

	// The disk fallback answers when no handler holds the memory.
	if h := app.imHandler; h != nil {
		h.stickyCodingWorkbenchMemory.Delete(owner)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, stickyCodingMemoryFileName)
	payload := `{"kind":"local","last_outcome":"failed","last_outcome_at_unix":1700000000}`
	if err := os.WriteFile(file, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	app.imHandler = nil
	outcome := app.stickyCodingTurnOutcomeForTask(filepath.ToSlash(dir), []string{"coding_dev", "working_dir:" + dir})
	if outcome == nil || outcome.Status != "failed" || outcome.UpdatedAt.IsZero() {
		t.Fatalf("disk outcome = %#v, want failed with a time", outcome)
	}
}

// A parallel wave can finish its last step successfully after an earlier step
// failed. The turn is failed; the last-finishing step must not decide it.
func TestLocalCodingTurnOutcomeUsesTheWholePlan(t *testing.T) {
	passed := workflow.TaskRunResult{TaskIndex: 1, Status: workflow.TaskPassed}
	failed := workflow.TaskRunResult{TaskIndex: 2, Status: workflow.TaskFailed}
	skipped := workflow.TaskRunResult{TaskIndex: 3, Status: workflow.TaskSkipped}

	if got := localCodingTurnOutcome([]workflow.TaskRunResult{passed}, nil); got != "completed" {
		t.Fatalf("single pass = %q, want completed", got)
	}
	// The mixed wave: T1 passed (and finished last), T2 failed, T3 skipped.
	if got := localCodingTurnOutcome([]workflow.TaskRunResult{passed, failed, skipped}, nil); got != "failed" {
		t.Fatalf("mixed wave = %q, want failed", got)
	}
	// A stop after a failed step is still a failed turn: the header badge
	// reports the failure, so the row must too.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := localCodingTurnOutcome([]workflow.TaskRunResult{passed, failed}, ctx); got != "failed" {
		t.Fatalf("cancelled after failure = %q, want failed", got)
	}
	// A stop before any step produced a verdict is a cancellation.
	if got := localCodingTurnOutcome(nil, ctx); got != "cancelled" {
		t.Fatalf("cancelled before start = %q, want cancelled", got)
	}
	// Every step skipped without a stop says nothing.
	if got := localCodingTurnOutcome([]workflow.TaskRunResult{skipped}, nil); got != "" {
		t.Fatalf("all skipped = %q, want no verdict", got)
	}

	if got := codingWorkbenchTurnOutcomeFromPlan("completed", 2, false); got != "failed" {
		t.Fatalf("plan failure over executor success = %q, want failed", got)
	}
	if got := codingWorkbenchTurnOutcomeFromPlan("completed", 0, true); got != "cancelled" {
		t.Fatalf("clean plan, user stop = %q, want cancelled", got)
	}
}

// The outcome is written at turn end, so an exact same-second tie with the
// ledger goes to the outcome. Otherwise a step that finished in that second
// could keep a failed turn looking completed.
func TestWorkbenchOutcomeWinsSameSecondTie(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()

	const projectPath = "D:/work/task"
	const workDir = "D:/repo"
	ref := normalizeCodingRuntimeRef(workDir)
	owner := projectSessionOwnerID(projectPath)
	tiedAt := time.Unix(1_700_000_000, 0)
	handler := &IMMessageHandler{app: app}
	handler.stickyCodingWorkbenchMemory.Store(owner, stickyCodingWorkbenchMemory{
		Kind: "local", LastOutcome: "failed", LastOutcomeAtUnix: tiedAt.Unix(),
	})
	app.imHandler = handler

	ledger := map[string]string{ref: string(codingruntime.TaskCompleted)}
	ledgerAt := map[string]time.Time{ref: tiedAt}
	result := &ProjectSearchResult{
		ProjectPath: projectPath,
		HasOutput:   true,
		Tags:        []string{"coding_dev", "working_dir:" + workDir},
	}
	app.applyTaskListWorkflow(result, projectPath, nil, ledger, ledgerAt, nil, nil)
	if result.ActiveWorkflow == nil || result.ActiveWorkflow.Status != "failed" {
		t.Fatalf("tied row = %#v, want the failed turn outcome", result.ActiveWorkflow)
	}
}
