package skill

import (
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ResolveStepResult holds the outcome of step resolution.
type ResolveStepResult struct {
	// Step is the resolved step with all placeholders substituted.
	Step corelib.NLSkillStep

	// BindResult contains the parameter binding outcome (warnings, etc.).
	BindResult BindResult
}

// ResolveStep performs parameter binding and template substitution on a skill
// step. This is the shared implementation used by both GUI and TUI runners.
//
// The resolution process:
//  1. BindParams: alias resolution, defaults, required validation.
//  2. Template substitution: replace placeholders in all step.Params values.
//  3. CLI args appending: for explicit schema params with CLIFlag.
//  4. craft_tool input/output injection: for craft_tool steps.
//  5. working_dir resolution: relative paths resolved against skillDir.
//
// The quoteFunc parameter controls how values are quoted for shell safety.
// GUI passes its platform-aware quoteSkillInputForShell; TUI can pass a
// simpler implementation or nil for no quoting (when the execution layer
// handles quoting separately).
func ResolveStep(
	step corelib.NLSkillStep,
	vars map[string]string,
	skillDir string,
	params []corelib.NLSkillParam,
	quoteFunc func(string) string,
) (ResolveStepResult, error) {
	result := ResolveStepResult{Step: step}

	// Phase 1: Parameter binding.
	bindVars := vars
	if len(params) > 0 {
		result.BindResult = BindParams(params, vars)
		for _, w := range result.BindResult.Warnings {
			log.Printf("[skill-resolve] param bind warning: %s", w)
		}
		if result.BindResult.HasErrors() {
			return result, fmt.Errorf("参数绑定失败: %s", result.BindResult.ErrorString())
		}
		// Merge resolved vars: canonical names override aliases, but keep
		// any vars not consumed by params (e.g., captured vars from previous steps).
		bindVars = make(map[string]string, len(result.BindResult.ResolvedVars)+len(vars))
		for k, v := range vars {
			bindVars[k] = v
		}
		for k, v := range result.BindResult.ResolvedVars {
			bindVars[k] = v
		}

		// Phase 1b: CLI args appending (only for explicit schema with CLIFlag).
		if len(result.BindResult.CLIArgs) > 0 {
			if cmd, ok := step.Params["command"].(string); ok && cmd != "" {
				filtered := FilterConsumedCLIArgs(result.BindResult.CLIArgs, params, cmd)
				if len(filtered) > 0 {
					cp := copyParams(step.Params)
					var quotedParts []string
					for i := 0; i < len(filtered); i += 2 {
						quotedParts = append(quotedParts, filtered[i]) // flag
						if i+1 < len(filtered) {
							val := filtered[i+1]
							if quoteFunc != nil {
								val = quoteFunc(val)
							}
							quotedParts = append(quotedParts, val)
						}
					}
					cp["command"] = cmd + " " + strings.Join(quotedParts, " ")
					step.Params = cp
				}
			}
		}
	}

	// Phase 2: Template substitution.
	resolved := step
	if resolvedParams := resolveValue(step.Params, bindVars, quoteFunc); resolvedParams != nil {
		if m, ok := resolvedParams.(map[string]interface{}); ok {
			resolved.Params = m
		}
	} else if step.Params != nil {
		resolved.Params = map[string]interface{}{}
	}

	// Phase 3: craft_tool input/output injection.
	if resolved.Action == "craft_tool" && len(bindVars) != 0 {
		if resolved.Params == nil {
			resolved.Params = map[string]interface{}{}
		}
		for _, key := range []string{"input", "output", "topic"} {
			if v, _ := resolved.Params[key].(string); strings.TrimSpace(v) != "" {
				continue
			}
			if value := strings.TrimSpace(bindVars[key]); value != "" {
				resolved.Params[key] = value
			}
		}
	}

	// Phase 4: working_dir resolution.
	if workDir, _ := resolved.Params["working_dir"].(string); workDir != "" && !filepath.IsAbs(workDir) && skillDir != "" {
		resolved.Params["working_dir"] = filepath.Clean(filepath.Join(skillDir, workDir))
	}

	result.Step = resolved
	return result, nil
}

// FilterConsumedCLIArgs removes CLI flag+value pairs from cliArgs when the
// corresponding param name is already referenced as a placeholder in the
// command template. This prevents double-application: if a param has both
// a CLIFlag and a template placeholder, the template substitution handles it.
func FilterConsumedCLIArgs(cliArgs []string, params []corelib.NLSkillParam, originalCmd string) []string {
	consumedFlags := make(map[string]bool)
	for _, p := range params {
		if p.CLIFlag != "" && CommandReferencesParam(originalCmd, p.Name) {
			consumedFlags[p.CLIFlag] = true
		}
	}
	if len(consumedFlags) == 0 {
		return cliArgs
	}
	var filtered []string
	for i := 0; i < len(cliArgs); i += 2 {
		if i+1 < len(cliArgs) && !consumedFlags[cliArgs[i]] {
			filtered = append(filtered, cliArgs[i], cliArgs[i+1])
		}
	}
	return filtered
}

// CommandReferencesParam checks if a command string contains a placeholder
// reference to the given param name ({{name}}, ${name}, or {name}).
func CommandReferencesParam(cmd, paramName string) bool {
	return strings.Contains(cmd, "{{"+paramName+"}}") ||
		strings.Contains(cmd, "${"+paramName+"}") ||
		strings.Contains(cmd, "{"+paramName+"}")
}

// resolveValue recursively substitutes placeholders in all string values
// within a params structure (map, slice, or string).
func resolveValue(value interface{}, vars map[string]string, quoteFunc func(string) string) interface{} {
	switch typed := value.(type) {
	case string:
		return substituteWithQuote(typed, vars, quoteFunc)
	case map[string]interface{}:
		resolved := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			resolved[key] = resolveValue(item, vars, quoteFunc)
		}
		return resolved
	case []interface{}:
		resolved := make([]interface{}, len(typed))
		for i, item := range typed {
			resolved[i] = resolveValue(item, vars, quoteFunc)
		}
		return resolved
	default:
		return value
	}
}

// substituteWithQuote replaces placeholders in a string, optionally quoting
// values for shell safety. When quoteFunc is nil, values are inserted as-is.
// After substitution, any remaining unresolved placeholders are stripped
// (they represent optional parameters that were not provided).
func substituteWithQuote(command string, vars map[string]string, quoteFunc func(string) string) string {
	if command == "" || len(vars) == 0 {
		return StripUnresolvedPlaceholders(command)
	}
	// Sort keys to ensure deterministic replacement order. This prevents
	// edge cases where {key} could partially match inside {{key_longer}}
	// if processed in the wrong order.
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := vars[key]
		quoted := value
		if quoteFunc != nil {
			quoted = quoteFunc(value)
		}
		// Replace quoted-placeholder patterns first (e.g. "{{key}}" → value),
		// then replace bare placeholders ({{key}} → value).
		// This avoids double-quoting when SKILL.md authors wrap placeholders
		// in quotes that quoteFunc would also add.
		for _, placeholder := range []string{"{{" + key + "}}", "${" + key + "}", "{" + key + "}"} {
			doubleQuoted := `"` + placeholder + `"`
			singleQuoted := `'` + placeholder + `'`
			command = strings.ReplaceAll(command, doubleQuoted, quoted)
			command = strings.ReplaceAll(command, singleQuoted, quoted)
			command = strings.ReplaceAll(command, placeholder, quoted)
		}
	}
	return StripUnresolvedPlaceholders(command)
}

// copyParams creates a shallow copy of a params map.
func copyParams(params map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(params))
	for k, v := range params {
		cp[k] = v
	}
	return cp
}
