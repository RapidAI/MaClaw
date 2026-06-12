package skill

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRepairGate_NilExecutor_PassesByDefault(t *testing.T) {
	gate := &RepairGate{Config: defaultRepairGateConfig(RepairGateConfig{})}
	// No executor → passes by default.
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, []map[string]string{{"input": "x"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass with nil executor, got: %s", result.Reason)
	}
}

func TestRepairGate_EmptyHistoricalArgs_PassesByDefault(t *testing.T) {
	gate := NewRepairGate(RepairGateConfig{}, &mockSandboxExecutor{alwaysFail: true})
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass with empty args, got: %s", result.Reason)
	}
}

func TestRepairGate_AllSucceed_Passes(t *testing.T) {
	executor := &mockSandboxExecutor{alwaysFail: false}
	gate := NewRepairGate(RepairGateConfig{MaxReplayRuns: 3, MinPassRate: 0.67}, executor)

	args := []map[string]string{
		{"input": "a"},
		{"input": "b"},
		{"input": "c"},
	}
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass with all successes, got: %s", result.Reason)
	}
	if result.PassRate != 1.0 {
		t.Errorf("expected pass rate 1.0, got %f", result.PassRate)
	}
}

func TestRepairGate_AllFail_Fails(t *testing.T) {
	executor := &mockSandboxExecutor{alwaysFail: true}
	gate := NewRepairGate(RepairGateConfig{MaxReplayRuns: 3, MinPassRate: 0.67}, executor)

	args := []map[string]string{
		{"input": "a"},
		{"input": "b"},
		{"input": "c"},
	}
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Errorf("expected fail with all failures, got: %s", result.Reason)
	}
	if result.PassRate != 0 {
		t.Errorf("expected pass rate 0, got %f", result.PassRate)
	}
}

func TestRepairGate_TwoOfThree_Passes(t *testing.T) {
	executor := &mockSandboxExecutor{failIndices: map[int]bool{1: true}} // second run fails
	gate := NewRepairGate(RepairGateConfig{MaxReplayRuns: 3, MinPassRate: 0.67}, executor)

	args := []map[string]string{
		{"input": "a"},
		{"input": "b"},
		{"input": "c"},
	}
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass with 2/3, got: %s", result.Reason)
	}
}

func TestRepairGate_OneOfThree_Fails(t *testing.T) {
	executor := &mockSandboxExecutor{failIndices: map[int]bool{0: true, 1: true}} // first two fail
	gate := NewRepairGate(RepairGateConfig{MaxReplayRuns: 3, MinPassRate: 0.67}, executor)

	args := []map[string]string{
		{"input": "a"},
		{"input": "b"},
		{"input": "c"},
	}
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Errorf("expected fail with 1/3, got: %s", result.Reason)
	}
}

func TestRepairGate_LimitsToMaxReplayRuns(t *testing.T) {
	executor := &mockSandboxExecutor{}
	gate := NewRepairGate(RepairGateConfig{MaxReplayRuns: 2}, executor)

	args := []map[string]string{
		{"input": "a"},
		{"input": "b"},
		{"input": "c"},
		{"input": "d"},
		{"input": "e"},
	}
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.RunResults) != 2 {
		t.Errorf("expected 2 runs (limited), got %d", len(result.RunResults))
	}
}

func TestRepairGate_ContextCancelled(t *testing.T) {
	executor := &mockSandboxExecutor{delay: 5 * time.Second}
	gate := NewRepairGate(RepairGateConfig{MaxReplayRuns: 3}, executor)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := gate.Verify(ctx, &corelib.NLSkillEntry{Name: "test"}, nil, []map[string]string{{"input": "a"}})
	if err == nil {
		t.Error("expected context error")
	}
}

// --- Mock ---

type mockSandboxExecutor struct {
	alwaysFail  bool
	failIndices map[int]bool
	callCount   int
	delay       time.Duration
}

func (m *mockSandboxExecutor) RunInSandbox(ctx context.Context, skill *corelib.NLSkillEntry, steps []corelib.NLSkillStep, args map[string]string, timeout time.Duration) (bool, string, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(m.delay):
		}
	}
	idx := m.callCount
	m.callCount++

	if m.alwaysFail {
		return false, "mock failure", nil
	}
	if m.failIndices != nil && m.failIndices[idx] {
		return false, "mock failure at index", nil
	}
	return true, "mock success", nil
}
