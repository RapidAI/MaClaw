package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func workflowPhaseClassification(labels ...intent.IntentLabel) intent.ClassificationResult {
	result := intent.ClassificationResult{Confidence: .98}
	if len(labels) > 0 {
		result.Primary = labels[0]
		result.Secondary = labels[1:]
	}
	return result
}

// The defect this exists for: a workflow phase carries the phase text, the phase
// text describes the project, and the classifier reads that back as
// workflow_task. Before the trim, the phase was refused as an unserved
// capability — a workflow aborting itself, mid-run, for still sounding like
// itself.
func TestAWorkflowPhaseIsNotRefusedForSoundingLikeAWorkflow(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	ctx := withSemanticWorkflowLoop(context.Background(), true)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "做一份完整的市场调研和商业计划", "desktop", "root-phase", "turn-phase",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err != nil {
		t.Fatalf("a workflow phase was refused by the semantic gate: %v", err)
	}
	if handled {
		t.Fatal("a workflow-task-only phase must fall through to the pipeline that already serves phases, not be claimed by the managed surface")
	}
}

// Only the redundant label is dropped. A phase that is also a coding turn is
// still a coding turn, and must keep the managed surface it earns.
func TestAWorkflowPhaseKeepsTheCapabilityItAlsoClaims(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	trimmed := semanticClassificationForWorkflowLoop(true,
		workflowPhaseClassification(intent.LabelWorkflowTask, intent.LabelCoding))
	if trimmed.Primary != intent.LabelCoding || len(trimmed.Secondary) != 0 {
		t.Fatalf("trim left %v/%v, want coding alone", trimmed.Primary, trimmed.Secondary)
	}
	ctx := withSemanticWorkflowLoop(context.Background(), true)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "把这个函数改一下", "desktop", "root-mixed", "turn-mixed",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask, intent.LabelCoding)), nil,
	)
	if err != nil || !handled {
		t.Fatalf("a coding phase lost its managed surface: handled=%v err=%v", handled, err)
	}
}

// The exemption is scoped to phases. An ordinary chat turn must still be
// refused, or the whole point of not auto-starting workflows from chat is lost.
func TestAnOrdinaryTurnIsStillRefusedForWorkflowTask(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "做一份完整的市场调研和商业计划", "desktop", "root-chat", "turn-chat",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err == nil {
		t.Fatal("an ordinary workflow_task turn planned instead of being refused")
	}
	if !handled {
		t.Fatal("the refusal fell through to the legacy router")
	}
}

// What the user is told when that refusal happens. "The capability catalog does
// not cover this request" describes an internal migration state and hides a
// route that exists, so the refusal must name the way in.
func TestTheWorkflowRefusalPointsAtTheWayIn(t *testing.T) {
	resp := semanticHostRejectResponseForPlanError(
		semanticUnmappedCapabilityError{Label: intent.LabelWorkflowTask})
	if resp == nil {
		t.Fatal("no response")
	}
	if !strings.Contains(resp.Text, "工作流") {
		t.Fatalf("refusal text %q never mentions the route it is redirecting to", resp.Text)
	}
	if resp.Error == "semantic_capability_unmet" {
		t.Fatal("a routed refusal must be separable from a genuine catalog gap in telemetry")
	}
}

// The refusal names a slash command, so it is only honest while that command
// is still routed. Mentioning "工作流" would survive the command being renamed
// or dropped, and the user would be sent to type something the dispatcher no
// longer recognizes — a more precise version of the dead end this refusal was
// written to remove.
func TestTheCommandTheWorkflowRefusalNamesIsStillRouted(t *testing.T) {
	resp := semanticHostRejectResponseForPlanError(
		semanticUnmappedCapabilityError{Label: intent.LabelWorkflowTask})
	if resp == nil {
		t.Fatal("no response")
	}
	commands := 0
	for _, field := range strings.Fields(resp.Text) {
		if !strings.HasPrefix(field, "/") {
			continue
		}
		commands++
		if kind := classifyImmediateIMCommand(field); kind == imCommandUnknown {
			t.Fatalf("refusal sends the user to %q, which no command routes", field)
		}
	}
	if commands == 0 {
		t.Fatalf("refusal text %q names no command, so this guard checks nothing", resp.Text)
	}
}

// Every other unmapped family keeps the generic refusal: there is no better
// answer to give, and inventing one per family is how a refusal starts lying.
func TestOtherUnmappedFamiliesKeepTheGenericRefusal(t *testing.T) {
	generic := semanticHostRejectResponse()
	routed := semanticHostRejectResponseForPlanError(
		semanticUnmappedCapabilityError{Label: semanticSyntheticUnmappedLabel})
	if routed.Text != generic.Text || routed.Error != generic.Error {
		t.Fatalf("an unrelated unmapped label got the workflow message: %+v", routed)
	}
	// A non-planning failure (empty surface, missing surface) is not a
	// statement about any family at all.
	if plain := semanticHostRejectResponseForPlanError(nil); plain.Text != generic.Text {
		t.Fatalf("a nil plan error was routed as if it named a family: %+v", plain)
	}
}

func ptrClassification(result intent.ClassificationResult) *intent.ClassificationResult {
	return &result
}
