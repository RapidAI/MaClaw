package skill

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRepairGate_NilExecutor_Unverified(t *testing.T) {
	gate := &RepairGate{Config: defaultRepairGateConfig(RepairGateConfig{})}
	// No executor → passes by default.
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, []map[string]string{{"input": "x"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed || result.Status != "unverified" {
		t.Errorf("expected unverified with nil executor, got status=%s passed=%v: %s", result.Status, result.Passed, result.Reason)
	}
	if result.EvidenceMode != "none" {
		t.Fatalf("nil executor evidence mode=%q, want none", result.EvidenceMode)
	}
}

func TestDefaultSandboxRejectsUnsupportedAction(t *testing.T) {
	// Historically shell_tool soft-passed as "no bash steps". After the fix,
	// unsupported actions (that cannot normalize) must fail the sandbox runner.
	exec := NewDefaultSandboxExecutor()
	ok, _, err := exec.RunInSandbox(
		context.Background(),
		&corelib.NLSkillEntry{Name: "bad"},
		[]corelib.NLSkillStep{{Action: "totally_unknown_action", Params: map[string]interface{}{}}},
		map[string]string{"url": "https://example.com"},
		5*time.Second,
	)
	if ok || err == nil {
		t.Fatalf("expected unsupported action to fail sandbox, ok=%v err=%v", ok, err)
	}
}

func TestDefaultSandboxNormalizesShellToolToBash(t *testing.T) {
	// shell_tool with a command becomes bash and is eligible for sandbox exec.
	// Use a harmless command so verification can pass when the shell is available.
	exec := NewDefaultSandboxExecutor()
	ok, out, err := exec.RunInSandbox(
		context.Background(),
		&corelib.NLSkillEntry{Name: "wget-like"},
		[]corelib.NLSkillStep{{
			Action: "shell_tool",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
		map[string]string{},
		10*time.Second,
	)
	if err != nil {
		t.Fatalf("sandbox err: %v out=%s", err, out)
	}
	if !ok {
		t.Fatalf("expected shell_tool→bash sandbox pass, out=%s", out)
	}
}

func TestRepairGate_EmptyHistoricalArgs_Unverified(t *testing.T) {
	gate := NewRepairGate(RepairGateConfig{}, &mockSandboxExecutor{alwaysFail: true})
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "test"}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed || result.Status != "unverified" {
		t.Errorf("expected unverified with empty args, got status=%s passed=%v: %s", result.Status, result.Passed, result.Reason)
	}
}

func TestRepairGate_NonBashWithoutAdapter_Unverified(t *testing.T) {
	gate := NewRepairGate(RepairGateConfig{}, &mockSandboxExecutor{})
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "craft"}, []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "summarize"}}}, []map[string]string{{"input": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "unverified" || result.EvidenceMode != "none" || result.Passed {
		t.Fatalf("result = %+v, want fail-closed unverified", result)
	}
}

func TestRepairGate_NonBashMockCannotPass(t *testing.T) {
	gate := NewRepairGate(RepairGateConfig{}, &mockSandboxExecutor{})
	gate.NonBashAdapter = mockNonBashReplayAdapter{mode: "mock", success: true}
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "craft"}, []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "summarize"}}}, []map[string]string{{"input": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "unverified" || result.EvidenceMode != "mock" || result.Passed {
		t.Fatalf("result = %+v, mock evidence must not pass", result)
	}
}

func TestRepairGate_NonBashRealAdapterCanPass(t *testing.T) {
	gate := NewRepairGate(RepairGateConfig{MinPassRate: 1}, &mockSandboxExecutor{})
	gate.NonBashAdapter = mockNonBashReplayAdapter{mode: "real", success: true}
	result, err := gate.Verify(context.Background(), &corelib.NLSkillEntry{Name: "craft"}, []corelib.NLSkillStep{{Action: "call_mcp_tool", Params: map[string]interface{}{"tool": "x"}}}, []map[string]string{{"input": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsRealPass() {
		t.Fatalf("result = %+v, real adapter evidence should pass", result)
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
	if result.EvidenceMode != "real" {
		t.Fatalf("successful replay evidence mode=%q, want real", result.EvidenceMode)
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

type mockNonBashReplayAdapter struct {
	mode    string
	success bool
}

func (m mockNonBashReplayAdapter) ReplayNonBash(context.Context, *corelib.NLSkillEntry, []corelib.NLSkillStep, map[string]string, time.Duration) (NonBashReplayResult, error) {
	return NonBashReplayResult{Success: m.success, EvidenceMode: m.mode, Output: "adapter replay"}, nil
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
