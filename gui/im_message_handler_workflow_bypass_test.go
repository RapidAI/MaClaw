package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestShouldBypassWorkflowForIntentDoesNotCallTreeLLM(t *testing.T) {
	var llmCalls atomic.Int32
	h := &IMMessageHandler{unifiedClassifier: intent.New(intent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			llmCalls.Add(1)
			return `{"top":[{"skill":"search","score":0.96}]}`, nil
		},
		LLMTimeout: time.Second,
	})}

	if h.shouldBypassWorkflowForIntent("desktop-user:test", "find recent Go concurrency guidance", false) {
		t.Fatal("unavailable embedding must fall through instead of using an L3 workflow bypass")
	}
	if got := llmCalls.Load(); got != 0 {
		t.Fatalf("workflow bypass must not block on L3, got %d calls", got)
	}
}

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

func TestShouldBypassWorkflowForClassification_EmbeddingPolicyUsesWorkflowCandidateSet(t *testing.T) {
	defs := intent.DefaultDefinitions()
	candidates := intent.WorkflowCandidateLabels(defs)

	for label, isCandidate := range candidates {
		if !isCandidate {
			continue
		}
		t.Run(string(label), func(t *testing.T) {
			result := intent.ClassificationResult{Primary: label, Confidence: 0.86}
			if shouldBypassWorkflowForClassification(result, true, 0.70) {
				t.Fatalf("must not be rejected by embedding-only policy because it can trigger workflows")
			}
		})
	}

	for _, def := range defs {
		if def.MayTriggerWorkflow || candidates[def.Label] || def.Label == intent.LabelContinuation {
			continue
		}
		t.Run(string(def.Label), func(t *testing.T) {
			result := intent.ClassificationResult{Primary: def.Label, Confidence: 0.86}
			if !shouldBypassWorkflowForClassification(result, candidates[def.Label], 0.70) {
				t.Fatalf("should be rejected by embedding-only policy because it cannot trigger workflows")
			}
		})
	}
}

func TestShouldBypassWorkflowForClassification_KeepsOfficeWorkflowCandidate(t *testing.T) {
	result := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.88}
	if shouldBypassWorkflowForClassification(result, true, 0.70) {
		t.Fatal("office is workflow-capable and must reach full fusion for presentation_design detection")
	}
}

func TestShouldBypassWorkflowForClassification_BypassesOfficeAfterTreeRejectsWorkflow(t *testing.T) {
	result := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.88, Layer: 23}
	if !shouldBypassWorkflowForClassification(result, true, 0.70) {
		t.Fatal("office with full-fusion empty workflow_type should bypass workflow takeover")
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

func TestShouldBypassWorkflowForClassification_KeepsUnknownForUnderstanding(t *testing.T) {
	result := intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30}
	if shouldBypassWorkflowForClassification(result, true, 0.70) {
		t.Fatalf("unknown/low-confidence UIC results must fall through to IntentUnderstandingManager")
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

func TestShouldEscapeActiveUnderstandingForClassification_KeepsWorkflowCapableLabels(t *testing.T) {
	cases := []intent.IntentLabel{
		intent.LabelOffice,
		intent.LabelCoding,
		intent.LabelWorkflowTask,
	}
	for _, label := range cases {
		result := intent.ClassificationResult{Primary: label, Confidence: 0.88, Layer: 23}
		if shouldEscapeActiveUnderstandingForClassification(result, true, 0.70) {
			t.Fatalf("%s can belong to an active workflow clarification and must not escape understanding", label)
		}
	}
}

func TestShouldEscapeActiveUnderstandingForClassification_EscapesDirectExecution(t *testing.T) {
	cases := []intent.IntentLabel{
		intent.LabelSSH,
		intent.LabelMaintenance,
		intent.LabelBugFix,
		intent.LabelNonCoding,
	}
	for _, label := range cases {
		result := intent.ClassificationResult{Primary: label, Confidence: 0.88, Layer: 23}
		if !shouldEscapeActiveUnderstandingForClassification(result, false, 0.70) {
			t.Fatalf("%s should escape active understanding for normal agent/tool handling", label)
		}
	}
}
