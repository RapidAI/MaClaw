package v2

import "testing"

func TestPhaseTemplateToSpecPreservesExplicitSemantics(t *testing.T) {
	phase := PhaseTemplate{
		ID:            "generate",
		Name:          "Generate",
		NeedsConfirm:  false,
		ToolPolicy:    ToolPolicyFull,
		Kind:          PhaseKindArtifactGeneration,
		MutationScope: MutationScopeArtifact,
	}

	spec := phaseTemplateToSpec(WorkflowType("deck_builder"), phase)
	if spec.Kind != PhaseKindArtifactGeneration {
		t.Fatalf("Kind = %q, want %q", spec.Kind, PhaseKindArtifactGeneration)
	}
	if spec.MutationScope != MutationScopeArtifact {
		t.Fatalf("MutationScope = %q, want %q", spec.MutationScope, MutationScopeArtifact)
	}
}

func TestPhaseSpecToTemplatePreservesSemanticsForMachineFallback(t *testing.T) {
	registry := NewWorkflowRegistry()
	registry.MustRegister(&TemplateSpec{
		Type: WorkflowType("artifact_semantics_roundtrip"),
		Name: "artifact semantics roundtrip",
		Phases: []PhaseSpec{{
			ID:            "generate",
			Name:          "Generate",
			NeedsConfirm:  false,
			ToolPolicy:    ToolPolicyFull,
			Kind:          PhaseKindArtifactGeneration,
			MutationScope: MutationScopeArtifact,
		}},
	})

	engine := NewWorkflowEngine(registry, nil, nil, nil)
	machine := setupTestMachine()
	engine.SetMachine(machine)

	if _, err := engine.StartWorkflow("user1", StructuredIntent{
		Category: WorkflowType("artifact_semantics_roundtrip"),
		Summary:  "generate a deck",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	tmpl := machine.GetRegistry().Get("artifact_semantics_roundtrip")
	if tmpl == nil {
		t.Fatal("machine template registration missing")
	}
	if len(tmpl.Phases) != 1 {
		t.Fatalf("phase count = %d, want 1", len(tmpl.Phases))
	}
	if tmpl.Phases[0].Kind != PhaseKindArtifactGeneration {
		t.Fatalf("Kind = %q, want %q", tmpl.Phases[0].Kind, PhaseKindArtifactGeneration)
	}
	if tmpl.Phases[0].MutationScope != MutationScopeArtifact {
		t.Fatalf("MutationScope = %q, want %q", tmpl.Phases[0].MutationScope, MutationScopeArtifact)
	}
}

func TestPhaseSpecToTemplateDerivesSemanticsForLegacyTemplateSpecs(t *testing.T) {
	tmpl := phaseSpecToTemplate(WorkflowPresentationDesign, PhaseSpec{
		ID:           "ppt_generation",
		Name:         "PPT Generation",
		NeedsConfirm: false,
		ToolPolicy:   ToolPolicyFull,
	})
	if tmpl.Kind != PhaseKindArtifactGeneration {
		t.Fatalf("Kind = %q, want %q", tmpl.Kind, PhaseKindArtifactGeneration)
	}
	if tmpl.MutationScope != MutationScopeArtifact {
		t.Fatalf("MutationScope = %q, want %q", tmpl.MutationScope, MutationScopeArtifact)
	}
}

func TestStateMachineCreateDerivesPhaseSemanticsForLegacyTemplates(t *testing.T) {
	machine := setupTestMachine()
	machine.GetRegistry().Register(&WorkflowTemplate{
		Type: "presentation_design",
		Name: "legacy presentation",
		Phases: []PhaseTemplate{{
			ID:           "ppt_generation",
			Name:         "PPT Generation",
			NeedsConfirm: false,
			ToolPolicy:   ToolPolicyFull,
		}},
	})

	state, err := machine.Create("semantic-user", "presentation_design", `D:\work\deck`, "make deck")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if len(state.Phases) != 1 {
		t.Fatalf("phase count = %d, want 1", len(state.Phases))
	}
	if state.Phases[0].Kind != PhaseKindArtifactGeneration {
		t.Fatalf("Kind = %q, want %q", state.Phases[0].Kind, PhaseKindArtifactGeneration)
	}
	if state.Phases[0].MutationScope != MutationScopeArtifact {
		t.Fatalf("MutationScope = %q, want %q", state.Phases[0].MutationScope, MutationScopeArtifact)
	}
}

func TestPhaseMetadataPrefersExplicitTemplateSemantics(t *testing.T) {
	metas := PhaseMetadata(&TemplateSpec{
		Type: WorkflowPresentationDesign,
		Phases: []PhaseSpec{{
			ID:            "ppt_generation",
			Name:          "PPT Generation",
			ToolPolicy:    ToolPolicyFull,
			Kind:          PhaseKindArtifactGeneration,
			MutationScope: MutationScopeArtifact,
		}},
	})
	if len(metas) != 1 {
		t.Fatalf("phase count = %d, want 1", len(metas))
	}
	if metas[0].Kind != PhaseKindArtifactGeneration {
		t.Fatalf("Kind = %q, want %q", metas[0].Kind, PhaseKindArtifactGeneration)
	}
	if metas[0].MutationScope != MutationScopeArtifact {
		t.Fatalf("MutationScope = %q, want %q", metas[0].MutationScope, MutationScopeArtifact)
	}
}

func TestGetActivePhaseToolFilterReturnsNoneWhileAwaitingReview(t *testing.T) {
	engine := NewWorkflowEngine(NewWorkflowRegistry(), nil, nil, nil)
	userID := "awaiting-review-filter-user"
	if _, err := engine.StartWorkflow(userID, StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build a project",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if _, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, "requirements doc with enough substance to trigger review gating"); err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance failed: %v", err)
	}
	if got := engine.GetActivePhaseToolFilter(userID); got != ToolFilterNone {
		t.Fatalf("GetActivePhaseToolFilter = %q, want %q while awaiting review", got, ToolFilterNone)
	}
}

func TestBuiltinAcademicTemplateRetainsRichInputSchemaInEngineRegistry(t *testing.T) {
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowNSFCGeneral)
	if tmpl == nil {
		t.Fatal("academic template missing from engine registry")
	}
	if len(tmpl.Phases) == 0 || tmpl.Phases[0].InputSchema == nil {
		t.Fatal("academic template phase 1 input schema missing")
	}
	schema := tmpl.Phases[0].InputSchema
	if !schema.AcceptsResume {
		t.Fatal("AcceptsResume should be preserved in engine registry")
	}
	if schema.AcceptsSupplementary == nil {
		t.Fatal("AcceptsSupplementary should be preserved in engine registry")
	}
	if len(schema.Variants) < 2 {
		t.Fatalf("Variants should be preserved, got %d", len(schema.Variants))
	}
	if len(schema.Variants[1].Fields) == 0 {
		t.Fatal("manual variant fields should be preserved")
	}
}

func TestPhaseSpecToTemplatePreservesRichInputSchemaForLegacyTemplateSpecs(t *testing.T) {
	tmpl := phaseSpecToTemplate(WorkflowType("legacy_form_roundtrip"), PhaseSpec{
		ID:           "profile",
		Name:         "Profile",
		NeedsConfirm: true,
		ToolPolicy:   ToolPolicyDocOnly,
		InputSchema: &PhaseInputSchemaSpec{
			Title:         "Profile Form",
			Description:   "collect profile",
			AcceptsResume: true,
			AcceptsSupplementary: &SupplementaryDocConfigSpec{
				Label:         "Attachments",
				Description:   "upload optional docs",
				MaxFiles:      3,
				AcceptedTypes: []string{".pdf", ".docx"},
			},
			Variants: []PhaseInputVariantSpec{
				{
					ID:    "manual_mode",
					Label: "Manual",
					Fields: []PhaseInputFieldSpec{{
						Name:     "name",
						Label:    "Name",
						Type:     "text",
						Required: true,
						Reusable: true,
					}},
				},
			},
		},
	})
	if tmpl.InputSchema == nil {
		t.Fatal("InputSchema should be preserved")
	}
	if !tmpl.InputSchema.AcceptsResume {
		t.Fatal("AcceptsResume should survive PhaseSpec -> PhaseTemplate conversion")
	}
	if tmpl.InputSchema.AcceptsSupplementary == nil || tmpl.InputSchema.AcceptsSupplementary.MaxFiles != 3 {
		t.Fatalf("AcceptsSupplementary lost during conversion: %#v", tmpl.InputSchema.AcceptsSupplementary)
	}
	if len(tmpl.InputSchema.Variants) != 1 || len(tmpl.InputSchema.Variants[0].Fields) != 1 {
		t.Fatalf("Variants lost during conversion: %#v", tmpl.InputSchema.Variants)
	}
	if !tmpl.InputSchema.Variants[0].Fields[0].Reusable {
		t.Fatal("Reusable should survive PhaseSpec -> PhaseTemplate conversion")
	}
}
