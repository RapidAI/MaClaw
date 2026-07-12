package doctor

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// FormatReport renders a compact human-readable doctor report for TUI/chat.
func FormatReport(r Report) string {
	var b strings.Builder
	status := "READY"
	if !r.OK {
		status = "NOT READY"
	}
	fmt.Fprintf(&b, "MaClaw doctor — %s\n%s\n", status, r.Summary)
	if r.ConfigPath != "" {
		fmt.Fprintf(&b, "config: %s\n", r.ConfigPath)
	}
	if r.BaseDir != "" {
		fmt.Fprintf(&b, "home:   %s\n", r.BaseDir)
	}
	// Prominent shared-loop mode line for operators scanning /doctor output.
	if line := sharedLoopLineFromReport(r); line != "" {
		fmt.Fprintf(&b, "%s\n", line)
	}
	// Adaptive prompt hit rate + estimated system-prompt token savings.
	if line := agent.FormatPromptProfileLine(); line != "" {
		fmt.Fprintf(&b, "%s\n", line)
	}
	b.WriteString("\n")
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "  %-5s %-22s %s\n", strings.ToUpper(string(c.Status)), c.ID, c.Message)
		if c.Hint != "" && (c.Status == StatusFail || c.Status == StatusWarn) {
			fmt.Fprintf(&b, "        → %s\n", c.Hint)
		}
	}
	if r.Blockers > 0 || r.Warnings > 0 {
		fmt.Fprintf(&b, "\nblockers=%d warnings=%d\n", r.Blockers, r.Warnings)
	}
	return strings.TrimRight(b.String(), "\n")
}

// sharedLoopLineFromReport extracts a one-line summary from agent.shared_loop
// check detail when present (doctor.Run always includes it).
func sharedLoopLineFromReport(r Report) string {
	for _, c := range r.Checks {
		if c.ID != "agent.shared_loop" || c.Detail == nil {
			continue
		}
		env := SharedLoopEnv{Mode: "off", Percent: 100}
		if mode, ok := c.Detail["mode"].(string); ok && mode != "" {
			env.Mode = mode
		}
		switch p := c.Detail["percent"].(type) {
		case int:
			env.Percent = p
		case int64:
			env.Percent = int(p)
		case float64:
			env.Percent = int(p)
		}
		if envStr, ok := c.Detail["env"].(string); ok {
			env.EnvOverride = envStr
		}
		if wp, ok := c.Detail["workflow_pilot"].(bool); ok {
			env.WorkflowPilot = wp
		}
		if ce, ok := c.Detail["config_enabled"].(bool); ok {
			env.ConfigEnabled = ce
		}
		if cm, ok := c.Detail["config_migrated"].(bool); ok {
			env.ConfigMigrated = cm
		}
		if def, ok := c.Detail["default"].(bool); ok {
			env.DefaultEnabled = def
		}
		return FormatSharedLoopLine(env)
	}
	return ""
}
