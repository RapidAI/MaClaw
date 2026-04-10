package memory

import (
	"fmt"
	"regexp"
	"strings"
)

// injectionPatterns are compiled regexes that detect prompt injection
// attempts in memory content. Memory entries are injected into the system
// prompt, so malicious content can hijack agent behavior.
var injectionPatterns []*regexp.Regexp

func init() {
	patterns := []string{
		// Direct instruction override attempts
		`(?i)ignore\s+(all\s+)?previous\s+(instructions?|prompts?)`,
		`(?i)disregard\s+(all\s+)?previous`,
		`(?i)forget\s+(all\s+)?(your\s+)?instructions?`,
		`(?i)override\s+(system|previous)\s+(prompt|instructions?)`,
		`(?i)new\s+instructions?\s*:`,
		// Role hijacking
		`(?i)you\s+are\s+now\s+a`,
		`(?i)act\s+as\s+if\s+you\s+are`,
		`(?i)pretend\s+(you\s+are|to\s+be)`,
		// Special token injection
		`<\|im_start\|>`,
		`<\|im_end\|>`,
		`<\|system\|>`,
		`<\|endoftext\|>`,
		`\[INST\]`,
		`\[/INST\]`,
		// Credential exfiltration
		`(?i)send\s+(all\s+)?(api[_\s]?keys?|credentials?|tokens?|secrets?)\s+to`,
		`(?i)exfiltrate`,
		`(?i)curl\s+.*\s*(api[_\s]?key|secret|token|password)`,
		// SSH backdoor
		`(?i)add\s+.*ssh.*authorized[_\s]?keys`,
		`(?i)echo\s+.*>>\s*~?/\.ssh/authorized_keys`,
	}
	for _, p := range patterns {
		injectionPatterns = append(injectionPatterns, regexp.MustCompile(p))
	}
}

// ScanForInjection checks content for prompt injection patterns.
// Returns an error describing the matched threat if found, nil if clean.
func ScanForInjection(content string) error {
	if content == "" {
		return nil
	}

	// Check for invisible Unicode characters (zero-width spaces, etc.)
	// that can hide malicious instructions.
	for _, r := range content {
		switch {
		case r == '\u200B', // zero-width space
			r == '\u200C', // zero-width non-joiner
			r == '\u200D', // zero-width joiner
			r == '\u2060', // word joiner
			r == '\uFEFF': // zero-width no-break space (BOM)
			return fmt.Errorf("content contains invisible Unicode character U+%04X", r)
		}
	}

	// Check against injection patterns.
	for _, re := range injectionPatterns {
		if loc := re.FindStringIndex(content); loc != nil {
			matched := content[loc[0]:loc[1]]
			if len(matched) > 40 {
				matched = matched[:40] + "..."
			}
			return fmt.Errorf("content matches injection pattern: %q", matched)
		}
	}

	// Check for suspiciously high ratio of "system:" or "IMPORTANT:" prefixes
	// that might be trying to inject system-level instructions.
	lower := strings.ToLower(content)
	if strings.HasPrefix(lower, "system:") || strings.HasPrefix(lower, "important:") {
		return fmt.Errorf("content starts with reserved prefix that may be used for injection")
	}

	return nil
}
