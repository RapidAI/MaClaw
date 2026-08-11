package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestParseCodingExecRetryCommand(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"重试失败", codingExecRetryActionFailed},
		{"重试失败任务", codingExecRetryActionFailed},
		{"重試失敗", codingExecRetryActionFailed},
		{"retry failed", codingExecRetryActionFailed},
		{"继续执行", codingExecRetryActionResume},
		{"继续编码", codingExecRetryActionResume},
		{"繼續執行", codingExecRetryActionResume},
		{"continue coding", codingExecRetryActionResume},
		{"review child results", codingExecRetryActionReviewChildren},
		{"审阅子任务结果", codingExecRetryActionReviewChildren},
		{"随便聊聊", ""},
		{"继续", ""}, // too generic — not claimed (workflow confirm)
		// Long chat mentioning retry/fail must not be claimed.
		{"please retry later if the build fails for unrelated reasons and keep going", ""},
	}
	for _, tc := range cases {
		if got := parseCodingExecRetryCommand(tc.in); got != tc.want {
			t.Fatalf("parseCodingExecRetryCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestChildHandoffTaskItemsFromResultsRequiresExplicitRuntimeMarker(t *testing.T) {
	tasks := []*v2.TaskItem{{Index: 1, Title: "handoff"}, {Index: 2, Title: "ordinary skip"}}
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskSkipped, RuntimeTaskID: "runtime-parent", RuntimeHandoff: true},
		{TaskIndex: 2, Status: v2.TaskSkipped, RuntimeTaskID: "unrelated", Error: "dependency not met"},
	}
	got := childHandoffTaskItemsFromResults(tasks, results)
	if len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("child handoff tasks = %#v", got)
	}
}

func TestCodingExecResumeActionsOffersExplicitChildReview(t *testing.T) {
	previous, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { setAgentViewLang(previous) })
	setAgentViewLang("en")
	actions := codingExecResumeActions(codingExecCheckpoint{
		Tasks:   []*v2.TaskItem{{Index: 1, Title: "handoff"}},
		Results: []v2.TaskRunResult{{TaskIndex: 1, Status: v2.TaskSkipped, RuntimeTaskID: "runtime-parent", RuntimeHandoff: true}},
	})
	for _, action := range actions {
		if action.Command == "review child results" {
			return
		}
	}
	t.Fatalf("child review action missing: %#v", actions)
}

func TestCodingExecTextFollowsUILanguage(t *testing.T) {
	previous, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { setAgentViewLang(previous) })

	setAgentViewLang("en")
	if got := codingExecCmdRetryFailed(); got != "retry failed" {
		t.Fatalf("en retry cmd = %q", got)
	}
	if got := codingExecText("Hello", "你好", "你好繁"); got != "Hello" {
		t.Fatalf("en text = %q", got)
	}

	setAgentViewLang("zh-Hans")
	if got := codingExecCmdRetryFailed(); got != "重试失败" {
		t.Fatalf("zh-Hans retry cmd = %q", got)
	}

	setAgentViewLang("zh-Hant")
	if got := codingExecCmdRetryFailed(); got != "重試失敗" {
		t.Fatalf("zh-Hant retry cmd = %q", got)
	}
	if got := codingExecCmdCancelWorkflow(); got != "取消工作流程" {
		t.Fatalf("zh-Hant cancel cmd = %q", got)
	}
	if got := codingExecText("Hello", "你好", "你好繁"); got != "你好繁" {
		t.Fatalf("zh-Hant text = %q", got)
	}
	if got := codingExecStatusLabel(v2.TaskPassed); got != "通過" {
		t.Fatalf("zh-Hant status = %q", got)
	}
}

func TestIncompleteTaskItemsFromResults(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B"},
		{Index: 3, Title: "C"},
		{Index: 4, Title: "D"},
		{Index: 5, Title: "E"},
	}
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskPassed},
		{TaskIndex: 2, Status: v2.TaskFailed},
		{TaskIndex: 3, Status: v2.TaskSkipped, Error: "cancelled"},
		{TaskIndex: 4, Status: v2.TaskSkipped, Error: "dependency not met"},
		{TaskIndex: 5, Status: v2.TaskSkipped, Error: "用户取消"},
	}
	got := incompleteTaskItemsFromResults(tasks, results)
	// Failed + cancelled (en/zh) only; permanent dep-not-met stays out.
	if len(got) != 3 || got[0].Index != 2 || got[1].Index != 3 || got[2].Index != 5 {
		t.Fatalf("incomplete = %#v", got)
	}
}

func TestIsCodingExecCancelError(t *testing.T) {
	if !isCodingExecCancelError("cancelled") || !isCodingExecCancelError("canceled by user") {
		t.Fatal("english cancel")
	}
	if !isCodingExecCancelError("用户取消") {
		t.Fatal("chinese cancel")
	}
	if isCodingExecCancelError("dependency not met") || isCodingExecCancelError("") {
		t.Fatal("non-cancel must be false")
	}
}

func TestPersistCodingExecCheckpointAtomicRename(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	// Seed an existing file so Windows-style replace path is exercised.
	cp := codingExecCheckpoint{
		UserID:      userID,
		ProjectPath: dir,
		Tasks:       []*v2.TaskItem{{Index: 1, Title: "A"}},
		Results:     []v2.TaskRunResult{{TaskIndex: 1, Status: v2.TaskFailed}},
		UpdatedAt:   time.Now(),
		WorkflowID:  "wf-seed",
	}
	persistCodingExecCheckpointToDisk(userID, cp)
	cp.WorkflowID = "wf-updated"
	cp.Tasks = []*v2.TaskItem{{Index: 1, Title: "B"}}
	persistCodingExecCheckpointToDisk(userID, cp)

	got, ok := loadCodingExecCheckpointFromDisk(userID, dir)
	if !ok || got.WorkflowID != "wf-updated" || len(got.Tasks) != 1 || got.Tasks[0].Title != "B" {
		t.Fatalf("reload after overwrite = ok=%v %#v", ok, got)
	}
	// Temp file must not linger.
	if _, err := os.Stat(codingExecCheckpointFilePath(userID, dir) + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should be gone, err=%v", err)
	}
}

func TestRepairCompletedCodingWorkflowProjectionDoesNotReplayExecutor(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	wf := buildWorkflowV2State(v2.NewMemoryStore())
	state := &v2.WorkflowState{
		ID: "wf-completed-ledger", UserID: userID, Type: "coding", ProjectPath: dir,
		Status: v2.StatusActive,
		Phases: []v2.Phase{{ID: v2.PhaseCodingImplementation, ToolPolicy: v2.ToolPolicyFull, Status: v2.PhaseExecuting}},
	}
	if err := wf.store.Save(state); err != nil {
		t.Fatal(err)
	}
	cp := codingExecCheckpoint{
		UserID: userID, WorkflowID: state.ID, WorkflowPhaseID: v2.PhaseCodingImplementation, ProjectPath: dir, ProjectionPending: true, UpdatedAt: time.Now(),
		Tasks:   []*v2.TaskItem{{Index: 1, Title: "already completed"}},
		Results: []v2.TaskRunResult{{TaskIndex: 1, Title: "already completed", Status: v2.TaskPassed, RuntimeTaskID: "opaque-runtime-task"}},
	}
	persistCodingExecCheckpointToDisk(userID, cp)
	app := &App{testHomeDir: t.TempDir()}
	ledger := app.ensureCodingRuntimeStore()
	if ledger == nil {
		t.Fatal("ledger unavailable")
	}
	ledgerTask, err := ledger.CreateTask(codingruntime.Task{TaskID: "opaque-runtime-task", WorkflowID: state.ID, PhaseID: v2.PhaseCodingImplementation, ProjectRef: dir, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ledger.StartAttempt(ledgerTask.TaskID, "test-owner", time.Minute, codingruntime.PolicySnapshot{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.FinishAttempt(attempt.AttemptID, "test-owner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted}, time.Now()); err != nil {
		t.Fatal(err)
	}
	app.repairCompletedCodingWorkflowProjections(wf)
	app.closeCodingRuntimeStore()
	updated, err := wf.store.Load(userID)
	if err != nil || updated == nil || updated.Phases[0].Status != v2.PhaseCompleted || !strings.Contains(updated.Phases[0].Output, "通过: 1") {
		t.Fatalf("completed projection was not repaired: state=%#v err=%v", updated, err)
	}
	if _, err := os.Stat(codingExecCheckpointFilePath(userID, dir)); !os.IsNotExist(err) {
		t.Fatalf("completed projection marker remains: %v", err)
	}
}

func TestRepairCompletedCodingWorkflowProjectionFailsClosedWithoutCompletedLedger(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	wf := buildWorkflowV2State(v2.NewMemoryStore())
	state := &v2.WorkflowState{
		ID: "wf-incomplete-ledger", UserID: userID, Type: "coding", ProjectPath: dir,
		Status: v2.StatusActive,
		Phases: []v2.Phase{{ID: v2.PhaseCodingImplementation, ToolPolicy: v2.ToolPolicyFull, Status: v2.PhaseExecuting}},
	}
	if err := wf.store.Save(state); err != nil {
		t.Fatal(err)
	}
	cp := codingExecCheckpoint{
		UserID: userID, WorkflowID: state.ID, WorkflowPhaseID: v2.PhaseCodingImplementation, ProjectPath: dir, ProjectionPending: true, UpdatedAt: time.Now(),
		Tasks:   []*v2.TaskItem{{Index: 1, Title: "unproven completion"}},
		Results: []v2.TaskRunResult{{TaskIndex: 1, Title: "unproven completion", Status: v2.TaskPassed, RuntimeTaskID: "missing-runtime-task"}},
	}
	persistCodingExecCheckpointToDisk(userID, cp)
	app := &App{testHomeDir: t.TempDir()}
	app.repairCompletedCodingWorkflowProjections(wf)
	app.closeCodingRuntimeStore()
	updated, err := wf.store.Load(userID)
	if err != nil || updated == nil || updated.Phases[0].Status != v2.PhaseExecuting || updated.Phases[0].Output != "" {
		t.Fatalf("unproven ledger result advanced workflow: state=%#v err=%v", updated, err)
	}
	if _, err := os.Stat(codingExecCheckpointFilePath(userID, dir)); err != nil {
		t.Fatalf("unproven projection marker was removed: %v", err)
	}
}

func TestRepairCompletedCodingWorkflowProjectionFailsClosedOnBindingMismatch(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	wf := buildWorkflowV2State(v2.NewMemoryStore())
	state := &v2.WorkflowState{
		ID: "wf-binding", UserID: userID, Type: "coding", ProjectPath: dir, Status: v2.StatusActive,
		Phases: []v2.Phase{{ID: v2.PhaseCodingImplementation, ToolPolicy: v2.ToolPolicyFull, Status: v2.PhaseExecuting}},
	}
	if err := wf.store.Save(state); err != nil {
		t.Fatal(err)
	}
	cp := codingExecCheckpoint{
		UserID: userID, WorkflowID: state.ID, WorkflowPhaseID: v2.PhaseCodingImplementation, ProjectPath: dir, ProjectionPending: true, UpdatedAt: time.Now(),
		Tasks: []*v2.TaskItem{{Index: 1, Title: "wrong binding"}}, Results: []v2.TaskRunResult{{TaskIndex: 1, Title: "wrong binding", Status: v2.TaskPassed, RuntimeTaskID: "wrong-binding-task"}},
	}
	persistCodingExecCheckpointToDisk(userID, cp)
	app := &App{testHomeDir: t.TempDir()}
	ledger := app.ensureCodingRuntimeStore()
	if ledger == nil {
		t.Fatal("ledger unavailable")
	}
	// A terminal task for another phase must never be projected into the
	// active implementation phase during crash recovery.
	task, err := ledger.CreateTask(codingruntime.Task{TaskID: "wrong-binding-task", WorkflowID: state.ID, PhaseID: "review", ProjectRef: dir, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ledger.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: dir, Mode: "local"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted}, time.Now()); err != nil {
		t.Fatal(err)
	}
	app.repairCompletedCodingWorkflowProjections(wf)
	app.closeCodingRuntimeStore()
	updated, err := wf.store.Load(userID)
	if err != nil || updated == nil || updated.Phases[0].Status != v2.PhaseExecuting || updated.Phases[0].Output != "" {
		t.Fatalf("mismatched runtime task advanced workflow: state=%#v err=%v", updated, err)
	}
	if _, err := os.Stat(codingExecCheckpointFilePath(userID, dir)); err != nil {
		t.Fatalf("mismatched projection marker was removed: %v", err)
	}
}

func TestFailedSubsetRerunDoesNotKeepDependsOn(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B", DependsOn: []int{1}},
	}
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskPassed},
		{TaskIndex: 2, Status: v2.TaskFailed, Error: "boom"},
	}
	failed := failedTaskItemsFromResults(tasks, results)
	subset := tasksForSubsetRerun(failed)
	if len(subset) != 1 || subset[0].Index != 2 || len(subset[0].DependsOn) != 0 {
		t.Fatalf("subset = %#v", subset)
	}
}

func TestTasksForSubsetRerunClearsDependsOn(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Index: 2, Title: "B", DependsOn: []int{1}},
	}
	got := tasksForSubsetRerun(tasks)
	if len(got) != 1 || len(got[0].DependsOn) != 0 {
		t.Fatalf("expected DependsOn cleared, got %#v", got[0])
	}
	// Original unchanged.
	if len(tasks[0].DependsOn) != 1 {
		t.Fatal("original task DependsOn mutated")
	}
}

func TestAllCodingTasksPassed(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B"},
	}
	if allCodingTasksPassed(nil, nil) {
		t.Fatal("empty tasks must not pass")
	}
	if allCodingTasksPassed(tasks, nil) {
		t.Fatal("missing results must not pass")
	}
	if allCodingTasksPassed(tasks, []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskPassed},
		{TaskIndex: 2, Status: v2.TaskFailed},
	}) {
		t.Fatal("failed task must not pass")
	}
	if !allCodingTasksPassed(tasks, []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskPassed},
		{TaskIndex: 2, Status: v2.TaskPassed},
	}) {
		t.Fatal("all passed should pass even if a cancel race set cancelled elsewhere")
	}
	// Partial results: only one of two tasks reported.
	if allCodingTasksPassed(tasks, []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskPassed},
	}) {
		t.Fatal("incomplete result set must not pass")
	}
}

func TestBuildCodingRemoteTaskPromptIncludesContext(t *testing.T) {
	desc, ctx := buildCodingRemoteTaskPrompt(&v2.TaskItem{
		Index:       1,
		Title:       "Fix login",
		Description: "Handle timeout",
		Files:       []string{"auth.go"},
	}, "need auth", "jwt design")
	if !strings.Contains(desc, "Fix login") || !strings.Contains(desc, "Handle timeout") || !strings.Contains(desc, "auth.go") {
		t.Fatalf("desc = %q", desc)
	}
	if !strings.Contains(ctx, "need auth") || !strings.Contains(ctx, "jwt design") {
		t.Fatalf("ctx = %q", ctx)
	}
	if mapRemoteCodingSubAgentStatus("ok") != v2.TaskPassed {
		t.Fatal("ok should map to passed")
	}
	if mapRemoteCodingSubAgentStatus("cancelled") != v2.TaskSkipped {
		t.Fatal("cancelled should map to skipped")
	}
	if mapRemoteCodingSubAgentStatus("waiting_child") != v2.TaskSkipped {
		t.Fatal("waiting_child should not be projected as a failed or completed task")
	}
	if got := mapLocalCodingSubAgentStatusForRetry(TaskExecWaitingChild); got != v2.TaskSkipped {
		t.Fatalf("local waiting_child = %q, want skipped", got)
	}
	if mapRemoteCodingSubAgentStatus("boom") != v2.TaskFailed {
		t.Fatal("unknown should map to failed")
	}
}

func TestCodingExecCheckpointMatchesActive(t *testing.T) {
	cp := codingExecCheckpoint{
		WorkflowID: "wf-1",
		Tasks:      []*v2.TaskItem{{Index: 1, Title: "A"}},
		UpdatedAt:  time.Now(),
	}
	if codingExecCheckpointMatchesActive(cp, nil) {
		t.Fatal("nil state must not match")
	}
	wrongType := &v2.WorkflowState{ID: "wf-1", Type: "product_design", Status: v2.StatusActive}
	// Force execution phase: ToolPolicyFull on current phase.
	wrongType.Phases = []v2.Phase{{ID: "implementation", ToolPolicy: v2.ToolPolicyFull, Status: v2.PhaseExecuting}}
	wrongType.CurrentPhase = 0
	if codingExecCheckpointMatchesActive(cp, wrongType) {
		t.Fatal("non-coding type must not match")
	}
	wrongID := &v2.WorkflowState{
		ID: "wf-other", Type: "coding", Status: v2.StatusActive, CurrentPhase: 0,
		Phases: []v2.Phase{{ID: "implementation", ToolPolicy: v2.ToolPolicyFull, Status: v2.PhaseExecuting}},
	}
	if codingExecCheckpointMatchesActive(cp, wrongID) {
		t.Fatal("mismatched workflow id must not match")
	}
	notExec := &v2.WorkflowState{
		ID: "wf-1", Type: "coding", Status: v2.StatusActive, CurrentPhase: 0,
		Phases: []v2.Phase{{ID: "design", ToolPolicy: v2.ToolPolicyDocOnly, Status: v2.PhaseWaitingConfirm}},
	}
	if codingExecCheckpointMatchesActive(cp, notExec) {
		t.Fatal("non-execution phase must not match")
	}
	okState := &v2.WorkflowState{
		ID: "wf-1", Type: "coding", Status: v2.StatusActive, CurrentPhase: 0,
		Phases: []v2.Phase{{ID: "implementation", ToolPolicy: v2.ToolPolicyFull, Status: v2.PhaseExecuting}},
	}
	if !codingExecCheckpointMatchesActive(cp, okState) {
		t.Fatal("matching coding execution state should pass")
	}
	// Legacy checkpoint without workflow id still requires active coding execution.
	legacy := codingExecCheckpoint{Tasks: []*v2.TaskItem{{Index: 1}}, UpdatedAt: time.Now()}
	if !codingExecCheckpointMatchesActive(legacy, okState) {
		t.Fatal("legacy checkpoint with matching phase should pass")
	}
}

func TestCodingExecCheckpointUsableWithoutEngine(t *testing.T) {
	h := &IMMessageHandler{}
	cp := codingExecCheckpoint{
		Tasks:     []*v2.TaskItem{{Index: 1}},
		UpdatedAt: time.Now(),
	}
	if !h.codingExecCheckpointUsable("u1", cp) {
		t.Fatal("bare handler without workflow engine should allow age-valid checkpoint")
	}
}

func TestCodingExecResumeActions(t *testing.T) {
	cp := codingExecCheckpoint{
		Tasks: []*v2.TaskItem{{Index: 1}, {Index: 2}, {Index: 3}},
		Results: []v2.TaskRunResult{
			{TaskIndex: 1, Status: v2.TaskPassed},
			{TaskIndex: 2, Status: v2.TaskFailed},
			{TaskIndex: 3, Status: v2.TaskSkipped, Error: "cancelled"},
		},
	}
	actions := codingExecResumeActions(cp)
	if len(actions) < 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Command != "重试失败" || actions[0].Style != "primary" {
		t.Fatalf("first action = %#v", actions[0])
	}
	if actions[1].Command != "继续执行" {
		t.Fatalf("second action = %#v", actions[1])
	}
	if actions[2].Command != "取消工作流" || actions[2].Style != "danger" {
		t.Fatalf("cancel action = %#v", actions[2])
	}
}

func TestCodingExecCheckpointDurableRoundTrip(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	h := &IMMessageHandler{}
	cp := codingExecCheckpoint{
		ProjectPath: dir,
		IsRemote:    false,
		Tasks:       []*v2.TaskItem{{Index: 1, Title: "A", Description: "do A"}},
		Results:     []v2.TaskRunResult{{TaskIndex: 1, Status: v2.TaskFailed, Error: "boom"}},
		WorkflowID:  "wf-1",
	}
	h.storeCodingExecCheckpoint(userID, cp)

	path := codingExecCheckpointFilePath(userID, dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected disk file at %s: %v", path, err)
	}

	// Clear memory only; disk remains.
	h.codingExecCheckpoint.Delete(userID)
	got, ok := h.loadCodingExecCheckpoint(userID)
	if !ok || len(got.Tasks) != 1 || got.Tasks[0].Title != "A" {
		t.Fatalf("reload from disk = ok=%v %#v", ok, got)
	}
	if got.WorkflowID != "wf-1" {
		t.Fatalf("workflow id = %q", got.WorkflowID)
	}

	h.clearCodingExecCheckpoint(userID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disk file should be removed, err=%v", err)
	}
	if _, ok := h.loadCodingExecCheckpoint(userID); ok {
		t.Fatal("checkpoint should be gone after clear")
	}
}

func TestCodingExecCheckpointExpiredRejected(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	cp := codingExecCheckpoint{
		UserID:      userID,
		ProjectPath: dir,
		Tasks:       []*v2.TaskItem{{Index: 1, Title: "old"}},
		Results:     []v2.TaskRunResult{{TaskIndex: 1, Status: v2.TaskFailed}},
		UpdatedAt:   time.Now().Add(-codingExecCheckpointMaxAge - time.Hour),
	}
	persistCodingExecCheckpointToDisk(userID, cp)
	if codingExecCheckpointStillValid(cp) {
		t.Fatal("expired checkpoint should be invalid")
	}
	h := &IMMessageHandler{}
	if _, ok := h.loadCodingExecCheckpoint(userID); ok {
		t.Fatal("expired disk checkpoint must not load")
	}
}

func TestTryQueueCodingExecRetryCommandNoCheckpoint(t *testing.T) {
	h := &IMMessageHandler{}
	// Without a checkpoint, do not claim the phrase (leave chat free).
	if route := h.tryQueueCodingExecRetryCommand("u1", "重试失败"); route != nil {
		t.Fatalf("expected nil without checkpoint, got %#v", route)
	}
}

func TestTryQueueCodingExecRetryCommandQueues(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "u-retry"
	h.storeCodingExecCheckpoint(userID, codingExecCheckpoint{
		Tasks: []*v2.TaskItem{{Index: 1, Title: "A"}, {Index: 2, Title: "B"}},
		Results: []v2.TaskRunResult{
			{TaskIndex: 1, Status: v2.TaskPassed},
			{TaskIndex: 2, Status: v2.TaskFailed},
		},
	})
	// Sticky pure coding must not survive arming a workflow checkpoint retry.
	h.pendingTemplateRemoteCoding.Store(userID, remoteCodingTemplateContext{SessionID: "ssh-old", ProjectDir: "/old"})
	h.pendingTemplateCodingProjectPath.Store(userID, "D:/old-local")
	route := h.tryQueueCodingExecRetryCommand(userID, "重试失败")
	if route == nil || !route.WorkflowAgentLoop {
		t.Fatalf("expected WorkflowAgentLoop, got %#v", route)
	}
	if route.Response != nil {
		t.Fatal("must not set Response (would skip agent loop)")
	}
	raw, ok := h.pendingCodingExecRetryAction.Load(userID)
	if !ok || raw.(string) != codingExecRetryActionFailed {
		t.Fatalf("pending action = %#v", raw)
	}
	if _, ok := h.pendingTemplateRemoteCoding.Load(userID); ok {
		t.Fatal("pure remote sticky pending must be cleared for workflow retry")
	}
	if _, ok := h.pendingTemplateCodingProjectPath.Load(userID); ok {
		t.Fatal("pure local sticky pending must be cleared for workflow retry")
	}
}

func TestConsumePendingTemplateYieldsToCodingExecRetry(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "u-steal"
	h.pendingV2SubAgentExecution.Store(userID, true)
	h.pendingTemplateRemoteCoding.Store(userID, remoteCodingTemplateContext{SessionID: "ssh-1", ProjectDir: "/repo", WorkDir: "/repo"})
	h.pendingCodingExecRetryAction.Store(userID, codingExecRetryActionFailed)

	resp, handled := h.consumePendingTemplateSubAgentExecution(
		IMUserMessage{UserID: userID, Text: "重试失败"},
		"重试失败", nil, "req-1", nil, nil,
	)
	if handled || resp != nil {
		t.Fatalf("checkpoint retry must win over pure sticky remote, got handled=%v resp=%v", handled, resp)
	}
	// Pure sticky pending must still be present (not consumed) so clearing helpers can drop it.
	if _, ok := h.pendingTemplateRemoteCoding.Load(userID); !ok {
		t.Fatal("pure remote pending should remain until workflow path clears it")
	}
}

func TestHandleWorkflowV2ExecutionPhaseWithProgressConsumesOrphanRetry(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "u-orphan"
	h.pendingCodingExecRetryAction.Store(userID, codingExecRetryActionFailed)
	// No active execution phase → clear message, action consumed.
	resp := h.handleWorkflowV2ExecutionPhaseWithProgress(userID, nil, nil, nil, nil)
	// Default UI lang is zh-Hans; message is localized.
	if resp == nil || !(strings.Contains(resp.Text, "不在编码执行阶段") || strings.Contains(resp.Text, "coding execution phase")) {
		t.Fatalf("expected orphan-retry message, got %#v", resp)
	}
	if _, ok := h.pendingCodingExecRetryAction.Load(userID); ok {
		t.Fatal("pending retry action must be consumed even when phase is wrong")
	}
}
