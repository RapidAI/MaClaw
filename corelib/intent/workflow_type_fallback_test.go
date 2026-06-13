package intent

import (
	"testing"
)

// TestFusionToClassification_DegradedMode_InfersWorkflowType verifies that
// when the L3 tree channel fails (degraded mode), fusionToClassification
// infers WorkflowType from IntentDefinition.WorkflowTypes via the
// FusionConfig.WorkflowTypeMap.
//
// This is the core mechanism that prevents workflow bypass when LLMs are
// unavailable: embedding-only mode can still produce WorkflowType="coding"
// for a confident LabelCoding classification.
func TestFusionToClassification_DegradedMode_InfersWorkflowType(t *testing.T) {
	defs := DefaultDefinitions()
	cfg := DefaultFusionConfigWithWorkflowTypes(defs)

	u := &UnifiedIntentClassifier{fusionCfg: cfg}

	// Simulate degraded mode: embedding-only, tree channel failed.
	// Top candidate is coding with no WorkflowType (tree didn't provide it).
	fr := FusionResult{
		Verdict: VerdictAmbiguous, // typical for embedding-only
		Top: FusedCandidate{
			Label:      LabelCoding,
			FinalScore: 0.61,
			EmbScore:   0.61,
			InEmb:      true,
			InTree:     false,
			// WorkflowType is empty — tree channel failed
		},
		RunnerUp: &FusedCandidate{
			Label:      LabelSSH,
			FinalScore: 0.54,
		},
		Degraded:       true,
		ActiveChannels: []string{"embedding"},
	}

	result := u.fusionToClassification(fr)

	if result.WorkflowType != "coding" {
		t.Errorf("WorkflowType = %q, want %q (should be inferred from definition)", result.WorkflowType, "coding")
	}
	if !result.CreationOriented {
		t.Error("CreationOriented = false, want true (coding + workflow_type=coding)")
	}
	if !result.Degraded {
		t.Error("Degraded = false, want true")
	}
}

// TestFusionToClassification_DegradedMode_InfersOfficeWorkflowType verifies
// that office intent also gets WorkflowType inferred in degraded mode.
func TestFusionToClassification_DegradedMode_InfersOfficeWorkflowType(t *testing.T) {
	defs := DefaultDefinitions()
	cfg := DefaultFusionConfigWithWorkflowTypes(defs)

	u := &UnifiedIntentClassifier{fusionCfg: cfg}

	fr := FusionResult{
		Verdict: VerdictClear,
		Top: FusedCandidate{
			Label:      LabelOffice,
			FinalScore: 0.75,
			InEmb:      true,
		},
		Degraded:       true,
		ActiveChannels: []string{"embedding"},
	}

	result := u.fusionToClassification(fr)

	if result.WorkflowType != "presentation_design" {
		t.Errorf("WorkflowType = %q, want %q", result.WorkflowType, "presentation_design")
	}
}

// TestFusionToClassification_NormalMode_TreeWorkflowTypePreserved verifies
// that when the tree channel provides WorkflowType, the definition fallback
// does NOT override it.
func TestFusionToClassification_NormalMode_TreeWorkflowTypePreserved(t *testing.T) {
	defs := DefaultDefinitions()
	cfg := DefaultFusionConfigWithWorkflowTypes(defs)

	u := &UnifiedIntentClassifier{fusionCfg: cfg}

	fr := FusionResult{
		Verdict: VerdictClear,
		Top: FusedCandidate{
			Label:        LabelCoding,
			FinalScore:   0.85,
			InEmb:        true,
			InTree:       true,
			WorkflowType: "coding", // tree provided it
		},
		Degraded:       false,
		ActiveChannels: []string{"embedding", "tree"},
	}

	result := u.fusionToClassification(fr)

	if result.WorkflowType != "coding" {
		t.Errorf("WorkflowType = %q, want %q", result.WorkflowType, "coding")
	}
}

// TestFusionToClassification_TreeActiveEmptyWorkflowType_NoOverride verifies
// that when the tree channel IS active and deliberately returned empty
// WorkflowType (e.g., "修改函数返回值" — coding but not creation-oriented),
// the definition fallback does NOT override the tree's judgment.
func TestFusionToClassification_TreeActiveEmptyWorkflowType_NoOverride(t *testing.T) {
	defs := DefaultDefinitions()
	cfg := DefaultFusionConfigWithWorkflowTypes(defs)

	u := &UnifiedIntentClassifier{fusionCfg: cfg}

	fr := FusionResult{
		Verdict: VerdictClear,
		Top: FusedCandidate{
			Label:        LabelCoding,
			FinalScore:   0.85,
			InEmb:        true,
			InTree:       true,        // tree was active
			WorkflowType: "",          // tree deliberately returned empty
		},
		Degraded:       false,
		ActiveChannels: []string{"embedding", "tree"},
	}

	result := u.fusionToClassification(fr)

	if result.WorkflowType != "" {
		t.Errorf("WorkflowType = %q, want empty (tree active, deliberately empty — must not override)",
			result.WorkflowType)
	}
}

// TestFusionToClassification_LowVerdict_NoWorkflowTypeInference verifies
// that VerdictLow does NOT infer WorkflowType — low confidence means we
// don't trust the label enough to trigger a workflow.
func TestFusionToClassification_LowVerdict_NoWorkflowTypeInference(t *testing.T) {
	defs := DefaultDefinitions()
	cfg := DefaultFusionConfigWithWorkflowTypes(defs)

	u := &UnifiedIntentClassifier{fusionCfg: cfg}

	fr := FusionResult{
		Verdict: VerdictLow,
		Top: FusedCandidate{
			Label:      LabelCoding,
			FinalScore: 0.10,
			InEmb:      true,
		},
		Degraded:       true,
		ActiveChannels: []string{"embedding"},
	}

	result := u.fusionToClassification(fr)

	if result.WorkflowType != "" {
		t.Errorf("WorkflowType = %q, want empty (VerdictLow should not infer)", result.WorkflowType)
	}
	if result.Primary != LabelAmbiguous {
		t.Errorf("Primary = %s, want %s (VerdictLow → Ambiguous)", result.Primary, LabelAmbiguous)
	}
}

// TestFusionToClassification_NonWorkflowLabel_NoInference verifies that
// labels without WorkflowTypes (e.g., SSH, search) don't get a spurious
// WorkflowType in degraded mode.
func TestFusionToClassification_NonWorkflowLabel_NoInference(t *testing.T) {
	defs := DefaultDefinitions()
	cfg := DefaultFusionConfigWithWorkflowTypes(defs)

	u := &UnifiedIntentClassifier{fusionCfg: cfg}

	for _, label := range []IntentLabel{LabelSSH, LabelSearch, LabelBugFix, LabelNonCoding} {
		fr := FusionResult{
			Verdict: VerdictClear,
			Top: FusedCandidate{
				Label:      label,
				FinalScore: 0.80,
				InEmb:      true,
			},
			Degraded:       true,
			ActiveChannels: []string{"embedding"},
		}

		result := u.fusionToClassification(fr)

		if result.WorkflowType != "" {
			t.Errorf("label=%s: WorkflowType = %q, want empty (non-workflow label)", label, result.WorkflowType)
		}
	}
}

// TestBuildWorkflowTypeMap verifies the map is built correctly from definitions.
func TestBuildWorkflowTypeMap(t *testing.T) {
	defs := DefaultDefinitions()
	m := BuildWorkflowTypeMap(defs)

	// coding maps to "coding" (first WorkflowType, used as degraded-mode default)
	if wf, ok := m[LabelCoding]; !ok || wf != "coding" {
		t.Errorf("LabelCoding: got (%q, %v), want (\"coding\", true)", wf, ok)
	}

	// office has exactly one WorkflowType
	if wf, ok := m[LabelOffice]; !ok || wf != "presentation_design" {
		t.Errorf("LabelOffice: got (%q, %v), want (\"presentation_design\", true)", wf, ok)
	}

	// SSH has no WorkflowTypes
	if _, ok := m[LabelSSH]; ok {
		t.Error("LabelSSH should not be in WorkflowTypeMap")
	}

	// bug_fix has no WorkflowTypes
	if _, ok := m[LabelBugFix]; ok {
		t.Error("LabelBugFix should not be in WorkflowTypeMap")
	}
}

// TestDegradedFlagPropagation verifies that the Degraded flag propagates
// from FusionResult through ClassificationResult to GateIntentResult.
func TestDegradedFlagPropagation(t *testing.T) {
	defs := DefaultDefinitions()
	cfg := DefaultFusionConfigWithWorkflowTypes(defs)

	u := &UnifiedIntentClassifier{fusionCfg: cfg}

	// Degraded fusion result
	fr := FusionResult{
		Verdict: VerdictAmbiguous,
		Top: FusedCandidate{
			Label:      LabelCoding,
			FinalScore: 0.61,
			InEmb:      true,
		},
		Degraded:       true,
		ActiveChannels: []string{"embedding"},
	}

	cr := u.fusionToClassification(fr)
	if !cr.Degraded {
		t.Error("ClassificationResult.Degraded = false, want true")
	}

	// Non-degraded fusion result
	fr2 := FusionResult{
		Verdict: VerdictClear,
		Top: FusedCandidate{
			Label:        LabelCoding,
			FinalScore:   0.85,
			InEmb:        true,
			InTree:       true,
			WorkflowType: "coding",
		},
		Degraded:       false,
		ActiveChannels: []string{"embedding", "tree"},
	}

	cr2 := u.fusionToClassification(fr2)
	if cr2.Degraded {
		t.Error("ClassificationResult.Degraded = true, want false")
	}
}
