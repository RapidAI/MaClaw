package progress

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

// SummaryRule defines how to extract a human-readable summary from a tool call.
// Adding support for a new tool requires only adding one entry to toolSummaryRules.
type SummaryRule struct {
	// VerbKey is an i18n key for the action verb shown to the user.
	VerbKey string

	// ArgKey is the JSON key in the tool args to extract the object of the action.
	// If empty, Static must be true (or PrefixFunc supplies the full summary).
	// Ignored when ArgKeys is non-empty.
	ArgKey string

	// ArgKeys are alternate JSON keys tried in order (first non-empty wins).
	// Prefer this when callers use multiple names for the same parameter
	// (e.g. run_skill: name / skill_name / skill).
	ArgKeys []string

	// MaxLen truncates the extracted arg value to this many runes. 0 = no limit.
	MaxLen int

	// Static means the Verb alone is the full summary (no arg extraction needed).
	Static bool

	// Phase is the coarse phase tag for this tool (e.g. "searching", "writing").
	Phase string

	// PrefixFunc optionally transforms the verb based on a sub-action arg.
	// For example, SSH tool uses the "action" arg to produce localized connect/exec text.
	PrefixFunc func(args map[string]any, lang string) string
}

// toolSummaryRules is the declarative mapping from tool name to summary extraction rule.
// To add a new tool, add one entry here. No other code changes needed.
var toolSummaryRules = map[string]SummaryRule{
	"web_search": {
		VerbKey: i18n.MsgToolActionWebSearch, ArgKey: "query", MaxLen: 30, Phase: "searching",
	},
	"web_fetch": {
		VerbKey: i18n.MsgToolActionWebFetch, ArgKey: "url", MaxLen: 50, Phase: "searching",
	},
	"write_file": {
		VerbKey: i18n.MsgToolActionWriteFile, ArgKey: "path", MaxLen: 50, Phase: "writing",
	},
	"edit_file": {
		VerbKey: i18n.MsgToolActionEditFile, ArgKey: "path", MaxLen: 50, Phase: "writing",
	},
	"bash": {
		VerbKey: i18n.MsgToolActionRunCommand, ArgKey: "command", MaxLen: 30, Phase: "executing",
	},
	"search_files": {
		VerbKey: i18n.MsgToolActionSearchFiles, ArgKey: "pattern", MaxLen: 40, Phase: "searching",
	},
	"grep_search": {
		VerbKey: i18n.MsgToolActionGrep, ArgKey: "pattern", MaxLen: 40, Phase: "searching",
	},
	"list_directory": {
		VerbKey: i18n.MsgToolActionListDir, ArgKey: "path", MaxLen: 50, Phase: "reading",
	},
	"ssh": {
		VerbKey: i18n.MsgToolActionSSH, Phase: "remote",
		PrefixFunc: sshActionPrefix,
	},
	"generate_pdf": {
		VerbKey: i18n.MsgToolActionGeneratePDF, Static: true, Phase: "generating",
	},
	"run_skill": {
		VerbKey: i18n.MsgToolActionRunSkill, ArgKeys: []string{"name", "skill_name", "skill"}, MaxLen: 30, Phase: "skill",
	},
	"send_file": {
		VerbKey: i18n.MsgToolActionSendFile, ArgKey: "path", MaxLen: 50, Phase: "delivering",
	},
	"send_to_im": {
		VerbKey: i18n.MsgToolActionSendFile, ArgKey: "path", MaxLen: 50, Phase: "delivering",
	},
	"screenshot": {
		VerbKey: i18n.MsgToolActionScreenshot, Static: true, Phase: "capturing",
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
// as silent to avoid noisy fallback messages). Defaults to Chinese labels.
func ExtractMilestone(toolName string, args map[string]any, completed bool) *Milestone {
	return ExtractMilestoneLang(toolName, args, completed, "zh")
}

// ExtractMilestoneLang is like ExtractMilestone but localizes verbs to lang.
func ExtractMilestoneLang(toolName string, args map[string]any, completed bool, lang string) *Milestone {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	if toolName == "" || silentTools[toolName] {
		return nil
	}

	rule, ok := toolSummaryRules[toolName]
	if !ok {
		// Unknown tool — don't generate noise. If it becomes important,
		// add it to toolSummaryRules.
		return nil
	}

	summary := buildSummary(rule, args, lang)
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
func buildSummary(rule SummaryRule, args map[string]any, lang string) string {
	verb := i18n.T(rule.VerbKey, lang)
	hasArgKey := rule.ArgKey != "" || len(rule.ArgKeys) > 0
	if rule.PrefixFunc != nil {
		if custom := rule.PrefixFunc(args, lang); custom != "" {
			verb = custom
		}
		// When PrefixFunc provides the full summary and there's no ArgKey,
		// the verb IS the complete summary.
		if !hasArgKey {
			return verb
		}
	}

	if rule.Static {
		return verb
	}

	argVal := extractRuleArg(args, rule)
	if argVal == "" {
		return i18n.Tf(i18n.MsgMilestoneVerbEllipsis, lang, verb)
	}

	argVal = truncateRunes(argVal, rule.MaxLen)
	return fmt.Sprintf("%s: %s", verb, argVal)
}

func extractRuleArg(args map[string]any, rule SummaryRule) string {
	if len(rule.ArgKeys) > 0 {
		for _, key := range rule.ArgKeys {
			if v := extractStringArg(args, key); v != "" {
				return v
			}
		}
		return ""
	}
	return extractStringArg(args, rule.ArgKey)
}

// sshActionPrefix returns a complete summary for SSH tool calls.
func sshActionPrefix(args map[string]any, lang string) string {
	action := extractStringArg(args, "action")
	switch action {
	case "connect":
		host := extractStringArg(args, "host")
		base := i18n.T(i18n.MsgToolActionSSHConnect, lang)
		if host != "" {
			return base + " " + host
		}
		return base
	case "exec":
		cmd := extractStringArg(args, "command")
		base := i18n.T(i18n.MsgToolActionSSHExec, lang)
		if cmd != "" {
			return base + ": " + truncateRunes(cmd, 30)
		}
		return base
	case "close":
		return i18n.T(i18n.MsgToolActionSSHClose, lang)
	case "close_all":
		return i18n.T(i18n.MsgToolActionSSHCloseAll, lang)
	default:
		return i18n.T(i18n.MsgToolActionSSH, lang)
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
// Defaults to Chinese labels; prefer MergeMilestonesLang when GUI language is known.
func MergeMilestones(milestones []Milestone) string {
	return MergeMilestonesLang("zh", milestones)
}

// MergeMilestonesLang is like MergeMilestones but localizes templates to lang.
func MergeMilestonesLang(lang string, milestones []Milestone) string {
	if len(milestones) == 0 {
		return ""
	}
	if len(milestones) == 1 {
		m := milestones[0]
		if m.Completed {
			return m.Summary
		}
		return i18n.Tf(i18n.MsgMilestoneWorking, lang, m.Summary)
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
		parts = append(parts, completed...)
	default:
		parts = append(parts, i18n.Tf(i18n.MsgMilestoneDoneSteps, lang, len(completed)))
	}

	if len(parts) == 0 {
		if current != "" {
			return i18n.Tf(i18n.MsgMilestoneWorking, lang, current)
		}
		return ""
	}

	joined := strings.Join(parts, " + ")
	if current != "" {
		return i18n.Tf(i18n.MsgMilestoneDoneWorking, lang, joined, current)
	}
	return i18n.Tf(i18n.MsgMilestoneDoneList, lang, joined)
}
