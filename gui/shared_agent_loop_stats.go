package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

// SharedAgentLoopStatus is the runtime snapshot for settings / doctor UIs.
type SharedAgentLoopStatus struct {
	Mode            string `json:"mode"`
	Percent         int    `json:"percent"`
	WorkflowPilot   bool   `json:"workflow_pilot"`
	ConfigEnabled   bool   `json:"config_enabled"`
	ConfigMigrated  bool   `json:"config_migrated"`
	DefaultEnabled  bool   `json:"default_enabled"`
	EnvOverride     string `json:"env_override,omitempty"` // raw MACLAW_SHARED_AGENT_LOOP when set
	EnvLocksMode    bool   `json:"env_locks_mode"`         // true when env overrides config toggle
	SharedTurns     int64  `json:"shared_turns"`
	LegacyTurns     int64  `json:"legacy_turns"`
	SharedSuccess   int64  `json:"shared_success"`
	SharedError     int64  `json:"shared_error"`
	SharedCancelled int64  `json:"shared_cancelled"`
	// SkipCanary / SkipIneligible count turns that stayed on legacy while mode=on
	// (or would-have-been shared while mode=shadow for ShadowEligible).
	SkipCanary     int64              `json:"skip_canary"`
	SkipIneligible int64              `json:"skip_ineligible"`
	ShadowEligible int64              `json:"shadow_eligible"`
	SkipByReason   map[string]int64   `json:"skip_by_reason,omitempty"`
	LastSkipReason string             `json:"last_skip_reason,omitempty"`
	LastSkipAt     string             `json:"last_skip_at,omitempty"`
	LastSharedAt   string             `json:"last_shared_at,omitempty"`
	LastLegacyAt   string             `json:"last_legacy_at,omitempty"`
	LastRoute      modelRouteDecision `json:"last_route,omitempty"`
	// LastUsage is the most recent RunLoop TurnUsage folded into process stats.
	LastUsage agent.TurnUsage `json:"last_usage,omitempty"`
	// ProcessUsage aggregates TurnUsage for this process (shared-path loops that
	// report tokens via accumulateLoopResultUsage).
	ProcessUsage agent.TurnUsage `json:"process_usage,omitempty"`
	// Adaptive prompt profile hit rate (process-local + durable under stats/).
	PromptLightTurns     int64            `json:"prompt_light_turns"`
	PromptFullTurns      int64            `json:"prompt_full_turns"`
	PromptLightPct       int              `json:"prompt_light_percent"`
	PromptEstTokensSaved int64            `json:"prompt_est_tokens_saved"`
	LastPromptProfile    string           `json:"last_prompt_profile,omitempty"`
	LastPromptAt         string           `json:"last_prompt_at,omitempty"`
	LastPromptSaved      int              `json:"last_prompt_saved_tokens,omitempty"`
	LastPromptTask       string           `json:"last_prompt_task,omitempty"`
	LastPromptReason     string           `json:"last_prompt_reason,omitempty"`
	PromptByTask         map[string]int64 `json:"prompt_by_task,omitempty"`
	PromptLightDenies    int64            `json:"prompt_light_tool_denies"`
	PromptLastDeniedTool string           `json:"prompt_last_denied_tool,omitempty"`
	PromptByDeniedTool   map[string]int64 `json:"prompt_by_denied_tool,omitempty"`
	PromptLightUpgrades  int64            `json:"prompt_light_upgrades"`
	PromptLastUpgrade    string           `json:"prompt_last_upgrade_reason,omitempty"`
	// Quality A/B + derived rates (from agent.GetPromptProfileStats).
	PromptAbEligibleLight int64 `json:"prompt_ab_eligible_light,omitempty"`
	PromptAbSampleFull    int64 `json:"prompt_ab_sample_full,omitempty"`
	PromptAbSamplePercent int   `json:"prompt_ab_sample_percent,omitempty"`
	PromptUpgradeRatePct  int   `json:"prompt_upgrade_rate_percent,omitempty"`
	PromptDenyRatePct     int   `json:"prompt_deny_rate_percent,omitempty"`
	// PromptProfileEnv / PromptProfileForced surface MACLAW_PROMPT_PROFILE when set.
	PromptProfileEnv    string `json:"prompt_profile_env,omitempty"`    // raw env value
	PromptProfileForced string `json:"prompt_profile_forced,omitempty"` // light|full when override active
	// PercentFromEnv / WorkflowFromEnv explain whether env overrode config.
	PercentFromEnv  bool `json:"percent_from_env"`
	WorkflowFromEnv bool `json:"workflow_from_env"`
	// ConfigCanaryPercent is the raw config value when set (may differ from Percent).
	ConfigCanaryPercent *int `json:"config_canary_percent,omitempty"`
	// ConfigWorkflow is the raw config shared_agent_loop_workflow flag.
	ConfigWorkflow bool `json:"config_workflow"`
	// LightRetryEnabled mirrors MACLAW_PROMPT_LIGHT_RETRY (default on).
	LightRetryEnabled bool `json:"light_retry_enabled"`
	// Hub connection + the adaptive_prompt snapshot included in machine.heartbeat
	// (fleet path; admin GET /api/admin/adaptive-prompt/metrics aggregates these).
	HubConnected       bool   `json:"hub_connected"`
	HubURL             string `json:"hub_url,omitempty"`
	HubAdaptiveSummary string `json:"hub_adaptive_summary,omitempty"` // empty when no stats to report
	// HubCostOpsSummary is the local cost_ops heartbeat snapshot (what GUI would send to Hub).
	HubCostOpsSummary string `json:"hub_cost_ops_summary,omitempty"`
	// ExportDir is the default directory for portable adaptive-prompt exports.
	ExportDir string `json:"export_dir,omitempty"`
	// Tool compression process stats (Phase 4).
	ToolCompressSavedBytes int64            `json:"tool_compress_saved_bytes,omitempty"`
	ToolCompressSpills     int64            `json:"tool_compress_spills,omitempty"`
	ToolCompressProjects   int64            `json:"tool_compress_projects,omitempty"`
	ToolCompressByTool     map[string]int64 `json:"tool_compress_by_tool,omitempty"`
	// Cost tracker (process session / daily) when OpenHuman cost module is live.
	CostSessionUSD   float64 `json:"cost_session_usd,omitempty"`
	CostDailyUSD     float64 `json:"cost_daily_usd,omitempty"`
	CostBudgetUSD    float64 `json:"cost_budget_usd,omitempty"`
	CostOverBudget   bool    `json:"cost_over_budget,omitempty"`
	CostSessionLine  string  `json:"cost_session_line,omitempty"`
	CostDailyLine    string  `json:"cost_daily_line,omitempty"`
	// Fleet daily sum across host-pid slots in ~/.maclaw/stats/llm_cost_daily.json.
	CostFleetUSD        float64 `json:"cost_fleet_usd,omitempty"`
	CostFleetCalls      int     `json:"cost_fleet_calls,omitempty"`
	CostFleetInstances  int     `json:"cost_fleet_instances,omitempty"`
	CostFleetLine       string  `json:"cost_fleet_line,omitempty"`
	// Cost-route tier stats (~/.maclaw/stats/cost_route.json).
	CostRouteDecisions int64            `json:"cost_route_decisions,omitempty"`
	CostRouteApplied   int64            `json:"cost_route_applied,omitempty"`
	CostRouteShadow    int64            `json:"cost_route_shadow,omitempty"`
	CostRouteLastTier  string           `json:"cost_route_last_tier,omitempty"`
	CostRouteLastMode  string           `json:"cost_route_last_mode,omitempty"`
	CostRouteByTier    map[string]int64 `json:"cost_route_by_tier,omitempty"`
	CostRouteLine      string           `json:"cost_route_line,omitempty"`
	// Denial ledger (Phase 5).
	DenialPaused       bool   `json:"denial_paused,omitempty"`
	DenialConsecutive  int    `json:"denial_consecutive,omitempty"`
	DenialThreshold    int    `json:"denial_threshold,omitempty"`
	DenialLastTool     string `json:"denial_last_tool,omitempty"`
	DenialPauseMessage string `json:"denial_pause_message,omitempty"`
	ProcessStartedAt   string `json:"process_started_at,omitempty"`
}

type sharedAgentLoopCounters struct {
	sharedTurns     atomic.Int64
	legacyTurns     atomic.Int64
	sharedSuccess   atomic.Int64
	sharedError     atomic.Int64
	sharedCancelled atomic.Int64
	skipCanary      atomic.Int64
	skipIneligible  atomic.Int64
	shadowEligible  atomic.Int64

	mu             sync.Mutex
	lastSharedAt   time.Time
	lastLegacyAt   time.Time
	lastSkipAt     time.Time
	lastSkipReason string
	skipByReason   map[string]int64
	lastUsage      agent.TurnUsage
	processUsage   agent.TurnUsage
}

var (
	processSharedLoopStats     sharedAgentLoopCounters
	processSharedLoopStartedAt = time.Now().UTC()
)

func recordSharedAgentLoopTurn(success, cancelled, errored bool) {
	processSharedLoopStats.sharedTurns.Add(1)
	processSharedLoopStats.mu.Lock()
	processSharedLoopStats.lastSharedAt = time.Now().UTC()
	processSharedLoopStats.mu.Unlock()
	switch {
	case cancelled:
		processSharedLoopStats.sharedCancelled.Add(1)
	case errored:
		processSharedLoopStats.sharedError.Add(1)
	case success:
		processSharedLoopStats.sharedSuccess.Add(1)
	}
}

func recordLegacyAgentLoopTurn() {
	processSharedLoopStats.legacyTurns.Add(1)
	processSharedLoopStats.mu.Lock()
	processSharedLoopStats.lastLegacyAt = time.Now().UTC()
	processSharedLoopStats.mu.Unlock()
}

// recordSharedLoopSkip records why a turn stayed on the legacy path while the
// strangler was active (mode on/shadow). reason is a short code from eligibility
// or "canary". kind is "canary" | "ineligible" | "shadow".
func recordSharedLoopSkip(kind, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	switch kind {
	case "canary":
		processSharedLoopStats.skipCanary.Add(1)
	case "shadow":
		processSharedLoopStats.shadowEligible.Add(1)
	default:
		processSharedLoopStats.skipIneligible.Add(1)
	}
	processSharedLoopStats.mu.Lock()
	if processSharedLoopStats.skipByReason == nil {
		processSharedLoopStats.skipByReason = make(map[string]int64)
	}
	key := kind + ":" + reason
	processSharedLoopStats.skipByReason[key]++
	processSharedLoopStats.lastSkipReason = key
	processSharedLoopStats.lastSkipAt = time.Now().UTC()
	processSharedLoopStats.mu.Unlock()
}

// recordLoopUsage folds turn-level token/cost into process-local doctor stats.
func recordLoopUsage(u agent.TurnUsage) {
	if u.Empty() {
		return
	}
	processSharedLoopStats.mu.Lock()
	processSharedLoopStats.lastUsage = u
	processSharedLoopStats.processUsage.Add(u)
	processSharedLoopStats.mu.Unlock()
}

// SetSharedAgentLoopEnabled writes shared_agent_loop_enabled to config.
// Also marks SharedAgentLoopMigrated so the one-time migrator will not override
// an explicit user choice. Env MACLAW_SHARED_AGENT_LOOP still wins at runtime
// when set — the returned status reflects effective mode after save.
func (a *App) SetSharedAgentLoopEnabled(enabled bool) (SharedAgentLoopStatus, error) {
	if a == nil {
		return SharedAgentLoopStatus{}, fmt.Errorf("app not available")
	}
	_, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		// Always ensure migrated so we never re-force enable after opt-out.
		changed := false
		if cfg.SharedAgentLoopEnabled != enabled {
			cfg.SharedAgentLoopEnabled = enabled
			changed = true
		}
		if !cfg.SharedAgentLoopMigrated {
			cfg.SharedAgentLoopMigrated = true
			changed = true
		}
		return changed
	})
	if err != nil {
		return SharedAgentLoopStatus{}, err
	}
	log.Printf("[shared-loop] config shared_agent_loop_enabled=%v (user toggle)", enabled)
	return a.GetSharedAgentLoopStatus(), nil
}

// SetSharedAgentLoopCanaryPercent writes shared_agent_loop_canary_percent (0..100).
// Env MACLAW_SHARED_AGENT_LOOP_PERCENT still wins at runtime when set.
// Also marks SharedAgentLoopMigrated. Wails binding for System Doctor.
func (a *App) SetSharedAgentLoopCanaryPercent(percent int) (SharedAgentLoopStatus, error) {
	if a == nil {
		return SharedAgentLoopStatus{}, fmt.Errorf("app not available")
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	_, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		changed := false
		if cfg.SharedAgentLoopCanaryPercent == nil || *cfg.SharedAgentLoopCanaryPercent != percent {
			p := percent
			cfg.SharedAgentLoopCanaryPercent = &p
			changed = true
		}
		if !cfg.SharedAgentLoopMigrated {
			cfg.SharedAgentLoopMigrated = true
			changed = true
		}
		return changed
	})
	if err != nil {
		return SharedAgentLoopStatus{}, err
	}
	log.Printf("[shared-loop] config shared_agent_loop_canary_percent=%d", percent)
	return a.GetSharedAgentLoopStatus(), nil
}

// SetSharedAgentLoopWorkflow writes shared_agent_loop_workflow (non-doc pilot).
// Env MACLAW_SHARED_AGENT_LOOP_WORKFLOW still wins at runtime when set.
func (a *App) SetSharedAgentLoopWorkflow(enabled bool) (SharedAgentLoopStatus, error) {
	if a == nil {
		return SharedAgentLoopStatus{}, fmt.Errorf("app not available")
	}
	_, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		changed := false
		if cfg.SharedAgentLoopWorkflow != enabled {
			cfg.SharedAgentLoopWorkflow = enabled
			changed = true
		}
		if !cfg.SharedAgentLoopMigrated {
			cfg.SharedAgentLoopMigrated = true
			changed = true
		}
		return changed
	})
	if err != nil {
		return SharedAgentLoopStatus{}, err
	}
	log.Printf("[shared-loop] config shared_agent_loop_workflow=%v", enabled)
	return a.GetSharedAgentLoopStatus(), nil
}

// ResetAdaptivePromptStats clears process-local and durable adaptive prompt
// counters (light/full hit rate + estimated token savings). Wails binding for
// System Doctor.
func (a *App) ResetAdaptivePromptStats() (SharedAgentLoopStatus, error) {
	if err := agent.ResetPromptProfileStats(); err != nil {
		return SharedAgentLoopStatus{}, err
	}
	log.Printf("[shared-loop] adaptive prompt stats reset")
	return a.GetSharedAgentLoopStatus(), nil
}

// ExportAdaptivePromptStats writes a portable adaptive-prompt snapshot under
// ~/.maclaw/stats/exports/ (same shape as `maclaw-cli shared-loop export --write`).
// Wails binding for System Doctor multi-instance fleet collection.
func (a *App) ExportAdaptivePromptStats() (map[string]interface{}, error) {
	exp := agent.BuildPromptProfileExport()
	path := agent.DefaultPromptProfileExportPath()
	if err := agent.WritePromptProfileExport(path, exp); err != nil {
		return nil, err
	}
	exp.SourcePath = path
	log.Printf("[shared-loop] adaptive prompt stats exported to %s", path)
	return map[string]interface{}{
		"ok":               true,
		"action":           "export",
		"path":             path,
		"written":          path,
		"exported_at":      exp.ExportedAt,
		"host":             exp.Host,
		"instance_id":      exp.InstanceID,
		"summary":          exp.Summary,
		"light_turns":      exp.Stats.LightTurns,
		"full_turns":       exp.Stats.FullTurns,
		"est_tokens_saved": exp.Stats.EstTokensSaved,
		"hint":             "Ship this JSON to ops host; aggregate with: maclaw-cli shared-loop merge-exports FILE…",
	}, nil
}

// PreviewSharedLoopCanary reports whether userID is in the sticky canary at the
// current effective percent (or percent override when 0..100 is passed; use -1
// for env>config resolution — same as runtime sharedLoopPercentFor).
// Wails binding for operator tooling / System Doctor canary checks.
func (a *App) PreviewSharedLoopCanary(userID string, percent int) map[string]interface{} {
	cfg := corelib.AppConfig{}
	if a != nil {
		if loaded, err := a.LoadConfig(); err == nil {
			cfg = loaded
		}
	}
	preview := doctor.PreviewSharedLoopCanaryWithConfig(userID, percent, cfg)
	return map[string]interface{}{
		"ok":      true,
		"user_id": preview.UserID,
		"percent": preview.Percent,
		"bucket":  preview.Bucket,
		"allows":  preview.Allows,
		"summary": fmt.Sprintf("canary user=%q percent=%d bucket=%d allows=%v",
			preview.UserID, preview.Percent, preview.Bucket, preview.Allows),
	}
}

// GetSharedAgentLoopStatus returns mode + process-local counters (Wails binding).
func (a *App) GetSharedAgentLoopStatus() SharedAgentLoopStatus {
	cfg := corelib.AppConfig{}
	if a != nil {
		if loaded, err := a.LoadConfig(); err == nil {
			cfg = loaded
		}
	}
	return a.buildSharedAgentLoopStatus(cfg)
}

// buildSharedAgentLoopStatus fills status from an already-loaded config (no extra LoadConfig).
func (a *App) buildSharedAgentLoopStatus(cfg corelib.AppConfig) SharedAgentLoopStatus {
	// Doctor resolution — matches agent.shared_loop check.
	resolved := doctor.ResolveSharedLoopEnv(cfg)
	envRaw := resolved.EnvOverride
	st := SharedAgentLoopStatus{
		Mode:                resolved.Mode,
		Percent:             resolved.Percent,
		WorkflowPilot:       resolved.WorkflowPilot,
		ConfigEnabled:       cfg.SharedAgentLoopEnabled,
		ConfigMigrated:      cfg.SharedAgentLoopMigrated,
		DefaultEnabled:      resolved.DefaultEnabled,
		EnvOverride:         envRaw,
		EnvLocksMode:        envRaw != "",
		PercentFromEnv:      resolved.PercentFromEnv,
		WorkflowFromEnv:     resolved.WorkflowFromEnv,
		ConfigCanaryPercent: cfg.SharedAgentLoopCanaryPercent,
		ConfigWorkflow:      cfg.SharedAgentLoopWorkflow,
		SharedTurns:         processSharedLoopStats.sharedTurns.Load(),
		LegacyTurns:         processSharedLoopStats.legacyTurns.Load(),
		SharedSuccess:       processSharedLoopStats.sharedSuccess.Load(),
		SharedError:         processSharedLoopStats.sharedError.Load(),
		SharedCancelled:     processSharedLoopStats.sharedCancelled.Load(),
		SkipCanary:          processSharedLoopStats.skipCanary.Load(),
		SkipIneligible:      processSharedLoopStats.skipIneligible.Load(),
		ShadowEligible:      processSharedLoopStats.shadowEligible.Load(),
		ProcessStartedAt:    processSharedLoopStartedAt.Format(time.RFC3339),
	}
	if a != nil {
		st.LastRoute = a.GetLastModelRoute()
	}
	processSharedLoopStats.mu.Lock()
	if !processSharedLoopStats.lastSharedAt.IsZero() {
		st.LastSharedAt = processSharedLoopStats.lastSharedAt.Format(time.RFC3339)
	}
	if !processSharedLoopStats.lastLegacyAt.IsZero() {
		st.LastLegacyAt = processSharedLoopStats.lastLegacyAt.Format(time.RFC3339)
	}
	if !processSharedLoopStats.lastSkipAt.IsZero() {
		st.LastSkipAt = processSharedLoopStats.lastSkipAt.Format(time.RFC3339)
	}
	st.LastSkipReason = processSharedLoopStats.lastSkipReason
	if len(processSharedLoopStats.skipByReason) > 0 {
		st.SkipByReason = make(map[string]int64, len(processSharedLoopStats.skipByReason))
		for k, v := range processSharedLoopStats.skipByReason {
			st.SkipByReason[k] = v
		}
	}
	st.LastUsage = processSharedLoopStats.lastUsage
	st.ProcessUsage = processSharedLoopStats.processUsage
	processSharedLoopStats.mu.Unlock()
	ps := agent.GetPromptProfileStats()
	st.PromptLightTurns = ps.LightTurns
	st.PromptFullTurns = ps.FullTurns
	st.PromptLightPct = ps.LightPercent
	st.PromptEstTokensSaved = ps.EstTokensSaved
	st.LastPromptProfile = ps.LastProfile
	st.LastPromptAt = ps.LastAt
	st.LastPromptSaved = ps.LastSavedTokens
	st.LastPromptTask = ps.LastTask
	st.LastPromptReason = ps.LastReason
	st.PromptByTask = ps.ByTask
	st.PromptLightDenies = ps.LightToolDenies
	st.PromptLastDeniedTool = ps.LastDeniedTool
	st.PromptByDeniedTool = ps.ByDeniedTool
	st.PromptLightUpgrades = ps.LightUpgrades
	st.PromptLastUpgrade = ps.LastUpgradeReason
	st.PromptAbEligibleLight = ps.AbEligibleLight
	st.PromptAbSampleFull = ps.AbSampleFull
	st.PromptAbSamplePercent = ps.AbSamplePercent
	st.PromptUpgradeRatePct = ps.UpgradeRatePercent
	st.PromptDenyRatePct = ps.DenyRatePercent
	st.PromptProfileEnv = stringsTrimEnv(agent.PromptProfileEnvKey)
	if forced, ok := agent.EnvPromptProfileOverride(); ok {
		st.PromptProfileForced = string(forced)
	}
	st.LightRetryEnabled = agent.LightToolRetryEnabled()
	st.ExportDir = agent.PromptProfileExportDir()
	if a != nil {
		rs := a.GetRemoteConnectionStatus()
		st.HubConnected = rs.Connected
		st.HubURL = strings.TrimSpace(rs.HubURL)
	}
	if hb := agent.AdaptivePromptHeartbeatStat(); hb != nil {
		st.HubAdaptiveSummary = strings.TrimSpace(hb.Summary)
		if st.HubAdaptiveSummary == "" {
			st.HubAdaptiveSummary = fmt.Sprintf("adaptive-prompt: light %d%% (%d/%d)",
				hb.LightPercent, hb.LightTurns, hb.LightTurns+hb.FullTurns)
		}
	}
	if cop := llm.CostOpsHeartbeatStat(); cop != nil {
		parts := make([]string, 0, 2)
		if s := strings.TrimSpace(cop.RouteSummary); s != "" {
			parts = append(parts, s)
		}
		if s := strings.TrimSpace(cop.DailySummary); s != "" {
			parts = append(parts, s)
		}
		st.HubCostOpsSummary = strings.Join(parts, " | ")
		if st.HubCostOpsSummary == "" {
			st.HubCostOpsSummary = fmt.Sprintf("cost-ops mode=%s decisions=%d daily=$%.4f",
				cop.CostRouteMode, cop.RouteDecisions, cop.DailyCostUSD)
		}
	}
	cs := toolresult.GetCompressionStats()
	st.ToolCompressSavedBytes = cs.SavedBytes
	st.ToolCompressSpills = cs.Spills
	st.ToolCompressProjects = cs.Projects
	st.ToolCompressByTool = cs.ByToolSaved
	st.CostBudgetUSD = cfg.DailyLLMBudgetUSD
	if a != nil && a.ohModules.costTracker != nil {
		ct := a.ohModules.costTracker
		st.CostSessionUSD = ct.SessionCost()
		st.CostDailyUSD = ct.DailyCost()
		st.CostSessionLine = ct.SessionSummary()
		st.CostDailyLine = ct.DailySummary()
		st.CostOverBudget = ct.IsOverBudget()
	}
	if fleet := llm.LoadCostDailyFleet(); fleet.Calls > 0 || fleet.CostUSD > 0 {
		st.CostFleetUSD = fleet.CostUSD
		st.CostFleetCalls = fleet.Calls
		st.CostFleetInstances = fleet.Instances
		st.CostFleetLine = llm.FormatCostDailyFleetLine()
	}
	if rs := llm.LoadCostRouteStats(); rs.Decisions > 0 {
		st.CostRouteDecisions = rs.Decisions
		st.CostRouteApplied = rs.Applied
		st.CostRouteShadow = rs.Shadow
		st.CostRouteLastTier = rs.LastTier
		st.CostRouteLastMode = rs.LastMode
		st.CostRouteByTier = rs.ByTier
		st.CostRouteLine = llm.FormatCostRouteStatsLine()
	}
	if snap := security.ProcessDenialLedger().Snapshot(); snap.Threshold > 0 || snap.TotalDenies > 0 || snap.Paused {
		st.DenialPaused = snap.Paused
		st.DenialConsecutive = snap.ConsecutiveDenies
		st.DenialThreshold = snap.Threshold
		st.DenialLastTool = snap.LastTool
		st.DenialPauseMessage = snap.PauseMessage
	}
	return st
}

// ClearDenialPause clears security denial auto-pause (Wails / operator recovery).
func (a *App) ClearDenialPause() map[string]interface{} {
	security.ProcessDenialLedger().ClearPause()
	snap := security.ProcessDenialLedger().Snapshot()
	return map[string]interface{}{
		"ok":      true,
		"action":  "denial-pause-clear",
		"paused":  snap.Paused,
		"summary": "denial ledger pause cleared",
	}
}

// OpenAdaptivePromptExportsDir ensures ~/.maclaw/stats/exports exists and opens
// it in the system file manager (Wails binding for System Doctor).
// Explorer/open failures are soft: path is still returned with opened=false so
// the UI can show the directory even in headless environments.
func (a *App) OpenAdaptivePromptExportsDir() (map[string]interface{}, error) {
	dir := agent.PromptProfileExportDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	opened := true
	var openErr string
	if a != nil {
		if err := a.OpenProjectDirectory(dir); err != nil {
			opened = false
			openErr = err.Error()
			log.Printf("[shared-loop] open exports dir %s: %v", dir, err)
		}
	}
	out := map[string]interface{}{
		"ok":     true,
		"path":   dir,
		"opened": opened,
	}
	if openErr != "" {
		out["error"] = openErr
	}
	return out, nil
}
