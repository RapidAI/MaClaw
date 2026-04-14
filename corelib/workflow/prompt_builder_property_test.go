package workflow

import (
	"strings"
	"testing"
	"testing/quick"
)

// Feature: maclaw-agent-workflow, Property 8: BuildPhasePrompt 结构完整性
// For any active WorkflowState with any phase index and previous outputs,
// BuildPhaseSystemPrompt output contains: phase name, LLM instruction,
// intent summary, previous outputs, checklist items.
// **Validates: Requirements 5.2, 6.1, 6.2**
func TestProperty8_BuildPhasePromptStructuralCompleteness(t *testing.T) {
	registry := NewWorkflowRegistry()

	workflowTypes := []WorkflowType{
		WorkflowCoding, WorkflowProductDesign, WorkflowInnovation,
		WorkflowBusinessPlan, WorkflowTesting,
	}

	f := func(typeIdx, phaseIdx uint8) bool {
		wt := workflowTypes[int(typeIdx)%len(workflowTypes)]
		tmpl := registry.Match(wt)
		if tmpl == nil || len(tmpl.Phases) == 0 {
			return true
		}
		pi := int(phaseIdx) % len(tmpl.Phases)
		phase := &tmpl.Phases[pi]

		state := &WorkflowState{
			ID:           "wf-test",
			UserID:       "u1",
			Type:         wt,
			Intent:       StructuredIntent{Category: wt, Summary: "测试摘要", Goals: []string{"目标1"}, Constraints: []string{"约束1"}},
			CurrentPhase: phase.ID,
			PhaseIndex:   pi,
			PhaseOutputs: make(map[string]string),
		}
		// Fill previous phase outputs
		for i := 0; i < pi; i++ {
			state.PhaseOutputs[tmpl.Phases[i].ID] = "前序阶段" + tmpl.Phases[i].Name + "的产出物"
		}

		prompt := BuildPhaseSystemPrompt(state, phase, registry)

		// Must contain phase name
		if !strings.Contains(prompt, phase.Name) {
			t.Logf("missing phase name %q in prompt", phase.Name)
			return false
		}
		// Must contain LLM instruction (phase.Prompt)
		if !strings.Contains(prompt, phase.Prompt) {
			t.Logf("missing phase prompt in output")
			return false
		}
		// Must contain intent summary
		if !strings.Contains(prompt, "测试摘要") {
			t.Logf("missing intent summary")
			return false
		}
		// Must contain checklist items
		for _, item := range phase.Checklist {
			if !strings.Contains(prompt, item) {
				t.Logf("missing checklist item %q", item)
				return false
			}
		}
		// Must contain previous outputs (if any)
		for i := 0; i < pi; i++ {
			prevName := tmpl.Phases[i].Name
			if !strings.Contains(prompt, prevName) {
				t.Logf("missing previous phase name %q", prevName)
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 8 (BuildPhasePrompt completeness) failed: %v", err)
	}
}

// Feature: maclaw-agent-workflow, Property 12: 工具过滤策略与阶段配置一致
// For any PhaseTemplate, GetToolFilterForPhase returns the value matching
// PhaseTemplate.ToolPolicy.
// **Validates: Requirements 6.3, 6.4**
func TestProperty12_ToolFilterMatchesPhaseConfig(t *testing.T) {
	registry := NewWorkflowRegistry()

	workflowTypes := []WorkflowType{
		WorkflowCoding, WorkflowProductDesign, WorkflowInnovation,
		WorkflowBusinessPlan, WorkflowTesting,
	}

	f := func(typeIdx, phaseIdx uint8) bool {
		wt := workflowTypes[int(typeIdx)%len(workflowTypes)]
		tmpl := registry.Match(wt)
		if tmpl == nil || len(tmpl.Phases) == 0 {
			return true
		}
		pi := int(phaseIdx) % len(tmpl.Phases)
		phase := &tmpl.Phases[pi]

		got := GetToolFilterForPhase(phase)
		return got == phase.ToolPolicy
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 12 (tool filter matches phase config) failed: %v", err)
	}

	// Also test nil phase
	if got := GetToolFilterForPhase(nil); got != ToolFilterNone {
		t.Errorf("GetToolFilterForPhase(nil): expected none, got %s", got)
	}
}
