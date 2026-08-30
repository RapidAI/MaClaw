package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// skillExecutorProvider adapts SkillExecutor to the tool.SkillProvider interface,
// enabling skill-aware tool routing (BM25 matching + enrichRunSkillDescription).
type skillExecutorProvider struct {
	executor *SkillExecutor
}

func (p *skillExecutorProvider) ListActiveSkills() []tool.SkillSummary {
	if p.executor == nil {
		return nil
	}
	skills := p.executor.List()
	var out []tool.SkillSummary
	for _, s := range skills {
		// Empty/unknown status is runnable (same contract as StartRunForOwner).
		// staged / needs_review / disabled / needs_setup must not enter BM25 skill routing
		// (prevents broken Hub wget/curl skills from crowding out paper_pdf_translator
		// or built-in download_file).
		switch normalizeSkillEntryStatus(s.Status) {
		case skillEntryStatusActive, skillEntryStatusUnknown:
		default:
			continue
		}
		if isShellBrowserAutomationSkill(s) {
			continue
		}
		// MaClaw App-only packages are installable containers, not directly
		// runnable skills. Keeping them out of this index prevents the agent from
		// selecting a container that the runner must correctly reject.
		if skill.IsKnowledgeSkillType(s.Type) || skill.IsInstructionOnlySkillType(s.Type) {
			continue
		}
		if skill.IsAgentGuidedWorkflowSkill(&corelib.NLSkillEntry{Source: s.Source, Steps: s.Steps, SkillDir: s.SkillDir}) {
			continue
		}
		if strings.TrimSpace(s.Description) == "" && len(s.Triggers) == 0 {
			continue
		}
		out = append(out, tool.SkillSummary{
			Name:                s.Name,
			Triggers:            s.Triggers,
			Description:         s.Description,
			Capabilities:        s.Capabilities,
			RequiresTools:       s.RequiresTools,
			FallbackForTools:    s.FallbackForTools,
			RequiresToolsets:    s.RequiresToolsets,
			FallbackForToolsets: s.FallbackForToolsets,
		})
	}
	return out
}
