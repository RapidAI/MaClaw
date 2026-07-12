package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// RunDoctor evaluates local readiness using the shared corelib/doctor report.
// This is the GUI/Wails surface for the same checks used by maclaw-cli doctor
// and TUI /doctor. It does not open network connections.
//
// agent.shared_loop is included by doctor.Run from config+env. Runtime turn
// counters are exposed separately via GetSharedAgentLoopStatus.
func (a *App) RunDoctor() doctor.Report {
	cfg := corelib.AppConfig{}
	cfgPath := filepath.Join(maclawpath.DefaultBaseDir(), "config.json")
	if a != nil {
		if loaded, err := a.LoadConfig(); err == nil {
			cfg = loaded
		}
	}
	extra := make([]doctor.Check, 0, 1)
	// Process-local posture + counters (not in corelib doctor — runtime only).
	// Always emit so operators see mode/canary even before any chat traffic.
	// Reuse already-loaded cfg — avoid a second LoadConfig via GetSharedAgentLoopStatus.
	st := a.buildSharedAgentLoopStatus(cfg)
	detail := map[string]any{
		"mode":             st.Mode,
		"percent":          st.Percent,
		"workflow_pilot":   st.WorkflowPilot,
		"config_enabled":   st.ConfigEnabled,
		"config_migrated":  st.ConfigMigrated,
		"env_override":     st.EnvOverride,
		"env_locks_mode":   st.EnvLocksMode,
		"shared_turns":     st.SharedTurns,
		"legacy_turns":     st.LegacyTurns,
		"shared_success":   st.SharedSuccess,
		"shared_error":     st.SharedError,
		"shared_cancelled": st.SharedCancelled,
		"skip_canary":      st.SkipCanary,
		"skip_ineligible":  st.SkipIneligible,
		"shadow_eligible":  st.ShadowEligible,
		"last_skip_reason": st.LastSkipReason,
		"last_shared_at":   st.LastSharedAt,
		"last_legacy_at":   st.LastLegacyAt,
		"process_started":  st.ProcessStartedAt,
	}
	if len(st.SkipByReason) > 0 {
		detail["skip_by_reason"] = st.SkipByReason
	}
	msg := fmt.Sprintf("process shared-loop mode=%s", strings.TrimSpace(st.Mode))
	if st.Mode == "" {
		msg = "process shared-loop mode=off"
	}
	if st.Percent > 0 && st.Percent < 100 {
		msg += fmt.Sprintf("; canary %d%% (sticky by user id)", st.Percent)
	}
	if st.WorkflowPilot {
		msg += "; workflow pilot on"
	}
	if st.EnvLocksMode && st.EnvOverride != "" {
		msg += "; env MACLAW_SHARED_AGENT_LOOP locks mode"
	}
	turns := st.SharedTurns + st.LegacyTurns
	if turns > 0 {
		sharedPct := int((st.SharedTurns * 100) / turns)
		msg += fmt.Sprintf("; path shared=%d (%d%%) legacy=%d ok=%d err=%d",
			st.SharedTurns, sharedPct, st.LegacyTurns, st.SharedSuccess, st.SharedError)
		if st.SharedCancelled > 0 {
			msg += fmt.Sprintf(" cancelled=%d", st.SharedCancelled)
		}
	} else {
		msg += "; no path traffic yet"
	}
	if st.SkipCanary+st.SkipIneligible+st.ShadowEligible > 0 {
		msg += fmt.Sprintf("; skip canary=%d ineligible=%d shadow=%d",
			st.SkipCanary, st.SkipIneligible, st.ShadowEligible)
		if r := strings.TrimSpace(st.LastSkipReason); r != "" {
			msg += " last=" + r
		}
	}
	if sum := st.ProcessUsage.Summary(); sum != "" {
		msg += "; usage " + sum
		detail["process_usage"] = st.ProcessUsage
	}
	if sum := st.LastUsage.Summary(); sum != "" {
		detail["last_usage"] = st.LastUsage
		detail["last_usage_summary"] = sum
	}
	if st.LastRoute.Model != "" || st.LastRoute.Task != "" {
		detail["last_route"] = st.LastRoute
	}
	if st.PromptLightTurns+st.PromptFullTurns > 0 {
		detail["prompt_light_turns"] = st.PromptLightTurns
		detail["prompt_full_turns"] = st.PromptFullTurns
		detail["prompt_light_percent"] = st.PromptLightPct
		detail["prompt_est_tokens_saved"] = st.PromptEstTokensSaved
		detail["last_prompt_profile"] = st.LastPromptProfile
		detail["last_prompt_at"] = st.LastPromptAt
		detail["last_prompt_saved_tokens"] = st.LastPromptSaved
		detail["last_prompt_task"] = st.LastPromptTask
		msg += fmt.Sprintf("; adaptive prompt light=%d%% (%d/%d) est_saved=%d tokens",
			st.PromptLightPct, st.PromptLightTurns, st.PromptLightTurns+st.PromptFullTurns, st.PromptEstTokensSaved)
	}
	if st.PromptLightDenies > 0 {
		detail["prompt_light_tool_denies"] = st.PromptLightDenies
		detail["prompt_last_denied_tool"] = st.PromptLastDeniedTool
		if len(st.PromptByDeniedTool) > 0 {
			detail["prompt_by_denied_tool"] = st.PromptByDeniedTool
		}
		msg += fmt.Sprintf("; light_deny=%d", st.PromptLightDenies)
		// Prefer top-N breakdown (bash:2+1tools) over last-only, same as adaptive_prompt check.
		if by := doctor.FormatDeniedToolTop(st.PromptByDeniedTool, st.PromptLastDeniedTool); by != "" {
			msg += "(" + by + ")"
		}
	}
	if st.PromptLightUpgrades > 0 {
		detail["prompt_light_upgrades"] = st.PromptLightUpgrades
		detail["prompt_last_upgrade_reason"] = st.PromptLastUpgrade
		msg += fmt.Sprintf("; light_upgrade=%d", st.PromptLightUpgrades)
		if r := doctor.CompactUpgradeReason(st.PromptLastUpgrade, 24); r != "" {
			msg += "(" + r + ")"
		}
	}
	if len(st.PromptByTask) > 0 {
		detail["prompt_by_task"] = st.PromptByTask
	}
	if st.PromptAbEligibleLight > 0 {
		detail["prompt_ab_eligible_light"] = st.PromptAbEligibleLight
		detail["prompt_ab_sample_full"] = st.PromptAbSampleFull
		msg += fmt.Sprintf("; ab=%d/%d", st.PromptAbSampleFull, st.PromptAbEligibleLight)
	}
	if st.PromptAbSamplePercent > 0 {
		detail["prompt_ab_sample_percent"] = st.PromptAbSamplePercent
		msg += fmt.Sprintf("; ab_pct=%d", st.PromptAbSamplePercent)
	}
	if st.PromptUpgradeRatePct > 0 {
		detail["prompt_upgrade_rate_percent"] = st.PromptUpgradeRatePct
		msg += fmt.Sprintf("; upgrade_rate=%d%%", st.PromptUpgradeRatePct)
	}
	if st.PromptDenyRatePct > 0 {
		detail["prompt_deny_rate_percent"] = st.PromptDenyRatePct
		msg += fmt.Sprintf("; deny_rate=%d%%", st.PromptDenyRatePct)
	}
	if st.PromptProfileForced != "" {
		detail["prompt_profile_forced"] = st.PromptProfileForced
	}
	detail["light_retry_enabled"] = st.LightRetryEnabled
	if !st.LightRetryEnabled {
		msg += "; light_retry=off"
	}
	detail["hub_connected"] = st.HubConnected
	if st.HubURL != "" {
		detail["hub_url"] = st.HubURL
	}
	if st.HubConnected {
		msg += "; hub connected"
		if st.HubAdaptiveSummary != "" {
			detail["hub_adaptive_summary"] = st.HubAdaptiveSummary
			msg += " (heartbeat adaptive_prompt ready)"
		} else {
			msg += " (no adaptive_prompt to report yet)"
		}
		if st.HubCostOpsSummary != "" {
			detail["hub_cost_ops_summary"] = st.HubCostOpsSummary
			msg += " (heartbeat cost_ops ready)"
		}
	} else if st.HubURL != "" {
		msg += "; hub offline"
	}
	if st.ExportDir != "" {
		detail["export_dir"] = st.ExportDir
	}
	if st.ToolCompressProjects > 0 {
		detail["tool_compress_projects"] = st.ToolCompressProjects
		detail["tool_compress_spills"] = st.ToolCompressSpills
		detail["tool_compress_saved_bytes"] = st.ToolCompressSavedBytes
		if len(st.ToolCompressByTool) > 0 {
			detail["tool_compress_by_tool"] = st.ToolCompressByTool
		}
		if st.ToolCompressSavedBytes > 0 {
			msg += fmt.Sprintf("; tool_compress saved=%dB spills=%d",
				st.ToolCompressSavedBytes, st.ToolCompressSpills)
		}
	}
	if st.CostSessionLine != "" || st.CostDailyLine != "" || st.CostFleetLine != "" || st.CostRouteLine != "" {
		detail["cost_session_usd"] = st.CostSessionUSD
		detail["cost_daily_usd"] = st.CostDailyUSD
		if st.CostBudgetUSD > 0 {
			detail["cost_budget_usd"] = st.CostBudgetUSD
		}
		if st.CostFleetLine != "" {
			detail["cost_fleet_usd"] = st.CostFleetUSD
			detail["cost_fleet_calls"] = st.CostFleetCalls
			detail["cost_fleet_instances"] = st.CostFleetInstances
		}
		if st.CostRouteLine != "" {
			detail["cost_route_decisions"] = st.CostRouteDecisions
			detail["cost_route_applied"] = st.CostRouteApplied
			detail["cost_route_shadow"] = st.CostRouteShadow
			if st.CostRouteLastTier != "" {
				detail["cost_route_last_tier"] = st.CostRouteLastTier
			}
			if st.CostRouteLastMode != "" {
				detail["cost_route_last_mode"] = st.CostRouteLastMode
			}
			if len(st.CostRouteByTier) > 0 {
				detail["cost_route_by_tier"] = st.CostRouteByTier
			}
		}
		if st.CostDailyLine != "" {
			msg += "; " + st.CostDailyLine
		} else if st.CostSessionLine != "" {
			msg += "; " + st.CostSessionLine
		}
		if st.CostFleetLine != "" && st.CostFleetLine != st.CostDailyLine {
			msg += "; fleet " + st.CostFleetLine
		}
		if st.CostRouteLine != "" {
			msg += "; " + st.CostRouteLine
		}
		if st.CostOverBudget {
			msg += " OVER_BUDGET"
		}
	}
	if st.DenialPaused || st.DenialConsecutive > 0 {
		detail["denial_paused"] = st.DenialPaused
		detail["denial_consecutive"] = st.DenialConsecutive
		detail["denial_threshold"] = st.DenialThreshold
		if st.DenialLastTool != "" {
			detail["denial_last_tool"] = st.DenialLastTool
		}
		if st.DenialPaused {
			msg += "; DENIAL_PAUSED"
			if st.DenialPauseMessage != "" {
				detail["denial_pause_message"] = st.DenialPauseMessage
			}
		}
	}
	extra = append(extra, doctor.Check{
		ID:      "agent.shared_loop_stats",
		Status:  doctor.StatusInfo,
		Message: msg,
		Detail:  detail,
		Hint:    "Toggle in System Doctor; force mode: MACLAW_SHARED_AGENT_LOOP=on|off|shadow; canary: MACLAW_SHARED_AGENT_LOOP_PERCENT=0..100; fleet: Hub adaptive_prompt heartbeat + export/merge",
	})
	return doctor.Run(doctor.Input{
		Config:      cfg,
		ConfigPath:  cfgPath,
		ExtraChecks: extra,
	})
}

func stringsTrimEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// RunDoctorFormatted returns a human-readable doctor report for debug panels.
func (a *App) RunDoctorFormatted() string {
	return doctor.FormatReport(a.RunDoctor())
}

// GetLastModelRoute returns the most recent turn model-routing decision
// (for Settings / debug panels). Empty when no agent loop has run yet.
func (a *App) GetLastModelRoute() modelRouteDecision {
	if a == nil {
		return modelRouteDecision{}
	}
	a.lastModelRouteMu.RLock()
	defer a.lastModelRouteMu.RUnlock()
	return a.lastModelRoute
}

func (a *App) recordLastModelRoute(d modelRouteDecision) {
	if a == nil {
		return
	}
	a.lastModelRouteMu.Lock()
	a.lastModelRoute = d
	a.lastModelRouteMu.Unlock()
}
