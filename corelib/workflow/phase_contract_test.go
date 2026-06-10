package workflow

import "testing"

func mustPhase(t *testing.T, tmpl *WorkflowTemplate, phaseID string) PhaseTemplate {
	t.Helper()
	if tmpl == nil {
		t.Fatal("template is nil")
	}
	for _, phase := range tmpl.Phases {
		if phase.ID == phaseID {
			return phase
		}
	}
	t.Fatalf("phase %s not found in %s", phaseID, tmpl.Type)
	return PhaseTemplate{}
}

func TestDerivePhaseContractCodingTaskBreakdownPlanning(t *testing.T) {
	tmpl := NewWorkflowRegistry().Match(WorkflowCoding)
	phase := mustPhase(t, tmpl, PhaseCodingTaskBreakdown)

	c := DerivePhaseContract(tmpl, phase)
	if c.Kind != PhaseKindCodePlanning {
		t.Fatalf("Kind=%s, want %s", c.Kind, PhaseKindCodePlanning)
	}
	if c.MutationScope != MutationScopeWorkflowDoc {
		t.Fatalf("MutationScope=%s, want %s", c.MutationScope, MutationScopeWorkflowDoc)
	}
	if !c.ExpectsDocument || !c.RequiresReview || !c.UsesSystemDocPersistence {
		t.Fatalf("planning phase must be reviewable system-doc phase: %#v", c)
	}
	if !c.AllowsRepoInspection {
		t.Fatalf("planning phase must allow repo inspection: %#v", c)
	}
	if c.AllowsProjectMutation || c.AllowsDelegation || c.ActivatesOrchestrator {
		t.Fatalf("planning phase must not mutate/delegate/orchestrate: %#v", c)
	}
}

func TestDerivePhaseContractDocOnlyStillAllowsReadContext(t *testing.T) {
	c := DerivePhaseContract(nil, PhaseTemplate{ID: "doc", ToolPolicy: ToolFilterDocOnly, NeedsConfirm: true})
	if !c.AllowsRepoInspection {
		t.Fatalf("doc-only phases expose read_file/list_directory and should report read-context capability: %#v", c)
	}
	if c.AllowsProjectMutation || c.AllowsDelegation || c.ActivatesOrchestrator {
		t.Fatalf("doc-only phase must stay non-mutating: %#v", c)
	}
}

func TestDerivePhaseContractCodingImplementationActivatesOrchestrator(t *testing.T) {
	tmpl := NewWorkflowRegistry().Match(WorkflowCoding)
	phase := mustPhase(t, tmpl, PhaseCodingImplementation)

	c := DerivePhaseContract(tmpl, phase)
	if c.Kind != PhaseKindExecution {
		t.Fatalf("Kind=%s, want %s", c.Kind, PhaseKindExecution)
	}
	if c.MutationScope != MutationScopeProject {
		t.Fatalf("MutationScope=%s, want %s", c.MutationScope, MutationScopeProject)
	}
	if !c.AllowsProjectMutation || !c.AllowsDelegation || !c.ActivatesOrchestrator {
		t.Fatalf("implementation must be project execution orchestrator phase: %#v", c)
	}
	if c.ExpectsDocument || c.RequiresReview {
		t.Fatalf("implementation must not be review document phase: %#v", c)
	}
}

func TestDerivePhaseContractFullArtifactGenerationDoesNotActivateOrchestrator(t *testing.T) {
	cases := []struct {
		workflow WorkflowType
		phaseID  string
	}{
		{WorkflowBusinessPlan, "bp_doc_generation"},
		{WorkflowPresentationDesign, "ppt_generation"},
	}

	for _, tc := range cases {
		t.Run(string(tc.workflow)+"/"+tc.phaseID, func(t *testing.T) {
			tmpl := NewWorkflowRegistry().Match(tc.workflow)
			phase := mustPhase(t, tmpl, tc.phaseID)
			if phase.ToolPolicy != ToolFilterFull {
				t.Fatalf("fixture phase ToolPolicy=%s, want full", phase.ToolPolicy)
			}
			c := DerivePhaseContract(tmpl, phase)
			if c.Kind != PhaseKindArtifactGeneration {
				t.Fatalf("Kind=%s, want %s", c.Kind, PhaseKindArtifactGeneration)
			}
			if c.MutationScope != MutationScopeArtifact {
				t.Fatalf("MutationScope=%s, want %s", c.MutationScope, MutationScopeArtifact)
			}
			if c.AllowsProjectMutation || c.AllowsDelegation || c.ActivatesOrchestrator {
				t.Fatalf("artifact generation must not look like project execution: %#v", c)
			}
		})
	}
}

func TestDerivePhaseContractUnknownFullTemplateFailsClosed(t *testing.T) {
	tmpl := &WorkflowTemplate{
		Type: WorkflowType("custom"),
		Phases: []PhaseTemplate{{
			ID:         "full_unknown",
			ToolPolicy: ToolFilterFull,
		}},
	}
	c := DerivePhaseContract(tmpl, tmpl.Phases[0])
	if c.MutationScope != MutationScopeNone {
		t.Fatalf("unknown full MutationScope=%s, want %s", c.MutationScope, MutationScopeNone)
	}
	if c.ActivatesOrchestrator || c.AllowsProjectMutation || c.AllowsDelegation {
		t.Fatalf("unknown full phase must fail closed for project execution: %#v", c)
	}
}

func TestDerivePhaseRuntimeGateSeparatesBlockingFromStaticContract(t *testing.T) {
	tmpl := NewWorkflowRegistry().Match(WorkflowCoding)
	phase := mustPhase(t, tmpl, PhaseCodingRequirements)
	ws := &WorkflowState{
		Type:         WorkflowCoding,
		CurrentPhase: phase.ID,
		PhaseIndex:   0,
		Status:       WorkflowActive,
	}

	gate := DerivePhaseRuntimeGate(tmpl, ws)
	if !gate.WaitingForPhaseForm || !gate.BlocksAgentLoop {
		t.Fatalf("unsubmitted input schema should block phase loop: %#v", gate)
	}
	if gate.Contract.Kind == "" || gate.Contract.ToolPolicy != phase.ToolPolicy {
		t.Fatalf("runtime gate should include static contract: %#v", gate)
	}

	ws.PhaseFormSkipped = true
	gate = DerivePhaseRuntimeGate(tmpl, ws)
	if gate.WaitingForPhaseForm || gate.BlocksAgentLoop {
		t.Fatalf("satisfied form gate should not block: %#v", gate)
	}

	ws.PendingReviewPhaseID = phase.ID
	gate = DerivePhaseRuntimeGate(tmpl, ws)
	if !gate.AwaitingReview || !gate.BlocksAgentLoop {
		t.Fatalf("pending review should block independently from static contract: %#v", gate)
	}
}

func TestValidateWorkflowTemplateContractBuiltinTemplates(t *testing.T) {
	for _, tmpl := range NewWorkflowRegistry().All() {
		if errs := ValidateWorkflowTemplateContract(tmpl); len(errs) != 0 {
			t.Fatalf("%s contract validation errors: %v", tmpl.Type, errs)
		}
	}
}

func TestValidateWorkflowTemplateContractRejectsDirectorySemanticTextField(t *testing.T) {
	for _, fieldName := range []string{"project_path", "project_root", "repo_path", "repository_root", "workspace_root", "worktree_path"} {
		t.Run(fieldName, func(t *testing.T) {
			tmpl := &WorkflowTemplate{Type: "bad_directory_field", Phases: []PhaseTemplate{{
				ID:         "intake",
				ToolPolicy: ToolFilterDocOnly,
				InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{
					Name: fieldName,
					Type: "text",
				}}},
			}}}

			if errs := ValidateWorkflowTemplateContract(tmpl); len(errs) == 0 {
				t.Fatalf("expected %s text field to violate directory picker contract", fieldName)
			}
		})
	}
}

func TestValidateWorkflowTemplateContractRejectsUnsupportedInputFieldType(t *testing.T) {
	for _, fieldType := range []string{"direcotry", "object"} {
		t.Run(fieldType, func(t *testing.T) {
			tmpl := &WorkflowTemplate{Type: "bad_field_type", Phases: []PhaseTemplate{{
				ID:         "intake",
				ToolPolicy: ToolFilterDocOnly,
				InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{
					Name: "target",
					Type: fieldType,
				}}},
			}}}

			if errs := ValidateWorkflowTemplateContract(tmpl); len(errs) == 0 {
				t.Fatal("expected unsupported field type to violate input schema contract")
			}
		})
	}
}

func TestValidateWorkflowTemplateContractAllowsFilePathTextField(t *testing.T) {
	tmpl := &WorkflowTemplate{Type: "file_path_field", Phases: []PhaseTemplate{{
		ID:         "intake",
		ToolPolicy: ToolFilterDocOnly,
		InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{
			Name: "file_path",
			Type: "text",
		}}},
	}}}

	if errs := ValidateWorkflowTemplateContract(tmpl); len(errs) != 0 {
		t.Fatalf("file_path should not be forced to directory picker: %v", errs)
	}
}

func TestValidateWorkflowTemplateContractRejectsConflictingScopes(t *testing.T) {
	cases := []struct {
		name string
		tmpl *WorkflowTemplate
	}{
		{
			name: "reviewable project mutation",
			tmpl: &WorkflowTemplate{Type: "bad_review_project", Phases: []PhaseTemplate{{
				ID:            "plan",
				NeedsConfirm:  true,
				ToolPolicy:    ToolFilterFull,
				Kind:          PhaseKindExecution,
				MutationScope: MutationScopeProject,
			}}},
		},
		{
			name: "planning artifact mutation",
			tmpl: &WorkflowTemplate{Type: "bad_planning_artifact", Phases: []PhaseTemplate{{
				ID:            "plan",
				NeedsConfirm:  true,
				ToolPolicy:    ToolFilterPlanning,
				Kind:          PhaseKindCodePlanning,
				MutationScope: MutationScopeArtifact,
			}}},
		},
		{
			name: "reviewable full policy",
			tmpl: &WorkflowTemplate{Type: "bad_reviewable_full", Phases: []PhaseTemplate{{
				ID:           "plan",
				NeedsConfirm: true,
				ToolPolicy:   ToolFilterFull,
			}}},
		},
		{
			name: "unknown full policy",
			tmpl: &WorkflowTemplate{Type: "bad_unknown_full", Phases: []PhaseTemplate{{
				ID:         "generate",
				ToolPolicy: ToolFilterFull,
			}}},
		},
		{
			name: "artifact project scope",
			tmpl: &WorkflowTemplate{Type: "bad_artifact_project", Phases: []PhaseTemplate{{
				ID:            "generate",
				ToolPolicy:    ToolFilterFull,
				Kind:          PhaseKindArtifactGeneration,
				MutationScope: MutationScopeProject,
			}}},
		},
		{
			name: "ops wrong policy",
			tmpl: &WorkflowTemplate{Type: "bad_ops_policy", Phases: []PhaseTemplate{{
				ID:            "ops",
				ToolPolicy:    ToolFilterFull,
				Kind:          PhaseKindOpsExecution,
				MutationScope: MutationScopeOps,
			}}},
		},
		{
			name: "execution wrong policy",
			tmpl: &WorkflowTemplate{Type: "bad_execution_policy", Phases: []PhaseTemplate{{
				ID:            "execute",
				ToolPolicy:    ToolFilterPlanning,
				Kind:          PhaseKindExecution,
				MutationScope: MutationScopeProject,
			}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := ValidateWorkflowTemplateContract(tc.tmpl); len(errs) == 0 {
				t.Fatalf("expected contract validation error for %#v", tc.tmpl)
			}
		})
	}
}
