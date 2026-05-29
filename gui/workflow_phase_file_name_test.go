package main

import "testing"

func TestWorkflowPhaseFileName(t *testing.T) {
	tests := []struct {
		name    string
		phaseID string
		want    string
	}{
		// Known coding phases
		{"coding: requirements", "requirements", "01-requirements.md"},
		{"coding: tech_design", "tech_design", "02-technical-design.md"},
		{"coding: task_breakdown", "task_breakdown", "03-task-breakdown.md"},
		{"coding: implementation", "implementation", "04-implementation.md"},
		{"coding: review", "review", "05-review.md"},

		// Known ops_maintenance phases
		{"ops: ops_intake", "ops_intake", "01-ops-intake.md"},
		{"ops: readonly_collection", "readonly_collection", "02-readonly-collection.md"},
		{"ops: artifact_plan", "artifact_plan", "03-maintenance-artifacts.md"},
		{"ops: risk_policy", "risk_policy", "04-risk-policy.md"},
		{"ops: controlled_execution", "controlled_execution", "05-controlled-execution.md"},

		// Known product_design phases
		{"product_design: problem_discovery", "problem_discovery", "01-problem-discovery.md"},
		{"product_design: solution_design", "solution_design", "02-solution-design.md"},
		{"product_design: prd", "prd", "03-prd.md"},
		{"product_design: prototype", "prototype", "04-prototype.md"},

		// Known innovation phases
		{"innovation: opportunity", "opportunity", "01-opportunity.md"},
		{"innovation: action_plan", "action_plan", "05-action-plan.md"},

		// Known testing phases
		{"testing: test_strategy", "test_strategy", "01-test-strategy.md"},
		{"testing: defect_report", "defect_report", "05-defect-report.md"},

		// Known literature_review phases
		{"literature_review: topic_definition", "topic_definition", "01-topic-definition.md"},
		{"literature_review: review_writing", "review_writing", "05-review-writing.md"},

		// Known experiment_design phases
		{"experiment_design: hypothesis_formulation", "hypothesis_formulation", "01-hypothesis.md"},

		// Known grant_proposal phases
		{"grant_proposal: budget_plan", "budget_plan", "05-budget-plan.md"},

		// Known paper_writing phases
		{"paper_writing: paper_outline", "paper_outline", "01-paper-outline.md"},

		// Known bid_response phases
		{"bid_response: bid_parsing", "bid_parsing", "01-bid-parsing.md"},
		{"bid_response: document_assembly", "document_assembly", "05-document-assembly.md"},

		// Known contract_review phases
		{"contract_review: contract_parsing", "contract_parsing", "01-contract-parsing.md"},

		// Known compliance_audit phases
		{"compliance_audit: audit_scope", "audit_scope", "01-audit-scope.md"},
		{"compliance_audit: audit_report", "audit_report", "05-audit-report.md"},

		// Known patent_analysis phases
		{"patent_analysis: tech_parsing", "tech_parsing", "01-tech-parsing.md"},
		{"patent_analysis: analysis_report", "analysis_report", "05-analysis-report.md"},

		// Known changjiang_scholar phases
		{"changjiang_scholar: scholar_profile", "scholar_profile", "01-scholar-profile.md"},

		// Known changjiang_scholar_review phases
		{"changjiang_scholar_review: review_criteria", "review_criteria", "01-review-criteria.md"},
		{"changjiang_scholar_review: review_conclusion", "review_conclusion", "05-conclusion.md"},

		// Canonical aliases (normalized by canonicalWorkflowPhaseID)
		{"alias: design -> tech_design", "design", "02-technical-design.md"},
		{"alias: tasks -> task_breakdown", "tasks", "03-task-breakdown.md"},

		// Unknown phase ID — sanitized correctly
		{"unknown: custom_phase", "custom_phase", "custom-phase.md"},
		{"unknown: My Custom Phase", "My Custom Phase", "my-custom-phase.md"},
		{"unknown: UPPER_CASE_ID", "UPPER_CASE_ID", "upper-case-id.md"},

		// Empty string — fallback
		{"fallback: empty string", "", "workflow-phase.md"},

		// Whitespace only — fallback
		{"fallback: whitespace only", "   ", "workflow-phase.md"},

		// Unicode only — fallback (no ASCII letters/digits)
		{"fallback: unicode only", "你好", "workflow-phase.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workflowPhaseFileName(tt.phaseID)
			if got != tt.want {
				t.Errorf("workflowPhaseFileName(%q) = %q, want %q", tt.phaseID, got, tt.want)
			}
		})
	}
}
