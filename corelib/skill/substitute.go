package skill

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// placeholderRe matches {{key}}, ${key}, and {key} placeholders in command strings.
// Used by SubstituteVariables and SynthesizePlaceholders.
var placeholderRe = regexp.MustCompile(`\{\{([A-Za-z0-9_. -]+)\}\}|\$\{([A-Za-z0-9_. -]+)\}|\{([A-Za-z0-9_. -]+)\}`)

const unresolvedPlaceholderValuePattern = `(?:\{\{[A-Za-z0-9_. -]+\}\}|\$\{[A-Za-z0-9_. -]+\}|\{[A-Za-z0-9_. -]+\})`

var optionalCLIPlaceholderArgRe = regexp.MustCompile(
	`(^|\s+)-{1,2}[A-Za-z0-9][A-Za-z0-9_.-]*(?:(?:=|:|\s+)(?:"` + unresolvedPlaceholderValuePattern + `"|'` + unresolvedPlaceholderValuePattern + `'|` + unresolvedPlaceholderValuePattern + `))`,
)

var optionalSlashPlaceholderArgRe = regexp.MustCompile(
	`(^|\s+)/[A-Za-z0-9][A-Za-z0-9_.-]*(?:(?:=|:|\s+)(?:"` + unresolvedPlaceholderValuePattern + `"|'` + unresolvedPlaceholderValuePattern + `'|` + unresolvedPlaceholderValuePattern + `))`,
)

const unresolvedQuotedPlaceholderPattern = `"` + unresolvedPlaceholderValuePattern + `"|'` + unresolvedPlaceholderValuePattern + `'|` + "`" + unresolvedPlaceholderValuePattern + "`"

var unresolvedQuotedPlaceholderTokenRe = regexp.MustCompile(
	`(^|\s+)(?:` + unresolvedQuotedPlaceholderPattern + `)`,
)

// SubstituteVariables replaces {{key}}, ${key}, and {key} placeholders in
// a command string with values from the vars map. This is the shared
// implementation used by both GUI and TUI skill runners.
//
// Keys not found in vars are left unchanged (caller should use
// StripUnresolvedPlaceholders afterward if desired).
func SubstituteVariables(command string, vars map[string]string) string {
	if command == "" || len(vars) == 0 {
		return command
	}
	result := command
	for key, value := range vars {
		result = replaceCanonicalPlaceholderLiteral(result, key, value)
	}
	return result
}

func replaceCanonicalPlaceholderLiteral(command, key, value string) string {
	target := canonicalRunVarKey(key)
	if target == "" || command == "" {
		return command
	}
	return placeholderRe.ReplaceAllStringFunc(command, func(match string) string {
		if canonicalRunVarKey(placeholderKeyFromMatch(match)) != target {
			return match
		}
		return value
	})
}

// SubstituteVariablesWithQuote replaces placeholders with optionally quoted
// values, de-duplicates author-provided quotes when quoteFunc is present, and
// strips unresolved optional placeholders. This is the shared command-template
// substitution path for runners that embed user values into shell strings.
func SubstituteVariablesWithQuote(command string, vars map[string]string, quoteFunc func(string) string) string {
	if command == "" || len(vars) == 0 {
		return StripUnresolvedPlaceholders(command)
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := vars[key]
		if quoteFunc != nil {
			value = quoteFunc(value)
		}
		command = replaceCanonicalPlaceholder(command, key, value, quoteFunc != nil)
	}
	return StripUnresolvedPlaceholders(command)
}

// StripUnresolvedPlaceholders removes any remaining {{key}}, ${key}, {key}
// placeholders from a command string. Called after SubstituteVariables to
// clean up optional parameters that were not provided.
//
// Note: This should only be called after BindParams has validated required
// parameters. Any required placeholder reaching this point indicates a bug
// in the calling code.
func StripUnresolvedPlaceholders(command string) string {
	command = optionalCLIPlaceholderArgRe.ReplaceAllString(command, "$1")
	command = optionalSlashPlaceholderArgRe.ReplaceAllString(command, "$1")
	command = unresolvedQuotedPlaceholderTokenRe.ReplaceAllString(command, "$1")
	return placeholderRe.ReplaceAllString(command, "")
}

// ExtractPlaceholderKeys returns all unique placeholder keys found in a
// command string. Used by SynthesizeParams to auto-generate parameter
// schemas from command templates.
func ExtractPlaceholderKeys(command string) []string {
	matches := placeholderRe.FindAllStringSubmatch(command, -1)
	seen := make(map[string]bool)
	var keys []string
	for _, m := range matches {
		// m[1] = {{key}}, m[2] = ${key}, m[3] = {key}
		key := placeholderFirstNonEmpty(m[1], m[2], m[3])
		if key != "" && !seen[key] && !isBaseDirPlaceholder(key) {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

func isBaseDirPlaceholder(key string) bool {
	key = strings.ReplaceAll(canonicalRunVarKey(key), "_", "")
	return key == "basedir"
}

// QuoteForShell wraps a value in double quotes with proper escaping for
// safe embedding in shell commands. Returns the value unquoted if it
// contains no special characters.
//
// Note: This is a platform-agnostic utility. The GUI's quoteSkillInputForShell
// provides platform-specific quoting (single quotes on Unix, double quotes +
// percent escaping on Windows) and should be preferred in execution paths.
func QuoteForShell(value string) string {
	if value == "" {
		return `""`
	}
	// If value contains no special characters, return as-is.
	if !strings.ContainsAny(value, " \t\n\"'`$\\!#&|;(){}[]<>?*~") {
		return value
	}
	// Use double quotes (works on both Windows and Unix).
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return fmt.Sprintf(`"%s"`, escaped)
}

// QuoteForRunnerShell wraps a user-supplied value for embedding into the
// runner's shell command string. Windows uses double quotes because cmd.exe and
// PowerShell do not treat single quotes consistently for all child processes;
// POSIX shells use single quotes.
func QuoteForRunnerShell(value string) string {
	if value == "" {
		if runtime.GOOS == "windows" {
			return `""`
		}
		return "''"
	}
	if runtime.GOOS == "windows" {
		escaped := strings.ReplaceAll(value, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, `%`, `%%`)
		return `"` + escaped + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// QuoteForShellPreference quotes a value for a declared shell. Empty
// preference uses the runner default (cmd-style on Windows, POSIX elsewhere).
func QuoteForShellPreference(value, preferredShell string) string {
	switch normalizeShellPreference(preferredShell) {
	case "powershell":
		if value == "" {
			return `""`
		}
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	case "bash":
		if value == "" {
			return "''"
		}
		return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
	case "cmd":
		return QuoteForRunnerShell(value)
	default:
		return QuoteForRunnerShell(value)
	}
}

func normalizeShellPreference(preferredShell string) string {
	switch strings.ToLower(strings.TrimSpace(preferredShell)) {
	case "cmd", "cmd.exe", "windows", "win_cmd":
		return "cmd"
	case "powershell", "pwsh", "ps", "ps1":
		return "powershell"
	case "bash", "sh", "zsh":
		return "bash"
	default:
		return ""
	}
}

func placeholderFirstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
