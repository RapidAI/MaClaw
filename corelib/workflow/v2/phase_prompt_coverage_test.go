package v2

import (
	"strings"
	"testing"
)

// TestAllTemplatePhases_HaveNonDefaultInstruction verifies that every phase of
// every registered template produces a non-empty, non-default phaseInstruction.
// This catches regressions when new templates are added without corresponding
// phase instruction cases.
//
// Templates with specialized execution modes (maintenance, review templates)
// are excluded — they use SubAgent/ExecMode paths that don't rely on phaseInstruction.
func TestAllTemplatePhases_HaveNonDefaultInstruction(t *testing.T) {
	registry := NewTemplateRegistry()
	RegisterBuiltinTemplates(registry)

	// The default instruction starts with this string — we want to ensure
	// each phase has its OWN instruction, not the generic fallback.
	const defaultPrefix = "请基于前序阶段的产出物和用户需求，生成本阶段的完整文档内容"

	// Templates with specialized execution that don't use phaseInstruction for all phases.
	skipTemplates := map[string]bool{
		"maintenance":                    true, // SubAgent-driven
		"ops_maintenance":                true, // specialized ops flow
		"changjiang_scholar_review":      true, // review templates use parametric instructions
		"nsfc_distinguished_youth_review": true,
		"nsfc_excellent_youth_review":    true,
		"nsfc_youth_review":              true,
		"nsfc_general_review":            true,
		"nsfc_key_review":                true,
	}

	for _, tmpl := range registry.templates {
		if skipTemplates[tmpl.Type] {
			continue
		}
		t.Run(tmpl.Type, func(t *testing.T) {
			for _, phase := range tmpl.Phases {
				instruction := phaseInstruction(WorkflowType(tmpl.Type), phase.ID)
				if strings.TrimSpace(instruction) == "" {
					t.Errorf("phase %q has empty instruction", phase.ID)
					continue
				}
				if strings.Contains(instruction, defaultPrefix) {
					t.Errorf("phase %q uses the generic default instruction — add a specific case in phaseInstruction()", phase.ID)
				}
			}
		})
	}
}

// TestFinalPhases_HaveMinLengthRequirement verifies that the last phase of each
// doc-only template (the final deliverable) includes a minimum length constraint
// or "final deliverable" marker in its instruction. This ensures the NeedsConfirm
// gate won't prematurely finalize on short LLM output.
//
// Templates with artifact generation (PPT, patent .docx), specialized execution,
// or parametric instructions (academic) are excluded.
func TestFinalPhases_HaveMinLengthRequirement(t *testing.T) {
	registry := NewTemplateRegistry()
	RegisterBuiltinTemplates(registry)

	// Templates where the final phase has specialized delivery (not plain doc)
	skipTemplates := map[string]bool{
		"coding":                          true,
		"maintenance":                     true,
		"ops_maintenance":                 true,
		"testing":                         true,
		"paper_reproduction":              true,
		"presentation_design":             true, // generates .pptx artifact
		"patent_application":              true, // generates .docx files
		"us_patent_application":           true, // generates .docx files
		"gaokao_application":              true, // specialized plan format
		"changjiang_scholar":              true, // parametric academic
		"nsfc_distinguished_youth":        true,
		"nsfc_excellent_youth":            true,
		"nsfc_youth":                      true,
		"nsfc_general":                    true,
		"nsfc_key":                        true,
		"changjiang_scholar_review":       true,
		"nsfc_distinguished_youth_review": true,
		"nsfc_excellent_youth_review":     true,
		"nsfc_youth_review":               true,
		"nsfc_general_review":             true,
		"nsfc_key_review":                 true,
	}

	for _, tmpl := range registry.templates {
		if skipTemplates[tmpl.Type] {
			continue
		}
		if len(tmpl.Phases) == 0 {
			continue
		}
		lastPhase := tmpl.Phases[len(tmpl.Phases)-1]
		t.Run(tmpl.Type+"/"+lastPhase.ID, func(t *testing.T) {
			instruction := phaseInstruction(WorkflowType(tmpl.Type), lastPhase.ID)
			lower := strings.ToLower(instruction)
			// Check for minimum length indicators or final deliverable markers
			hasLengthReq := strings.Contains(lower, "不少于") ||
				strings.Contains(lower, "至少") ||
				strings.Contains(lower, "minimum") ||
				strings.Contains(instruction, "最终交付物") ||
				strings.Contains(instruction, "这是最终交付物") ||
				strings.Contains(instruction, "完整") // "完整综述"/"完整报告" etc.
			if !hasLengthReq {
				t.Errorf("final phase %q instruction lacks minimum length requirement or final deliverable marker", lastPhase.ID)
			}
		})
	}
}
