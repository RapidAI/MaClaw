package main

import (
	"fmt"
	"sort"
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func (h *IMMessageHandler) toolListSkills() string {
	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	skills := exec.List()

	var b strings.Builder

	// Show local skills grouped by namespace (Requirement 5.4).
	if len(skills) > 0 {
		// Group skills by publisher namespace.
		type nsGroup struct {
			publisher string
			skills    []NLSkillDefinition
		}
		groupMap := make(map[string]*nsGroup)
		var groupOrder []string
		for _, s := range skills {
			key := s.Publisher
			if key == "" {
				key = "__local__"
			}
			if _, ok := groupMap[key]; !ok {
				groupMap[key] = &nsGroup{publisher: s.Publisher}
				groupOrder = append(groupOrder, key)
			}
			groupMap[key].skills = append(groupMap[key].skills, s)
		}

		// Sort: local skills first, then namespaced groups alphabetically.
		sort.SliceStable(groupOrder, func(i, j int) bool {
			if groupOrder[i] == "__local__" {
				return true
			}
			if groupOrder[j] == "__local__" {
				return false
			}
			return groupOrder[i] < groupOrder[j]
		})

		b.WriteString("=== 本地已注册 Skill ===\n")
		for _, key := range groupOrder {
			g := groupMap[key]
			if key == "__local__" {
				b.WriteString("\n[Local]\n")
			} else {
				b.WriteString(fmt.Sprintf("\n[%s]\n", g.publisher))
			}
			for _, s := range g.skills {
				line := fmt.Sprintf("- %s", s.Name)
				// Show skill_id (publisher.name) when available for precise referencing.
				if s.SkillID != "" {
					line = fmt.Sprintf("- %s (id: %s)", s.Name, s.SkillID)
				} else if s.Publisher != "" {
					// Show qualified name for namespaced skills (legacy format).
					line = fmt.Sprintf("- %s:%s", s.Publisher, s.Name)
				}
				// Show directory name alias if different from display name.
				if s.DirName != "" && s.DirName != s.Name {
					line += fmt.Sprintf(" (alias: %s)", s.DirName)
				}
				// Show [knowledge] type indicator for knowledge skills.
				if normalizeSkillTypeKind(s.Type).IsKnowledge() {
					line += " [knowledge]"
				}
				line += fmt.Sprintf(" [%s]: %s", s.Status, s.Description)
				switch normalizeSkillEntrySource(s.Source) {
				case skillEntrySourceHub:
					line += fmt.Sprintf(" (来源: Hub, trust: %s)", s.TrustLevel)
				case skillEntrySourceFile:
					line += " (来源: 本地文件)"
				}
				if s.UsageCount > 0 {
					successRate := float64(s.SuccessCount) / float64(s.UsageCount) * 100
					line += fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, successRate)
					// Flag skills needing improvement: failure rate > 30% with at least 10 usages.
					if s.UsageCount >= 10 {
						failureRate := float64(s.FailureCount) / float64(s.UsageCount)
						if failureRate > 0.30 {
							line += " [needs_improvement]"
						}
					}
				}
				if s.LastError != "" {
					line += fmt.Sprintf(" (最近错误: %s)", s.LastError)
				}
				if labels := skillHealthLabels(s); len(labels) > 0 {
					line += " " + strings.Join(labels, " ")
				}
				b.WriteString(line + "\n")
			}
		}
	} else {
		b.WriteString("本地没有已注册的 Skill。\n")
	}

	// If local skills are empty or few, also show Hub recommendations.
	if len(skills) < 3 && h.getSkillHubClient() != nil {
		recs := h.getSkillHubClient().GetRecommendations()
		if len(recs) > 0 {
			b.WriteString("\n=== SkillHub 推荐 Skill（可用 install_skill_hub 安装）===\n")
			for _, r := range recs {
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (trust: %s, downloads: %d, hub: %s)\n",
					r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, r.HubURL))
			}
		} else {
			b.WriteString("\n提示：可以使用 search_skill_hub 工具在 SkillHub 上搜索更多 Skill。\n")
		}
	}

	return b.String()
}

func skillHealthLabels(s NLSkillDefinition) []string {
	labels := make([]string, 0, 3)
	if s.UsageCount >= 3 && s.SuccessCount == 0 {
		labels = append(labels, "[needs_review]")
	} else if s.UsageCount >= 5 && s.SuccessRate >= 0.8 && strings.TrimSpace(s.LastError) == "" {
		labels = append(labels, "[healthy]")
	}
	if skillDefinitionHasIncompleteContract(s) {
		labels = append(labels, "[missing_contract]")
	}
	return labels
}

func skillDefinitionHasIncompleteContract(s NLSkillDefinition) bool {
	return cskill.HasIncompleteSkillContract(s.Type, s.Steps, s.Params, s.RequiredArgs)
}
