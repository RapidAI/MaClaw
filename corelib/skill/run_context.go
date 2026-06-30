package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
)

func NormalizeRunVars(runArgs map[string]interface{}) map[string]string {
	if len(runArgs) == 0 {
		return nil
	}
	vars := map[string]string{}
	rawArgs, _ := lookupRunArg(runArgs, "args")
	mergeRunVarMap(vars, rawArgs, true)
	mergeRunVarJSON(vars, rawArgs, true)
	if value, ok := rawArgs.(string); ok {
		value = strings.TrimSpace(value)
		if value != "" && !strings.HasPrefix(value, "{") && !strings.HasPrefix(value, "[") {
			if _, exists := vars["input"]; !exists {
				vars["input"] = value
			}
		}
	}
	for _, key := range RunVarFallbackKeys {
		key = canonicalRunVarKey(key)
		raw, ok := lookupRunArg(runArgs, key)
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
		key = canonicalRunVarKey(key)
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

func lookupRunArg(runArgs map[string]interface{}, key string) (interface{}, bool) {
	if runArgs == nil {
		return nil, false
	}
	if raw, ok := runArgs[key]; ok {
		return raw, true
	}
	for candidate, raw := range runArgs {
		if canonicalRunVarKey(candidate) == key {
			return raw, true
		}
	}
	return nil, false
}

func mergeRunVarMap(vars map[string]string, raw interface{}, overwrite bool) {
	add := func(key string, value interface{}) {
		key = canonicalRunVarKey(key)
		if key == "" || value == nil {
			return
		}
		if isNestedRunVarControlKey(key) {
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

func isNestedRunVarControlKey(key string) bool {
	key = canonicalRunVarKey(key)
	if key == "operation" {
		return false
	}
	return isRunControlKey(key)
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

func canonicalRunVarKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	runes := []rune(key)
	var b strings.Builder
	lastUnderscore := false
	for i, r := range runes {
		if r == '-' || r == '.' || unicode.IsSpace(r) {
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
			continue
		}
		if unicode.IsUpper(r) {
			if i > 0 && !lastUnderscore {
				prev := runes[i-1]
				var next rune
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && next != 0 && unicode.IsLower(next)) {
					b.WriteByte('_')
				}
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
		lastUnderscore = false
	}
	return strings.Trim(b.String(), "_")
}

// CanonicalRunVarKey exposes the runner's canonical parameter-key normalizer so
// callers that validate skill arguments do not drift from BindParams.
func CanonicalRunVarKey(key string) string {
	return canonicalRunVarKey(key)
}

// FoldUnconsumedArgsToInput merges undeclared user-provided args into the
// "input" carrier key when the skill has no explicit parameter contract.
//
// This preserves semantic args passed to a contractless skill (for example
// city="南京") as runner input context instead of silently discarding them.
// Downstream craft_tool/documentation fallback steps can then consume that
// context through the standard "input" carrier. Bash commands still need an
// explicit parameter contract or placeholders to receive concrete CLI args.
//
// Call this AFTER the unconsumed-args check has determined the skill is
// contractless (allowed set is empty) and accepted the args.
func FoldUnconsumedArgsToInput(vars map[string]string, params []corelib.NLSkillParam) {
	if len(vars) == 0 {
		return
	}
	// Build the set of keys that are already consumed by carrier keys or
	// the parameter binding system.
	consumed := ParameterBindingKeySet(params)
	consumed["input"] = true
	consumed["user_prompt"] = true
	consumed["output"] = true
	consumed["operation"] = true

	var parts []string
	for key, value := range vars {
		canonical := canonicalRunVarKey(key)
		if canonical == "" || consumed[canonical] || isUndeclaredRunCarrierKey(key) {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, key+": "+value)
	}
	if len(parts) == 0 {
		return
	}
	sort.Strings(parts)
	folded := strings.Join(parts, "\n")
	if existing := strings.TrimSpace(vars["input"]); existing != "" {
		vars["input"] = existing + "\n" + folded
	} else {
		vars["input"] = folded
	}
}

// IsContractlessSkill reports whether a skill has no explicit parameter
// contract: no declared params, no required_args, and no placeholders in step
// or pipeline templates. Used by the unconsumed-args checker and the runner to
// decide whether to accept/fold unknown args.
func IsContractlessSkill(entry *corelib.NLSkillEntry) bool {
	if entry == nil {
		return true
	}
	if len(entry.Params) > 0 || len(entry.RequiredArgs) > 0 {
		return false
	}
	return !StepsHavePlaceholders(entry.Steps) && !PipelineHavePlaceholders(entry.Pipeline)
}

// StepsHavePlaceholders checks whether any executable step template contains
// {{placeholder}}, ${placeholder}, or {placeholder}.
func StepsHavePlaceholders(steps []corelib.NLSkillStep) bool {
	return len(StepPlaceholderKeySet(steps)) > 0
}

// StepPlaceholderKeySet returns canonical placeholder keys used by step params
// and when conditions, including fallback steps. It intentionally does not scan
// capture or poll/loop regex fields because regex quantifiers like {4} would
// be false positives.
func StepPlaceholderKeySet(steps []corelib.NLSkillStep) map[string]bool {
	keys := map[string]bool{}
	for _, step := range steps {
		addStepPlaceholderKeys(keys, &step, map[*corelib.NLSkillStep]bool{})
	}
	return keys
}

func addStepPlaceholderKeys(dst map[string]bool, step *corelib.NLSkillStep, seen map[*corelib.NLSkillStep]bool) {
	if step == nil || seen[step] {
		return
	}
	seen[step] = true
	extractPlaceholdersFromParams(step.Params, func(key string) {
		if key = canonicalRunVarKey(key); key != "" {
			dst[key] = true
		}
	})
	for _, key := range ExtractPlaceholderKeys(step.When) {
		if key = canonicalRunVarKey(key); key != "" {
			dst[key] = true
		}
	}
	addStepPlaceholderKeys(dst, step.FallbackStep, seen)
}

// PipelineHavePlaceholders checks whether any pipeline param or checkpoint
// message contains a pipeline variable template such as {{input}}.
func PipelineHavePlaceholders(steps []corelib.SkillPipelineStep) bool {
	return len(PipelinePlaceholderKeySet(steps)) > 0
}

// PipelinePlaceholderKeySet returns canonical placeholder keys used by pipeline
// params and checkpoint messages.
func PipelinePlaceholderKeySet(steps []corelib.SkillPipelineStep) map[string]bool {
	keys := map[string]bool{}
	for _, step := range steps {
		for _, value := range step.Params {
			addPlaceholderKeys(keys, value)
		}
		addPlaceholderKeys(keys, step.CheckpointMessage)
	}
	return keys
}

func addPlaceholderKeys(dst map[string]bool, text string) {
	for _, key := range ExtractPlaceholderKeys(text) {
		if key = canonicalRunVarKey(key); key != "" {
			dst[key] = true
		}
	}
}

func isRunControlKey(key string) bool {
	key = canonicalRunVarKey(key)
	if IsManageSkillRunnerControlKey(key) {
		return true
	}
	switch key {
	case "args", "env", "extra_env", "environment", "steps", "step", "operation", "dry_run", "pipeline_stack", "pipeline_internal_call", "staged_cleanup_paths", "live_output":
		return true
	default:
		return false
	}
}

// IsManageSkillRunnerControlKey reports whether a manage_skill argument is consumed
// by the manage_skill tool itself and should not be forwarded as runner input.
// Runtime selectors such as query and mode intentionally remain forwardable.
func IsManageSkillRunnerControlKey(key string) bool {
	switch canonicalRunVarKey(key) {
	case "action", "name", "skill", "skill_name", "qualified_name", "skill_id", "hub_url", "install_ref", "auto_run", "wait_seconds", "run_id", "auto_fix", "force", "step_index", "field", "value", "find", "replace", "reason", "runtime_platform", "runtime_policy_owner_id":
		return true
	default:
		return false
	}
}

func ExtractRunExtraEnvFromArgs(runArgs map[string]interface{}) map[string]string {
	if len(runArgs) == 0 {
		return nil
	}
	result := map[string]string{}
	for _, key := range []string{"env", "extra_env", "environment"} {
		raw, ok := lookupRunArg(runArgs, key)
		if !ok {
			if rawArgs, hasArgs := lookupRunArg(runArgs, "args"); hasArgs && rawArgs != nil {
				raw, ok = lookupNestedRunControlArg(rawArgs, key)
			}
		}
		if !ok {
			continue
		}
		for name, value := range ExtractRunExtraEnv(raw) {
			result[name] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
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
			if !isConcreteEnvAssignment(key, value) {
				continue
			}
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
	for name := range ctx.ProvidedEnvVars {
		markProvidedEnvVar(ctx, name)
	}
	if entry != nil {
		ctx.SkillDir = strings.TrimSpace(entry.SkillDir)
	}
	for name := range CollectSkillProvidedEnv(entry) {
		markProvidedEnvVar(ctx, name)
	}
	for name, value := range extraEnv {
		if !isConcreteEnvAssignment(name, value) {
			continue
		}
		markProvidedEnvVar(ctx, name)
	}
	return ctx
}

func BuildRunCheckContextForRunner(entry *corelib.NLSkillEntry, extraEnv map[string]string, runner string) *CheckContext {
	ctx := BuildRunCheckContext(entry, extraEnv)
	if normalizeRunnerBackend(runner) == RunnerBackendGUI && entry != nil {
		proxyProbeSteps := PrecheckExecutableSteps(entry.Steps, nil)
		proxyRequiredEnv := entry.RequiredEnv
		if len(proxyProbeSteps) == 0 && len(entry.Steps) > 0 {
			proxyRequiredEnv = nil
		}
		if corelib.NeedsOpenAIProxyAuto(proxyRequiredEnv, extraEnv, proxyProbeSteps, entry.SkillDir) {
			markProvidedEnvVar(ctx, "OPENAI_API_KEY")
			markProvidedEnvVar(ctx, "OPENAI_BASE_URL")
			markProvidedEnvVar(ctx, "OPENAI_MODEL")
		}
	}
	return ctx
}

func markProvidedEnvVar(ctx *CheckContext, name string) {
	if ctx == nil || ctx.ProvidedEnvVars == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	ctx.ProvidedEnvVars[name] = true
	ctx.ProvidedEnvVars[strings.ToUpper(name)] = true
	ctx.ProvidedEnvVars[strings.ToLower(name)] = true
}

func HydrateRunMetadata(dst, src *corelib.NLSkillEntry) {
	if dst == nil || src == nil {
		return
	}
	dstHadExecutionDefinition := hasExecutionDefinition(dst)
	dstIsKnowledge := isKnowledgeSkillType(dst.Type)
	if dst.Type == "" {
		dst.Type = src.Type
		dstIsKnowledge = isKnowledgeSkillType(dst.Type)
	}
	if dst.Content == "" {
		dst.Content = src.Content
	}
	if !dstIsKnowledge && len(dst.Steps) == 0 {
		dst.Steps = append([]corelib.NLSkillStep(nil), src.Steps...)
	}
	if !dstIsKnowledge && len(dst.Params) == 0 {
		dst.Params = append([]corelib.NLSkillParam(nil), src.Params...)
	}
	if !dstIsKnowledge && len(dst.RequiredArgs) == 0 {
		dst.RequiredArgs = append([]string(nil), src.RequiredArgs...)
	}
	if !dstIsKnowledge && len(dst.RequiredEnv) == 0 {
		dst.RequiredEnv = append([]string(nil), src.RequiredEnv...)
	}
	if len(dst.RequiresPython) == 0 {
		dst.RequiresPython = append([]string(nil), src.RequiresPython...)
	}
	if len(dst.RequiresNode) == 0 {
		dst.RequiresNode = append([]string(nil), src.RequiresNode...)
	}
	if len(dst.Capabilities) == 0 {
		dst.Capabilities = append([]string(nil), src.Capabilities...)
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
	if !dstIsKnowledge && len(dst.Pipeline) == 0 {
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
	fallbackName := strings.TrimSpace(entry.Name)
	if fallbackName == "" {
		fallbackName = strings.TrimSpace(entry.DirName)
	}
	imported, _, err := loadSkillFromDir(entry.SkillDir, fallbackName)
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
	paramsByName := map[string]corelib.NLSkillParam{}
	for _, param := range entry.Params {
		if name := canonicalRunVarKey(param.Name); name != "" {
			param.Name = name
			paramsByName[name] = param
		}
	}
	for _, arg := range entry.RequiredArgs {
		arg = canonicalRunVarKey(arg)
		if arg == "" {
			continue
		}
		if promoteRunParamAlias(vars, runParamForName(arg, paramsByName)) {
			continue
		}
		if strings.TrimSpace(vars[arg]) == "" {
			if value := inferRunParamValue(runParamForName(arg, paramsByName), candidates); value != "" {
				vars[arg] = value
			}
		}
	}
	for _, param := range entry.Params {
		name := canonicalRunVarKey(param.Name)
		if name == "" {
			continue
		}
		param.Name = name
		if promoteRunParamAlias(vars, param) {
			continue
		}
		if strings.TrimSpace(vars[name]) == "" {
			if value := inferRunParamValue(param, candidates); value != "" {
				vars[name] = value
			}
		}
	}
	applyExplicitInputForSingleMissingParam(entry, vars, runArgs, paramsByName)
}

func promoteRunParamAlias(vars map[string]string, param corelib.NLSkillParam) bool {
	name := canonicalRunVarKey(param.Name)
	if name == "" {
		return false
	}
	if value, ok := lookupCanonicalVar(vars, name); ok && strings.TrimSpace(value) != "" {
		vars[name] = value
		return true
	}
	for _, alias := range runParamInferenceNames(param) {
		alias = canonicalRunVarKey(alias)
		if alias == "" || alias == name {
			continue
		}
		if value, ok := lookupCanonicalVar(vars, alias); ok && strings.TrimSpace(value) != "" {
			vars[name] = value
			return true
		}
	}
	return false
}

func runParamForName(name string, paramsByName map[string]corelib.NLSkillParam) corelib.NLSkillParam {
	name = canonicalRunVarKey(name)
	if param, ok := paramsByName[name]; ok {
		return param
	}
	param := corelib.NLSkillParam{Name: name}
	if aliases, ok := commonParamAliases[name]; ok {
		param.Aliases = aliases
	}
	return param
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
	for _, key := range []string{"input", "user_prompt", "text", "content", "message", "prompt", "query", "task", "description"} {
		if value, ok := lookupCanonicalVar(vars, key); ok {
			add(value)
		}
		if raw, ok := lookupRunArg(runArgs, key); ok {
			add(fmt.Sprintf("%v", raw))
		}
	}
	if raw, ok := lookupRunArg(runArgs, "args"); ok {
		if value, ok := runVarString(raw); ok && !strings.HasPrefix(strings.TrimSpace(value), "{") {
			add(value)
		}
	}
	return result
}

func inferRunArgValue(arg string, candidates []string) string {
	return inferRunParamValue(corelib.NLSkillParam{Name: arg}, candidates)
}

func inferRunParamValue(param corelib.NLSkillParam, candidates []string) string {
	paramNames := runParamInferenceNames(param)
	for _, candidate := range candidates {
		for _, name := range paramNames {
			if value := extractNamedRunArg(candidate, name); value != "" {
				return value
			}
		}
	}
	return ""
}

func applyExplicitInputForSingleMissingParam(entry *corelib.NLSkillEntry, vars map[string]string, runArgs map[string]interface{}, paramsByName map[string]corelib.NLSkillParam) {
	input := explicitRunInputValue(vars, runArgs)
	if input == "" {
		return
	}
	missing := map[string]corelib.NLSkillParam{}
	addMissing := func(param corelib.NLSkillParam) {
		name := canonicalRunVarKey(param.Name)
		if name == "" || strings.TrimSpace(vars[name]) != "" {
			return
		}
		param.Name = name
		missing[name] = param
	}
	for _, arg := range entry.RequiredArgs {
		addMissing(runParamForName(arg, paramsByName))
	}
	for _, param := range entry.Params {
		if param.Required {
			addMissing(param)
		}
	}
	if len(missing) != 1 {
		return
	}
	for name, param := range missing {
		if value := inferExplicitInputParamValue(param, input); value != "" {
			vars[name] = value
		}
	}
}

func explicitRunInputValue(vars map[string]string, runArgs map[string]interface{}) string {
	if !runArgBool(runArgs, "_skill_infer_natural_prompt") {
		return ""
	}
	if raw, ok := lookupRunArg(runArgs, "user_prompt"); ok {
		if value, ok := runVarString(raw); ok {
			value = strings.TrimSpace(value)
			if value != "" && !strings.HasPrefix(value, "{") && !strings.HasPrefix(value, "[") {
				return value
			}
		}
	}
	if value, ok := lookupCanonicalVar(vars, "user_prompt"); ok {
		value = strings.TrimSpace(value)
		if value != "" && !strings.HasPrefix(value, "{") && !strings.HasPrefix(value, "[") {
			return value
		}
	}
	return ""
}

func runArgBool(runArgs map[string]interface{}, key string) bool {
	raw, ok := lookupRunArg(runArgs, key)
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
	default:
		return false
	}
}

func inferExplicitInputParamValue(param corelib.NLSkillParam, input string) string {
	switch runParamKind(param) {
	case "city":
		return extractExplicitInputCity(input)
	default:
		return ""
	}
}

func extractExplicitInputCity(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if value := extractNamedRunArg(input, "city"); value != "" {
		return value
	}
	if value := extractNamedRunArg(input, "location"); value != "" {
		return value
	}
	if value := extractNamedRunArg(input, "\u57ce\u5e02"); value != "" {
		return value
	}
	if value := extractNamedRunArg(input, "\u5730\u70b9"); value != "" {
		return value
	}
	for _, pattern := range []string{
		`(?i)(?:^|\s)(?:weather|forecast)\s+(?:in|for|at)\s+([A-Za-z][A-Za-z .'-]*[A-Za-z])(?:\s|$|[,.!?;:])`,
		`(?i)(?:^|\s)(?:weather|forecast)\s+([A-Za-z][A-Za-z .'-]*[A-Za-z])(?:\s|$|[,.!?;:])`,
		`(?i)(?:^|\s)(?:in|for|at)\s+([A-Za-z][A-Za-z .'-]*[A-Za-z])(?:\s|$|[,.!?;:])`,
	} {
		if re, err := regexp.Compile(pattern); err == nil {
			if m := re.FindStringSubmatch(input); len(m) > 1 {
				return cleanInferredRunArgValue(m[1])
			}
		}
	}
	if looksLikeDirectCityInput(input) {
		return input
	}
	return ""
}

func looksLikeDirectCityInput(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	lower := strings.ToLower(input)
	for _, token := range []string{"weather", "forecast", "\u5929\u6c14", "\u67e5\u8be2", "\u67e5\u4e00\u4e0b", "\u660e\u5929", "\u540e\u5929"} {
		if strings.Contains(lower, token) {
			return false
		}
	}
	if strings.ContainsAny(input, "\n\r{}[]:=") {
		return false
	}
	words := strings.Fields(input)
	if len(words) > 3 {
		return false
	}
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsSpace(r) || r == '-' || r == '\'' || r == '.' {
			continue
		}
		if unicode.Is(unicode.Han, r) {
			continue
		}
		return false
	}
	return true
}

func runParamInferenceNames(param corelib.NLSkillParam) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = canonicalRunVarKey(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(param.Name)
	for _, alias := range param.Aliases {
		add(alias)
	}
	for _, alias := range commonParamAliases[canonicalRunVarKey(param.Name)] {
		add(alias)
	}
	switch runParamKind(param) {
	case "city":
		for _, alias := range []string{"city", "location", "place", "area", "\u57ce\u5e02", "\u5730\u70b9"} {
			add(alias)
		}
	case "target_language":
		for _, alias := range []string{"target_language", "target_lang", "to_lang", "language", "lang", "\u76ee\u6807\u8bed\u8a00", "\u8bed\u8a00"} {
			add(alias)
		}
	case "source_language":
		for _, alias := range []string{"source_language", "source_lang", "from_lang", "\u6e90\u8bed\u8a00"} {
			add(alias)
		}
	}
	return names
}

func runParamKind(param corelib.NLSkillParam) string {
	parts := []string{normalizeRunParamToken(param.Name), strings.ToLower(param.Description)}
	for _, alias := range param.Aliases {
		parts = append(parts, normalizeRunParamToken(alias))
	}
	joined := strings.Join(parts, " ")
	if strings.Contains(joined, "targetlanguage") || strings.Contains(joined, "targetlang") || strings.Contains(joined, "tolang") || strings.Contains(joined, "\u76ee\u6807\u8bed\u8a00") {
		return "target_language"
	}
	if strings.Contains(joined, "sourcelanguage") || strings.Contains(joined, "sourcelang") || strings.Contains(joined, "fromlang") || strings.Contains(joined, "\u6e90\u8bed\u8a00") {
		return "source_language"
	}
	if strings.Contains(joined, "city") || strings.Contains(joined, "location") || strings.Contains(joined, "place") || strings.Contains(joined, "weathercity") || strings.Contains(joined, "\u57ce\u5e02") || strings.Contains(joined, "\u5730\u70b9") || strings.Contains(joined, "\u5929\u6c14") {
		return "city"
	}
	if strings.Contains(joined, "language") || strings.Contains(joined, "lang") || strings.Contains(joined, "\u8bed\u8a00") {
		return "target_language"
	}
	if strings.Contains(joined, "format") || strings.Contains(joined, "fmt") || strings.Contains(joined, "\u683c\u5f0f") {
		return "format"
	}
	return ""
}
func normalizeRunParamToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return replacer.Replace(value)
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
	for _, marker := range []string{" \u7684", " for ", " with ", " today", " tomorrow", " tonight", " now", " please", " \u8bf7", "\u4eca\u5929", "\u660e\u5929", "\u4eca\u665a", "\uff0c", ",", "\u3002", ";", "\uff1b"} {
		if idx := strings.Index(value, marker); idx > 0 {
			value = strings.TrimSpace(value[:idx])
		}
	}
	return value
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
		merged[k] = v
	}
	if len(merged) > 0 {
		params["extra_env"] = merged
	}
}

// PrepareResolvedStepEnv applies the runner env contract to a step after
// ResolveStep has performed parameter binding. Bash steps receive required
// environment names plus caller-supplied env values in the command params so
// BuildCommandEnv can inject them uniformly in GUI and TUI runners.
func PrepareResolvedStepEnv(step corelib.NLSkillStep, requiredEnv []string, extraEnv map[string]string) corelib.NLSkillStep {
	if !stepCanReceiveCommandEnv(step) {
		return step
	}
	step = NormalizeStepForRunnerCopy(step, "")
	if NormalizeStepActionName(step.Action) != "bash" {
		return step
	}
	if len(requiredEnv) == 0 && len(extraEnv) == 0 {
		return step
	}
	if step.Params == nil {
		step.Params = map[string]interface{}{}
	}
	if len(requiredEnv) > 0 {
		MergeRequiredEnvParam(step.Params, requiredEnv)
	}
	if len(extraEnv) > 0 {
		MergeExtraEnvParam(step.Params, extraEnv)
	}
	return step
}

func stepCanReceiveCommandEnv(step corelib.NLSkillStep) bool {
	action := NormalizeStepActionName(step.Action)
	switch action {
	case "bash", "run", "exec", "execute", "command", "shell", "sh", "cmd", "script", "python", "python3", "node", "js", "javascript", "powershell", "pwsh":
		return true
	case "":
		return hasNonEmptyParam(step.Params, "command") || hasNonEmptyParam(step.Params, "cmd") || hasNonEmptyParam(step.Params, "run") || hasNonEmptyParam(step.Params, "script")
	default:
		return false
	}
}

func BuildCommandEnv(base []string, params map[string]interface{}) []string {
	env := append([]string(nil), base...)
	for _, envName := range stringListParam(firstNonNilStepParam(params, "required_env", "requires_env", "required_environment")) {
		if value := os.Getenv(envName); value != "" {
			env = upsertCommandEnv(env, envName, value)
		}
	}
	extra := ExtractRunExtraEnv(firstNonNilStepParam(params, "extra_env", "env", "environment"))
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !isConcreteEnvAssignment(key, extra[key]) {
			continue
		}
		env = upsertCommandEnv(env, key, extra[key])
	}
	return env
}

func upsertCommandEnv(env []string, key, value string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return env
	}
	assignment := fmt.Sprintf("%s=%s", key, value)
	for i, item := range env {
		name, _, ok := strings.Cut(item, "=")
		if ok && envNameEqual(name, key) {
			env[i] = assignment
			return env
		}
	}
	return append(env, assignment)
}

func envNameEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func isConcreteEnvAssignment(key, value string) bool {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return false
	}
	for _, placeholder := range []string{
		"$" + key,
		"${" + key + "}",
		"%" + key + "%",
		"{{" + key + "}}",
		"{" + key + "}",
	} {
		if strings.EqualFold(value, placeholder) {
			return false
		}
	}
	return true
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
