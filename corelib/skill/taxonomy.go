package skill

// taxonomy.go classifies skills by their structural characteristics and
// generates type-appropriate LLM context injection text.
//
// Classification is based on the skill's intrinsic structure (steps, content),
// not on mutable runtime state (solidification). A skill that was originally
// craft_tool and got solidified to bash is still classified as craftable —
// because its FallbackStep reveals its true nature. This prevents injection
// strategy from oscillating when solidification promotes/reverts steps.

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SkillTaxonomy describes a skill's structural category.
type SkillTaxonomy string

const (
	// TaxonomyExecutable: has steps with concrete actions (bash, call_mcp_tool, etc.).
	// The Runner executes these — LLM only needs parameter schema + step summary.
	TaxonomyExecutable SkillTaxonomy = "executable"

	// TaxonomyKnowledge: no steps, only documentation. LLM needs full document.
	TaxonomyKnowledge SkillTaxonomy = "knowledge"

	// TaxonomyCraftable: has (or had) craft_tool steps. LLM needs task template
	// + parameter schema + truncated documentation.
	TaxonomyCraftable SkillTaxonomy = "craftable"
)

// ClassifySkill determines a skill's taxonomy from its structure.
// Classification is purely structural — no keyword matching, no content analysis.
//
// Importantly, this checks both current steps AND FallbackStep to determine
// the skill's intrinsic nature. A solidified skill (craft_tool → bash with
// FallbackStep) is still classified as craftable, because:
//   1. It may revert to craft_tool at any time
//   2. Its documentation context needs are the same as before solidification
//   3. Oscillating between craftable/executable injection would confuse the LLM
func ClassifySkill(skill *corelib.NLSkillEntry) SkillTaxonomy {
	if len(skill.Steps) == 0 {
		return TaxonomyKnowledge
	}
	for _, step := range skill.Steps {
		if step.Action == "craft_tool" {
			return TaxonomyCraftable
		}
		// A solidified step has FallbackStep pointing to the original craft_tool.
		// The skill's intrinsic nature is craftable, not executable.
		if step.FallbackStep != nil && step.FallbackStep.Action == "craft_tool" {
			return TaxonomyCraftable
		}
	}
	return TaxonomyExecutable
}

// FormatSkillForContext generates the LLM context injection text for a skill,
// using a type-appropriate strategy:
//
//   - Executable: parameter schema + step summary (compact, ~50-200 tokens)
//   - Knowledge:  full documentation content (existing behavior)
//   - Craftable:  description + parameter schema + truncated documentation
func FormatSkillForContext(skill *corelib.NLSkillEntry, taxonomy SkillTaxonomy, docContent string) string {
	switch taxonomy {
	case TaxonomyExecutable:
		return formatExecutableContext(skill)
	case TaxonomyKnowledge:
		return formatKnowledgeContext(skill, docContent)
	case TaxonomyCraftable:
		return formatCraftableContext(skill, docContent)
	default:
		return formatKnowledgeContext(skill, docContent)
	}
}

func formatExecutableContext(skill *corelib.NLSkillEntry) string {
	var b strings.Builder
	b.WriteString("### Skill: ")
	b.WriteString(skill.Name)
	b.WriteString("\n")

	if skill.Description != "" {
		b.WriteString(skill.Description)
		b.WriteString("\n")
	}

	params := CompleteParamsForRunner(skill.Params, skill.Steps, skill.RequiredArgs)
	if schema := FormatParamSchema(params); schema != "" {
		b.WriteString(schema)
	}

	if len(skill.Steps) > 0 {
		b.WriteString("执行步骤:\n")
		for i, step := range skill.Steps {
			b.WriteString(fmt.Sprintf("  %d. %s", i+1, step.Action))
			if step.Name != "" {
				b.WriteString(": ")
				b.WriteString(step.Name)
			} else if step.Label != "" {
				b.WriteString(" [")
				b.WriteString(step.Label)
				b.WriteString("]")
			}
			b.WriteString("\n")
		}
	}

	if skill.Mode == "api_workflow" && len(skill.Operations) > 0 {
		b.WriteString("操作:\n")
		for _, op := range skill.Operations {
			b.WriteString(fmt.Sprintf("  - %s: %s\n", op.Name, op.Description))
		}
	}

	return b.String()
}

func formatKnowledgeContext(skill *corelib.NLSkillEntry, docContent string) string {
	var b strings.Builder
	b.WriteString("### Skill: ")
	b.WriteString(skill.Name)
	b.WriteString("\n")

	if skill.Description != "" {
		b.WriteString(skill.Description)
		b.WriteString("\n")
	}

	content := docContent
	if content == "" {
		content = skill.Content
	}
	if content != "" {
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n")
	}

	return b.String()
}

func formatCraftableContext(skill *corelib.NLSkillEntry, docContent string) string {
	var b strings.Builder
	b.WriteString("### Skill: ")
	b.WriteString(skill.Name)
	b.WriteString("\n")

	if skill.Description != "" {
		b.WriteString(skill.Description)
		b.WriteString("\n")
	}

	params := CompleteParamsForRunner(skill.Params, skill.Steps, skill.RequiredArgs)
	if schema := FormatParamSchema(params); schema != "" {
		b.WriteString(schema)
	}

	content := docContent
	if content == "" {
		content = skill.Content
	}
	if content != "" {
		truncated := truncateRunes(content, 500)
		if truncated != content {
			truncated += "\n... (文档已截断，完整内容在 SKILL.md 中)"
		}
		b.WriteString("\n")
		b.WriteString(truncated)
		b.WriteString("\n")
	}

	return b.String()
}

// truncateRunes truncates a string to at most maxRunes runes, preferring
// to break at a paragraph boundary (\n\n) or line boundary (\n).
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	candidate := string(runes[:maxRunes])
	if idx := strings.LastIndex(candidate, "\n\n"); idx > maxRunes/2 {
		return candidate[:idx]
	}
	if idx := strings.LastIndex(candidate, "\n"); idx > maxRunes/2 {
		return candidate[:idx]
	}
	return candidate
}
