package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// TestWorkflowPhaseFileNamesUniquePerTemplate is the anti-drift guard for the
// hand-maintained knownPhaseFileNames map. For every registered backend template
// it asserts that the file names workflowPhaseFileName produces for that
// template's phases are pairwise-unique. A collision means two distinct phases
// persist to the same path under .maclaw/workflow/, so one phase's document
// silently overwrites the other (the paper_writing methodology/results_presentation
// bug this guard was added to prevent recurring).
//
// Because it iterates registry.All() (the single source of truth), adding or
// renaming a template phase that introduces a collision fails this test rather
// than shipping a data-loss bug.
func TestWorkflowPhaseFileNamesUniquePerTemplate(t *testing.T) {
	for _, tmpl := range workflow.NewWorkflowRegistry().All() {
		if tmpl == nil {
			continue
		}
		seen := make(map[string]string, len(tmpl.Phases))
		for _, meta := range workflow.PhaseMetadata(tmpl) {
			fileName := workflowPhaseFileName(meta.ID)
			if prev, ok := seen[fileName]; ok {
				t.Errorf("%s: phases %q and %q both map to file %q — one document would overwrite the other",
					tmpl.Type, prev, meta.ID, fileName)
				continue
			}
			seen[fileName] = meta.ID
		}
	}
}

// TestWorkflowPhaseFileNamesNonEmptyPerTemplate asserts every registered phase
// produces a non-empty, .md-suffixed file name, so no phase's document is ever
// written to a degenerate path.
func TestWorkflowPhaseFileNamesNonEmptyPerTemplate(t *testing.T) {
	for _, tmpl := range workflow.NewWorkflowRegistry().All() {
		if tmpl == nil {
			continue
		}
		for _, meta := range workflow.PhaseMetadata(tmpl) {
			fileName := workflowPhaseFileName(meta.ID)
			if fileName == "" || fileName == ".md" {
				t.Errorf("%s: phase %q produced degenerate file name %q", tmpl.Type, meta.ID, fileName)
			}
		}
	}
}

// TestWorkflowPhaseFileNameForTemplateOrdersByPosition is the anti-drift guard
// for the position-aware namer. For every registered template it asserts the
// file names workflowPhaseFileNameForTemplate produces are (a) pairwise-unique,
// (b) prefixed with the phase's 1-based template position, and (c) strictly
// increasing in backend phase order — so a directory listing always sorts in the
// true workflow order, even for phase IDs that appear at different positions in
// different templates (e.g. solution_design, strategy_recommendation) and for
// phases absent from the flat knownPhaseFileNames map.
func TestWorkflowPhaseFileNameForTemplateOrdersByPosition(t *testing.T) {
	for _, tmpl := range workflow.NewWorkflowRegistry().All() {
		if tmpl == nil {
			continue
		}
		metas := workflow.PhaseMetadata(tmpl)
		seen := make(map[string]string, len(metas))
		lastPrefix := 0
		for _, meta := range metas {
			fileName := workflowPhaseFileNameForTemplate(tmpl, meta.ID)

			// (a) unique within the template
			if prev, ok := seen[fileName]; ok {
				t.Errorf("%s: phases %q and %q both map to %q", tmpl.Type, prev, meta.ID, fileName)
			}
			seen[fileName] = meta.ID

			// (b) prefixed with the 1-based template position
			wantPrefix := meta.Index + 1
			gotPrefix := -1
			if i := strings.IndexByte(fileName, '-'); i > 0 {
				if n, err := parsePositiveInt(fileName[:i]); err == nil {
					gotPrefix = n
				}
			}
			if gotPrefix != wantPrefix {
				t.Errorf("%s/%s: file %q prefix = %d, want %d (1-based template position)",
					tmpl.Type, meta.ID, fileName, gotPrefix, wantPrefix)
			}

			// (c) strictly increasing in phase order
			if gotPrefix <= lastPrefix {
				t.Errorf("%s/%s: prefix %d not strictly greater than previous %d",
					tmpl.Type, meta.ID, gotPrefix, lastPrefix)
			}
			lastPrefix = gotPrefix
		}
	}
}

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
