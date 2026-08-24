package longhorizon

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const CommandPrefix = "@horizon"

func ParseAdmitCommand(text string) (body string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, CommandPrefix) {
		return "", false
	}
	rest := trimmed[len(CommandPrefix):]
	if rest == "" {
		return "", true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	if unicode.IsSpace(r) {
		return strings.TrimSpace(rest), true
	}
	return "", false
}

func IsCancelText(text string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	trimmed = strings.Trim(trimmed, " .\u3002!\uff01")
	switch trimmed {
	case "/cancel", "/stop", "cancel", "abort", "stop", "quit",
		"\u53d6\u6d88", "\u505c\u6b62", "\u653e\u5f03", "\u9000\u51fa":
		return true
	}
	return false
}

func IsResumeText(text string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	trimmed = strings.Trim(trimmed, " .\u3002!\uff01")
	switch trimmed {
	case "resume", "restore", "continue", "\u6062\u590d", "\u7ee7\u7eed":
		return true
	}
	return false
}

func IsAbandonText(text string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	trimmed = strings.Trim(trimmed, " .\u3002!\uff01")
	switch trimmed {
	case "abandon", "discard", "drop", "\u653e\u5f03":
		return true
	}
	return false
}

func StripNonLetterPrefix(text string) string {
	return strings.TrimLeftFunc(text, unicode.IsSpace)
}
