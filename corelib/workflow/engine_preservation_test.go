package workflow

import (
	"fmt"
	"testing"
	"testing/quick"
)

// Feature: ssh-tool-workflow-hijack, Property 2: Preservation — Sections 0-3 Behavior Unchanged
//
// For any input that is handled by sections 0, 1, 2, or 3 of HandleInput
// (input-waiting, skip words, confirm words with output, PendingConfirm),
// the behavior SHALL be identical to the original function — advancePhase,
// skip rejection, reminder text, or PendingConfirm=true respectively —
// preserving all existing workflow state transitions.
//
// **Validates: Requirements 3.1, 3.2, 3.3, 3.5, 3.8**

// ---------------------------------------------------------------------------
// Sub-property 2a: Confirm words during NeedsConfirm=true + hasOutput=true
//                  → Advance=true (section 2 preservation)
// ---------------------------------------------------------------------------

// TestPreservation_Section2_ConfirmWordsAdvance tests that confirm words
// during a NeedsConfirm phase with existing output advance the phase.
func TestPreservation_Section2_ConfirmWordsAdvance(t *testing.T) {
	// All confirm words defined in engine.go
	allConfirmWords := []string{"下一步", "确认", "继续", "没问题", "可以", "好的", "通过"}

	// Workflow types whose first phase has NeedsConfirm=true
	workflowTypes := []WorkflowType{
		WorkflowCoding,
		WorkflowProductDesign,
		WorkflowInnovation,
		WorkflowBusinessPlan,
		WorkflowTesting,
		WorkflowPresentationDesign,
		WorkflowLiteratureReview,
		WorkflowResearchReport,
	}

	f := func(typeIdx, wordIdx uint8) bool {
		wt := workflowTypes[int(typeIdx)%len(workflowTypes)]
		word := allConfirmWords[int(wordIdx)%len(allConfirmWords)]

		engine, _ := newTestEngine()
		userID := fmt.Sprintf("u_pres2a_%d_%d", typeIdx, wordIdx)

		intent := StructuredIntent{Category: wt, Summary: "test preservation"}
		_, err := engine.StartWorkflow(userID, intent)
		if err != nil {
			t.Logf("StartWorkflow(%s) failed: %v", wt, err)
			return false
		}

		// Inject phase output so hasOutput=true (required for section 2).
		state := engine.GetActiveWorkflow(userID)
		if state == nil {
			t.Logf("no active workflow for %s", userID)
			return false
		}
		engine.mu.Lock()
		state.PhaseOutputs[state.CurrentPhase] = "mock phase output document"
		engine.mu.Unlock()

		origPhaseIndex := state.PhaseIndex

		resp, err := engine.HandleInput(userID, word)
		if err != nil {
			t.Logf("HandleInput(%s, %q) failed: %v", wt, word, err)
			return false
		}

		// Section 2: confirm word + NeedsConfirm + hasOutput → advancePhase → Advance=true
		if !resp.Advance {
			t.Logf("FAIL: wt=%s word=%q → Advance=false (expected true)", wt, word)
			return false
		}

		// Phase should have advanced
		newState := engine.GetActiveWorkflow(userID)
		if newState != nil && newState.PhaseIndex <= origPhaseIndex && !resp.Complete {
			t.Logf("FAIL: wt=%s word=%q → PhaseIndex did not advance (%d → %d)",
				wt, word, origPhaseIndex, newState.PhaseIndex)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2a (Section 2: confirm words advance phase) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2b: Skip words during CanSkip=true phases → Advance=true
//                  (section 1 preservation)
// ---------------------------------------------------------------------------

// TestPreservation_Section1_SkipWordsCanSkipTrue tests that skip words
// during a CanSkip=true phase advance the phase.
func TestPreservation_Section1_SkipWordsCanSkipTrue(t *testing.T) {
	allSkipWords := []string{"跳过", "skip"}

	// We need to reach a CanSkip=true phase. In the coding template,
	// task_breakdown (index 2) has CanSkip=true. We advance there first.
	f := func(wordIdx uint8) bool {
		word := allSkipWords[int(wordIdx)%len(allSkipWords)]

		engine, _ := newTestEngine()
		userID := "u_pres2b_" + word

		intent := StructuredIntent{Category: WorkflowCoding, Summary: "test skip"}
		_, err := engine.StartWorkflow(userID, intent)
		if err != nil {
			t.Logf("StartWorkflow failed: %v", err)
			return false
		}

		// Advance to task_breakdown (index 2, CanSkip=true) by injecting
		// outputs and confirming through requirements and tech_design.
		engine.mu.Lock()
		engine.workflows[userID].PhaseOutputs["requirements"] = "mock requirements"
		engine.mu.Unlock()
		engine.HandleInput(userID, "确认")

		engine.mu.Lock()
		engine.workflows[userID].PhaseOutputs["tech_design"] = "mock tech design"
		engine.mu.Unlock()
		engine.HandleInput(userID, "确认")

		state := engine.GetActiveWorkflow(userID)
		if state == nil || state.CurrentPhase != "task_breakdown" {
			t.Logf("expected task_breakdown phase, got %v", state)
			return false
		}
		if state.PhaseIndex != 2 {
			t.Logf("expected PhaseIndex=2, got %d", state.PhaseIndex)
			return false
		}

		resp, err := engine.HandleInput(userID, word)
		if err != nil {
			t.Logf("HandleInput(%q) failed: %v", word, err)
			return false
		}

		// Section 1: skip word + CanSkip=true → advancePhase → Advance=true
		if !resp.Advance {
			t.Logf("FAIL: word=%q → Advance=false (expected true)", word)
			return false
		}

		// Phase should have advanced past task_breakdown
		newState := engine.GetActiveWorkflow(userID)
		if newState != nil && newState.PhaseIndex <= 2 {
			t.Logf("FAIL: word=%q → PhaseIndex did not advance past 2 (%d)", word, newState.PhaseIndex)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2b (Section 1: skip words on CanSkip=true advance) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2c: Skip words during CanSkip=false phases
//                  → RunAgentLoop=false + rejection text (section 1 preservation)
// ---------------------------------------------------------------------------

// TestPreservation_Section1_SkipWordsCanSkipFalse tests that skip words
// during a CanSkip=false phase return rejection text without advancing.
func TestPreservation_Section1_SkipWordsCanSkipFalse(t *testing.T) {
	allSkipWords := []string{"跳过", "skip"}

	// Coding template: requirements (index 0) has CanSkip=false.
	// Also test with other workflow types whose first phase has CanSkip=false.
	workflowTypes := []WorkflowType{
		WorkflowCoding,
		WorkflowProductDesign,
		WorkflowInnovation,
		WorkflowBusinessPlan,
		WorkflowTesting,
	}

	f := func(typeIdx, wordIdx uint8) bool {
		wt := workflowTypes[int(typeIdx)%len(workflowTypes)]
		word := allSkipWords[int(wordIdx)%len(allSkipWords)]

		engine, _ := newTestEngine()
		userID := fmt.Sprintf("u_pres2c_%d_%d", typeIdx, wordIdx)

		intent := StructuredIntent{Category: wt, Summary: "test skip reject"}
		_, err := engine.StartWorkflow(userID, intent)
		if err != nil {
			t.Logf("StartWorkflow(%s) failed: %v", wt, err)
			return false
		}

		state := engine.GetActiveWorkflow(userID)
		origPhaseIndex := state.PhaseIndex
		origPhase := state.CurrentPhase

		resp, err := engine.HandleInput(userID, word)
		if err != nil {
			t.Logf("HandleInput(%s, %q) failed: %v", wt, word, err)
			return false
		}

		// Section 1: skip word + CanSkip=false → rejection text, RunAgentLoop=false
		if resp.RunAgentLoop {
			t.Logf("FAIL: wt=%s word=%q → RunAgentLoop=true (expected false)", wt, word)
			return false
		}
		if resp.Advance {
			t.Logf("FAIL: wt=%s word=%q → Advance=true (expected false)", wt, word)
			return false
		}
		if resp.Text == "" {
			t.Logf("FAIL: wt=%s word=%q → Text empty (expected rejection text)", wt, word)
			return false
		}

		// Phase should NOT have advanced
		newState := engine.GetActiveWorkflow(userID)
		if newState == nil {
			t.Logf("FAIL: wt=%s word=%q → workflow became nil", wt, word)
			return false
		}
		if newState.PhaseIndex != origPhaseIndex || newState.CurrentPhase != origPhase {
			t.Logf("FAIL: wt=%s word=%q → phase changed (%d/%s → %d/%s)",
				wt, word, origPhaseIndex, origPhase, newState.PhaseIndex, newState.CurrentPhase)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2c (Section 1: skip words on CanSkip=false reject) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2d: Non-confirm text during NeedsConfirm=true + hasOutput=true
//                  → PendingConfirm=true (section 3 preservation)
// ---------------------------------------------------------------------------

// TestPreservation_Section3_PendingConfirm tests that non-confirm text
// during a NeedsConfirm phase with existing output returns PendingConfirm=true.
func TestPreservation_Section3_PendingConfirm(t *testing.T) {
	// Inputs that do NOT match any confirm or skip words.
	nonConfirmInputs := []string{
		"我想修改一下需求",
		"请帮我看看这个",
		"这里有个问题",
		"add a login feature",
		"把第三条改成用户认证",
		"check server status",
		"查看驱网服务器资源状态",
		"random text 12345",
	}

	workflowTypes := []WorkflowType{
		WorkflowCoding,
		WorkflowProductDesign,
		WorkflowInnovation,
		WorkflowBusinessPlan,
		WorkflowTesting,
		WorkflowPresentationDesign,
	}

	f := func(typeIdx, inputIdx uint8) bool {
		wt := workflowTypes[int(typeIdx)%len(workflowTypes)]
		input := nonConfirmInputs[int(inputIdx)%len(nonConfirmInputs)]

		engine, _ := newTestEngine()
		userID := fmt.Sprintf("u_pres2d_%d_%d", typeIdx, inputIdx)

		intent := StructuredIntent{Category: wt, Summary: "test pending confirm"}
		_, err := engine.StartWorkflow(userID, intent)
		if err != nil {
			t.Logf("StartWorkflow(%s) failed: %v", wt, err)
			return false
		}

		// Inject phase output so hasOutput=true (required for section 3).
		state := engine.GetActiveWorkflow(userID)
		if state == nil {
			t.Logf("no active workflow for %s", userID)
			return false
		}
		engine.mu.Lock()
		state.PhaseOutputs[state.CurrentPhase] = "mock phase output document"
		engine.mu.Unlock()

		origPhaseIndex := state.PhaseIndex
		origPhase := state.CurrentPhase

		resp, err := engine.HandleInput(userID, input)
		if err != nil {
			t.Logf("HandleInput(%s, %q) failed: %v", wt, input, err)
			return false
		}

		// Section 3: NeedsConfirm + hasOutput + non-confirm text → PendingConfirm=true
		if !resp.PendingConfirm {
			t.Logf("FAIL: wt=%s input=%q → PendingConfirm=false (expected true)", wt, input)
			return false
		}

		// Phase should NOT have advanced
		newState := engine.GetActiveWorkflow(userID)
		if newState == nil {
			t.Logf("FAIL: wt=%s input=%q → workflow became nil", wt, input)
			return false
		}
		if newState.PhaseIndex != origPhaseIndex || newState.CurrentPhase != origPhase {
			t.Logf("FAIL: wt=%s input=%q → phase changed", wt, input)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2d (Section 3: non-confirm text returns PendingConfirm) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2e: Non-substantial input during IsWaitingForInput=true
//                  → RunAgentLoop=false + reminder text (section 0 preservation)
// ---------------------------------------------------------------------------

// TestPreservation_Section0_InputWaitingReminder tests that non-substantial
// input during an input-waiting workflow returns a reminder with RunAgentLoop=false.
func TestPreservation_Section0_InputWaitingReminder(t *testing.T) {
	// Short, non-substantial inputs that should NOT pass isSubstantialInput.
	// (isSubstantialInput requires ≥50 chars, or file extension indicators,
	// or upload keywords like "已上传"/"附件")
	nonSubstantialInputs := []string{
		"好的",
		"开始",
		"ok",
		"开工",
		"hello",
		"继续",
		"嗯",
	}

	// Input-driven workflow types (have RequiresInput)
	inputDrivenTypes := []WorkflowType{
		WorkflowBidResponse,
		WorkflowContractReview,
		WorkflowDueDiligence,
		WorkflowComplianceAudit,
		WorkflowPatentAnalysis,
	}

	f := func(typeIdx, inputIdx uint8) bool {
		wt := inputDrivenTypes[int(typeIdx)%len(inputDrivenTypes)]
		input := nonSubstantialInputs[int(inputIdx)%len(nonSubstantialInputs)]

		engine, _ := newTestEngine()
		userID := fmt.Sprintf("u_pres2e_%d_%d", typeIdx, inputIdx)

		intent := StructuredIntent{Category: wt, Summary: "test input waiting"}
		_, err := engine.StartWorkflow(userID, intent)
		if err != nil {
			t.Logf("StartWorkflow(%s) failed: %v", wt, err)
			return false
		}

		// Verify the workflow is in input-waiting state.
		state := engine.GetActiveWorkflow(userID)
		if state == nil {
			t.Logf("no active workflow for %s", userID)
			return false
		}

		origPhaseIndex := state.PhaseIndex
		origPhase := state.CurrentPhase

		resp, err := engine.HandleInput(userID, input)
		if err != nil {
			t.Logf("HandleInput(%s, %q) failed: %v", wt, input, err)
			return false
		}

		// Section 0: IsWaitingForInput + non-substantial → RunAgentLoop=false + reminder text
		if resp.RunAgentLoop {
			t.Logf("FAIL: wt=%s input=%q → RunAgentLoop=true (expected false)", wt, input)
			return false
		}
		if resp.Text == "" {
			t.Logf("FAIL: wt=%s input=%q → Text empty (expected reminder text)", wt, input)
			return false
		}

		// Phase should NOT have advanced
		newState := engine.GetActiveWorkflow(userID)
		if newState == nil {
			t.Logf("FAIL: wt=%s input=%q → workflow became nil", wt, input)
			return false
		}
		if newState.PhaseIndex != origPhaseIndex || newState.CurrentPhase != origPhase {
			t.Logf("FAIL: wt=%s input=%q → phase changed", wt, input)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2e (Section 0: input-waiting non-substantial returns reminder) failed: %v", err)
	}
}
