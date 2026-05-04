package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

func NormalizeRunVars(runArgs map[string]interface{}) map[string]string {
	if len(runArgs) == 0 {
		return nil
	}
	vars := map[string]string{}
	mergeRunVarMap(vars, runArgs["args"], true)
	mergeRunVarJSON(vars, runArgs["args"], true)
	for _, key := range RunVarFallbackKeys {
		raw, ok := runArgs[key]
		if !ok || raw == nil {
			continue
		}
		if key == "input" || key == "output" {
			mergeRunVarMap(vars, raw, false)
			mergeRunVarJSON(vars, raw, false)
		}
		if _, exists := vars[key]; exists {
			continue
		}
		if value, ok := runVarString(raw); ok {
			vars[key] = value
		}
	}
	for key, raw := range runArgs {
		if isRunControlKey(key) {
			continue
		}
		if _, exists := vars[key]; exists {
			continue
		}
		if value, ok := runVarString(raw); ok {
			vars[key] = value
		}
	}
	if len(vars) == 0 {
		return nil
	}
	return vars
}

func mergeRunVarMap(vars map[string]string, raw interface{}, overwrite bool) {
	add := func(key string, value interface{}) {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			return
		}
		if !overwrite {
			if _, exists := vars[key]; exists {
				return
			}
		}
		if s, ok := runVarString(value); ok {
			vars[key] = s
		}
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		for key, value := range v {
			add(key, value)
		}
	case map[string]string:
		for key, value := range v {
			add(key, value)
		}
	}
}

func mergeRunVarJSON(vars map[string]string, raw interface{}, overwrite bool) {
	s, ok := raw.(string)
	if !ok {
		return
	}
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' {
		return
	}
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(s), &parsed) != nil {
		return
	}
	mergeRunVarMap(vars, parsed, overwrite)
}

func runVarString(value interface{}) (string, bool) {
	if value == nil {
		return "", false
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false
		}
		return v, true
	case map[string]interface{}, []interface{}, map[string]string, []string:
		data, err := json.Marshal(v)
		if err != nil || len(data) == 0 {
			return "", false
		}
		return string(data), true
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s == "" {
			return "", false
		}
		return s, true
	}
}

func isRunControlKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "action", "name", "skill", "skill_name", "qualified_name", "args", "env", "steps", "step", "dry_run":
		return true
	default:
		return false
	}
}

func ExtractRunExtraEnv(raw interface{}) map[string]string {
	result := map[string]string{}
	add := func(key, value string) {
		key = strings.TrimSpace(key)
		if key != "" {
			result[key] = value
		}
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if value != nil {
				add(key, fmt.Sprintf("%v", value))
			}
		}
	case map[string]string:
		for key, value := range v {
			add(key, value)
		}
	case []interface{}:
		for _, item := range v {
			mergeEnvItem(result, item)
		}
	case []string:
		for _, item := range v {
			mergeEnvItem(result, item)
		}
	case string:
		if mergeEnvJSON(result, v) {
			break
		}
		for _, item := range strings.Split(v, ",") {
			mergeEnvItem(result, item)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func mergeEnvJSON(result map[string]string, raw string) bool {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '{' {
		return false
	}
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return false
	}
	for key, value := range parsed {
		key = strings.TrimSpace(key)
		if key != "" && value != nil {
			result[key] = fmt.Sprintf("%v", value)
		}
	}
	return true
}

func mergeEnvItem(result map[string]string, raw interface{}) {
	switch v := raw.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if strings.TrimSpace(key) != "" && value != nil {
				result[strings.TrimSpace(key)] = fmt.Sprintf("%v", value)
			}
		}
		return
	case map[string]string:
		for key, value := range v {
			if strings.TrimSpace(key) != "" {
				result[strings.TrimSpace(key)] = value
			}
		}
		return
	}
	item := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if item == "" {
		return
	}
	if mergeEnvJSON(result, item) {
		return
	}
	if key, value, ok := strings.Cut(item, "="); ok {
		key = strings.TrimSpace(key)
		if key != "" {
			result[key] = value
		}
		return
	}
	if value := os.Getenv(item); value != "" {
		result[item] = value
	}
}

func CollectSkillProvidedEnv(entry *corelib.NLSkillEntry) map[string]string {
	provided := map[string]string{}
	if entry == nil {
		return nil
	}
	for _, step := range entry.Steps {
		for key, value := range ExtractRunExtraEnv(firstNonNilStepParam(step.Params, "extra_env", "env", "environment")) {
			provided[key] = value
		}
	}
	if len(provided) == 0 {
		return nil
	}
	return provided
}

func BuildRunCheckContext(entry *corelib.NLSkillEntry, extraEnv map[string]string) *CheckContext {
	ctx := DefaultCheckContext()
	if ctx.ProvidedEnvVars == nil {
		ctx.ProvidedEnvVars = map[string]bool{}
	}
	if entry != nil {
		ctx.SkillDir = strings.TrimSpace(entry.SkillDir)
	}
	for name := range CollectSkillProvidedEnv(entry) {
		ctx.ProvidedEnvVars[name] = true
	}
	for name := range extraEnv {
		if strings.TrimSpace(name) != "" {
			ctx.ProvidedEnvVars[name] = true
		}
	}
	return ctx
}

func HydrateRunMetadata(dst, src *corelib.NLSkillEntry) {
	if dst == nil || src == nil {
		return
	}
	dstHadExecutionDefinition := hasExecutionDefinition(dst)
	if len(dst.Steps) == 0 {
		dst.Steps = append([]corelib.NLSkillStep(nil), src.Steps...)
	}
	if len(dst.Params) == 0 {
		dst.Params = append([]corelib.NLSkillParam(nil), src.Params...)
	}
	if len(dst.RequiredArgs) == 0 {
		dst.RequiredArgs = append([]string(nil), src.RequiredArgs...)
	}
	if len(dst.RequiredEnv) == 0 {
		dst.RequiredEnv = append([]string(nil), src.RequiredEnv...)
	}
	if len(dst.RequiresPython) == 0 {
		dst.RequiresPython = append([]string(nil), src.RequiresPython...)
	}
	if len(dst.RequiresNode) == 0 {
		dst.RequiresNode = append([]string(nil), src.RequiresNode...)
	}
	if len(dst.RequiresTools) == 0 {
		dst.RequiresTools = append([]string(nil), src.RequiresTools...)
	}
	if len(dst.FallbackForTools) == 0 {
		dst.FallbackForTools = append([]string(nil), src.FallbackForTools...)
	}
	if len(dst.RequiresToolsets) == 0 {
		dst.RequiresToolsets = append([]string(nil), src.RequiresToolsets...)
	}
	if len(dst.FallbackForToolsets) == 0 {
		dst.FallbackForToolsets = append([]string(nil), src.FallbackForToolsets...)
	}
	if len(dst.RequiredCredentialFiles) == 0 {
		dst.RequiredCredentialFiles = append([]string(nil), src.RequiredCredentialFiles...)
	}
	if len(dst.Platforms) == 0 {
		dst.Platforms = append([]string(nil), src.Platforms...)
	}
	if len(dst.Operations) == 0 {
		dst.Operations = append([]corelib.NLSkillOperation(nil), src.Operations...)
	}
	if len(dst.Pipeline) == 0 {
		dst.Pipeline = append([]corelib.SkillPipelineStep(nil), src.Pipeline...)
	}
	if len(dst.References) == 0 {
		dst.References = append([]corelib.SkillReference(nil), src.References...)
	}
	if dst.Mode == "" {
		dst.Mode = src.Mode
	}
	if dst.ExecMode == "" {
		dst.ExecMode = src.ExecMode
	}
	if dst.GlobalTimeout == 0 {
		dst.GlobalTimeout = src.GlobalTimeout
	}
	if dst.PreferredShell == "" {
		dst.PreferredShell = src.PreferredShell
	}
	if dst.SkillDir == "" {
		dst.SkillDir = src.SkillDir
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	if !dstHadExecutionDefinition {
		dst.ProducesArtifact = src.ProducesArtifact
		dst.RequiresGUI = src.RequiresGUI
		dst.Stateful = src.Stateful
	} else {
		dst.ProducesArtifact = dst.ProducesArtifact || src.ProducesArtifact
		dst.RequiresGUI = dst.RequiresGUI || src.RequiresGUI
		dst.Stateful = dst.Stateful || src.Stateful
	}
}

func HydrateRunMetadataFromDir(entry *corelib.NLSkillEntry) error {
	if entry == nil || strings.TrimSpace(entry.SkillDir) == "" {
		return nil
	}
	imported, err := ImportMarkdownSkillDir(entry.SkillDir, MarkdownSkillOptions{NameFallback: entry.Name})
	if err != nil {
		return err
	}
	HydrateRunMetadata(entry, imported)
	return nil
}

func hasExecutionDefinition(entry *corelib.NLSkillEntry) bool {
	if entry == nil {
		return false
	}
	return len(entry.Steps) > 0 || len(entry.Pipeline) > 0
}

func firstNonNilStepParam(params map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := params[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func ApplyRunInputInference(entry *corelib.NLSkillEntry, vars map[string]string, runArgs map[string]interface{}) {
	if entry == nil || len(vars) == 0 {
		return
	}
	candidates := runInputCandidates(vars, runArgs)
	for _, arg := range entry.RequiredArgs {
		arg = strings.TrimSpace(arg)
		if arg != "" && strings.TrimSpace(vars[arg]) == "" {
			if value := inferRunArgValue(arg, candidates); value != "" {
				vars[arg] = value
			}
		}
	}
	for _, param := range entry.Params {
		name := strings.TrimSpace(param.Name)
		if name != "" && strings.TrimSpace(vars[name]) == "" {
			if value := inferRunArgValue(name, candidates); value != "" {
				vars[name] = value
			}
		}
	}
}

func runInputCandidates(vars map[string]string, runArgs map[string]interface{}) []string {
	seen := map[string]bool{}
	var result []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, key := range []string{"input", "user_prompt", "text", "query"} {
		add(vars[key])
		if runArgs != nil {
			if raw, ok := runArgs[key]; ok {
				add(fmt.Sprintf("%v", raw))
			}
		}
	}
	return result
}

func inferRunArgValue(arg string, candidates []string) string {
	arg = strings.ToLower(strings.TrimSpace(arg))
	for _, candidate := range candidates {
		if value := extractNamedRunArg(candidate, arg); value != "" {
			return value
		}
	}
	for _, candidate := range candidates {
		if value := fallbackRunArgValue(arg, candidate); value != "" {
			return value
		}
	}
	return ""
}

func extractNamedRunArg(text, arg string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	patterns := []string{
		`(?i)(?:^|[\s,;\x{ff0c}\x{ff1b}])` + regexp.QuoteMeta(arg) + `\s*[:=\x{ff1a}]\s*([^,;\x{ff0c}\x{ff1b}\n]+)`,
		`(?i)--` + regexp.QuoteMeta(arg) + `(?:=|\s+)("[^"]+"|'[^']+'|\S+)`,
	}
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			if m := re.FindStringSubmatch(text); len(m) > 1 {
				return cleanInferredRunArgValue(m[1])
			}
		}
	}
	return ""
}

func cleanInferredRunArgValue(value string) string {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	for _, marker := range []string{" \u7684", " for ", " with ", " please", " \u8bf7", "\uff0c", ",", "\u3002", ";", "\uff1b"} {
		if idx := strings.Index(value, marker); idx > 0 {
			value = strings.TrimSpace(value[:idx])
		}
	}
	return value
}

func fallbackRunArgValue(arg, text string) string {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "{") {
		return ""
	}
	switch arg {
	case "city", "text", "query", "url", "path", "file", "input":
		return text
	default:
		return ""
	}
}

func MergeRequiredEnvParam(params map[string]interface{}, required []string) {
	if params == nil || len(required) == 0 {
		return
	}
	seen := map[string]struct{}{}
	var merged []interface{}
	appendName := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	switch existing := params["required_env"].(type) {
	case []interface{}:
		for _, item := range existing {
			if name, ok := item.(string); ok {
				appendName(name)
			}
		}
	case []string:
		for _, name := range existing {
			appendName(name)
		}
	case string:
		for _, name := range strings.Split(existing, ",") {
			appendName(name)
		}
	}
	for _, name := range required {
		appendName(name)
	}
	if len(merged) > 0 {
		params["required_env"] = merged
	}
}

func MergeExtraEnvParam(params map[string]interface{}, extraEnv map[string]string) {
	if params == nil || len(extraEnv) == 0 {
		return
	}
	merged := map[string]interface{}{}
	switch existing := params["extra_env"].(type) {
	case map[string]interface{}:
		for k, v := range existing {
			if strings.TrimSpace(k) != "" {
				merged[k] = v
			}
		}
	case map[string]string:
		for k, v := range existing {
			if strings.TrimSpace(k) != "" {
				merged[k] = v
			}
		}
	}
	for k, v := range extraEnv {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if _, exists := merged[k]; !exists {
			merged[k] = v
		}
	}
	if len(merged) > 0 {
		params["extra_env"] = merged
	}
}

func BuildCommandEnv(base []string, params map[string]interface{}) []string {
	env := append([]string(nil), base...)
	for _, envName := range stringListParam(firstNonNilStepParam(params, "required_env", "requires_env", "required_environment")) {
		if value := os.Getenv(envName); value != "" {
			env = append(env, fmt.Sprintf("%s=%s", envName, value))
		}
	}
	for key, value := range ExtractRunExtraEnv(firstNonNilStepParam(params, "extra_env", "env", "environment")) {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}

func stringListParam(raw interface{}) []string {
	var result []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			add(fmt.Sprintf("%v", item))
		}
	case []string:
		for _, item := range v {
			add(item)
		}
	case string:
		for _, item := range strings.Split(v, ",") {
			add(item)
		}
	}
	return result
}
