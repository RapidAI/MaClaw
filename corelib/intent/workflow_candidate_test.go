package intent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestWorkflowCandidateLabels_IncludesAmbiguousAndUnknown verifies the
// safety net: Ambiguous and Unknown are always workflow candidates regardless
// of MayTriggerWorkflow, because we can't confidently reject them.
func TestWorkflowCandidateLabels_IncludesAmbiguousAndUnknown(t *testing.T) {
	// Even with empty definitions, Ambiguous and Unknown should be candidates.
	candidates := WorkflowCandidateLabels(nil)
	if !candidates[LabelAmbiguous] {
		t.Error("LabelAmbiguous not in candidates with nil definitions")
	}
	if !candidates[LabelUnknown] {
		t.Error("LabelUnknown not in candidates with nil definitions")
	}
}

// TestWorkflowCandidateLabels_OnlyMayTriggerWorkflow verifies that only
// definitions with MayTriggerWorkflow=true are included.
func TestWorkflowCandidateLabels_OnlyMayTriggerWorkflow(t *testing.T) {
	defs := []IntentDefinition{
		{Label: "alpha", MayTriggerWorkflow: true},
		{Label: "beta", MayTriggerWorkflow: false},
		{Label: "gamma", MayTriggerWorkflow: true},
	}
	candidates := WorkflowCandidateLabels(defs)

	if !candidates["alpha"] {
		t.Error("alpha (MayTriggerWorkflow=true) not in candidates")
	}
	if candidates["beta"] {
		t.Error("beta (MayTriggerWorkflow=false) should not be in candidates")
	}
	if !candidates["gamma"] {
		t.Error("gamma (MayTriggerWorkflow=true) not in candidates")
	}
}

// TestDefaultDefinitions_MayTriggerWorkflow_Consistency verifies that the
// MayTriggerWorkflow field is set correctly on all default definitions.
func TestDefaultDefinitions_MayTriggerWorkflow_Consistency(t *testing.T) {
	defs := DefaultDefinitions()

	// Expected workflow-capable labels.
	expectWorkflow := map[IntentLabel]bool{
		LabelCoding:       true,
		LabelOffice:       true,
		LabelWorkflowTask: true,
	}

	for _, def := range defs {
		if expectWorkflow[def.Label] && !def.MayTriggerWorkflow {
			t.Errorf("%s: MayTriggerWorkflow should be true", def.Label)
		}
		if !expectWorkflow[def.Label] && def.MayTriggerWorkflow {
			t.Errorf("%s: MayTriggerWorkflow should be false (not a workflow-capable intent)", def.Label)
		}
	}
}

// TestIsWorkflowCandidate_ViaClassifier verifies the UIC method delegates
// correctly to the pre-computed workflow candidate set.
func TestIsWorkflowCandidate_ViaClassifier(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})

	// Coding should be a workflow candidate.
	if !uic.IsWorkflowCandidate(LabelCoding) {
		t.Error("IsWorkflowCandidate(LabelCoding) = false, want true")
	}

	// DocumentDelivery should NOT be a workflow candidate.
	if uic.IsWorkflowCandidate(LabelDocumentDelivery) {
		t.Error("IsWorkflowCandidate(LabelDocumentDelivery) = true, want false")
	}
	if uic.IsWorkflowCandidate(LabelDocumentOpen) {
		t.Error("IsWorkflowCandidate(LabelDocumentOpen) = true, want false")
	}

	// Ambiguous should always be a candidate.
	if !uic.IsWorkflowCandidate(LabelAmbiguous) {
		t.Error("IsWorkflowCandidate(LabelAmbiguous) = false, want true")
	}
}

// TestGetWorkflowRejectThreshold_DefaultAndOverride verifies the threshold
// is sourced from FusionConfig and can be overridden.
func TestGetWorkflowRejectThreshold_DefaultAndOverride(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})

	// Default threshold.
	if got := uic.GetWorkflowRejectThreshold(); got != DefaultWorkflowRejectThreshold {
		t.Errorf("default threshold = %.2f, want %.2f", got, DefaultWorkflowRejectThreshold)
	}

	// Override via SetFusionConfig.
	cfg := uic.GetFusionConfig()
	cfg.WorkflowRejectThreshold = 0.85
	uic.SetFusionConfig(cfg)
	if got := uic.GetWorkflowRejectThreshold(); got != 0.85 {
		t.Errorf("overridden threshold = %.2f, want 0.85", got)
	}

	// Zero threshold falls back to default.
	cfg.WorkflowRejectThreshold = 0
	uic.SetFusionConfig(cfg)
	if got := uic.GetWorkflowRejectThreshold(); got != DefaultWorkflowRejectThreshold {
		t.Errorf("zero threshold should fallback to default %.2f, got %.2f",
			DefaultWorkflowRejectThreshold, got)
	}
}
