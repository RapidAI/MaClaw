package skill

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// NormalizeSkillForRunner rewrites community skill shapes into the small set of
// actions and parameter names that the runners execute natively. It is
// intentionally conservative: known aliases are normalized, unknown fields are
// preserved, and unknown actions are left untouched so callers still get a
// precise runner error instead of a silent behavior change.
func NormalizeSkillForRunner(skill *corelib.NLSkillEntry) {
	if skill == nil || len(skill.Steps) == 0 {
		return
	}
	for i := range skill.Steps {
		skill.Steps[i] = NormalizeStepForRunner(skill.Steps[i], skill.SkillDir)
	}
}

// NormalizeStepForRunnerCopy adapts a single step without mutating caller-owned
// map fields. Use this for read-only prechecks; execution paths can call
// NormalizeStepForRunner directly when they intentionally solidify the step.
func NormalizeStepForRunnerCopy(step corelib.NLSkillStep, skillDir string) corelib.NLSkillStep {
	if step.Params != nil {
		params := make(map[string]interface{}, len(step.Params))
		for key, value := range step.Params {
			params[key] = value
		}
		step.Params = params
	}
	if step.Capture != nil {
		capture := make(map[string]string, len(step.Capture))
		for key, value := range step.Capture {
			capture[key] = value
		}
		step.Capture = capture
	}
	return NormalizeStepForRunner(step, skillDir)
}

// NormalizeStepForRunner adapts a single step to the runner contract.
func NormalizeStepForRunner(step corelib.NLSkillStep, skillDir string) corelib.NLSkillStep {
	step.Action = normalizeActionName(step.Action)
	if step.Params == nil {
		step.Params = map[string]interface{}{}
	}
	normalizeStructuredCommandParams(step.Params)
	normalizeCommonParamAliases(step.Params)

	switch step.Action {
	case "", "run", "exec", "execute", "command", "shell", "sh", "cmd", "script":
		if command := firstStringParam(step.Params, "command", "cmd", "run", "script"); command != "" {
			step.Action = "bash"
			step.Params["command"] = normalizeScriptReference(command, skillDir)
		} else if runtime := normalizeRuntimeName(firstStringParam(step.Params, "language", "lang", "runtime", "interpreter")); runtime != "" && firstStringParam(step.Params, "code", "source") != "" {
			step.Action = "bash"
			step.Params["command"] = languageCommand(runtime, step.Params, skillDir)
		} else if hasCraftInstructions(step.Params) {
			step.Action = "craft_tool"
			normalizeCraftToolParams(step.Params)
		}
	case "python", "python3":
		step.Action = "bash"
		step.Params["command"] = languageCommand("python", step.Params, skillDir)
	case "node", "js", "javascript":
		step.Action = "bash"
		step.Params["command"] = languageCommand("node", step.Params, skillDir)
	case "powershell", "pwsh":
		step.Action = "bash"
		step.Params["command"] = languageCommand("powershell", step.Params, skillDir)
	case "bash":
		if command := firstStringParam(step.Params, "command", "cmd", "run", "script"); command != "" {
			step.Params["command"] = normalizeScriptReference(command, skillDir)
		}
	case "craft", "llm", "agent", "prompt", "instructions":
		step.Action = "craft_tool"
		normalizeCraftToolParams(step.Params)
	case "mcp", "tool", "call_tool":
		step.Action = "call_mcp_tool"
		normalizeMCPToolParams(step.Params)
	case "craft_tool":
		normalizeCraftToolParams(step.Params)
	case "call_mcp_tool":
		normalizeMCPToolParams(step.Params)
	}

	step.OnError = normalizeOnErrorPolicy(step.OnError)
	return step
}

func normalizeOnErrorPolicy(policy string) string {
	switch normalizeActionName(policy) {
	case "", "stop", "fail", "abort", "halt", "error":
		return "stop"
	case "continue", "ignore", "ignored", "warn", "warning", "skip_on_error", "continue_on_error":
		return "continue"
	case "skip":
		return "skip"
	default:
		return policy
	}
}

func normalizeActionName(action string) string {
	return NormalizeStepActionName(action)
}

func normalizeCommonParamAliases(params map[string]interface{}) {
	normalizeShellPreferenceParam(params)
	copyFirstAlias(params, "working_dir", "cwd", "workdir", "dir")
	copyFirstAlias(params, "timeout", "timeout_seconds", "timeout_sec")
	copyFirstAlias(params, "timeout_seconds", "timeout", "timeout_sec")
	copyFirstAlias(params, "interval_seconds", "interval", "poll_interval")
	copyFirstAlias(params, "success_pattern", "until_match", "match", "pattern")
	copyFirstAlias(params, "command", "cmd", "run", "shell_command")
	copyFirstAlias(params, "extra_env", "env", "environment")
	copyFirstAlias(params, "required_env", "requires_env", "required_environment")
	normalizeNumericParams(params, "timeout", "timeout_seconds", "interval_seconds")
	normalizeStringListParam(params, "required_env")
	normalizeStringMapParam(params, "extra_env")
}

func normalizeStructuredCommandParams(params map[string]interface{}) {
	for _, key := range []string{"command", "cmd", "run", "script", "shell_command"} {
		if raw, ok := params[key]; ok {
			if command, ok := commandValueString(raw); ok {
				params[key] = command
			}
		}
	}
	if raw, ok := params["args"]; ok {
		switch raw.(type) {
		case string, []string, []interface{}:
		default:
			if args, ok := commandArgsString(raw); ok {
				params["args"] = args
			}
		}
	}
}

func commandValueString(raw interface{}) (string, bool) {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	case []string:
		return quoteCommandParts(v), true
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				parts = append(parts, s)
			}
		}
		return quoteCommandParts(parts), true
	case map[string]interface{}:
		return commandMapString(v)
	case map[string]string:
		converted := make(map[string]interface{}, len(v))
		for key, value := range v {
			converted[key] = value
		}
		return commandMapString(converted)
	default:
		return "", false
	}
}

func commandMapString(m map[string]interface{}) (string, bool) {
	program := firstStringParam(m, "program", "cmd", "command", "executable", "binary")
	if program == "" {
		return "", false
	}
	parts := []string{program}
	if args, ok := commandArgsString(firstNonNil(m["args"], m["argv"], m["arguments"])); ok && args != "" {
		parts = append(parts, args)
	}
	return strings.Join(parts, " "), true
}

func commandArgsString(raw interface{}) (string, bool) {
	switch v := raw.(type) {
	case nil:
		return "", true
	case string:
		return strings.TrimSpace(v), true
	case []string:
		return quoteCommandParts(v), true
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				parts = append(parts, s)
			}
		}
		return quoteCommandParts(parts), true
	default:
		return "", false
	}
}

func quoteCommandParts(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		quoted = append(quoted, QuoteForShell(part))
	}
	return strings.Join(quoted, " ")
}

func normalizeShellPreferenceParam(params map[string]interface{}) {
	shell := firstStringParam(params, "preferred_shell", "shell", "shell_name")
	if shell == "" {
		return
	}
	if normalized, ok := normalizePreferredShellName(shell); ok {
		if !hasNonEmptyParam(params, "preferred_shell") {
			params["preferred_shell"] = normalized
		}
		return
	}
	if !hasNonEmptyParam(params, "command") {
		if shellValue, _ := params["shell"].(string); strings.TrimSpace(shellValue) != "" {
			params["command"] = shellValue
		}
	}
}

func normalizePreferredShellName(shell string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "bash", "sh", "zsh":
		return "bash", true
	case "cmd", "cmd.exe", "windows", "win_cmd":
		return "cmd", true
	case "powershell", "pwsh", "ps", "ps1":
		return "powershell", true
	default:
		return "", false
	}
}

func normalizeCraftToolParams(params map[string]interface{}) {
	copyFirstAlias(params, "instructions", "instruction", "prompt", "task", "text")
	copyFirstAlias(params, "task", "prompt", "text")
	if _, ok := params["verification_mode"]; !ok {
		params["verification_mode"] = "artifact_optional"
	}
	if _, ok := params["register_policy"]; !ok {
		params["register_policy"] = "manual"
	}
}

func normalizeMCPToolParams(params map[string]interface{}) {
	normalizeMCPUsesParam(params)
	copyFirstAlias(params, "server_id", "server", "mcp_server", "server_name")
	copyFirstAlias(params, "tool_name", "tool", "name")
	copyFirstAlias(params, "arguments", "args", "input", "params")
	if !hasNonEmptyParam(params, "arguments") {
		arguments := map[string]interface{}{}
		for key, value := range params {
			if isMCPControlParam(key) || value == nil {
				continue
			}
			arguments[key] = value
		}
		if len(arguments) > 0 {
			params["arguments"] = arguments
		}
	}
}

func normalizeMCPUsesParam(params map[string]interface{}) {
	uses := firstStringParam(params, "uses", "use")
	if uses == "" {
		return
	}
	for _, sep := range []string{"/", ".", ":"} {
		if before, after, ok := strings.Cut(uses, sep); ok {
			if !hasNonEmptyParam(params, "server_id") {
				params["server_id"] = strings.TrimSpace(before)
			}
			if !hasNonEmptyParam(params, "tool_name") {
				params["tool_name"] = strings.TrimSpace(after)
			}
			return
		}
	}
	if !hasNonEmptyParam(params, "tool_name") {
		params["tool_name"] = uses
	}
}

func isMCPControlParam(key string) bool {
	switch key {
	case "uses", "use", "server", "server_id", "mcp_server", "server_name", "tool", "tool_name", "name", "arguments", "args", "input", "params":
		return true
	default:
		return false
	}
}

func copyFirstAlias(params map[string]interface{}, canonical string, aliases ...string) {
	if hasNonEmptyParam(params, canonical) {
		return
	}
	for _, alias := range aliases {
		if hasNonEmptyParam(params, alias) {
			params[canonical] = params[alias]
			return
		}
	}
}

func hasNonEmptyParam(params map[string]interface{}, key string) bool {
	v, ok := params[key]
	if !ok || v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func firstStringParam(params map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := params[key]; ok {
			if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeNumericParam(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func normalizeNumericParams(params map[string]interface{}, keys ...string) {
	for _, key := range keys {
		if v, ok := normalizeNumericParam(params[key]); ok {
			params[key] = v
		}
	}
}

func normalizeStringListParam(params map[string]interface{}, key string) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return
	}
	switch v := raw.(type) {
	case []interface{}:
		return
	case []string:
		items := make([]interface{}, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(item); s != "" {
				items = append(items, s)
			}
		}
		params[key] = items
	case string:
		var items []interface{}
		for _, item := range splitCSV(v) {
			items = append(items, item)
		}
		params[key] = items
	}
}

func normalizeStringMapParam(params map[string]interface{}, key string) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		return
	case map[string]string:
		converted := make(map[string]interface{}, len(v))
		for k, value := range v {
			if strings.TrimSpace(k) != "" {
				converted[k] = value
			}
		}
		params[key] = converted
	case string:
		if converted := envAssignmentsToMap(splitCSV(v)); len(converted) > 0 {
			params[key] = converted
		}
	case []string:
		if converted := envAssignmentsToMap(v); len(converted) > 0 {
			params[key] = converted
		}
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, fmt.Sprintf("%v", item))
		}
		if converted := envAssignmentsToMap(items); len(converted) > 0 {
			params[key] = converted
		}
	}
}

func envAssignmentsToMap(items []string) map[string]interface{} {
	converted := map[string]interface{}{}
	for _, item := range items {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		converted[key] = strings.TrimSpace(value)
	}
	return converted
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func hasCraftInstructions(params map[string]interface{}) bool {
	return firstStringParam(params, "instructions", "instruction", "prompt", "task", "text") != ""
}

func normalizeRuntimeName(runtime string) string {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "py", "python", "python3":
		return "python"
	case "js", "javascript", "node", "nodejs":
		return "node"
	case "ps", "ps1", "pwsh", "powershell":
		return "powershell"
	default:
		return ""
	}
}

func languageCommand(runtime string, params map[string]interface{}, skillDir string) string {
	runtime = normalizeRuntimeName(runtime)
	command := firstStringParam(params, "command", "cmd", "run")
	if command == "" {
		command = firstStringParam(params, "script", "file", "path")
	}
	command = normalizeScriptReference(command, skillDir)
	if command == "" {
		code := firstStringParam(params, "code", "source")
		if code == "" {
			return runtime
		}
		switch runtime {
		case "python":
			return "python -c " + QuoteForShell(code)
		case "node":
			return "node -e " + QuoteForShell(code)
		case "powershell":
			return "powershell -NoProfile -ExecutionPolicy Bypass -Command " + QuoteForShell(code)
		}
	}
	if !commandStartsWithRuntime(command, runtime) {
		switch runtime {
		case "powershell":
			command = "powershell -NoProfile -ExecutionPolicy Bypass -File " + QuoteForShell(command)
		default:
			command = runtime + " " + QuoteForShell(command)
		}
	}
	if args := shellArgsParam(params["args"]); args != "" {
		command += " " + args
	}
	return command
}

func shellArgsParam(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []string:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(item); s != "" {
				parts = append(parts, QuoteForShell(s))
			}
		}
		return strings.Join(parts, " ")
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				parts = append(parts, QuoteForShell(s))
			}
		}
		return strings.Join(parts, " ")
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func normalizeScriptReference(command, skillDir string) string {
	command = strings.TrimSpace(command)
	if command != "" && skillDir != "" {
		command = strings.TrimSpace(resolveBaseDirInBlock(command, skillDir))
		command = quoteResolvedLocalScriptReferences(command, skillDir)
	}
	if command == "" || skillDir == "" {
		return command
	}
	if filepath.IsAbs(command) {
		return command
	}
	if !strings.ContainsAny(command, " \t\r\n\"'") {
		if shouldResolveRelativeScript(command) {
			return filepath.Clean(filepath.Join(skillDir, command))
		}
		return command
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return command
	}
	first := strings.Trim(fields[0], "\"'`")
	if first == "" || filepath.IsAbs(first) || !shouldResolveRelativeScript(first) {
		return command
	}
	resolved := filepath.Clean(filepath.Join(skillDir, first))
	return strings.Replace(command, fields[0], QuoteForShell(resolved), 1)
}

func quoteResolvedLocalScriptReferences(command, skillDir string) string {
	slashDir := filepath.ToSlash(strings.TrimRight(skillDir, `/\`))
	if command == "" || slashDir == "" || !strings.Contains(command, slashDir) {
		return command
	}
	var out strings.Builder
	last := 0
	pos := 0
	changed := false
	for {
		idxRel := strings.Index(command[pos:], slashDir)
		if idxRel < 0 {
			break
		}
		idx := pos + idxRel
		if idx > 0 && isShellQuote(command[idx-1]) {
			pos = idx + len(slashDir)
			continue
		}
		if idx > 0 && !isCommandPathBoundary(command[idx-1]) {
			pos = idx + len(slashDir)
			continue
		}
		end := idx + len(slashDir)
		for end < len(command) && !isCommandPathTerminator(command[end]) {
			end++
		}
		candidate := command[idx:end]
		if !isScriptFileName(filepath.Base(candidate)) {
			pos = idx + len(slashDir)
			continue
		}
		out.WriteString(command[last:idx])
		out.WriteString(QuoteForShell(candidate))
		last = end
		pos = end
		changed = true
	}
	if !changed {
		return command
	}
	out.WriteString(command[last:])
	return out.String()
}

func isShellQuote(ch byte) bool {
	return ch == '"' || ch == '\'' || ch == '`'
}

func isCommandPathBoundary(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '=' || ch == '(' || ch == ','
}

func isCommandPathTerminator(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '"' || ch == '\'' || ch == '`' || ch == ')' || ch == ','
}

func shouldResolveRelativeScript(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || strings.HasPrefix(path, `scripts/`) || strings.HasPrefix(path, `scripts\`) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py", ".js", ".mjs", ".cjs", ".sh", ".ps1", ".bat", ".cmd", ".rb", ".pl":
		return strings.ContainsAny(path, `/\`)
	}
	return false
}

func commandStartsWithRuntime(command, runtime string) bool {
	lower := strings.TrimSpace(strings.ToLower(command))
	if lower == runtime || strings.HasPrefix(lower, runtime+" ") {
		return true
	}
	if runtime == "python" && (lower == "python3" || strings.HasPrefix(lower, "python3 ")) {
		return true
	}
	if runtime == "powershell" && (lower == "pwsh" || strings.HasPrefix(lower, "pwsh ")) {
		return true
	}
	return false
}
