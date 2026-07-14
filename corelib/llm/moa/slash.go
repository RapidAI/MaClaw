package moa

import (
	"strings"
)

// SlashKind classifies a /moa command after shared parsing (GUI + TUI).
type SlashKind int

const (
	// SlashNone is not a /moa command.
	SlashNone SlashKind = iota
	// SlashHelp is bare /moa (show usage).
	SlashHelp
	// SlashOneShot arms one-shot MoA then runs Prompt (optional Preset).
	SlashOneShot
	// SlashSticky is /moa sticky|session … (host handles StickyArg / StickyPreset).
	SlashSticky
	// SlashStats is /moa stats.
	SlashStats
	// SlashUsage is a parse/usage error (see Hint).
	SlashUsage
)

// SlashCommand is the shared /moa parse result (design §9.2 + Phase 2 @preset).
type SlashCommand struct {
	Kind SlashKind
	// Preset is empty for default_preset; otherwise the name from @name or sticky on name.
	Preset string
	// Prompt is the user question for one-shot (after stripping /moa and optional @preset).
	Prompt string
	// StickyArg is on|off|status|"" for SlashSticky.
	StickyArg string
	// StickyPreset is optional preset for sticky on [preset].
	StickyPreset string
	// RawBody is text after the /moa token (trimmed).
	RawBody string
	// Hint is a short English usage note for SlashUsage (hosts localize if needed).
	Hint string
}

// ParseSlash parses a user line that may be a /moa command.
// Accepts leading whitespace; command token is case-insensitive.
//
//	/moa
//	/moa <prompt>
//	/moa @review <prompt>     (Phase 2)
//	/moa sticky on|off|status [preset]
//	/moa stats
func ParseSlash(input string) SlashCommand {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return SlashCommand{Kind: SlashNone}
	}
	lower := strings.ToLower(trimmed)
	if lower != "/moa" && !strings.HasPrefix(lower, "/moa ") &&
		!strings.HasPrefix(lower, "/moa\t") {
		// Also allow /moa followed by other unicode spaces via Fields path after prefix strip.
		if !strings.HasPrefix(lower, "/moa") {
			return SlashCommand{Kind: SlashNone}
		}
		// "/moa..." glued without space is not /moa (e.g. /moab).
		if len(trimmed) > 4 && !isASCIISpace(trimmed[4]) {
			return SlashCommand{Kind: SlashNone}
		}
	}

	body := ""
	if len(trimmed) > 4 {
		body = strings.TrimSpace(trimmed[4:])
	}
	out := SlashCommand{RawBody: body}
	if body == "" {
		out.Kind = SlashHelp
		return out
	}

	fields := strings.Fields(body)
	if len(fields) == 0 {
		out.Kind = SlashHelp
		return out
	}
	head := strings.ToLower(fields[0])

	// Subcommands only when they look like commands — not natural-language prompts
	// like "/moa sticky keys in redis" or "/moa stats about cost".
	switch head {
	case "sticky", "session":
		if isStickySubcommand(fields) {
			out.Kind = SlashSticky
			if len(fields) > 1 {
				out.StickyArg = strings.ToLower(fields[1])
			}
			if len(fields) > 2 {
				out.StickyPreset = normalizePresetToken(fields[2])
			}
			return out
		}
	case "stats", "stat":
		if len(fields) == 1 {
			out.Kind = SlashStats
			return out
		}
	}

	// Phase 2: @preset [prompt]
	if strings.HasPrefix(fields[0], "@") {
		name := strings.TrimSpace(fields[0][1:])
		// Allow @review or @ review via second field if first is bare "@".
		if name == "" && len(fields) > 1 {
			name = strings.TrimPrefix(fields[1], "@")
			name = strings.TrimSpace(name)
			rest := strings.TrimSpace(strings.Join(fields[2:], " "))
			if name == "" {
				out.Kind = SlashUsage
				out.Hint = "usage: /moa @preset <prompt>"
				return out
			}
			out.Preset = normalizePresetToken(name)
			if out.Preset == "" {
				out.Kind = SlashUsage
				out.Hint = "usage: /moa @preset <prompt> (invalid preset name)"
				return out
			}
			out.Prompt = rest
			if out.Prompt == "" {
				out.Kind = SlashUsage
				out.Hint = "usage: /moa @preset <prompt> (question required after preset)"
				return out
			}
			out.Kind = SlashOneShot
			return out
		}
		if name == "" {
			out.Kind = SlashUsage
			out.Hint = "usage: /moa @preset <prompt>"
			return out
		}
		out.Preset = normalizePresetToken(name)
		if out.Preset == "" {
			out.Kind = SlashUsage
			out.Hint = "usage: /moa @preset <prompt> (invalid preset name)"
			return out
		}
		out.Prompt = strings.TrimSpace(strings.Join(fields[1:], " "))
		if out.Prompt == "" {
			out.Kind = SlashUsage
			out.Hint = "usage: /moa @preset <prompt> (question required after preset)"
			return out
		}
		out.Kind = SlashOneShot
		return out
	}

	// Default: entire body is the one-shot prompt (default_preset).
	out.Kind = SlashOneShot
	out.Prompt = body
	return out
}

// isStickySubcommand is true for "/moa sticky" or "/moa sticky on|off|status|… [preset]".
// Natural language that merely starts with "sticky" is treated as a one-shot prompt.
func isStickySubcommand(fields []string) bool {
	if len(fields) <= 1 {
		return true // bare "sticky" → status/usage at host
	}
	switch strings.ToLower(fields[1]) {
	case "on", "1", "true", "enable",
		"off", "0", "false", "disable", "clear",
		"status", "stat", "state":
		return true
	default:
		return false
	}
}

// IsMoASlash reports whether input looks like a /moa command line.
func IsMoASlash(input string) bool {
	return ParseSlash(input).Kind != SlashNone
}

func normalizePresetToken(name string) string {
	return NormalizePresetToken(name)
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
