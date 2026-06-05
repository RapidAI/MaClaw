package progress

import (
	"fmt"
	"strings"
	"time"
)

// SummaryRule defines how to extract a human-readable summary from a tool call.
// Adding support for a new tool requires only adding one entry to toolSummaryRules.
type SummaryRule struct {
	// Verb is the Chinese action verb shown to the user (e.g. "搜索", "生成").
	Verb string

	// ArgKey is the JSON key in the tool args to extract the object of the action.
	// If empty, Static must be true.
	ArgKey string

	// MaxLen truncates the extracted arg value to this many runes. 0 = no limit.
	MaxLen int

	// Static means the Verb alone is the full summary (no arg extraction needed).
	Static bool

	// Phase is the coarse phase tag for this tool (e.g. "searching", "writing").
	Phase string

	// PrefixFunc optionally transforms the verb based on a sub-action arg.
	// For example, SSH tool uses the "action" arg to produce "连接服务器" vs "服务器执行".
	PrefixFunc func(args map[string]any) string
}

// toolSummaryRules is the declarative mapping from tool name to summary extraction rule.
// To add a new tool, add one entry here. No other code changes needed.
var toolSummaryRules = map[string]SummaryRule{
	"web_search": {
		Verb: "搜索", ArgKey: "query", MaxLen: 30, Phase: "searching",
	},
	"write_file": {
		Verb: "生成", ArgKey: "path", MaxLen: 50, Phase: "writing",
	},
	"bash": {
		Verb: "执行命令", ArgKey: "command", MaxLen: 30, Phase: "executing",
	},
	"ssh": {
		Verb: "服务器操作", Phase: "remote",
		PrefixFunc: sshActionPrefix,
	},
	"generate_pdf": {
		Verb: "生成 PDF 文档", Static: true, Phase: "generating",
	},
	"run_skill": {
		Verb: "执行技能", ArgKey: "skill_name", MaxLen: 30, Phase: "skill",
	},
	"send_file": {
		Verb: "发送文件", ArgKey: "path", MaxLen: 50, Phase: "delivering",
	},
	"screenshot": {
		Verb: "截取屏幕", Static: true, Phase: "capturing",
	},
}

// silentTools are internal tools that don't produce user-visible milestones.
// These are housekeeping operations the user doesn't care about.
var silentTools = map[string]bool{
	"read_file":          true,
	"read_code":          true,
	"memory":             true,
	"discover_tool":      true,
	"task":               true,
	"ask_user":           true,
	"manage_config":      true,
	"manage_template":    true,
	"manage_schedule":    true,
	"delegate_task":      true,
	"get_session_output": true, // coding session polling — internal
}

// IsSilentTool returns true if the tool should not produce milestones.
func IsSilentTool(toolName string) bool {
	return silentTools[toolName]
}

// ExtractMilestone creates a Milestone from a tool call. Returns nil for
// silent tools or tools without a summary rule (unknown tools are treated
// as silent to avoid noisy fallback messages).
func ExtractMilestone(toolName string, args map[string]any, completed bool) *Milestone {
	if silentTools[toolName] {
		return nil
	}

	rule, ok := toolSummaryRules[toolName]
	if !ok {
		// Unknown tool — don't generate noise. If it becomes important,
		// add it to toolSummaryRules.
		return nil
	}

	summary := buildSummary(rule, args)
	if summary == "" {
		return nil
	}

	return &Milestone{
		Time:      time.Now(),
		Tool:      toolName,
		Summary:   summary,
		Phase:     rule.Phase,
		Completed: completed,
	}
}

// buildSummary constructs the human-readable summary string from a rule and args.
func buildSummary(rule SummaryRule, args map[string]any) string {
	verb := rule.Verb
	if rule.PrefixFunc != nil {
		if custom := rule.PrefixFunc(args); custom != "" {
			verb = custom
		}
		// When PrefixFunc provides the full summary and there's no ArgKey,
		// the verb IS the complete summary.
		if rule.ArgKey == "" {
			return verb
		}
	}

	if rule.Static {
		return verb
	}

	argVal := extractStringArg(args, rule.ArgKey)
	if argVal == "" {
		return verb + "..."
	}

	argVal = truncateRunes(argVal, rule.MaxLen)
	return fmt.Sprintf("%s: %s", verb, argVal)
}

// sshActionPrefix returns a complete summary for SSH tool calls.
// Because SSH actions have heterogeneous args (connect uses host, exec uses command),
// PrefixFunc returns the full summary string and the rule is marked Static-like
// (no ArgKey) so buildSummary just uses the PrefixFunc output.
func sshActionPrefix(args map[string]any) string {
	action := extractStringArg(args, "action")
	switch action {
	case "connect":
		host := extractStringArg(args, "host")
		if host != "" {
			return fmt.Sprintf("连接服务器 %s", host)
		}
		return "连接服务器"
	case "exec":
		cmd := extractStringArg(args, "command")
		if cmd != "" {
			return fmt.Sprintf("服务器执行: %s", truncateRunes(cmd, 30))
		}
		return "服务器执行命令"
	case "close":
		return "断开服务器连接"
	case "close_all":
		return "断开所有服务器连接"
	default:
		return "服务器操作"
	}
}

// extractStringArg safely extracts a string value from a map.
func extractStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// truncateRunes truncates a string to maxLen runes, appending "..." if truncated.
func truncateRunes(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// MergeMilestones combines multiple milestones into a single progress message.
// Used by the pusher's merge window to avoid sending one message per milestone.
func MergeMilestones(milestones []Milestone) string {
	if len(milestones) == 0 {
		return ""
	}
	if len(milestones) == 1 {
		m := milestones[0]
		if m.Completed {
			return m.Summary
		}
		return "正在 " + m.Summary
	}

	// Collect completed summaries.
	var completed []string
	var current string
	for _, m := range milestones {
		if m.Completed && m.Summary != "" {
			completed = append(completed, m.Summary)
		} else if !m.Completed && m.Summary != "" {
			current = m.Summary
		}
	}

	var parts []string
	switch {
	case len(completed) <= 3:
		for _, s := range completed {
			parts = append(parts, s)
		}
	default:
		parts = append(parts, fmt.Sprintf("已完成 %d 个步骤", len(completed)))
	}

	suffix := ""
	if current != "" {
		suffix = fmt.Sprintf("，正在 %s", current)
	}

	if len(parts) == 0 {
		if current != "" {
			return "正在 " + current
		}
		return ""
	}

	return "已完成: " + strings.Join(parts, " + ") + suffix
}
