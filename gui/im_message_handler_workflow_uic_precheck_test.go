package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// TestWorkflowCandidateLabels_DerivedFromDefinitions verifies that the
// workflow-candidate set is derived from IntentDefinition.MayTriggerWorkflow,
// not from a hardcoded list. This is the core mechanism: adding a new intent
// that can trigger workflows requires only setting MayTriggerWorkflow=true
// in its definition — no separate list to maintain.
func TestWorkflowCandidateLabels_DerivedFromDefinitions(t *testing.T) {
	defs := intent.DefaultDefinitions()
	candidates := intent.WorkflowCandidateLabels(defs)

	// Coding and Office should be workflow candidates (MayTriggerWorkflow=true).
	for _, label := range []intent.IntentLabel{intent.LabelCoding, intent.LabelOffice} {
		if !candidates[label] {
			t.Errorf("WorkflowCandidateLabels does not contain %q — "+
				"this label has MayTriggerWorkflow=true in its definition", label)
		}
	}

	// Ambiguous and Unknown should always be candidates (hardcoded safety net).
	for _, label := range []intent.IntentLabel{intent.LabelAmbiguous, intent.LabelUnknown} {
		if !candidates[label] {
			t.Errorf("WorkflowCandidateLabels does not contain %q — "+
				"ambiguous/unknown must always proceed to IUM", label)
		}
	}

	// Non-workflow labels should NOT be candidates.
	nonWorkflow := []intent.IntentLabel{
		intent.LabelDocumentDelivery,
		intent.LabelDocumentOpen,
		intent.LabelNonCoding,
		intent.LabelSearch,
		intent.LabelBugFix,
		intent.LabelMaintenance,
		intent.LabelContinuation,
		intent.LabelSSH,
		intent.LabelBrowser,
	}
	for _, label := range nonWorkflow {
		if candidates[label] {
			t.Errorf("WorkflowCandidateLabels contains %q — "+
				"this label does NOT have MayTriggerWorkflow=true and should be fast-rejected", label)
		}
	}
}

// TestWorkflowCandidateLabels_NewDefinitionAutoRegisters verifies that
// adding a new IntentDefinition with MayTriggerWorkflow=true automatically
// makes it a workflow candidate without touching any other code.
func TestWorkflowCandidateLabels_NewDefinitionAutoRegisters(t *testing.T) {
	defs := intent.DefaultDefinitions()

	// Simulate adding a new intent definition with MayTriggerWorkflow=true.
	defs = append(defs, intent.IntentDefinition{
		Label:              "data_analysis",
		Domain:             "分析 (Analysis)",
		MayTriggerWorkflow: true,
	})

	candidates := intent.WorkflowCandidateLabels(defs)
	if !candidates["data_analysis"] {
		t.Error("New definition with MayTriggerWorkflow=true was not included in candidates — " +
			"the mechanism should auto-register new workflow-capable intents")
	}
}

// TestWorkflowRejectThreshold_InFusionConfig verifies the threshold is
// sourced from FusionConfig (tunable via calibration), not hardcoded.
func TestWorkflowRejectThreshold_InFusionConfig(t *testing.T) {
	cfg := intent.DefaultFusionConfig()
	if cfg.WorkflowRejectThreshold != intent.DefaultWorkflowRejectThreshold {
		t.Errorf("DefaultFusionConfig().WorkflowRejectThreshold = %.2f, want %.2f",
			cfg.WorkflowRejectThreshold, intent.DefaultWorkflowRejectThreshold)
	}
	if cfg.WorkflowRejectThreshold < 0.50 || cfg.WorkflowRejectThreshold > 0.95 {
		t.Errorf("WorkflowRejectThreshold = %.2f, want [0.50, 0.95] — "+
			"too low risks false rejections, too high lets everything through",
			cfg.WorkflowRejectThreshold)
	}
}

// TestMayTriggerWorkflow_CodingIsTrue is a critical safety check.
// LabelCoding MUST have MayTriggerWorkflow=true because coding messages
// like "开发一个贪吃蛇游戏" need to reach IntentUnderstandingManager.
func TestMayTriggerWorkflow_CodingIsTrue(t *testing.T) {
	for _, def := range intent.DefaultDefinitions() {
		if def.Label == intent.LabelCoding {
			if !def.MayTriggerWorkflow {
				t.Fatal("LabelCoding.MayTriggerWorkflow is false — this would prevent " +
					"coding workflow tasks from ever reaching IntentUnderstandingManager")
			}
			return
		}
	}
	t.Fatal("LabelCoding not found in DefaultDefinitions")
}

// TestMayTriggerWorkflow_OfficeIsTrue is a safety check.
// LabelOffice MUST have MayTriggerWorkflow=true because office messages
// like "帮我做一份PPT" could trigger presentation_design workflow.
func TestMayTriggerWorkflow_OfficeIsTrue(t *testing.T) {
	for _, def := range intent.DefaultDefinitions() {
		if def.Label == intent.LabelOffice {
			if !def.MayTriggerWorkflow {
				t.Fatal("LabelOffice.MayTriggerWorkflow is false — this would prevent " +
					"presentation_design workflow tasks from reaching IntentUnderstandingManager")
			}
			return
		}
	}
	t.Fatal("LabelOffice not found in DefaultDefinitions")
}

// TestMayTriggerWorkflow_DocumentDeliveryIsFalse verifies the original bug
// scenario: document_delivery should NOT trigger workflows.
func TestMayTriggerWorkflow_DocumentDeliveryIsFalse(t *testing.T) {
	for _, def := range intent.DefaultDefinitions() {
		if def.Label == intent.LabelDocumentDelivery {
			if def.MayTriggerWorkflow {
				t.Fatal("LabelDocumentDelivery.MayTriggerWorkflow is true — " +
					"file send requests like '把文件发给我' would bypass the UIC pre-check " +
					"and still reach IntentUnderstandingManager")
			}
			return
		}
	}
	t.Fatal("LabelDocumentDelivery not found in DefaultDefinitions")
}
