package skill

import (
	"fmt"
	"regexp"
	"strings"
)

// placeholderRe matches {{key}}, ${key}, and {key} placeholders in command strings.
// Used by SubstituteVariables and SynthesizePlaceholders.
var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}|\$\{(\w+)\}|\{(\w+)\}`)

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
		result = strings.ReplaceAll(result, "{{"+key+"}}", value)
		result = strings.ReplaceAll(result, "${"+key+"}", value)
		result = strings.ReplaceAll(result, "{"+key+"}", value)
	}
	return result
}

// StripUnresolvedPlaceholders removes any remaining {{key}}, ${key}, {key}
// placeholders from a command string. Called after SubstituteVariables to
// clean up optional parameters that were not provided.
//
// Note: This should only be called after BindParams has validated required
// parameters. Any required placeholder reaching this point indicates a bug
// in the calling code.
func StripUnresolvedPlaceholders(command string) string {
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
		if key != "" && !seen[key] && key != "baseDir" && key != "base_dir" {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
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

func placeholderFirstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
