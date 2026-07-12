package doctor

import (
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// AdaptivePromptCheck reports process + durable adaptive system-prompt stats.
// Info-only (never blocks readiness): helps operators see light-path hit rate
// and estimated system-prompt token savings.
func AdaptivePromptCheck() Check {
	st := agent.GetPromptProfileStats()
	envRaw := strings.TrimSpace(os.Getenv(agent.PromptProfileEnvKey))
	forced, forceOK := agent.EnvPromptProfileOverride()
	detail := map[string]any{
		"light_turns":          st.LightTurns,
		"full_turns":           st.FullTurns,
		"light_percent":        st.LightPercent,
		"est_tokens_saved":     st.EstTokensSaved,
		"last_profile":         st.LastProfile,
		"last_at":              st.LastAt,
		"last_full_tokens":     st.LastFullTokens,
		"last_light_tokens":    st.LastLightTokens,
		"last_saved_tokens":    st.LastSavedTokens,
		"last_task":            st.LastTask,
		"last_reason":          st.LastReason,
		"light_tool_denies":    st.LightToolDenies,
		"last_denied_tool":     st.LastDeniedTool,
		"light_upgrades":       st.LightUpgrades,
		"last_upgrade_reason":  st.LastUpgradeReason,
		"ab_eligible_light":    st.AbEligibleLight,
		"ab_sample_full":       st.AbSampleFull,
		"ab_sample_percent":    st.AbSamplePercent,
		"upgrade_rate_percent": st.UpgradeRatePercent,
		"deny_rate_percent":    st.DenyRatePercent,
		"stats_path":           agent.PromptProfileStatsPath(),
		"env":                  envRaw,
		"env_override":         forceOK,
	}
	if len(st.ByTask) > 0 {
		detail["by_task"] = st.ByTask
	}
	if len(st.ByDeniedTool) > 0 {
		detail["by_denied_tool"] = st.ByDeniedTool
	}
	if forceOK {
		detail["forced_profile"] = string(forced)
	}
	total := st.LightTurns + st.FullTurns
	hint := "Reset: maclaw-cli shared-loop stats-reset; force: MACLAW_PROMPT_PROFILE=light|full; quality A/B: MACLAW_PROMPT_AB_PERCENT=0..100"
	if total == 0 {
		msg := "adaptive prompt: no turns recorded yet (GUI/TUI/agentservice record light vs full system-prompt profile)"
		if forceOK {
			msg = fmt.Sprintf("adaptive prompt: no turns yet; %s=%s forces profile", agent.PromptProfileEnvKey, forced)
		}
		return Check{
			ID:      "agent.adaptive_prompt",
			Status:  StatusInfo,
			Message: msg,
			Hint:    "After chatting, re-run doctor; or: maclaw-cli shared-loop stats",
			Detail:  detail,
		}
	}
	msg := fmt.Sprintf("adaptive prompt light=%d%% (%d/%d)", st.LightPercent, st.LightTurns, total)
	if st.EstTokensSaved > 0 {
		msg += fmt.Sprintf("; est_saved=%d system-prompt tokens (shadow dual-build)", st.EstTokensSaved)
	}
	if st.LastProfile != "" {
		msg += "; last=" + st.LastProfile
		if st.LastSavedTokens > 0 && st.LastProfile == string(agent.PromptProfileLight) {
			msg += fmt.Sprintf("(-%d)", st.LastSavedTokens)
		}
	}
	if st.LastTask != "" {
		msg += "; task=" + st.LastTask
	}
	if st.LightToolDenies > 0 {
		msg += fmt.Sprintf("; light_deny=%d", st.LightToolDenies)
		if summary := formatDeniedToolTop(st.ByDeniedTool, st.LastDeniedTool); summary != "" {
			msg += "(" + summary + ")"
		}
	}
	if st.LightUpgrades > 0 {
		msg += fmt.Sprintf("; light_upgrade=%d", st.LightUpgrades)
		if r := compactDoctorUpgradeReason(st.LastUpgradeReason); r != "" {
			msg += "(" + r + ")"
		}
	}
	if st.AbEligibleLight > 0 {
		msg += fmt.Sprintf("; ab=%d/%d", st.AbSampleFull, st.AbEligibleLight)
	}
	if st.UpgradeRatePercent > 0 {
		msg += fmt.Sprintf("; upgrade_rate=%d%%", st.UpgradeRatePercent)
	}
	if st.DenyRatePercent > 0 {
		msg += fmt.Sprintf("; deny_rate=%d%%", st.DenyRatePercent)
	}
	if forceOK {
		msg += fmt.Sprintf("; env %s=%s locks profile", agent.PromptProfileEnvKey, forced)
	}
	if pct := agent.PromptABSamplePercent(); pct > 0 {
		msg += fmt.Sprintf("; ab_pct=%d", pct)
	}
	return Check{
		ID:      "agent.adaptive_prompt",
		Status:  StatusInfo,
		Message: msg,
		Hint:    hint,
		Detail:  detail,
	}
}

// FormatDeniedToolTop returns "bash:2" or "bash:2+1tools" for doctor one-liners.
// Exported so GUI shared_loop_stats can share the same compact shape.
func FormatDeniedToolTop(by map[string]int64, last string) string {
	return formatDeniedToolTop(by, last)
}

// formatDeniedToolTop returns "bash:2" or "bash:2+1tools" for doctor one-liners.
func formatDeniedToolTop(by map[string]int64, last string) string {
	if len(by) == 0 {
		return strings.TrimSpace(last)
	}
	topK, topV := "", int64(0)
	distinct := 0
	for k, v := range by {
		k = strings.TrimSpace(k)
		if k == "" || v <= 0 {
			continue
		}
		distinct++
		if v > topV || (v == topV && (topK == "" || k < topK)) {
			topK, topV = k, v
		}
	}
	if topK == "" {
		return strings.TrimSpace(last)
	}
	if distinct <= 1 {
		return fmt.Sprintf("%s:%d", topK, topV)
	}
	return fmt.Sprintf("%s:%d+%dtools", topK, topV, distinct-1)
}

// CompactUpgradeReason shortens upgrade reasons for operator lines
// (tool_deny_retry:bash → bash). maxLen<=0 uses 20.
func CompactUpgradeReason(reason string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 20
	}
	r := strings.TrimSpace(reason)
	if r == "" {
		return ""
	}
	const prefix = "tool_deny_retry:"
	if strings.HasPrefix(r, prefix) {
		r = strings.TrimSpace(strings.TrimPrefix(r, prefix))
	}
	if len(r) > maxLen {
		return r[:maxLen] + "…"
	}
	return r
}

func compactDoctorUpgradeReason(reason string) string {
	return CompactUpgradeReason(reason, 20)
}
