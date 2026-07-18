package main

import "github.com/RapidAI/CodeClaw/corelib/tool"

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
		// needs_review / disabled / needs_setup must not enter BM25 skill routing
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
