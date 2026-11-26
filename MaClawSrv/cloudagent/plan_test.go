package cloudagent

import (
	"encoding/json"
	"testing"
)

func testBook(t *testing.T) *book {
	t.Helper()
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// A bare send carries no task id, and the pending gate is exactly the
// state where no turn is running — planPendingTask is the only lookup
// that can still route the gate answer to the task that parked it.
func TestPlanPendingTaskRoutesBareGateAnswer(t *testing.T) {
	b := testBook(t)
	if got := b.planPendingTask(); got != "" {
		t.Fatalf("empty book pending task = %q", got)
	}
	rec := &planRecord{
		State:   planStatePending,
		Request: "做一个页面",
		Steps:   []planStep{{Index: 1, Title: "搭骨架", Status: "pending"}},
	}
	if err := b.planSave("task_a", rec); err != nil {
		t.Fatal(err)
	}
	if err := b.planSave("task_b", &planRecord{State: planStateDone, Request: "旧任务"}); err != nil {
		t.Fatal(err)
	}
	if got := b.planPendingTask(); got != "task_a" {
		t.Fatalf("pending task = %q, want task_a", got)
	}
	rec.State = planStateApproved
	if err := b.planSave("task_a", rec); err != nil {
		t.Fatal(err)
	}
	if got := b.planPendingTask(); got != "" {
		t.Fatalf("decided plan still pending: %q", got)
	}
}

// A finished turn must not leave a stale pending row in a done plan: the
// running step passes and the untouched tail is skipped, not parked.
func TestSettlePlanSkipsUntouchedTail(t *testing.T) {
	b := testBook(t)
	rec := &planRecord{
		State: planStateRunning,
		Steps: []planStep{
			{Index: 1, Title: "一步", Status: "passed"},
			{Index: 2, Title: "二步", Status: "running"},
			{Index: 3, Title: "三步", Status: "pending"},
		},
	}
	if err := b.planSave("task_1", rec); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{}
	inst := &instance{book: b, meta: Spec{ID: "cag_t"}}
	rt.settlePlan(inst, "task_1", "req_1", false)

	got := b.planLoadOrNil("task_1")
	if got == nil || got.State != planStateDone {
		t.Fatalf("state = %+v", got)
	}
	want := []string{"passed", "passed", "skipped"}
	for i, step := range got.Steps {
		if step.Status != want[i] {
			t.Fatalf("step %d status = %q, want %q (%+v)", i, step.Status, want[i], got.Steps)
		}
	}
}

// A failed turn keeps the untouched tail pending — the steps were never
// attempted, and marking them skipped would claim otherwise.
func TestSettlePlanFailureKeepsPendingTail(t *testing.T) {
	b := testBook(t)
	rec := &planRecord{
		State: planStateRunning,
		Steps: []planStep{
			{Index: 1, Title: "一步", Status: "running"},
			{Index: 2, Title: "二步", Status: "pending"},
		},
	}
	if err := b.planSave("task_1", rec); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{}
	inst := &instance{book: b, meta: Spec{ID: "cag_t"}}
	rt.settlePlan(inst, "task_1", "req_1", true)

	got := b.planLoadOrNil("task_1")
	if got == nil || got.State != planStateFailed {
		t.Fatalf("state = %+v", got)
	}
	if got.Steps[0].Status != "failed" || got.Steps[1].Status != "pending" {
		t.Fatalf("steps = %+v", got.Steps)
	}
}

// A decided plan gate must disappear from the watch snapshot even when the
// execution turn is still queued; otherwise reopening the task resurrects the
// confirmation buttons the user already clicked.
func TestWorkbenchDecisionRetiresPlanTerminalInWatchSnapshot(t *testing.T) {
	b := testBook(t)
	rt := &Runtime{}
	inst := &instance{book: b, meta: Spec{ID: "cag_t"}}
	const taskID = "task_plan_snapshot"
	rt.publish(inst, taskID, "req_plan", "response", "我先把这次任务拆成了执行计划，确认后开始实施。", map[string]any{
		"terminal": planTerminal(taskID),
	})
	rt.publishWorkbench(inst, taskID, "", &planRecord{
		State:    planStateApproved,
		Request:  "做一个页面",
		Steps:    []planStep{{Index: 1, Title: "搭骨架", Status: "pending"}},
		Revision: 2,
	})
	raw, err := rt.watchSnapshot(inst, taskID)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Terminal map[string]any `json:"terminal"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Terminal != nil {
		t.Fatalf("plan terminal survived the gate decision: %s", raw)
	}
}

func TestMarkPlanStepRunningWritesBackFailedStep(t *testing.T) {
	rec := &planRecord{Steps: []planStep{
		{Index: 1, Title: "一步", Status: "failed"},
		{Index: 2, Title: "二步", Status: "pending"},
	}}
	markPlanStepRunning(rec)
	if rec.Steps[0].Status != "running" {
		t.Fatalf("failed step was not rearmed: %+v", rec.Steps)
	}
	if rec.Steps[1].Status != "pending" {
		t.Fatalf("tail moved too early: %+v", rec.Steps)
	}
}

func TestMarkPlanStepRunningKeepsPlanOrderOverLaterRunningRow(t *testing.T) {
	rec := &planRecord{Steps: []planStep{
		{Index: 1, Title: "一步", Status: "failed"},
		{Index: 2, Title: "二步", Status: "running"},
	}}
	if !markPlanStepRunning(rec) {
		t.Fatal("ordered rearm reported no change")
	}
	if rec.Steps[0].Status != "running" || rec.Steps[1].Status != "pending" {
		t.Fatalf("steps = %+v", rec.Steps)
	}
}

// A successful model turn does not make a plan done while one of its rows is
// still failed. That terminal inconsistency is exactly what the approval
// retry path otherwise has to clean up later.
func TestSettlePlanNeverDoneWithFailedStep(t *testing.T) {
	b := testBook(t)
	rec := &planRecord{
		State: planStateRunning,
		Steps: []planStep{
			{Index: 1, Title: "一步", Status: "failed"},
			{Index: 2, Title: "二步", Status: "running"},
			{Index: 3, Title: "三步", Status: "pending"},
		},
	}
	if err := b.planSave("task_1", rec); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{}
	inst := &instance{book: b, meta: Spec{ID: "cag_t"}}
	rt.settlePlan(inst, "task_1", "req_1", false)

	got := b.planLoadOrNil("task_1")
	if got == nil || got.State != planStateFailed {
		t.Fatalf("state = %+v", got)
	}
	want := []string{"failed", "passed", "pending"}
	for i, step := range got.Steps {
		if step.Status != want[i] {
			t.Fatalf("step %d status = %q, want %q (%+v)", i, step.Status, want[i], got.Steps)
		}
	}
}

// A pending gate is durable state, while the lane terminal is only process
// memory. After a restart the watch snapshot must rebuild the actions or the
// user cannot answer the gate from the reopened panel.
func TestWatchSnapshotRestoresPendingPlanTerminal(t *testing.T) {
	b := testBook(t)
	rt := &Runtime{}
	inst := &instance{book: b, meta: Spec{ID: "cag_t"}}
	const taskID = "task_pending_after_restart"
	if err := b.planSave(taskID, &planRecord{
		State:    planStatePending,
		Request:  "做一个页面",
		Steps:    []planStep{{Index: 1, Title: "搭骨架", Status: "pending"}},
		Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := rt.watchSnapshot(inst, taskID)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Terminal map[string]any `json:"terminal"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	confirmation, _ := snap.Terminal["confirmation"].(map[string]any)
	if confirmation["id"] != "plan-approval;"+taskID {
		t.Fatalf("pending plan terminal missing after restart: %s", raw)
	}
}

// A newer clean done record is only a duplicate-click fallback. It must not
// hide the newest actionable failed plan a bare approval can still retry.
func TestPlanGateTaskPrefersActionablePlanOverNewerTerminal(t *testing.T) {
	b := testBook(t)
	failed := &planRecord{
		State: planStateFailed,
		Steps: []planStep{{Index: 1, Title: "一步", Status: "failed"}},
	}
	done := &planRecord{
		State: planStateDone,
		Steps: []planStep{{Index: 1, Title: "一步", Status: "passed"}},
	}
	if err := b.planSave("task_failed", failed); err != nil {
		t.Fatal(err)
	}
	if err := b.planSave("task_done", done); err != nil {
		t.Fatal(err)
	}
	if _, err := b.db.Exec(`UPDATE plan_state SET updated_at = ? WHERE task_id = ?`, "2026-01-01T00:00:00.000000000Z", "task_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.db.Exec(`UPDATE plan_state SET updated_at = ? WHERE task_id = ?`, "2026-01-02T00:00:00.000000000Z", "task_done"); err != nil {
		t.Fatal(err)
	}
	if got := b.planGateTask(); got != "task_failed" {
		t.Fatalf("gate task = %q, want task_failed", got)
	}
	if _, err := b.db.Exec(`DELETE FROM plan_state WHERE task_id = ?`, "task_failed"); err != nil {
		t.Fatal(err)
	}
	if got := b.planGateTask(); got != "task_done" {
		t.Fatalf("terminal fallback gate task = %q, want task_done", got)
	}
}
