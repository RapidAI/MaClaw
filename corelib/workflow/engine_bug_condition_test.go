package workflow

import (
	"fmt"
	"testing"
	"testing/quick"
)

// Feature: ssh-tool-workflow-hijack, Property 1: Bug Condition — Section 4 Default Branch Sets DefaultInput
//
// For any input that reaches HandleInput section 4 (default branch) — meaning
// the workflow is active, the input does not match confirm/skip words, and the
// phase is not in NeedsConfirm+hasOutput state — the returned WorkflowResponse
// SHALL have DefaultInput=true, preventing handleActiveWorkflow from setting
// workflowAgentLoopMarker.
//
// **Validates: Requirements 1.1, 1.2, 2.2**

// TestBugCondition_Section4_DefaultInput_CodingRequirements tests that an
// unrelated SSH request during a coding workflow at requirements phase with
// no output returns DefaultInput=true.
func TestBugCondition_Section4_DefaultInput_CodingRequirements(t *testing.T) {
	engine, _ := newTestEngine()
	intent := StructuredIntent{Category: WorkflowCoding, Summary: "build a web app"}
	_, err := engine.StartWorkflow("u_bug1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// No phase output exists — this is a fresh workflow at requirements phase.
	// Send an unrelated message that should reach section 4.
	resp, err := engine.HandleInput("u_bug1", "check server status")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}

	if !resp.DefaultInput {
		t.Errorf("expected DefaultInput=true for unrelated input at no-output phase, got false")
	}
	if !resp.RunAgentLoop {
		t.Errorf("expected RunAgentLoop=true, got false")
	}
	if resp.PhasePrompt == "" {
		t.Errorf("expected PhasePrompt to be non-empty")
	}
}

// TestBugCondition_Section4_DefaultInput_CodingChinese tests that a Chinese
// workflow trigger "开工" during a coding workflow at requirements phase with
// no output returns DefaultInput=true.
func TestBugCondition_Section4_DefaultInput_CodingChinese(t *testing.T) {
	engine, _ := newTestEngine()
	intent := StructuredIntent{Category: WorkflowCoding, Summary: "开发贪吃蛇游戏"}
	_, err := engine.StartWorkflow("u_bug2", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, err := engine.HandleInput("u_bug2", "开工")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}

	if !resp.DefaultInput {
		t.Errorf("expected DefaultInput=true for '开工' at no-output phase, got false")
	}
	if !resp.RunAgentLoop {
		t.Errorf("expected RunAgentLoop=true, got false")
	}
	if resp.PhasePrompt == "" {
		t.Errorf("expected PhasePrompt to be non-empty")
	}
}

// TestBugCondition_Section4_DefaultInput_PPTContentOutline tests that a random
// message during a PPT workflow at content_outline phase with no output returns
// DefaultInput=true.
func TestBugCondition_Section4_DefaultInput_PPTContentOutline(t *testing.T) {
	engine, _ := newTestEngine()
	intent := StructuredIntent{Category: WorkflowPresentationDesign, Summary: "产品介绍PPT"}
	_, err := engine.StartWorkflow("u_bug3", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Advance to content_outline (phase index 1) through the classified review
	// transition path.
	saveReviewOutputForCurrentPhase(t, engine, "u_bug3")
	if _, err = engine.ApplyReviewIntent("u_bug3", ReviewIntentConfirm, ""); err != nil {
		t.Fatalf("ApplyReviewIntent confirm failed: %v", err)
	}

	state := engine.GetActiveWorkflow("u_bug3")
	if state == nil || state.CurrentPhase != "content_outline" {
		t.Fatalf("expected to be at content_outline phase, got %v", state)
	}

	// No output at content_outline — send unrelated message.
	resp, err := engine.HandleInput("u_bug3", "hello world")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}

	if !resp.DefaultInput {
		t.Errorf("expected DefaultInput=true for 'hello world' at PPT no-output phase, got false")
	}
	if !resp.RunAgentLoop {
		t.Errorf("expected RunAgentLoop=true, got false")
	}
	if resp.PhasePrompt == "" {
		t.Errorf("expected PhasePrompt to be non-empty")
	}
}

// TestBugCondition_Section4_DefaultInput_Property is a property-based test that
// generates random non-confirm/non-skip text inputs across multiple workflow
// types at their first phase with no output, and asserts DefaultInput=true.
//
// **Validates: Requirements 1.1, 1.2, 2.2**
func TestBugCondition_Section4_DefaultInput_Property(t *testing.T) {
	// Workflow types whose first phase has NeedsConfirm=true and no output
	// (so non-confirm/non-skip text falls through to section 4).
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

	// Inputs that do NOT match any confirm or skip words, ensuring they
	// reach section 4 (default branch).
	section4Inputs := []string{
		"check server 2's status",
		"开工",
		"hello world",
		"查看驱网服务器资源状态",
		"what is the weather today",
		"npm install express",
		"登录4090服务器查看GPU",
		"random text 12345",
		"帮我翻译这段话",
		"run my tests",
	}

	f := func(typeIdx, inputIdx uint8) bool {
		wt := workflowTypes[int(typeIdx)%len(workflowTypes)]
		input := section4Inputs[int(inputIdx)%len(section4Inputs)]

		engine, _ := newTestEngine()
		userID := fmt.Sprintf("u_prop1_%s_%d_%d", wt, typeIdx, inputIdx)

		intent := StructuredIntent{Category: wt, Summary: "test"}
		_, err := engine.StartWorkflow(userID, intent)
		if err != nil {
			t.Logf("StartWorkflow(%s) failed: %v", wt, err)
			return false
		}

		// No phase output — first phase, fresh workflow.
		resp, err := engine.HandleInput(userID, input)
		if err != nil {
			t.Logf("HandleInput(%s, %q) failed: %v", wt, input, err)
			return false
		}

		// Section 4 must return DefaultInput=true.
		if !resp.DefaultInput {
			t.Logf("FAIL: wt=%s input=%q → DefaultInput=false (expected true)", wt, input)
			return false
		}
		if !resp.RunAgentLoop {
			t.Logf("FAIL: wt=%s input=%q → RunAgentLoop=false (expected true)", wt, input)
			return false
		}
		if resp.PhasePrompt == "" {
			t.Logf("FAIL: wt=%s input=%q → PhasePrompt empty (expected non-empty)", wt, input)
			return false
		}
		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 1 (Bug Condition: section 4 DefaultInput=true) failed: %v", err)
	}
}
