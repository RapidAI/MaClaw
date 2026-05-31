package workflow

import (
	"strings"
	"testing"
)

func TestBuiltinTemplates_Count(t *testing.T) {
	r := NewWorkflowRegistry()

	expected := map[WorkflowType]int{
		WorkflowCoding:                  5,
		WorkflowProductDesign:           4,
		WorkflowInnovation:              5,
		WorkflowBusinessPlan:            5,
		WorkflowTesting:                 5,
		WorkflowLiteratureReview:        5,
		WorkflowResearchReport:          5,
		WorkflowExperimentDesign:        5,
		WorkflowGrantProposal:           5,
		WorkflowPaperWriting:            5,
		WorkflowProjectProposal:         5,
		WorkflowEventPlanning:           5,
		WorkflowCompetitiveAnalysis:     5,
		WorkflowPresentationDesign:      5,
		WorkflowOpsMaintenance:          5,
		WorkflowChangjiangScholar:       5,
		WorkflowChangjiangScholarReview: 5,
	}

	for wt, wantPhases := range expected {
		tmpl := r.Match(wt)
		if tmpl == nil {
			t.Errorf("template %s not found", wt)
			continue
		}
		if len(tmpl.Phases) != wantPhases {
			t.Errorf("template %s: expected %d phases, got %d", wt, wantPhases, len(tmpl.Phases))
		}
	}
}

func TestBuiltinTemplates_PhaseIDsAndOrder(t *testing.T) {
	r := NewWorkflowRegistry()

	cases := []struct {
		wt       WorkflowType
		phaseIDs []string
	}{
		{WorkflowCoding, []string{"requirements", "tech_design", "task_breakdown", "implementation", "review"}},
		{WorkflowProductDesign, []string{"problem_discovery", "solution_design", "prd", "prototype"}},
		{WorkflowInnovation, []string{"opportunity", "ideation", "validation", "roadmap", "action_plan"}},
		{WorkflowBusinessPlan, []string{"bp_requirement", "bp_content", "bp_structure", "bp_visual_design", "bp_doc_generation"}},
		{WorkflowTesting, []string{"test_strategy", "test_design", "test_environment", "test_execution", "defect_report"}},
		{WorkflowLiteratureReview, []string{"topic_definition", "literature_search", "screening_classification", "content_extraction", "review_writing"}},
		{WorkflowResearchReport, []string{"requirement_scoping", "source_mapping", "report_collection", "insight_extraction", "synthesis_report"}},
		{WorkflowExperimentDesign, []string{"hypothesis_formulation", "experiment_design", "variable_control", "data_collection", "analysis_plan"}},
		{WorkflowGrantProposal, []string{"topic_justification", "research_status", "research_plan", "expected_outcomes", "budget_plan"}},
		{WorkflowPaperWriting, []string{"outline_design", "methodology", "results_presentation", "discussion_analysis", "submission_prep"}},
		{WorkflowProjectProposal, []string{"background_analysis", "goal_definition", "solution_design", "resource_assessment", "risk_contingency"}},
		{WorkflowEventPlanning, []string{"requirement_confirm", "scheme_planning", "process_design", "material_checklist", "execution_manual"}},
		{WorkflowCompetitiveAnalysis, []string{"target_definition", "competitor_identification", "dimension_comparison", "gap_analysis", "strategy_recommendation"}},
		{WorkflowPresentationDesign, []string{"audience_goal", "content_outline", "style_specification", "slide_scripting", "ppt_generation"}},
		{WorkflowOpsMaintenance, []string{"ops_intake", "readonly_collection", "artifact_plan", "risk_policy", "controlled_execution"}},
		{WorkflowChangjiangScholar, []string{"cj_personal_profile", "cj_academic_achievements", "cj_research_plan", "cj_talent_cultivation", "cj_recommendation_summary"}},
		{WorkflowChangjiangScholarReview, []string{"cj_completeness_check", "cj_achievement_evaluation", "cj_plan_feasibility", "cj_narrative_quality", "cj_improvement_report"}},
	}

	for _, tc := range cases {
		tmpl := r.Match(tc.wt)
		if tmpl == nil {
			t.Errorf("template %s not found", tc.wt)
			continue
		}
		if len(tmpl.Phases) != len(tc.phaseIDs) {
			t.Errorf("template %s: phase count mismatch", tc.wt)
			continue
		}
		for i, wantID := range tc.phaseIDs {
			if tmpl.Phases[i].ID != wantID {
				t.Errorf("template %s phase %d: expected ID %q, got %q", tc.wt, i, wantID, tmpl.Phases[i].ID)
			}
		}
	}
}

func TestBuiltinTemplates_RequiredFieldsNonEmpty(t *testing.T) {
	r := NewWorkflowRegistry()

	for _, wt := range []WorkflowType{
		WorkflowCoding, WorkflowProductDesign, WorkflowInnovation,
		WorkflowBusinessPlan, WorkflowTesting,
		WorkflowLiteratureReview, WorkflowResearchReport,
		WorkflowExperimentDesign, WorkflowGrantProposal, WorkflowPaperWriting,
		WorkflowProjectProposal, WorkflowEventPlanning, WorkflowCompetitiveAnalysis,
		WorkflowPresentationDesign, WorkflowOpsMaintenance,
		WorkflowChangjiangScholar, WorkflowChangjiangScholarReview,
	} {
		tmpl := r.Match(wt)
		if tmpl == nil {
			t.Errorf("template %s not found", wt)
			continue
		}
		if tmpl.Name == "" {
			t.Errorf("template %s: Name is empty", wt)
		}
		if tmpl.Description == "" {
			t.Errorf("template %s: Description is empty", wt)
		}
		for i, phase := range tmpl.Phases {
			if phase.ID == "" {
				t.Errorf("template %s phase %d: ID is empty", wt, i)
			}
			if phase.Name == "" {
				t.Errorf("template %s phase %d: Name is empty", wt, i)
			}
			if phase.Prompt == "" {
				t.Errorf("template %s phase %d: Prompt is empty", wt, i)
			}
			if len(phase.Checklist) == 0 {
				t.Errorf("template %s phase %d: Checklist is empty", wt, i)
			}
		}
	}
}

func TestBuiltinTemplates_CodingImplementationToolPolicyFull(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}
	// implementation is phase index 3
	implPhase := tmpl.Phases[3]
	if implPhase.ID != "implementation" {
		t.Fatalf("expected phase 3 to be implementation, got %s", implPhase.ID)
	}
	if implPhase.ToolPolicy != ToolFilterFull {
		t.Errorf("coding implementation phase: expected ToolPolicy=full, got %s", implPhase.ToolPolicy)
	}
}

func TestBuiltinTemplates_CodingTaskBreakdownUsesOneBasedDependencyLabels(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}
	phase := tmpl.Phases[2]
	if phase.ID != PhaseCodingTaskBreakdown {
		t.Fatalf("expected task breakdown phase, got %s", phase.ID)
	}
	if !strings.Contains(phase.Prompt, "任务编号从 T1 开始") || !strings.Contains(phase.Prompt, "依赖 T1") {
		t.Fatalf("task breakdown prompt should define one-based dependency labels, got %q", phase.Prompt)
	}
	if strings.Contains(phase.Prompt, "依赖 T0") {
		t.Fatalf("task breakdown prompt must not teach T0 dependencies: %q", phase.Prompt)
	}
}

func TestBuiltinTemplates_TestingTestExecutionToolPolicyFull(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowTesting)
	if tmpl == nil {
		t.Fatal("testing template not found")
	}
	// test_execution is phase index 3
	execPhase := tmpl.Phases[3]
	if execPhase.ID != "test_execution" {
		t.Fatalf("expected phase 3 to be test_execution, got %s", execPhase.ID)
	}
	if execPhase.ToolPolicy != ToolFilterFull {
		t.Errorf("testing test_execution phase: expected ToolPolicy=full, got %s", execPhase.ToolPolicy)
	}
}

func TestBuiltinTemplates_PresentationGenerationToolPolicyFull(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowPresentationDesign)
	if tmpl == nil {
		t.Fatal("presentation_design template not found")
	}
	lastPhase := tmpl.Phases[len(tmpl.Phases)-1]
	if lastPhase.ID != "ppt_generation" {
		t.Fatalf("expected last phase to be ppt_generation, got %s", lastPhase.ID)
	}
	if lastPhase.ToolPolicy != ToolFilterFull {
		t.Errorf("ppt_generation phase: expected ToolPolicy=full, got %s", lastPhase.ToolPolicy)
	}
}

func TestBuiltinTemplates_BusinessPlanDocGenerationToolPolicyFull(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowBusinessPlan)
	if tmpl == nil {
		t.Fatal("business_plan template not found")
	}
	lastPhase := tmpl.Phases[len(tmpl.Phases)-1]
	if lastPhase.ID != "bp_doc_generation" {
		t.Fatalf("expected last phase to be bp_doc_generation, got %s", lastPhase.ID)
	}
	if lastPhase.ToolPolicy != ToolFilterFull {
		t.Errorf("bp_doc_generation phase: expected ToolPolicy=full, got %s", lastPhase.ToolPolicy)
	}
	if lastPhase.NeedsConfirm {
		t.Error("bp_doc_generation phase: expected NeedsConfirm=false for execution phase")
	}
}

func TestBuiltinTemplates_BusinessPlanDocGenerationUsesStableASCIIFileNames(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowBusinessPlan)
	if tmpl == nil {
		t.Fatal("business_plan template not found")
	}
	lastPhase := tmpl.Phases[len(tmpl.Phases)-1]
	if !strings.Contains(lastPhase.Prompt, "{project_slug}_business-plan.md") {
		t.Fatalf("business plan prompt missing stable ASCII business plan filename: %s", lastPhase.Prompt)
	}
	if !strings.Contains(lastPhase.Prompt, "{project_slug}_speech-script.md") {
		t.Fatalf("business plan prompt missing stable ASCII speech script filename: %s", lastPhase.Prompt)
	}
	if strings.Contains(lastPhase.Prompt, "_商业计划书.md") || strings.Contains(lastPhase.Prompt, "_演讲稿.md") {
		t.Fatalf("business plan prompt should not require localized filename suffixes: %s", lastPhase.Prompt)
	}
}

func TestBuiltinTemplates_OpsMaintenanceControlledExecutionPolicy(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowOpsMaintenance)
	if tmpl == nil {
		t.Fatal("ops_maintenance template not found")
	}
	if len(tmpl.Phases) != 5 {
		t.Fatalf("ops_maintenance phase count: got %d, want 5", len(tmpl.Phases))
	}
	for i, phase := range tmpl.Phases[:4] {
		if phase.ToolPolicy != ToolFilterDocOnly {
			t.Errorf("ops_maintenance phase %d %s: expected doc_only before execution gate, got %s", i, phase.ID, phase.ToolPolicy)
		}
		if !phase.NeedsConfirm {
			t.Errorf("ops_maintenance phase %d %s: expected confirmation before proceeding", i, phase.ID)
		}
	}
	exec := tmpl.Phases[4]
	if exec.ID != "controlled_execution" {
		t.Fatalf("expected final phase controlled_execution, got %s", exec.ID)
	}
	if exec.ToolPolicy != ToolFilterOpsControlled {
		t.Errorf("controlled_execution phase: expected ToolPolicy=ops_controlled, got %s", exec.ToolPolicy)
	}
	if exec.NeedsConfirm {
		t.Error("controlled_execution phase: expected NeedsConfirm=false after risk policy confirmation")
	}
	if !exec.DisableOrchestrator {
		t.Error("controlled_execution phase: expected DisableOrchestrator=true so risk policy is not parsed as a task list")
	}
}

func TestBuiltinTemplates_OpsMaintenanceDocumentsCloseAllPolicyStrength(t *testing.T) {
	r := NewWorkflowRegistry()
	tmpl := r.Match(WorkflowOpsMaintenance)
	if tmpl == nil {
		t.Fatal("ops_maintenance template not found")
	}
	riskPolicy := tmpl.Phases[3]
	if riskPolicy.ID != "risk_policy" {
		t.Fatalf("expected phase 3 risk_policy, got %s", riskPolicy.ID)
	}
	if !strings.Contains(riskPolicy.Prompt, "close_all is a global SSH action") ||
		!strings.Contains(riskPolicy.Prompt, "risk_level L3") ||
		!strings.Contains(riskPolicy.Prompt, "approval_required double") {
		t.Fatalf("risk_policy prompt must document close_all L3/double requirement")
	}
	exec := tmpl.Phases[4]
	if !strings.Contains(exec.Prompt, "Treat close_all as executable only when the confirmed policy is L3 with double approval") {
		t.Fatalf("controlled_execution prompt must document close_all execution requirement")
	}
}
