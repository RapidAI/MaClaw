package v2

import (
	"strings"
	"testing"
)

func TestSaveExecutionProgressDoesNotAdvancePhase(t *testing.T) {
	store := NewMemoryStore()
	reg := NewTemplateRegistry()
	RegisterBuiltinTemplates(reg)
	m := NewStateMachine(store, reg)
	m.SetAllowTempTestPaths(true)

	state, err := m.Create("u1", "coding", "d:\\proj", "build app")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Advance to implementation phase (index of phase with ExecMode subagent).
	for i := range state.Phases {
		if state.Phases[i].ExecMode == ExecModeSubAgent {
			state.CurrentPhase = i
			state.Phases[i].Status = PhaseExecuting
			_ = store.Save(state)
			break
		}
	}
	before := m.GetActive("u1")
	if before == nil {
		t.Fatal("expected active workflow")
	}
	phaseIdx := before.CurrentPhase

	if err := m.SaveExecutionProgress("u1", "## partial report\n- failed T2"); err != nil {
		t.Fatalf("SaveExecutionProgress: %v", err)
	}
	after := m.GetActive("u1")
	if after == nil {
		t.Fatal("workflow should stay active")
	}
	if after.CurrentPhase != phaseIdx {
		t.Fatalf("phase advanced: %d -> %d", phaseIdx, after.CurrentPhase)
	}
	p := after.ActivePhase()
	if p == nil || p.Status != PhaseExecuting {
		t.Fatalf("phase status = %#v, want executing", p)
	}
	if !strings.Contains(p.Output, "partial report") {
		t.Fatalf("output = %q", p.Output)
	}
}
