package skill

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// MaintenanceExperienceHint is a serializable high-value curator recommendation
// for experience-learning snapshots and UI surfaces.
type MaintenanceExperienceHint struct {
	Action            string   `json:"action"`
	Skill             string   `json:"skill"`
	RelatedSkill      string   `json:"related_skill,omitempty"`
	Risk              string   `json:"risk,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	RecommendedAction string   `json:"recommended_action,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	HighValue         bool     `json:"high_value"`
}

// CollectHighValueMaintenanceHints builds a bounded list of high-value curator
// recommendations from the current skill library (read-only).
func CollectHighValueMaintenanceHints(skills []corelib.NLSkillEntry, max int) []MaintenanceExperienceHint {
	if max <= 0 {
		max = 12
	}
	if len(skills) == 0 {
		return []MaintenanceExperienceHint{}
	}
	plan := BuildSkillMaintenancePlan(skills, SkillMaintenancePlanOptions{MaxActions: max * 3})
	high := FilterHighValueMaintenanceActions(plan.Actions)
	out := make([]MaintenanceExperienceHint, 0, len(high))
	for _, action := range high {
		if len(out) >= max {
			break
		}
		out = append(out, MaintenanceExperienceHint{
			Action:            action.Action,
			Skill:             action.Skill,
			RelatedSkill:      action.RelatedSkill,
			Risk:              action.Risk,
			Reason:            action.Reason,
			RecommendedAction: action.RecommendedAction,
			Evidence:          append([]string(nil), action.Evidence...),
			HighValue:         true,
		})
	}
	return out
}

// IsHighValueMaintenanceAction reports whether a curator action is worth
// surfacing in experience learning / next-turn prompts (not every low-noise refresh).
func IsHighValueMaintenanceAction(action SkillMaintenanceAction) bool {
	switch strings.TrimSpace(action.Action) {
	case MaintenanceActionAttemptRepair,
		MaintenanceActionMarkNeedsReview,
		MaintenanceActionImproveContract,
		MaintenanceActionMergeDuplicate:
		return true
	case MaintenanceActionArchiveStale:
		return action.Risk == MaintenanceRiskMedium
	default:
		return false
	}
}

// FilterHighValueMaintenanceActions keeps only high-value plan rows, preserving order.
func FilterHighValueMaintenanceActions(actions []SkillMaintenanceAction) []SkillMaintenanceAction {
	out := make([]SkillMaintenanceAction, 0, len(actions))
	for _, action := range actions {
		if IsHighValueMaintenanceAction(action) {
			out = append(out, action)
		}
	}
	return out
}

// BuildHighValueMaintenanceExperienceContent formats high-value curator
// recommendations for durable experience/memory storage.
func BuildHighValueMaintenanceExperienceContent(plan SkillMaintenancePlan, max int) string {
	if max <= 0 {
		max = 10
	}
	high := FilterHighValueMaintenanceActions(plan.Actions)
	if len(high) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Skill 高价值治理建议（experience learning）\n\n")
	b.WriteString("生成时间: ")
	b.WriteString(plan.GeneratedAt)
	b.WriteString("\n")
	b.WriteString(plan.Summary)
	b.WriteString("\n\n")
	b.WriteString("以下建议来自本地只读 Curator，不自动改技能。优先用 manage_skill(action=maintenance_drafts) 或 GUI 草案人审处理。\n\n")
	for i, action := range high {
		if i >= max {
			b.WriteString(fmt.Sprintf("\n... 另有 %d 条高价值建议\n", len(high)-max))
			break
		}
		b.WriteString(fmt.Sprintf("## %d. %s → %s\n", i+1, action.Skill, action.Action))
		if action.Risk != "" {
			b.WriteString("- 风险: ")
			b.WriteString(action.Risk)
			b.WriteString("\n")
		}
		if action.Reason != "" {
			b.WriteString("- 原因: ")
			b.WriteString(action.Reason)
			b.WriteString("\n")
		}
		if action.RecommendedAction != "" {
			b.WriteString("- 建议: ")
			b.WriteString(action.RecommendedAction)
			b.WriteString("\n")
		}
		if len(action.Evidence) > 0 {
			b.WriteString("- 证据: ")
			b.WriteString(strings.Join(action.Evidence, "; "))
			b.WriteString("\n")
		}
		if action.RelatedSkill != "" {
			b.WriteString("- 相关技能: ")
			b.WriteString(action.RelatedSkill)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

// BuildMaintenanceExperiencePromptSection injects a short high-value curator
// section for the next LLM turn. Empty when there is nothing actionable.
func BuildMaintenanceExperiencePromptSection(skills []corelib.NLSkillEntry, max int) string {
	if max <= 0 {
		max = 5
	}
	if len(skills) == 0 {
		return ""
	}
	plan := BuildSkillMaintenancePlan(skills, SkillMaintenancePlanOptions{MaxActions: max * 3})
	high := FilterHighValueMaintenanceActions(plan.Actions)
	if len(high) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 技能治理提示（只读，需人审）\n")
	b.WriteString("以下为本地 Curator 的高价值建议，不要自动执行写盘；优先 maintenance_drafts / 人审 apply。\n")
	for i, action := range high {
		if i >= max {
			break
		}
		line := fmt.Sprintf("- [%s] %s：%s", action.Action, action.Skill, firstNonEmptyMaintenanceString(action.RecommendedAction, action.Reason))
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// IngestHighValueMaintenanceExperience records soft failure/review signals into
// UsageTracker so DistillRoutingHints / SkillMemory can prefer avoiding broken skills.
// Returns the number of experiences written.
//
// Only attempt_repair / mark_needs_review become Success=false records; contract/merge
// stay prompt/memory-only to avoid over-suppressing usable skills.
func IngestHighValueMaintenanceExperience(tracker *tool.UsageTracker, skills []corelib.NLSkillEntry, opts SkillMaintenancePlanOptions) int {
	if tracker == nil || len(skills) == 0 {
		return 0
	}
	plan := BuildSkillMaintenancePlan(skills, opts)
	written := 0
	for _, action := range FilterHighValueMaintenanceActions(plan.Actions) {
		if action.Action != MaintenanceActionAttemptRepair && action.Action != MaintenanceActionMarkNeedsReview {
			continue
		}
		name := strings.TrimSpace(action.Skill)
		if name == "" {
			continue
		}
		errorClass := ""
		for _, ev := range action.Evidence {
			if strings.HasPrefix(ev, "error_class=") {
				errorClass = strings.TrimPrefix(ev, "error_class=")
				break
			}
		}
		tokens := maintenanceExperienceQueryTokens(action)
		tracker.RecordExperience(tool.ToolExperience{
			ToolName:     "skill:" + name,
			QueryTokens:  tokens,
			Success:      false,
			FollowUp:     "review",
			TaskType:     "skill_maintenance",
			ErrorClass:   errorClass,
			FinalOutcome: action.Action,
		})
		written++
	}
	return written
}

func maintenanceExperienceQueryTokens(action SkillMaintenanceAction) []string {
	parts := []string{action.Skill, action.Action, action.Reason, action.RelatedSkill}
	parts = append(parts, action.Evidence...)
	tokMap := skillExperienceTokens(strings.Join(parts, " "))
	out := make([]string, 0, len(tokMap))
	for tok := range tokMap {
		out = append(out, tok)
	}
	// Stable-ish order for tests: sort not required for tracker, but keep deterministic.
	// skillExperienceTokens map iteration is random; cap size.
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}
