package main

import (
	"path/filepath"
	"strings"
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
	"prd":              "03-prd.md",
	"prototype":        "04-prototype.md",

	// innovation workflow
	"opportunity": "01-opportunity.md",
	"ideation":    "02-ideation.md",
	"validation":  "03-validation.md",
	"roadmap":     "04-roadmap.md",
	"action_plan": "05-action-plan.md",

	// business_plan workflow
	"bp_requirement":    "01-requirement.md",
	"bp_content":        "02-content.md",
	"bp_structure":      "03-structure.md",
	"bp_visual_design":  "04-visual-design.md",
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
	"screening_classification": "03-screening-classification.md",
	"content_extraction":       "04-content-extraction.md",
	"review_writing":           "05-review-writing.md",

	// research_report workflow
	"requirement_scoping": "01-requirement-scoping.md",
	"source_mapping":      "02-source-mapping.md",
	"report_collection":   "03-report-collection.md",
	"insight_extraction":  "04-insight-extraction.md",
	"synthesis_report":    "05-synthesis-report.md",

	// experiment_design workflow
	"hypothesis_formulation": "01-hypothesis.md",
	"experiment_design":      "02-experiment-design.md",
	"variable_control":       "03-variable-control.md",
	"data_collection":        "04-data-collection.md",
	"analysis_plan":          "05-analysis-plan.md",

	// grant_proposal workflow
	"topic_justification": "01-topic-justification.md",
	"research_status":     "02-research-status.md",
	"research_plan":       "03-research-plan.md",
	"expected_outcomes":   "04-expected-outcomes.md",
	"budget_plan":         "05-budget-plan.md",

	// paper_writing workflow
	"paper_outline":        "01-paper-outline.md",
	"literature_basis":     "02-literature-basis.md",
	"methodology":          "03-methodology.md",
	"results_writing":      "04-results.md",
	"paper_polish":         "05-polish.md",
	"outline_design":       "01-paper-outline.md",  // actual template phase ID (pos 1)
	"results_presentation": "03-methodology.md",    // actual template phase ID (pos 3)
	"discussion_analysis":  "04-results.md",        // actual template phase ID (pos 4)
	"submission_prep":      "05-polish.md",         // actual template phase ID (pos 5)

	// project_proposal workflow
	"project_background":  "01-background.md",
	"project_objectives":  "02-objectives.md",
	"project_plan":        "03-plan.md",
	"resource_budget":     "04-resource-budget.md",
	"risk_assessment":     "05-risk-assessment.md",
	"background_analysis": "01-background.md",  // actual template phase ID
	"goal_definition":     "02-objectives.md",  // actual template phase ID
	"resource_assessment": "04-resource-budget.md", // actual template phase ID
	"risk_contingency":    "05-risk-assessment.md", // actual template phase ID

	// event_planning workflow
	"event_objectives":   "01-objectives.md",
	"event_concept":      "02-concept.md",
	"event_logistics":    "03-logistics.md",
	"event_promotion":    "04-promotion.md",
	"event_execution":    "05-execution-plan.md",
	"requirement_confirm": "01-objectives.md",     // actual template phase ID
	"scheme_planning":     "02-concept.md",        // actual template phase ID
	"process_design":      "03-logistics.md",      // actual template phase ID
	"material_checklist":  "04-promotion.md",      // actual template phase ID
	"execution_manual":    "05-execution-plan.md", // actual template phase ID

	// competitive_analysis workflow
	"market_landscape":    "01-market-landscape.md",
	"competitor_profiles": "02-competitor-profiles.md",
	"feature_comparison":  "03-feature-comparison.md",
	"strategy_analysis":   "04-strategy-analysis.md",
	"recommendations":     "05-recommendations.md",

	// presentation_design workflow
	"audience_goal":   "01-audience-goal.md",
	"content_outline": "02-content-outline.md",
	"slide_scripting": "03-slide-scripting.md",
	"visual_design":   "04-visual-design.md",
	"ppt_generation":  "05-ppt-generation.md",

	// ops_maintenance workflow
	"ops_intake":           "01-ops-intake.md",
	"readonly_collection":  "02-readonly-collection.md",
	"artifact_plan":        "03-maintenance-artifacts.md",
	"risk_policy":          "04-risk-policy.md",
	"controlled_execution": "05-controlled-execution.md",

	// bid_response workflow
	"bid_parsing":       "01-bid-parsing.md",
	"qualification":     "02-qualification.md",
	"technical_proposal": "03-technical-proposal.md",
	"commercial_quote":  "04-commercial-quote.md",
	"document_assembly": "05-document-assembly.md",

	// contract_review workflow
	"contract_parsing": "01-contract-parsing.md",
	"clause_risk":      "02-clause-risk.md",
	"compliance_check": "03-compliance-check.md",
	"revision_suggest": "04-revision-suggestions.md",
	"review_opinion":   "05-review-opinion.md",

	// due_diligence workflow
	"company_profile": "01-company-profile.md",
	"business_dd":     "02-business-dd.md",
	"financial_dd":    "03-financial-dd.md",
	"legal_dd":        "04-legal-dd.md",
	"dd_conclusion":   "05-dd-conclusion.md",

	// compliance_audit workflow
	"audit_scope":       "01-audit-scope.md",
	"compliance_assess": "02-compliance-assessment.md",
	"risk_rating":       "03-risk-rating.md",
	"remediation_plan":  "04-remediation-plan.md",
	"audit_report":      "05-audit-report.md",

	// patent_analysis workflow
	"tech_parsing":      "01-tech-parsing.md",
	"prior_art":         "02-prior-art.md",
	"infringement_eval": "03-infringement-eval.md",
	"strategy_suggest":  "04-strategy-suggestions.md",
	"analysis_report":   "05-analysis-report.md",

	// changjiang_scholar workflow
	"scholar_profile":          "01-scholar-profile.md",
	"research_direction":       "02-research-direction.md",
	"achievement_summary":      "03-achievements.md",
	"development_plan":         "04-development-plan.md",
	"application_doc":          "05-application.md",
	"cj_personal_profile":      "01-scholar-profile.md",      // actual template phase ID
	"cj_academic_achievements": "02-research-direction.md",   // actual template phase ID
	"cj_research_plan":         "03-achievements.md",         // actual template phase ID
	"cj_talent_cultivation":    "04-development-plan.md",     // actual template phase ID
	"cj_recommendation_summary": "05-application.md",         // actual template phase ID

	// changjiang_scholar_review workflow
	"review_criteria":         "01-review-criteria.md",
	"material_review":         "02-material-review.md",
	"scoring_evaluation":      "03-scoring.md",
	"comparison_analysis":     "04-comparison.md",
	"review_conclusion":       "05-conclusion.md",
	"cj_completeness_check":   "01-review-criteria.md",  // actual template phase ID
	"cj_achievement_evaluation": "02-material-review.md", // actual template phase ID
	"cj_plan_feasibility":     "03-scoring.md",          // actual template phase ID
	"cj_narrative_quality":    "04-comparison.md",       // actual template phase ID
	"cj_improvement_report":   "05-conclusion.md",      // actual template phase ID
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
