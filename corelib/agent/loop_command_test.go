package agent

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNormalizeLoopConfig_Defaults(t *testing.T) {
	cfg := NormalizeLoopConfig(LoopCommandConfig{
		Goal:      "make tests pass",
		VerifyCmd: "go test ./...",
	})
	if cfg.MaxIterations != defaultLoopMaxIterations {
		t.Errorf("MaxIterations = %d, want %d", cfg.MaxIterations, defaultLoopMaxIterations)
	}
	if cfg.VerifyTimeout != defaultLoopVerifyTimeout {
		t.Errorf("VerifyTimeout = %v, want %v", cfg.VerifyTimeout, defaultLoopVerifyTimeout)
	}
	if cfg.MaxLLMIterationsPerCycle != defaultLoopMaxLLMItersPerCycle {
		t.Errorf("MaxLLMIterationsPerCycle = %d, want %d", cfg.MaxLLMIterationsPerCycle, defaultLoopMaxLLMItersPerCycle)
	}
}

func TestNormalizeLoopConfig_PreservesExplicit(t *testing.T) {
	cfg := NormalizeLoopConfig(LoopCommandConfig{
		Goal:                     "fix",
		VerifyCmd:                "npm test",
		MaxIterations:            5,
		VerifyTimeout:            30 * time.Second,
		MaxLLMIterationsPerCycle: 15,
	})
	if cfg.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5", cfg.MaxIterations)
	}
	if cfg.VerifyTimeout != 30*time.Second {
		t.Errorf("VerifyTimeout = %v, want 30s", cfg.VerifyTimeout)
	}
	if cfg.MaxLLMIterationsPerCycle != 15 {
		t.Errorf("MaxLLMIterationsPerCycle = %d, want 15", cfg.MaxLLMIterationsPerCycle)
	}
}

// fakeLoopCallbacks is a test double for LoopCommandCallbacks.
type fakeLoopCallbacks struct {
	modifyResults []LoopResult
	cancelled     bool
	callIndex     int

	iterationsStarted []int
	verifyStarted     []int
	successCalled     bool
	failureCalled     bool
}

func (f *fakeLoopCallbacks) RunModifyCycle(_ context.Context, _ string, _ int) LoopResult {
	if f.callIndex >= len(f.modifyResults) {
		return LoopResult{Text: "no more results"}
	}
	r := f.modifyResults[f.callIndex]
	f.callIndex++
	return r
}

func (f *fakeLoopCallbacks) OnIterationStart(i, _ int)          { f.iterationsStarted = append(f.iterationsStarted, i) }
func (f *fakeLoopCallbacks) OnVerifyStart(_ string, i int)      { f.verifyStarted = append(f.verifyStarted, i) }
func (f *fakeLoopCallbacks) OnVerifyDone(_ VerifyCommandResult, _ int) {}
func (f *fakeLoopCallbacks) OnSuccess(_ *LoopCommandState)      { f.successCalled = true }
func (f *fakeLoopCallbacks) OnFailure(_ *LoopCommandState)      { f.failureCalled = true }
func (f *fakeLoopCallbacks) IsCancelled() bool                  { return f.cancelled }

func TestRunLoopCommand_ImmediateSuccess(t *testing.T) {
	// Verify command: "echo ok" always exits 0.
	cfg := LoopCommandConfig{
		Goal:          "do nothing",
		VerifyCmd:     "echo ok",
		MaxIterations: 3,
	}
	cb := &fakeLoopCallbacks{
		modifyResults: []LoopResult{{Text: "done"}},
	}

	state := RunLoopCommand(context.Background(), cfg, cb)

	if state.Status != LoopStatusSucceeded {
		t.Errorf("Status = %q, want %q", state.Status, LoopStatusSucceeded)
	}
	if len(state.Iterations) != 1 {
		t.Errorf("Iterations = %d, want 1", len(state.Iterations))
	}
	if !cb.successCalled {
		t.Error("OnSuccess not called")
	}
}

func TestRunLoopCommand_FailsAfterMaxIterations(t *testing.T) {
	// Verify command always fails.
	cfg := LoopCommandConfig{
		Goal:          "impossible",
		VerifyCmd:     "exit 1",
		MaxIterations: 2,
	}
	cb := &fakeLoopCallbacks{
		modifyResults: []LoopResult{{Text: "try 1"}, {Text: "try 2"}},
	}

	state := RunLoopCommand(context.Background(), cfg, cb)

	if state.Status != LoopStatusFailed {
		t.Errorf("Status = %q, want %q", state.Status, LoopStatusFailed)
	}
	if len(state.Iterations) != 2 {
		t.Errorf("Iterations = %d, want 2", len(state.Iterations))
	}
	if !cb.failureCalled {
		t.Error("OnFailure not called")
	}
}

func TestRunLoopCommand_Cancelled(t *testing.T) {
	cfg := LoopCommandConfig{
		Goal:          "will cancel",
		VerifyCmd:     "echo ok",
		MaxIterations: 5,
	}
	cb := &fakeLoopCallbacks{
		cancelled:     true,
		modifyResults: []LoopResult{{Text: "never"}},
	}

	state := RunLoopCommand(context.Background(), cfg, cb)

	if state.Status != LoopStatusCancelled {
		t.Errorf("Status = %q, want %q", state.Status, LoopStatusCancelled)
	}
}

func TestExecuteVerifyCommand_ExitZero(t *testing.T) {
	result := ExecuteVerifyCommand(context.Background(), "echo hello", "", 5*time.Second)
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !result.Passed() {
		t.Error("Passed() = false, want true")
	}
}

func TestExecuteVerifyCommand_ExitNonZero(t *testing.T) {
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "cmd /c exit 1"
	} else {
		cmd = "exit 1"
	}
	result := ExecuteVerifyCommand(context.Background(), cmd, "", 5*time.Second)
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if result.Passed() {
		t.Error("Passed() = true, want false")
	}
}

func TestExecuteVerifyCommand_Timeout(t *testing.T) {
	// Use a command that reliably takes longer than the timeout.
	// On Windows, "ping -n 100 127.0.0.1" takes ~100 seconds.
	// On Unix, "sleep 10" takes 10 seconds.
	// With a 200ms timeout, both will be killed.
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "ping -n 100 127.0.0.1"
	} else {
		cmd = "sleep 10"
	}
	result := ExecuteVerifyCommand(context.Background(), cmd, "", 200*time.Millisecond)
	if !result.TimedOut {
		t.Errorf("TimedOut = false, want true (exit=%d, duration=%v)", result.ExitCode, result.Duration)
	}
	if result.Passed() {
		t.Error("Passed() = true, want false")
	}
}

func TestBuildLoopCyclePrompt_FirstIteration(t *testing.T) {
	cfg := LoopCommandConfig{
		Goal:          "make all tests pass",
		VerifyCmd:     "go test ./...",
		MaxIterations: 10,
	}
	state := &LoopCommandState{Config: cfg}

	prompt := buildLoopCyclePrompt(cfg, state, 0)

	if !containsAll(prompt, "make all tests pass", "go test ./...", "Iteration 1/10", "first iteration") {
		t.Errorf("First iteration prompt missing expected content:\n%s", prompt)
	}
}

func TestBuildLoopCyclePrompt_SubsequentIteration(t *testing.T) {
	cfg := LoopCommandConfig{
		Goal:          "fix the bug",
		VerifyCmd:     "npm test",
		MaxIterations: 5,
	}
	state := &LoopCommandState{
		Config: cfg,
		Iterations: []LoopIterationRecord{
			{
				Index: 0,
				VerifyResult: VerifyCommandResult{
					ExitCode: 1,
					Stderr:   "FAIL: TestFoo expected 42 got 0",
				},
			},
		},
	}

	prompt := buildLoopCyclePrompt(cfg, state, 1)

	if !containsAll(prompt, "fix the bug", "npm test", "Iteration 2/5", "failed", "TestFoo expected 42 got 0") {
		t.Errorf("Subsequent iteration prompt missing expected content:\n%s", prompt)
	}
}

func TestTruncateVerifyOutput_Short(t *testing.T) {
	s := "short output"
	if got := truncateVerifyOutput(s); got != s {
		t.Errorf("truncateVerifyOutput(%q) = %q, want %q", s, got, s)
	}
}

func TestTruncateVerifyOutput_Long(t *testing.T) {
	// Build a string longer than MaxVerifyOutputLen.
	long := make([]byte, MaxVerifyOutputLen+500)
	for i := range long {
		long[i] = 'x'
	}
	// Place marker near the end (within MaxVerifyOutputLen from the tail).
	long[len(long)-5] = 'E'
	long[len(long)-4] = 'N'
	long[len(long)-3] = 'D'
	long[len(long)-2] = '!'
	long[len(long)-1] = '!'

	got := truncateVerifyOutput(string(long))
	if len(got) > MaxVerifyOutputLen+20 { // +20 for the prefix
		t.Errorf("truncated output too long: %d", len(got))
	}
	// Tail should be preserved — "END!!" is within the last MaxVerifyOutputLen chars.
	if !strings.Contains(got, "END!!") {
		t.Errorf("tail not preserved, got suffix: %q", got[len(got)-20:])
	}
	// Should have truncation prefix.
	if !strings.HasPrefix(got, "...(truncated)") {
		t.Errorf("missing truncation prefix, got: %q", got[:30])
	}
}

// --- helpers ---

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
