package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestShouldBypassWorkflowForClassification_DirectExecutionIntents(t *testing.T) {
	cases := []intent.IntentLabel{
		intent.LabelSSH,
		intent.LabelMaintenance,
		intent.LabelBugFix,
		intent.LabelNonCoding,
	}
	for _, label := range cases {
		result := intent.ClassificationResult{Primary: label, Confidence: 0.82}
		if !shouldBypassWorkflowForClassification(result, false, 0.70) {
			t.Fatalf("%s should bypass workflow takeover", label)
		}
	}
}

func TestShouldBypassWorkflowForClassification_KeepsCreationWorkflow(t *testing.T) {
	result := intent.ClassificationResult{
		Primary:          intent.LabelCoding,
		Confidence:       0.86,
		WorkflowType:     "coding",
		CreationOriented: true,
	}
	if shouldBypassWorkflowForClassification(result, true, 0.70) {
		t.Fatalf("creation-oriented coding with workflow_type must remain workflow-eligible")
	}
}

func TestShouldBypassWorkflowForClassification_KeepsContinuationContextual(t *testing.T) {
	result := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.92}
	if shouldBypassWorkflowForClassification(result, false, 0.70) {
		t.Fatalf("continuation must be resolved from recent intent context, not bypassed by itself")
	}
}

func TestShouldBypassWorkflowForClassification_RejectsLowConfidence(t *testing.T) {
	result := intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.55}
	if shouldBypassWorkflowForClassification(result, false, 0.70) {
		t.Fatalf("low-confidence non-workflow intent should not bypass workflow")
	}
}

func TestShouldBypassWorkflowForClassification_DoesNotBypassDegradedCoding(t *testing.T) {
	result := intent.ClassificationResult{
		Primary:    intent.LabelCoding,
		Confidence: 0.82,
		Layer:      1,
		Degraded:   true,
	}
	if shouldBypassWorkflowForClassification(result, true, 0.70) {
		t.Fatalf("degraded coding without workflow_type should fall through for deeper workflow understanding")
	}
}
