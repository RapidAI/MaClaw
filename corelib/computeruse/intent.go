package computeruse

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ToolNames is the full Computer Use tool surface exposed to the agent.
var ToolNames = []string{
	"computer_observe",
	"computer_click",
	"computer_type",
	"computer_key",
	"computer_scroll",
	"computer_select",
	"computer_scroll_into_view",
	"computer_drag",
	"computer_wait",
	"computer_focus",
	"computer_find",
	"computer_done",
	"computer_playbook",
}

// LegacyGUICompeteTools are older raw GUI tools that should be de-prioritized
// when Computer Use is active (prefer ref-based computer_* instead).
var LegacyGUICompeteTools = map[string]bool{
	"gui_click":      true,
	"gui_type":       true,
	"gui_screenshot": true,
}

// HasExplicitTrigger reports whether the user message explicitly invokes
// Computer Use via the @computer / "computer use" trigger syntax. This is an
// addressing override, not intent detection — semantic activation is handled
// by the unified intent classifier (corelib/intent LabelComputerUse).
func HasExplicitTrigger(userText string) bool {
	t := strings.ToLower(strings.TrimSpace(userText))
	if t == "" {
		return false
	}
	return hasComputerMention(t) || hasComputerPhrase(t)
}

func hasComputerMention(text string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], "@computer")
		if i < 0 {
			return false
		}
		i += start
		end := i + len("@computer")
		if (i == 0 || !isMentionRuneBefore(text, i)) && (end == len(text) || !isMentionRuneAfter(text, end)) {
			return true
		}
		start = end
	}
}

func hasComputerPhrase(text string) bool {
	for _, phrase := range []string{"computer use", "computer_use", "computer-use"} {
		for start := 0; ; {
			i := strings.Index(text[start:], phrase)
			if i < 0 {
				break
			}
			i += start
			end := i + len(phrase)
			if (i == 0 || !isWordRuneBefore(text, i)) && (end == len(text) || !isWordRuneAfter(text, end)) {
				return true
			}
			start = end
		}
	}
	return false
}

func isMentionRuneBefore(text string, index int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

func isMentionRuneAfter(text string, index int) bool {
	r, _ := utf8.DecodeRuneInString(text[index:])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

func isWordRuneBefore(text string, index int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isWordRuneAfter(text string, index int) bool {
	r, _ := utf8.DecodeRuneInString(text[index:])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// IsComputerUseTool reports whether name is part of the CU surface.
func IsComputerUseTool(name string) bool {
	name = strings.TrimSpace(name)
	for _, n := range ToolNames {
		if n == name {
			return true
		}
	}
	return false
}
