package main

import (
	"fmt"
	"path/filepath"
	"strings"

	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// knownPhaseFileNames maps canonical phase IDs to predictable numbered file
// names. Known phases get numbered prefixes for consistent ordering within
// each workflow type's output directory. The map covers all ~110 phase IDs
// across 22 registered workflow templates.
var knownPhaseFileNames = map[string]string{
	// coding workflow
	"requirements":   "01-requirements.md",
	"tech_design":    "02-technical-design.md",
	"design":         "02-technical-design.md", // canonical form of tech_design
	"task_breakdown": "03-task-breakdown.md",
	"tasks":          "03-task-breakdown.md", // canonical form of task_breakdown
	"implementation": "04-implementation.md",
	"review":         "05-review.md",

	// product_design workflow
	"problem_discovery": "01-problem-discovery.md",
	"solution_design":   "02-solution-design.md",
	"prd":               "03-prd.md",
	"prototype":         "04-prototype.md",

	// innovation workflow
	"opportunity":    "01-opportunity.md",
	"trend_analysis": "01-trend-analysis.md",
	"ideation":       "02-ideation.md",
	"solution":       "03-solution.md",
	"validation":     "03-validation.md",
	"feasibility":    "04-feasibility.md",
	"roadmap":        "04-roadmap.md",
	"action_plan":    "05-action-plan.md",

	// business_plan workflow
	"bp_requirement":    "01-requirement.md",
	"market_analysis":   "01-market-analysis.md",
	"bp_content":        "02-content.md",
	"business_model":    "02-business-model.md",
	"bp_structure":      "03-structure.md",
	"financial_plan":    "03-financial-plan.md",
	"bp_visual_design":  "04-visual-design.md",
	"operations":        "04-operations.md",
	"bp_doc_generation": "05-doc-generation.md",

	// testing workflow
	"test_strategy":    "01-test-strategy.md",
	"test_design":      "02-test-design.md",
	"test_environment": "03-test-environment.md",
	"test_execution":   "04-test-execution.md",
	"defect_report":    "05-defect-report.md",

	// literature_review workflow
	"topic_definition":         "01-topic-definition.md",
	"literature_search":        "02-literature-search.md",
	"search_strategy":          "02-search-strategy.md",
	"screening_classification": "03-screening-classification.md",
	"screening":                "03-screening.md",
	"content_extraction":       "04-content-extraction.md",
	"review_writing":           "05-review-writing.md",
	"synthesis":                "05-synthesis.md",

	// research_report workflow
	"requirement_scoping": "01-requirement-scoping.md",
	"problem_definition":  "01-problem-definition.md",
	"source_mapping":      "02-source-mapping.md",
	"report_collection":   "03-report-collection.md",
	"insight_extraction":  "04-insight-extraction.md",
	"synthesis_report":    "05-synthesis-report.md",
	"conclusion":          "05-conclusion.md",

	// experiment_design workflow
	"hypothesis_formulation": "01-hypothesis.md",
	"hypothesis":             "01-hypothesis.md",
	"experiment_design":      "02-experiment-design.md",
	"variable_control":       "03-variable-control.md",
	"variables":              "03-variables.md",
	"data_collection":        "04-data-collection.md",
	"data_plan":              "04-data-plan.md",
	"analysis_plan":          "05-analysis-plan.md",

	// grant_proposal workflow
	"topic_justification": "01-topic-justification.md",
	"topic":               "01-topic.md",
	"research_status":     "02-research-status.md",
	"foundation":          "02-foundation.md",
	"research_plan":       "03-research-plan.md",
	"expected_outcomes":   "04-expected-outcomes.md",
	"budget_plan":         "05-budget-plan.md",
	"proposal":            "05-proposal.md",

	// paper_writing workflow
	// Current template phase IDs (outline_design, methodology, results_presentation,
	// discussion_analysis, submission_prep) map to unique, position-ordered names.
	// The four legacy IDs below (paper_outline/literature_basis/results_writing/
	// paper_polish) are retained for backward compatibility with any previously
	// persisted docs; they must not collide with the current IDs' file names.
	"outline_design":       "01-paper-outline.md",    // template phase index 0
	"methodology":          "02-methodology.md",      // template phase index 1
	"results_presentation": "03-results.md",          // template phase index 2
	"discussion_analysis":  "04-discussion.md",       // template phase index 3
	"submission_prep":      "05-submission.md",       // template phase index 4
	"paper_outline":        "01-paper-outline.md",    // legacy alias of outline_design
	"literature_basis":     "02-literature-basis.md", // legacy (no current equivalent)
	"literature":           "02-literature.md",       // current template phase ID
	"drafting":             "03-drafting.md",         // current template phase ID
	"figures":              "04-figures.md",          // current template phase ID
	"polishing":            "05-polishing.md",        // current template phase ID
	"results_writing":      "03-results.md",          // legacy alias of results_presentation
	"paper_polish":         "05-submission.md",       // legacy alias of submission_prep

	// project_proposal workflow
	"project_background":  "01-background.md",
	"project_objectives":  "02-objectives.md",
	"project_plan":        "03-plan.md",
	"resource_budget":     "04-resource-budget.md",
	"risk_assessment":     "05-risk-assessment.md",
	"background_analysis": "01-background.md",      // actual template phase ID
	"background":          "01-background.md",      // current template phase ID
	"goal_definition":     "02-objectives.md",      // actual template phase ID
	"objectives":          "02-objectives.md",      // current template phase ID
	"plan":                "03-plan.md",            // current template phase ID
	"resource_assessment": "04-resource-budget.md", // actual template phase ID
	"budget":              "04-budget.md",          // current template phase ID
	"risk_contingency":    "05-risk-assessment.md", // actual template phase ID
	"risk_plan":           "05-risk-plan.md",       // current template phase ID

	// event_planning workflow
	"event_objectives":    "01-objectives.md",
	"event_concept":       "02-concept.md",
	"event_logistics":     "03-logistics.md",
	"event_promotion":     "04-promotion.md",
	"event_execution":     "05-execution-plan.md",
	"requirement_confirm": "01-objectives.md",      // actual template phase ID
	"positioning":         "01-positioning.md",     // current template phase ID
	"scheme_planning":     "02-concept.md",         // actual template phase ID
	"creative":            "02-creative.md",        // current template phase ID
	"process_design":      "03-logistics.md",       // actual template phase ID
	"execution_plan":      "03-execution-plan.md",  // current template phase ID
	"material_checklist":  "04-promotion.md",       // actual template phase ID
	"budget_schedule":     "04-budget-schedule.md", // current template phase ID
	"execution_manual":    "05-execution-plan.md",  // actual template phase ID
	"contingency":         "05-contingency.md",     // current template phase ID

	// competitive_analysis workflow
	"market_landscape":    "01-market-landscape.md",
	"competitor_id":       "01-competitor-id.md",
	"competitor_profiles": "02-competitor-profiles.md",
	"comparison":          "02-comparison.md",
	"feature_comparison":  "03-feature-comparison.md",
	"swot":                "03-swot.md",
	"strategy_analysis":   "04-strategy-analysis.md",
	"differentiation":     "04-differentiation.md",
	"recommendations":     "05-recommendations.md",

	// presentation_design workflow
	// Backend order: audience_goal(0), content_outline(1), style_specification(2),
	// slide_scripting(3), ppt_generation(4).
	"audience_goal":       "01-audience-goal.md",
	"content_outline":     "02-content-outline.md",
	"style_specification": "03-style-specification.md", // template phase index 2
	"slide_scripting":     "04-slide-scripting.md",     // template phase index 3
	"ppt_generation":      "05-ppt-generation.md",      // template phase index 4
	"visual_design":       "03-visual-design.md",       // legacy (no current equivalent)

	// ops_maintenance workflow
	"ops_intake":           "01-ops-intake.md",
	"readonly_collection":  "02-readonly-collection.md",
	"artifact_plan":        "03-maintenance-artifacts.md",
	"risk_policy":          "04-risk-policy.md",
	"controlled_execution": "05-controlled-execution.md",

	// maintenance workflow (lightweight coding maintenance)
	"maint_analysis":     "01-maint-analysis.md",
	"maint_execution":    "02-maint-execution.md",
	"maint_verification": "03-maint-verification.md",

	// bid_response workflow
	"bid_parsing":        "01-bid-parsing.md",
	"tender_analysis":    "01-tender-analysis.md", // current template phase ID
	"qualification":      "02-qualification.md",
	"technical_proposal": "03-technical-proposal.md",
	"technical":          "03-technical.md", // current template phase ID
	"commercial_quote":   "04-commercial-quote.md",
	"commercial":         "04-commercial.md", // current template phase ID
	"document_assembly":  "05-document-assembly.md",
	"assembly":           "05-assembly.md", // current template phase ID

	// bid_review workflow (标书检查)
	"br_standards":   "01-br-standards.md",
	"br_bid_content": "02-br-bid-content.md",
	"br_conformity":  "03-br-conformity.md",
	"br_fix_report":  "04-br-fix-report.md",

	// contract_review workflow
	"contract_parsing": "01-contract-parsing.md",
	"parsing":          "01-parsing.md", // current template phase ID
	"clause_risk":      "02-clause-risk.md",
	"risk_analysis":    "02-risk-analysis.md", // current template phase ID
	"compliance_check": "03-compliance-check.md",
	"compliance":       "03-compliance.md", // current template phase ID
	"revision_suggest": "04-revision-suggestions.md",
	"suggestions":      "04-suggestions.md", // current template phase ID
	"review_opinion":   "05-review-opinion.md",
	"opinion":          "05-opinion.md", // current template phase ID

	// due_diligence workflow
	"company_profile": "01-company-profile.md",
	"business_dd":     "02-business-dd.md",
	"financial_dd":    "03-financial-dd.md",
	"legal_dd":        "04-legal-dd.md",
	"dd_conclusion":   "05-dd-conclusion.md",

	// compliance_audit workflow
	"audit_scope":       "01-audit-scope.md",
	"scope":             "01-scope.md", // current template phase ID
	"compliance_assess": "02-compliance-assessment.md",
	"assessment":        "02-assessment.md", // current template phase ID
	"risk_rating":       "03-risk-rating.md",
	"remediation_plan":  "04-remediation-plan.md",
	"remediation":       "04-remediation.md", // current template phase ID
	"audit_report":      "05-audit-report.md",

	// patent_analysis workflow
	"tech_parsing":      "01-tech-parsing.md",
	"prior_art":         "02-prior-art.md",
	"infringement_eval": "03-infringement-eval.md",
	"strategy_suggest":  "04-strategy-suggestions.md",
	"analysis_report":   "05-analysis-report.md",

	// changjiang_scholar workflow
	"scholar_profile":     "01-scholar-profile.md",
	"research_direction":  "02-research-direction.md",
	"achievement_summary": "03-achievements.md",
	"development_plan":    "04-development-plan.md",
	"application_doc":     "05-application.md",

	// changjiang_scholar_review workflow
	"review_criteria":           "01-review-criteria.md",
	"material_review":           "02-material-review.md",
	"scoring_evaluation":        "03-scoring.md",
	"comparison_analysis":       "04-comparison.md",
	"review_conclusion":         "05-conclusion.md",
	"cj_completeness_check":     "01-review-criteria.md",
	"cj_achievement_evaluation": "02-material-review.md",
	"cj_plan_feasibility":       "03-scoring.md",
	"cj_narrative_quality":      "04-comparison.md",
	"cj_improvement_report":     "05-conclusion.md",

	// --- Parametric academic phase IDs (generated by BuildAcademicApplicationTemplate) ---
	"cj_profile":    "01-scholar-profile.md",
	"cj_foundation": "02-academic-achievements.md",
	"cj_plan":       "03-research-plan.md",
	"cj_phase4":     "04-talent-cultivation.md",
	"cj_assembly":   "05-application.md",
	"dy_profile":    "01-eligibility.md",
	"dy_foundation": "02-research-foundation.md",
	"dy_plan":       "03-research-proposal.md",
	"dy_phase4":     "04-outcomes-budget.md",
	"dy_assembly":   "05-final-assembly.md",
	"ey_profile":    "01-eligibility.md",
	"ey_foundation": "02-research-accumulation.md",
	"ey_plan":       "03-research-proposal.md",
	"ey_phase4":     "04-outcomes-budget.md",
	"ey_assembly":   "05-final-assembly.md",
	"yf_profile":    "01-rationale.md",
	"yf_plan":       "03-methodology.md",
	"yf_phase4":     "04-budget.md",
	"yf_assembly":   "05-final-assembly.md",
	"gp_profile":    "01-rationale.md",
	"gp_plan":       "03-methodology.md",
	"gp_phase4":     "04-budget.md",
	"gp_assembly":   "05-final-assembly.md",
	"kp_profile":    "01-strategic-rationale.md",
	"kp_foundation": "02-team-foundation.md",
	"kp_plan":       "03-research-plan.md",
	"kp_phase4":     "04-budget-management.md",
	"kp_assembly":   "05-final-assembly.md",
}

func workflowPhaseFileName(phaseID string) string {
	canonical := canonicalWorkflowPhaseID(phaseID)
	if name, ok := knownPhaseFileNames[canonical]; ok {
		return name
	}
	stem := sanitizeWorkflowPhaseFileStem(canonical)
	if stem == "" {
		return "workflow-phase.md"
	}
	return stem + ".md"
}

// workflowPhaseFileStem returns just the descriptive stem for a phase (no numeric
// prefix, no extension). It strips any leading "NN-" prefix from the flat map
// entry so the stem can be recombined with a registry-derived position prefix.
func workflowPhaseFileStem(phaseID string) string {
	name := workflowPhaseFileName(phaseID)
	name = strings.TrimSuffix(name, filepath.Ext(name)) // drop .md
	// Drop a leading "NN-" ordering prefix if present.
	if i := strings.IndexByte(name, '-'); i > 0 {
		if _, err := parsePositiveInt(name[:i]); err == nil {
			return name[i+1:]
		}
	}
	return name
}

// workflowPhaseFileNameForTemplate produces the persisted file name for a phase
// using the phase's position WITHIN ITS TEMPLATE (the single source of truth) for
// the numeric ordering prefix, and the descriptive stem from the flat map.
//
// This is the mechanism fix for the flat knownPhaseFileNames map's inability to
// order phases correctly: a phase ID that appears at different positions in
// different templates (e.g. solution_design at index 1 in product_design and
// index 2 in project_proposal) gets the correct, monotonically-increasing prefix
// in each, and a template phase missing from the flat map still gets a numbered
// prefix instead of sorting unpredictably. When the template/registry is
// unavailable, it falls back to the flat-map name.
func workflowPhaseFileNameForTemplate(tmpl *workflow.TemplateSpec, phaseID string) string {
	if tmpl == nil {
		return workflowPhaseFileName(phaseID)
	}
	canonical := canonicalWorkflowPhaseID(phaseID)
	metas := workflow.PhaseMetadata(tmpl)
	for _, meta := range metas {
		if meta.ID == canonical {
			stem := workflowPhaseFileStem(phaseID)
			if stem == "" || stem == "workflow-phase" {
				stem = sanitizeWorkflowPhaseFileStem(canonical)
			}
			if stem == "" {
				stem = "workflow-phase"
			}
			return fmt.Sprintf("%02d-%s.md", meta.Index+1, stem)
		}
	}
	// Phase not part of this template (legacy/out-of-template id) — fall back.
	return workflowPhaseFileName(phaseID)
}

// parsePositiveInt parses a non-negative base-10 integer, rejecting empty input
// and any non-digit character (so "01" parses but "1a" does not).
func parsePositiveInt(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-digit")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func workflowPhaseKindFileName(phase workflowPhaseKind) string {
	switch phase {
	case workflowPhaseKind(workflowPhaseRequirements):
		return "01-requirements.md"
	case workflowPhaseKind(workflowPhaseDesign):
		return "02-technical-design.md"
	case workflowPhaseKind(workflowPhaseTasks):
		return "03-task-breakdown.md"
	default:
		return ""
	}
}

func workflowPhaseFileNameWithExt(phaseID, ext string) string {
	fileName := workflowPhaseFileName(phaseID)
	return workflowPhaseFileNameApplyExt(fileName, ext)
}

func workflowPhaseKindFileNameWithExt(phase workflowPhaseKind, ext string) string {
	fileName := workflowPhaseKindFileName(phase)
	if fileName == "" {
		fileName = workflowPhaseFileName(phase.String())
	}
	return workflowPhaseFileNameApplyExt(fileName, ext)
}

func workflowPhaseFileNameApplyExt(fileName, ext string) string {
	ext = stableWorkflowFileExt(ext)
	if ext == "" {
		return fileName
	}
	return strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ext
}

func stableWorkflowFileExt(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		return ""
	}
	for _, r := range ext {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return "." + ext
}

// sanitizeWorkflowPhaseFileStem produces a file stem containing only [a-z0-9-],
// with no leading/trailing/consecutive hyphens. Each run of non-[a-z0-9]
// characters in the input is replaced with a single hyphen.
// Returns empty string for inputs that produce no valid characters.
func sanitizeWorkflowPhaseFileStem(input string) string {
	lower := strings.ToLower(input)
	// Replace each run of non-[a-z0-9] characters with a single hyphen.
	var buf strings.Builder
	inRun := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if inRun && buf.Len() > 0 {
				buf.WriteByte('-')
			}
			inRun = false
			buf.WriteRune(r)
		} else {
			inRun = true
		}
	}
	result := buf.String()
	// Strip leading/trailing hyphens (shouldn't happen with above logic, but defensive).
	result = strings.Trim(result, "-")
	return result
}
