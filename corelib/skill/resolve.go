package skill

import (
	"fmt"
	"log"
	"path/filepath"
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
	originalAction := step.Action
	step = NormalizeStepForRunner(step, skillDir)
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
						flag := filtered[i]
						if i+1 < len(filtered) {
							quotedParts = append(quotedParts, renderCLIArgPair(flag, filtered[i+1], quoteFunc)...)
						} else if strings.TrimSpace(flag) != "" {
							quotedParts = append(quotedParts, strings.TrimSpace(flag))
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
	if resolvedParams := resolveStepParams(step.Action, step.Params, bindVars, quoteFunc); resolvedParams != nil {
		resolved.Params = resolvedParams
	} else if step.Params != nil {
		resolved.Params = map[string]interface{}{}
	}
	if NormalizeStepActionName(resolved.Action) == "bash" {
		resolved.Params = appendImplicitInputArgIfNeeded(originalAction, step.Params, resolved.Params, bindVars, result.BindResult.CLIArgs, quoteFunc)
	}

	// Phase 3: craft_tool input/output injection.
	if resolved.Action == "craft_tool" && len(bindVars) != 0 {
		if resolved.Params == nil {
			resolved.Params = map[string]interface{}{}
		}
		for _, key := range []string{"input", "output", "topic", "user_prompt"} {
			if v, _ := resolved.Params[key].(string); strings.TrimSpace(v) != "" {
				continue
			}
			if value := strings.TrimSpace(resolveRunVarValueForKey(key, bindVars)); value != "" {
				resolved.Params[key] = value
			}
		}
		if v, _ := resolved.Params["user_prompt"].(string); strings.TrimSpace(v) == "" {
			if value := strings.TrimSpace(firstRunVarValue(bindVars, "input", "query", "task")); value != "" {
				resolved.Params["user_prompt"] = value
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

func firstRunVarValue(vars map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(resolveRunVarValueForKey(key, vars)); value != "" {
			return value
		}
	}
	return ""
}

// FilterConsumedCLIArgs removes CLI flag+value pairs from cliArgs when the
// corresponding param name is already referenced as a placeholder in the
// command template. This prevents double-application: if a param has both
// a CLIFlag and a template placeholder, the template substitution handles it.
func FilterConsumedCLIArgs(cliArgs []string, params []corelib.NLSkillParam, originalCmd string) []string {
	consumedFlags := make(map[string]bool)
	primaryNames := schemaPrimaryParamNames(params)
	for _, p := range params {
		if p.CLIFlag != "" && commandReferencesParamBindingName(originalCmd, p, primaryNames) {
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

func commandReferencesParamBindingName(cmd string, param corelib.NLSkillParam, primaryNames map[string]bool) bool {
	for _, name := range paramBindingNamesForSchema(param, primaryNames) {
		if CommandReferencesParam(cmd, name) {
			return true
		}
	}
	return false
}

// CommandReferencesParam checks if a command string contains a placeholder
// reference to the given param name ({{name}}, ${name}, or {name}).
func CommandReferencesParam(cmd, paramName string) bool {
	paramName = canonicalRunVarKey(paramName)
	for _, key := range ExtractPlaceholderKeys(cmd) {
		if canonicalRunVarKey(key) == paramName {
			return true
		}
	}
	return false
}

func renderCLIArgPair(flag, value string, quoteFunc func(string) string) []string {
	flag = strings.TrimSpace(flag)
	if flag == "" || value == "" {
		return nil
	}
	if quoteFunc != nil {
		value = quoteFunc(value)
	}
	if strings.HasSuffix(flag, "=") || strings.HasSuffix(flag, ":") {
		return []string{flag + value}
	}
	return []string{flag, value}
}

func resolveStepParams(action string, params map[string]interface{}, vars map[string]string, quoteFunc func(string) string) map[string]interface{} {
	if params == nil {
		return nil
	}
	resolved := make(map[string]interface{}, len(params))
	for key, item := range params {
		resolved[key] = resolveValue(item, vars, quoteFuncForStepParam(action, key, quoteFunc))
	}
	return resolved
}

func appendImplicitInputArgIfNeeded(originalAction string, originalParams, resolvedParams map[string]interface{}, vars map[string]string, cliArgs []string, quoteFunc func(string) string) map[string]interface{} {
	if len(cliArgs) > 0 || len(vars) == 0 || resolvedParams == nil {
		return resolvedParams
	}
	if !actionAcceptsImplicitInputArg(originalAction) {
		return resolvedParams
	}
	originalCmd, _ := originalParams["command"].(string)
	resolvedCmd, _ := resolvedParams["command"].(string)
	if strings.TrimSpace(originalCmd) == "" || strings.TrimSpace(resolvedCmd) == "" {
		return resolvedParams
	}
	if len(ExtractPlaceholderKeys(originalCmd)) > 0 {
		return resolvedParams
	}
	if !commandAcceptsImplicitInputArg(originalCmd) {
		return resolvedParams
	}
	input := strings.TrimSpace(resolveRunVarValueForKey("input", vars))
	if input == "" {
		return resolvedParams
	}
	if quoteFunc != nil {
		input = quoteFunc(input)
	}
	cp := copyParams(resolvedParams)
	cp["command"] = strings.TrimRight(resolvedCmd, " \t\r\n") + " " + input
	return cp
}

func actionAcceptsImplicitInputArg(action string) bool {
	switch NormalizeStepActionName(action) {
	case "", "command", "run", "exec", "execute", "cmd", "script", "node", "js", "javascript", "python", "python3":
		return true
	default:
		return false
	}
}

func commandAcceptsImplicitInputArg(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, "\r\n;&|<>") {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	exe := strings.Trim(strings.ToLower(filepath.Base(fields[0])), `"'`)
	exe = strings.TrimSuffix(exe, ".exe")
	switch exe {
	case "node", "python", "python3", "deno", "bun", "tsx", "ts-node":
		for _, field := range fields[1:] {
			if scriptPathAcceptsImplicitInput(field) {
				return true
			}
		}
		return false
	default:
		return scriptPathAcceptsImplicitInput(fields[0])
	}
}

func scriptPathAcceptsImplicitInput(path string) bool {
	path = strings.Trim(strings.ToLower(path), `"'`)
	switch filepath.Ext(path) {
	case ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".py":
		return true
	default:
		return false
	}
}

func quoteFuncForStepParam(action, key string, quoteFunc func(string) string) func(string) string {
	if quoteFunc == nil {
		return nil
	}
	if NormalizeStepActionName(action) != "bash" {
		return nil
	}
	switch canonicalRunVarKey(key) {
	case "command":
		return quoteFunc
	default:
		return nil
	}
}

func resolveRunVarValueForKey(key string, vars map[string]string) string {
	key = canonicalRunVarKey(key)
	if key == "" {
		return ""
	}
	if value, ok := lookupCanonicalVar(vars, key); ok && strings.TrimSpace(value) != "" {
		return value
	}
	param := corelib.NLSkillParam{Name: key}
	if aliases, ok := commonParamAliases[key]; ok {
		param.Aliases = aliases
	}
	for _, alias := range runParamInferenceNames(param) {
		if alias == key {
			continue
		}
		if value, ok := lookupCanonicalVar(vars, alias); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// resolveValue recursively substitutes placeholders in a params value. The
// quote function must already be scoped by caller context; shell quoting should
// only be applied to shell command strings, not structured tool arguments.
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
	return SubstituteVariablesWithQuote(command, vars, quoteFunc)
}

func replaceCanonicalPlaceholder(command, key, value string, stripSurroundingQuotes bool) string {
	target := canonicalRunVarKey(key)
	if target == "" || command == "" {
		return command
	}

	matches := placeholderRe.FindAllStringIndex(command, -1)
	if len(matches) == 0 {
		return command
	}

	var builder strings.Builder
	last := 0
	changed := false
	for _, match := range matches {
		start, end := match[0], match[1]
		if canonicalRunVarKey(placeholderKeyFromMatch(command[start:end])) != target {
			continue
		}

		replaceStart, replaceEnd := start, end
		if stripSurroundingQuotes && start > 0 && end < len(command) {
			before, after := command[start-1], command[end]
			if (before == '"' && after == '"') ||
				(before == '\'' && after == '\'') ||
				(before == '`' && after == '`') {
				replaceStart = start - 1
				replaceEnd = end + 1
			}
		}

		builder.WriteString(command[last:replaceStart])
		builder.WriteString(value)
		last = replaceEnd
		changed = true
	}
	if !changed {
		return command
	}
	builder.WriteString(command[last:])
	return builder.String()
}

func placeholderKeyFromMatch(match string) string {
	match = strings.TrimSpace(match)
	if strings.HasPrefix(match, "{{") && strings.HasSuffix(match, "}}") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
	}
	if strings.HasPrefix(match, "${") && strings.HasSuffix(match, "}") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}"))
	}
	if strings.HasPrefix(match, "{") && strings.HasSuffix(match, "}") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}"))
	}
	return ""
}

// copyParams creates a shallow copy of a params map.
func copyParams(params map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(params))
	for k, v := range params {
		cp[k] = v
	}
	return cp
}
