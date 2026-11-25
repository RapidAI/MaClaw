package cloudagent

import (
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
