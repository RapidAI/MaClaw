package doctor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SharedLoopEnv describes runtime env/config controlling the shared agent loop.
type SharedLoopEnv struct {
	Mode          string // on | off | shadow
	Percent       int    // 0..100 canary
	WorkflowPilot bool
	EnvOverride   string // raw MACLAW_SHARED_AGENT_LOOP
	// PercentFromEnv is true when MACLAW_SHARED_AGENT_LOOP_PERCENT is set.
	PercentFromEnv bool
	// WorkflowFromEnv is true when MACLAW_SHARED_AGENT_LOOP_WORKFLOW is set.
	WorkflowFromEnv bool
	ConfigEnabled   bool
	ConfigMigrated  bool
	ConfigPercent   *int // raw config pointer (may be nil)
	ConfigWorkflow  bool
	DefaultEnabled  bool
}

// ResolveSharedLoopEnv derives shared-loop posture from config + environment.
// Env MACLAW_SHARED_AGENT_LOOP wins over config when set.
func ResolveSharedLoopEnv(cfg corelib.AppConfig) SharedLoopEnv {
	pct, pctFromEnv := ResolveSharedLoopPercent(cfg)
	wf, wfFromEnv := ResolveSharedLoopWorkflowPilot(cfg)
	out := SharedLoopEnv{
		Mode:            "off",
		Percent:         pct,
		WorkflowPilot:   wf,
		EnvOverride:     strings.TrimSpace(os.Getenv("MACLAW_SHARED_AGENT_LOOP")),
		PercentFromEnv:  pctFromEnv,
		WorkflowFromEnv: wfFromEnv,
		ConfigEnabled:   cfg.SharedAgentLoopEnabled,
		ConfigMigrated:  cfg.SharedAgentLoopMigrated,
		ConfigPercent:   cfg.SharedAgentLoopCanaryPercent,
		ConfigWorkflow:  cfg.SharedAgentLoopWorkflow,
		DefaultEnabled:  corelib.AppConfigDefaults().SharedAgentLoopEnabled,
	}
	if out.EnvOverride != "" {
		switch strings.ToLower(out.EnvOverride) {
		case "1", "true", "yes", "on":
			out.Mode = "on"
		case "shadow", "observe", "dry-run", "dryrun":
			out.Mode = "shadow"
		case "0", "false", "no", "off":
			out.Mode = "off"
		default:
			out.Mode = "off"
		}
	} else if envTruthy("MACLAW_SHARED_AGENT_LOOP_SHADOW") {
		out.Mode = "shadow"
	} else if cfg.SharedAgentLoopEnabled {
		out.Mode = "on"
	} else if out.DefaultEnabled && !cfg.SharedAgentLoopMigrated {
		// Unmigrated legacy file: product default is on, but config field is
		// still false until migration runs. Report as off-until-migrate.
		out.Mode = "off"
	}
	return out
}

// FormatSharedLoopLine returns a compact one-line summary for TUI/CLI status.
// Example: "shared-loop: on (canary 25%; env MACLAW_SHARED_AGENT_LOOP locks mode)"
func FormatSharedLoopLine(env SharedLoopEnv) string {
	var b strings.Builder
	fmt.Fprintf(&b, "shared-loop: %s", env.Mode)
	parts := make([]string, 0, 4)
	// Canary applies to both on and shadow eligibility (sticky membership).
	if (env.Mode == "on" || env.Mode == "shadow") && env.Percent < 100 {
		parts = append(parts, fmt.Sprintf("canary %d%%", env.Percent))
	}
	if env.WorkflowPilot {
		parts = append(parts, "workflow pilot")
	}
	if env.EnvOverride != "" {
		parts = append(parts, "env MACLAW_SHARED_AGENT_LOOP locks mode")
	} else if env.ConfigEnabled {
		parts = append(parts, "config enabled")
	} else if !env.ConfigMigrated && env.DefaultEnabled {
		parts = append(parts, "pending migrate")
	} else {
		parts = append(parts, "config disabled")
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(parts, "; "))
	}
	return b.String()
}

// SharedLoopCheck builds the agent.shared_loop doctor check.
func SharedLoopCheck(cfg corelib.AppConfig) Check {
	env := ResolveSharedLoopEnv(cfg)
	detail := map[string]any{
		"mode":              env.Mode,
		"percent":           env.Percent,
		"percent_from_env":  env.PercentFromEnv,
		"env":               env.EnvOverride,
		"workflow_pilot":    env.WorkflowPilot,
		"workflow_from_env": env.WorkflowFromEnv,
		"config_workflow":   env.ConfigWorkflow,
		"default":           env.DefaultEnabled,
		"config_enabled":    env.ConfigEnabled,
		"config_migrated":   env.ConfigMigrated,
	}
	if env.ConfigPercent != nil {
		detail["config_percent"] = *env.ConfigPercent
	}
	switch env.Mode {
	case "on":
		msg := "shared agent loop ON (eligible chat/background turns use corelib/agent.RunLoop)"
		if env.Percent < 100 {
			msg = fmt.Sprintf("shared agent loop ON with %d%% canary (sticky by user id)", env.Percent)
		}
		if env.WorkflowPilot {
			msg += "; workflow pilot enabled (non-doc)"
		}
		return Check{
			ID:      "agent.shared_loop",
			Status:  StatusOK,
			Message: msg,
			Detail:  detail,
		}
	case "shadow":
		return Check{
			ID:      "agent.shared_loop",
			Status:  StatusInfo,
			Message: "shared agent loop SHADOW (legacy executes; eligibility is logged only)",
			Hint:    "Set MACLAW_SHARED_AGENT_LOOP=1 to actually divert eligible turns",
			Detail:  detail,
		}
	default:
		return Check{
			ID:      "agent.shared_loop",
			Status:  StatusInfo,
			Message: "shared agent loop OFF (all turns use legacy IM loop)",
			Hint:    "Restart after upgrade for one-time migration, or set MACLAW_SHARED_AGENT_LOOP=1 / shared_agent_loop_enabled=true; shadow for dry-run",
			Detail:  detail,
		}
	}
}

func sharedLoopPercentFromEnv() int {
	n, _ := SharedLoopPercentFromEnv()
	return n
}

// SharedLoopPercentFromEnv returns 0..100 canary percentage from
// MACLAW_SHARED_AGENT_LOOP_PERCENT. fromEnv is false when the env is unset
// (caller should fall back to config / default 100).
func SharedLoopPercentFromEnv() (percent int, fromEnv bool) {
	v := strings.TrimSpace(os.Getenv("MACLAW_SHARED_AGENT_LOOP_PERCENT"))
	if v == "" {
		return 100, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 100, true
	}
	return clampPercent(n), true
}

// ResolveSharedLoopPercent prefers env when set, else config canary percent,
// else 100. Returns (percent, fromEnv).
func ResolveSharedLoopPercent(cfg corelib.AppConfig) (percent int, fromEnv bool) {
	if n, ok := SharedLoopPercentFromEnv(); ok {
		return n, true
	}
	if cfg.SharedAgentLoopCanaryPercent != nil {
		return clampPercent(*cfg.SharedAgentLoopCanaryPercent), false
	}
	return 100, false
}

// ResolveSharedLoopWorkflowPilot prefers env when set, else config flag.
// Returns (enabled, fromEnv).
func ResolveSharedLoopWorkflowPilot(cfg corelib.AppConfig) (enabled bool, fromEnv bool) {
	raw := strings.TrimSpace(os.Getenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW"))
	if raw != "" {
		switch strings.ToLower(raw) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		default:
			return false, true
		}
	}
	return cfg.SharedAgentLoopWorkflow, false
}

func clampPercent(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

// SharedLoopCanaryBucket returns the sticky FNV-1a bucket 0..99 for userID.
// Empty userID maps to 0 (always below any positive percent threshold).
func SharedLoopCanaryBucket(userID string) int {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 0
	}
	var h uint32 = 2166136261
	for i := 0; i < len(userID); i++ {
		h ^= uint32(userID[i])
		h *= 16777619
	}
	return int(h % 100)
}

// SharedLoopCanaryAllows reports whether userID is in the canary at percent (0..100).
// percent>=100 always allows; percent<=0 never allows (except empty userID is treated
// as allow when percent>0 so anonymous/desktop-local paths are not hard-blocked by canary).
func SharedLoopCanaryAllows(userID string, percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	if strings.TrimSpace(userID) == "" {
		return true
	}
	return SharedLoopCanaryBucket(userID) < percent
}

// SharedLoopCanaryPreview is a JSON-friendly canary membership preview for CLI/GUI.
type SharedLoopCanaryPreview struct {
	UserID  string `json:"user_id"`
	Percent int    `json:"percent"`
	Bucket  int    `json:"bucket"`
	Allows  bool   `json:"allows"`
}

// PreviewSharedLoopCanary builds a canary membership preview.
// percent<0 uses env/config resolution with an empty config (env-only + default 100).
// Prefer PreviewSharedLoopCanaryWithConfig when AppConfig is available.
func PreviewSharedLoopCanary(userID string, percent int) SharedLoopCanaryPreview {
	return PreviewSharedLoopCanaryWithConfig(userID, percent, corelib.AppConfig{})
}

// PreviewSharedLoopCanaryWithConfig uses percent when >=0; otherwise resolves from cfg+env.
func PreviewSharedLoopCanaryWithConfig(userID string, percent int, cfg corelib.AppConfig) SharedLoopCanaryPreview {
	if percent < 0 {
		percent, _ = ResolveSharedLoopPercent(cfg)
	}
	percent = clampPercent(percent)
	userID = strings.TrimSpace(userID)
	return SharedLoopCanaryPreview{
		UserID:  userID,
		Percent: percent,
		Bucket:  SharedLoopCanaryBucket(userID),
		Allows:  SharedLoopCanaryAllows(userID, percent),
	}
}

func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
