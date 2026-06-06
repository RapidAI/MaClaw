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
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
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
