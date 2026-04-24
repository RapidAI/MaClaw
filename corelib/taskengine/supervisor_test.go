package taskengine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ── test doubles ──

// fastClassifier is a retry classifier with zero wait times for fast tests.
type fastClassifier struct {
	maxRetries int
}

func (f *fastClassifier) Classify(err error, step StepSpec) FailureType {
	if err == nil {
		return FailureUnknown
	}
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "not found") {
		return FailureElementNotFound
	}
	if strings.Contains(strings.ToLower(msg), "timeout") {
		return FailureTimeout
	}
	return FailureUnknown
}

func (f *fastClassifier) Decide(failure FailureType, step StepSpec, retryCount int, snapshot *StateSnapshot) *RetryDecision {
	if retryCount >= f.maxRetries {
		return &RetryDecision{ShouldRetry: false, Reason: fmt.Sprintf("exceeded max retries (%d)", f.maxRetries)}
	}
	return &RetryDecision{ShouldRetry: true, WaitBefore: 0, Reason: "retrying (fast)"}
}

type fakeExecutor struct {
	results []error // per-call results; cycles if more calls than entries
	calls   int
}

func (f *fakeExecutor) Execute(ctx context.Context, step StepSpec) error {
	idx := f.calls
	f.calls++
	if len(f.results) == 0 {
		return nil
	}
	return f.results[idx%len(f.results)]
}

type fakeObserver struct {
	verifyResult *VerifyResult
	verifyErr    error
	snapshot     *StateSnapshot
	checkpoints  []Checkpoint
}

func (f *fakeObserver) Snapshot() (*StateSnapshot, error) {
	if f.snapshot != nil {
		return f.snapshot, nil
	}
	return &StateSnapshot{}, nil
}

func (f *fakeObserver) Verify(criteria []CriterionSpec) (*VerifyResult, error) {
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	if f.verifyResult != nil {
		return f.verifyResult, nil
	}
	// Default: all pass
	result := &VerifyResult{Passed: true}
	for _, c := range criteria {
		result.Details = append(result.Details, CriterionResult{Criterion: c, Passed: true, Actual: "ok"})
	}
	return result, nil
}

func (f *fakeObserver) WaitForStable(timeout time.Duration) error { return nil }

func (f *fakeObserver) TakeCheckpoint(stepIndex int) Checkpoint {
	if stepIndex < len(f.checkpoints) {
		return f.checkpoints[stepIndex]
	}
	return Checkpoint{StepIndex: stepIndex, Timestamp: time.Now()}
}

// ── tests ──

func TestExecute_AllStepsSucceed(t *testing.T) {
	exec := &fakeExecutor{}
	sup := NewSupervisor(SupervisorConfig{Executor: exec})

	spec := TaskSpec{
		Description: "test task",
		Steps: []StepSpec{
			{Action: "click", Params: map[string]string{"x": "100"}},
			{Action: "type", Params: map[string]string{"text": "hello"}},
		},
	}

	state, err := sup.Execute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Errorf("expected completed, got %s", state.Status)
	}
	if state.TotalSteps != 2 {
		t.Errorf("expected 2 total steps, got %d", state.TotalSteps)
	}
	if exec.calls != 2 {
		t.Errorf("expected 2 executor calls, got %d", exec.calls)
	}
}

func TestExecute_StepFailsAfterRetries(t *testing.T) {
	exec := &fakeExecutor{
		results: []error{fmt.Errorf("element not found"), fmt.Errorf("element not found"), fmt.Errorf("element not found"), fmt.Errorf("element not found")},
	}
	sup := NewSupervisor(SupervisorConfig{
		Executor:   exec,
		Classifier: &fastClassifier{maxRetries: 3},
	})

	spec := TaskSpec{
		Description: "failing task",
		Steps:       []StepSpec{{Action: "click"}},
		MaxRetries:  3,
		StepTimeout: 100 * time.Millisecond,
	}

	state, err := sup.Execute(spec)
	if err == nil {
		t.Fatal("expected error")
	}
	if state.Status != StatusFailed {
		t.Errorf("expected failed, got %s", state.Status)
	}
	// MaxRetries=3 means: up to 3 retries allowed → 1 initial + 3 retries = 4 total calls.
	if exec.calls != 4 {
		t.Errorf("expected 4 executor calls (1 initial + 3 retries), got %d", exec.calls)
	}
}

func TestExecute_PerStepVerification(t *testing.T) {
	exec := &fakeExecutor{} // all succeed
	obs := &fakeObserver{
		verifyResult: &VerifyResult{
			Passed:  false,
			Details: []CriterionResult{{Passed: false, Error: "text not found"}},
		},
	}
	sup := NewSupervisor(SupervisorConfig{
		Executor:   exec,
		Observer:   obs,
		Classifier: &fastClassifier{maxRetries: 1},
	})

	spec := TaskSpec{
		Description: "verify task",
		Steps: []StepSpec{
			{
				Action: "click",
				Verify: &CriterionSpec{Type: "text_contains", Pattern: "Welcome"},
			},
		},
		StepTimeout: 100 * time.Millisecond,
	}

	state, err := sup.Execute(spec)
	if err == nil {
		t.Fatal("expected error from per-step verification failure")
	}
	if state.Status != StatusFailed {
		t.Errorf("expected failed, got %s", state.Status)
	}
	if !strings.Contains(state.LastError, "verification") {
		t.Errorf("expected verification error, got: %s", state.LastError)
	}
}

func TestExecute_FinalSuccessCriteria_Pass(t *testing.T) {
	exec := &fakeExecutor{}
	obs := &fakeObserver{} // default: all pass
	sup := NewSupervisor(SupervisorConfig{Executor: exec, Observer: obs})

	spec := TaskSpec{
		Description: "criteria task",
		Steps:       []StepSpec{{Action: "click"}},
		SuccessCriteria: []CriterionSpec{
			{Type: "text_contains", Pattern: "Success"},
		},
	}

	state, err := sup.Execute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Errorf("expected completed, got %s", state.Status)
	}
}

func TestExecute_FinalSuccessCriteria_Fail(t *testing.T) {
	exec := &fakeExecutor{}
	obs := &fakeObserver{
		verifyResult: &VerifyResult{
			Passed:  false,
			Details: []CriterionResult{{Passed: false, Error: "URL mismatch"}},
		},
	}
	sup := NewSupervisor(SupervisorConfig{Executor: exec, Observer: obs})

	spec := TaskSpec{
		Description: "criteria fail task",
		Steps:       []StepSpec{{Action: "click"}},
		SuccessCriteria: []CriterionSpec{
			{Type: "url_contains", Pattern: "/dashboard"},
		},
	}

	state, err := sup.Execute(spec)
	if err == nil {
		t.Fatal("expected error from final criteria failure")
	}
	if state.Status != StatusFailed {
		t.Errorf("expected failed, got %s", state.Status)
	}
	if !strings.Contains(state.LastError, "success criteria not met") {
		t.Errorf("expected criteria error, got: %s", state.LastError)
	}
}

func TestExecute_Cancel(t *testing.T) {
	// Executor that blocks until context is cancelled
	blockExec := &blockingExecutor{}
	sup := NewSupervisor(SupervisorConfig{Executor: blockExec})

	spec := TaskSpec{
		Description: "cancel task",
		Steps:       []StepSpec{{Action: "slow_op"}},
		StepTimeout: 10 * time.Second,
	}

	done := make(chan struct{})
	var state *TaskState
	var execErr error
	go func() {
		state, execErr = sup.Execute(spec)
		close(done)
	}()

	// Wait for task to register
	time.Sleep(50 * time.Millisecond)

	// Cancel
	taskID := ""
	sup.mu.RLock()
	for id := range sup.tasks {
		taskID = id
	}
	sup.mu.RUnlock()

	if err := sup.Cancel(taskID); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Execute did not return after cancel")
	}

	if execErr == nil {
		t.Fatal("expected error after cancel")
	}
	if state.Status != StatusCancelled {
		t.Errorf("expected cancelled, got %s", state.Status)
	}
}

func TestExecute_Checkpoints(t *testing.T) {
	exec := &fakeExecutor{}
	obs := &fakeObserver{
		checkpoints: []Checkpoint{
			{StepIndex: 0, Extra: map[string]string{"url": "https://example.com"}},
			{StepIndex: 1, Extra: map[string]string{"url": "https://example.com/page2"}},
		},
	}
	sup := NewSupervisor(SupervisorConfig{Executor: exec, Observer: obs})

	spec := TaskSpec{
		Description: "checkpoint task",
		Steps: []StepSpec{
			{Action: "navigate"},
			{Action: "click"},
		},
	}

	state, err := sup.Execute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.Checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(state.Checkpoints))
	}
}

func TestExecute_NoObserver_SkipsVerification(t *testing.T) {
	exec := &fakeExecutor{}
	// No observer — verification should be skipped, not panic
	sup := NewSupervisor(SupervisorConfig{Executor: exec})

	spec := TaskSpec{
		Description: "no observer",
		Steps: []StepSpec{
			{Action: "click", Verify: &CriterionSpec{Type: "text_contains", Pattern: "x"}},
		},
		SuccessCriteria: []CriterionSpec{
			{Type: "url_contains", Pattern: "/home"},
		},
	}

	state, err := sup.Execute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Errorf("expected completed (verification skipped), got %s", state.Status)
	}
}

func TestExecute_MaxRetriesHonoredBySupervisor(t *testing.T) {
	// Classifier always says "retry" — Supervisor must enforce MaxRetries as hard cap.
	alwaysRetry := &fastClassifier{maxRetries: 999} // classifier never stops
	exec := &fakeExecutor{
		results: []error{
			fmt.Errorf("fail"), fmt.Errorf("fail"), fmt.Errorf("fail"),
			fmt.Errorf("fail"), fmt.Errorf("fail"), fmt.Errorf("fail"),
		},
	}
	sup := NewSupervisor(SupervisorConfig{Executor: exec, Classifier: alwaysRetry})

	spec := TaskSpec{
		Steps:      []StepSpec{{Action: "click"}},
		MaxRetries: 2, // allow 2 retries → 3 total attempts
	}

	state, err := sup.Execute(spec)
	if err == nil {
		t.Fatal("expected error")
	}
	if state.Status != StatusFailed {
		t.Errorf("expected failed, got %s", state.Status)
	}
	// 1 initial + 2 retries = 3 total calls, even though classifier allows 999
	if exec.calls != 3 {
		t.Errorf("expected 3 calls (MaxRetries=2 → 1+2), got %d", exec.calls)
	}
	if !strings.Contains(state.LastError, "exceeded max retries (2)") {
		t.Errorf("expected max retries error, got: %s", state.LastError)
	}
}

func TestExecute_CancelDuringRetryWait(t *testing.T) {
	// Classifier returns a long wait — cancel should interrupt it.
	slowClassifier := &slowRetryClassifier{waitTime: 10 * time.Second}
	exec := &fakeExecutor{results: []error{fmt.Errorf("fail"), fmt.Errorf("fail")}}
	sup := NewSupervisor(SupervisorConfig{Executor: exec, Classifier: slowClassifier})

	spec := TaskSpec{
		Steps:       []StepSpec{{Action: "click"}},
		MaxRetries:  5,
		StepTimeout: 100 * time.Millisecond,
	}

	done := make(chan struct{})
	var state *TaskState
	go func() {
		state, _ = sup.Execute(spec)
		close(done)
	}()

	// Wait for first attempt to fail and retry wait to start
	time.Sleep(200 * time.Millisecond)

	// Cancel during the retry wait
	sup.mu.RLock()
	var taskID string
	for id := range sup.tasks {
		taskID = id
	}
	sup.mu.RUnlock()
	_ = sup.Cancel(taskID)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Execute did not return after cancel during retry wait")
	}

	if state.Status != StatusCancelled {
		t.Errorf("expected cancelled, got %s", state.Status)
	}
}

func TestExecute_TaskCleanedUpAfterCompletion(t *testing.T) {
	exec := &fakeExecutor{}
	sup := NewSupervisor(SupervisorConfig{Executor: exec})

	spec := TaskSpec{Steps: []StepSpec{{Action: "click"}}}
	state, err := sup.Execute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Task should be removed from the map after completion
	_, found := sup.GetState(state.ID)
	if found {
		t.Error("expected task to be cleaned up after completion, but it's still in the map")
	}
}

func TestExecute_TaskCleanedUpAfterFailure(t *testing.T) {
	exec := &fakeExecutor{results: []error{fmt.Errorf("fail")}}
	sup := NewSupervisor(SupervisorConfig{
		Executor:   exec,
		Classifier: &fastClassifier{maxRetries: 0},
	})

	spec := TaskSpec{Steps: []StepSpec{{Action: "click"}}}
	state, _ := sup.Execute(spec)

	_, found := sup.GetState(state.ID)
	if found {
		t.Error("expected task to be cleaned up after failure, but it's still in the map")
	}
}

func TestExecute_StepTraces(t *testing.T) {
	exec := &fakeExecutor{}
	sup := NewSupervisor(SupervisorConfig{Executor: exec})

	spec := TaskSpec{
		Steps: []StepSpec{
			{Action: "navigate"},
			{Action: "click"},
			{Action: "type"},
		},
	}

	state, err := sup.Execute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.StepTraces) != 3 {
		t.Fatalf("expected 3 traces, got %d", len(state.StepTraces))
	}
	for i, tr := range state.StepTraces {
		if tr.Summary != "ok" {
			t.Errorf("trace %d: expected summary 'ok', got %q", i, tr.Summary)
		}
		if tr.EndedAt.IsZero() {
			t.Errorf("trace %d: EndedAt not set", i)
		}
	}
}

// ── test helpers ──

type blockingExecutor struct{}

func (b *blockingExecutor) Execute(ctx context.Context, step StepSpec) error {
	<-ctx.Done()
	return ctx.Err()
}

type slowRetryClassifier struct {
	waitTime time.Duration
}

func (s *slowRetryClassifier) Classify(err error, step StepSpec) FailureType {
	return FailureUnknown
}

func (s *slowRetryClassifier) Decide(failure FailureType, step StepSpec, retryCount int, snapshot *StateSnapshot) *RetryDecision {
	return &RetryDecision{ShouldRetry: true, WaitBefore: s.waitTime, Reason: "slow retry"}
}
