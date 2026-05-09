package skill

// runner_shared.go provides exported utility functions that are shared between
// GUI and TUI skill execution paths. Keeping runner selection, parameter
// binding, requirements, and file diagnostics in one place prevents the two
// frontends from drifting into subtly different execution contracts.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
)

// DetectImplicitRequiredArgs scans runner-substituted step parameters for
// {{key}} / ${key} placeholders that are not provided in vars. Returns the list
// of missing keys. This catches skills that use {{input}}/{{output}} without
// declaring required_args in their frontmatter.
func DetectImplicitRequiredArgs(steps []corelib.NLSkillStep, vars map[string]string) []string {
	return detectImplicitRequiredArgs(steps, vars, nil, nil)
}

// DetectImplicitRunRequiredArgs applies the runner's full parameter contract
// before deciding which undeclared placeholders should block execution.
// Explicit non-synthetic params are owned by the schema, while a non-empty
// required_args list means unresolved flag-style placeholders outside that
// list are optional CLI parameters. Positional undeclared placeholders still
// block execution.
func DetectImplicitRunRequiredArgs(steps []corelib.NLSkillStep, vars map[string]string, required []string, params []corelib.NLSkillParam) []string {
	return detectImplicitRequiredArgs(
		steps,
		vars,
		implicitRunRequiredArgSkipper(required, params),
		func(key string, vars map[string]string) bool {
			return runVarProvidedForKeyInSchema(key, vars, params)
		},
	)
}

func detectImplicitRequiredArgs(steps []corelib.NLSkillStep, vars map[string]string, skip func(key, command string) bool, provided func(key string, vars map[string]string) bool) []string {
	seen := make(map[string]bool)
	captured := make(map[string]bool)
	if provided == nil {
		provided = runVarProvidedForKey
	}
	for _, step := range steps {
		step = NormalizeStepForRunnerCopy(step, "")
		for _, context := range implicitRequiredPlaceholderContexts(step) {
			if context == "" {
				continue
			}
			for _, rawKey := range ExtractPlaceholderKeys(context) {
				key := canonicalRunVarKey(rawKey)
				if key == "" || seen[key] {
					continue
				}
				if provided(key, vars) {
					continue // already provided
				}
				if captured[key] {
					continue // produced by an earlier step capture
				}
				if skip != nil && skip(key, context) {
					continue
				}
				seen[key] = true
			}
		}
		for key := range step.Capture {
			markKnownRunVarKey(captured, key)
		}
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	return result
}

func implicitRequiredPlaceholderContexts(step corelib.NLSkillStep) []string {
	action := NormalizeStepActionName(step.Action)
	switch action {
	case "bash":
		return stringParamContexts(step.Params, "command", "working_dir")
	case "craft_tool":
		return stringParamContexts(step.Params, "task", "instructions", "input", "output", "content", "prompt", "description")
	default:
		return nil
	}
}

func stringParamContexts(params map[string]interface{}, keys ...string) []string {
	var contexts []string
	for _, key := range keys {
		if value, ok := params[key].(string); ok && strings.TrimSpace(value) != "" {
			contexts = append(contexts, value)
		}
	}
	return contexts
}

func implicitRunRequiredArgSkipper(required []string, params []corelib.NLSkillParam) func(key, command string) bool {
	legacyRequiredSet := map[string]bool{}
	requiredSet := map[string]bool{}
	hasLegacyRequiredArgs := false
	for _, raw := range required {
		if key := canonicalRunVarKey(raw); key != "" {
			legacyRequiredSet[key] = true
			requiredSet[key] = true
			hasLegacyRequiredArgs = true
		}
	}
	explicitParamSet := map[string]bool{}
	primaryNames := schemaPrimaryParamNames(params)
	for _, param := range params {
		bindingNames := paramBindingNamesForSchema(param, primaryNames)
		if len(bindingNames) == 0 {
			continue
		}
		matchesLegacyRequired := false
		for _, name := range bindingNames {
			if legacyRequiredSet[name] {
				matchesLegacyRequired = true
				break
			}
		}
		if param.Required {
			matchesLegacyRequired = true
		}
		if !param.Synthetic {
			for _, name := range bindingNames {
				explicitParamSet[name] = true
			}
		}
		if matchesLegacyRequired {
			for _, name := range bindingNames {
				requiredSet[name] = true
			}
		}
	}
	return func(key, command string) bool {
		key = canonicalRunVarKey(key)
		if key == "" {
			return true
		}
		if explicitParamSet[key] {
			return true
		}
		if requiredSet[key] {
			return true
		}
		return hasLegacyRequiredArgs && placeholderOnlyInCLIFlagArg(command, key)
	}
}

func placeholderOnlyInCLIFlagArg(command, key string) bool {
	key = canonicalRunVarKey(key)
	if key == "" || strings.TrimSpace(command) == "" {
		return false
	}
	withoutFlagArgs := optionalCLIPlaceholderArgRe.ReplaceAllString(command, "$1")
	withoutFlagArgs = optionalSlashPlaceholderArgRe.ReplaceAllString(withoutFlagArgs, "$1")
	for _, remaining := range ExtractPlaceholderKeys(withoutFlagArgs) {
		if canonicalRunVarKey(remaining) == key {
			return false
		}
	}
	return true
}

// MissingRequiredArgs returns required args that are not satisfied by vars.
// It applies the same canonical key and alias rules as placeholder binding so
// runner pre-checks cannot reject an invocation that ResolveStep would accept.
func MissingRequiredArgs(required []string, vars map[string]string) []string {
	seen := map[string]bool{}
	var missing []string
	for _, raw := range required {
		key := canonicalRunVarKey(raw)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if runVarProvidedForKey(key, vars) {
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}

// MissingRequiredParams returns required parameters from the declared schema
// that are not satisfied by vars or a parameter default. This mirrors the
// BindParams contract, but is intended for runner pre-checks so GUI/TUI can
// reject bad invocations before starting an async run.
func MissingRequiredParams(params []corelib.NLSkillParam, vars map[string]string) []string {
	seen := map[string]bool{}
	var missing []string
	primaryNames := schemaPrimaryParamNames(params)
	for _, param := range params {
		key := canonicalRunVarKey(param.Name)
		if key == "" || !param.Required || seen[key] {
			continue
		}
		seen[key] = true
		if strings.TrimSpace(param.Default) != "" || runVarProvidedForParamInSchema(param, vars, primaryNames) {
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}

// MissingRunRequiredArgs combines legacy required_args and the newer params
// schema, de-duplicating by canonical parameter name. Runners should use this
// for a single pre-execution contract check.
func MissingRunRequiredArgs(required []string, params []corelib.NLSkillParam, vars map[string]string) []string {
	seen := map[string]bool{}
	var missing []string
	paramsByName := map[string]corelib.NLSkillParam{}
	primaryNames := schemaPrimaryParamNames(params)
	for _, param := range params {
		for _, key := range paramBindingNamesForSchema(param, primaryNames) {
			if key != "" {
				paramsByName[key] = param
			}
		}
	}

	addMissing := func(key string) {
		key = canonicalRunVarKey(key)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		missing = append(missing, key)
	}

	for _, raw := range required {
		key := canonicalRunVarKey(raw)
		if key == "" {
			continue
		}
		if param, ok := paramsByName[key]; ok {
			if strings.TrimSpace(param.Default) != "" || runVarProvidedForParamInSchema(param, vars, primaryNames) {
				continue
			}
			addMissing(key)
			continue
		}
		if runVarProvidedForKey(key, vars) {
			continue
		}
		addMissing(key)
	}

	for _, key := range MissingRequiredParams(params, vars) {
		addMissing(key)
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}

// ResolveSelectedStepLabels resolves api_workflow step selection from runner
// args. An explicit operation overrides a raw steps selector so callers cannot
// accidentally combine two incompatible routing modes.
func ResolveSelectedStepLabels(entry *corelib.NLSkillEntry, runArgs map[string]interface{}) ([]string, error) {
	if entry == nil || !strings.EqualFold(entry.Mode, "api_workflow") {
		return nil, nil
	}
	var selected []string
	if runArgs != nil {
		if rawSteps, ok := lookupRunControlArg(runArgs, "steps"); ok {
			selected = appendStepSelectorValues(selected, rawSteps)
		}
		if rawOp, ok := lookupRunControlArg(runArgs, "operation"); ok && strings.TrimSpace(runControlArgString(rawOp)) != "" {
			opName := strings.TrimSpace(runControlArgString(rawOp))
			for _, op := range entry.Operations {
				if strings.EqualFold(op.Name, opName) {
					labels := appendStepSelectorValues(nil, op.Labels)
					if len(labels) == 0 {
						return nil, fmt.Errorf("skill %q operation %q has no step labels [action: inspect_skill]", entry.Name, opName)
					}
					if err := validateSelectedStepLabels(entry, labels); err != nil {
						return nil, err
					}
					return labels, nil
				}
			}
			return nil, fmt.Errorf("skill %q operation %q not found. Available operations: %s [action: choose_operation]", entry.Name, opName, AvailableOperationNames(entry.Operations))
		}
	}
	if len(selected) == 0 {
		opsWithLabels := operationsWithLabels(entry.Operations)
		if len(opsWithLabels) == 1 {
			labels := appendStepSelectorValues(nil, opsWithLabels[0].Labels)
			if err := validateSelectedStepLabels(entry, labels); err != nil {
				return nil, err
			}
			return labels, nil
		}
		if len(opsWithLabels) > 1 {
			return nil, fmt.Errorf("skill %q requires an operation. Available operations: %s [action: choose_operation]", entry.Name, AvailableOperationNames(opsWithLabels))
		}
		if len(entry.Operations) > 0 {
			return nil, fmt.Errorf("skill %q api_workflow operations have no step labels [action: inspect_skill]", entry.Name)
		}
	}
	if err := validateSelectedStepLabels(entry, selected); err != nil {
		return nil, err
	}
	return selected, nil
}

func lookupRunControlArg(runArgs map[string]interface{}, key string) (interface{}, bool) {
	key = canonicalRunVarKey(key)
	if key == "" || runArgs == nil {
		return nil, false
	}
	if raw, ok := lookupRunArg(runArgs, key); ok {
		return raw, true
	}
	rawArgs, ok := lookupRunArg(runArgs, "args")
	if !ok || rawArgs == nil {
		return nil, false
	}
	return lookupNestedRunControlArg(rawArgs, key)
}

func lookupNestedRunControlArg(raw interface{}, key string) (interface{}, bool) {
	switch v := raw.(type) {
	case map[string]interface{}:
		for candidate, value := range v {
			if canonicalRunVarKey(candidate) == key {
				return value, true
			}
		}
	case map[string]string:
		for candidate, value := range v {
			if canonicalRunVarKey(candidate) == key {
				return value, true
			}
		}
	case string:
		text := strings.TrimSpace(v)
		if len(text) < 2 || text[0] != '{' {
			return nil, false
		}
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(text), &parsed) != nil {
			return nil, false
		}
		return lookupNestedRunControlArg(parsed, key)
	}
	return nil, false
}

func runControlArgString(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case nil, map[string]interface{}, []interface{}, map[string]string, []string:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func validateSelectedStepLabels(entry *corelib.NLSkillEntry, selected []string) error {
	if entry == nil || len(selected) == 0 {
		return nil
	}
	available := map[string]bool{}
	var labels []string
	for _, step := range entry.Steps {
		label := strings.TrimSpace(step.Label)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if available[key] {
			continue
		}
		available[key] = true
		labels = append(labels, label)
	}
	var missing []string
	seenMissing := map[string]bool{}
	for _, label := range selected {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if available[key] {
			continue
		}
		if !seenMissing[key] {
			seenMissing[key] = true
			missing = append(missing, label)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	availableText := "(none)"
	if len(labels) > 0 {
		availableText = strings.Join(labels, ", ")
	}
	return fmt.Errorf("skill %q selected unknown step label(s): %s. Available labels: %s [action: inspect_skill]", entry.Name, strings.Join(missing, ", "), availableText)
}

func operationsWithLabels(operations []corelib.NLSkillOperation) []corelib.NLSkillOperation {
	var result []corelib.NLSkillOperation
	for _, op := range operations {
		if len(appendStepSelectorValues(nil, op.Labels)) == 0 {
			continue
		}
		result = append(result, op)
	}
	return result
}

func appendStepSelectorValues(dst []string, raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		for _, item := range strings.Split(v, ",") {
			if label := strings.TrimSpace(item); label != "" {
				dst = append(dst, label)
			}
		}
	case []string:
		for _, item := range v {
			if label := strings.TrimSpace(item); label != "" {
				dst = append(dst, label)
			}
		}
	case []interface{}:
		for _, item := range v {
			if label := strings.TrimSpace(fmt.Sprintf("%v", item)); label != "" && label != "<nil>" {
				dst = append(dst, label)
			}
		}
	}
	return dst
}

// SelectedExecutableSteps returns the subset of steps selected by api_workflow
// labels. Unlabeled steps are skipped when selection is active, matching runner
// execution semantics.
func SelectedExecutableSteps(steps []corelib.NLSkillStep, selected []string) []corelib.NLSkillStep {
	if len(selected) == 0 {
		return steps
	}
	var filtered []corelib.NLSkillStep
	for _, step := range steps {
		if strings.TrimSpace(step.Label) == "" {
			continue
		}
		if StepLabelSelected(step.Label, selected) {
			filtered = append(filtered, step)
		}
	}
	return filtered
}

func IsPipelineSkill(entry *corelib.NLSkillEntry) bool {
	if entry == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(entry.Mode), "pipeline") || len(entry.Pipeline) > 0
}

// PrecheckExecutableSteps returns the selected steps that can still execute
// under the runner's current initial variables. It mirrors runtime `when`
// behavior for conditions that are already decidable, while keeping steps whose
// condition may be satisfied by variables captured from earlier steps.
func PrecheckExecutableSteps(steps []corelib.NLSkillStep, vars map[string]string) []corelib.NLSkillStep {
	if len(steps) == 0 {
		return nil
	}
	known := knownRunVarKeys(vars)
	filtered := make([]corelib.NLSkillStep, 0, len(steps))
	for _, step := range steps {
		if shouldSkipStepForPrecheck(step.When, vars, known) {
			continue
		}
		filtered = append(filtered, step)
		for key := range step.Capture {
			markKnownRunVarKey(known, key)
		}
	}
	return filtered
}

func ResolveStepsForRunnerPrecheck(steps []corelib.NLSkillStep, vars map[string]string, skillDir string, params []corelib.NLSkillParam) ([]corelib.NLSkillStep, error) {
	if len(steps) == 0 {
		return nil, nil
	}
	resolved := make([]corelib.NLSkillStep, 0, len(steps))
	for _, step := range steps {
		result, err := ResolveStep(step, vars, skillDir, params, quoteRunnerPrecheckValue)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, result.Step)
	}
	return resolved, nil
}

func CheckRunnerRequirements(entry *corelib.NLSkillEntry, extraEnv map[string]string, runner string) []Violation {
	reqs := ExtractRequirements(entry, BuildRunCheckContextForRunner(entry, extraEnv, runner))
	if len(reqs) == 0 {
		return nil
	}
	registry := DefaultRegistry()
	violations := registry.CheckAll(reqs)
	remaining := registry.FixAll(violations)
	return PromoteRunnerBlockingViolations(remaining)
}

type RunnerExecutionPreparation struct {
	SelectedSteps          []string
	ExecutionSteps         []corelib.NLSkillStep
	PrecheckSteps          []corelib.NLSkillStep
	ResolvedPrecheckSteps  []corelib.NLSkillStep
	Params                 []corelib.NLSkillParam
	PrecheckParams         []corelib.NLSkillParam
	Warnings               []string
	FileWarnings           []string
	RequirementWarnings    []Violation
	RequirementDiagnostics []Violation
}

type PipelineRunnerPreparation struct {
	Params                 []corelib.NLSkillParam
	Warnings               []string
	RequirementWarnings    []Violation
	RequirementDiagnostics []Violation
}

func PreparePipelineRunnerExecution(entry *corelib.NLSkillEntry, vars map[string]string, runArgs map[string]interface{}, extraEnv map[string]string, runner string) (*PipelineRunnerPreparation, error) {
	if entry == nil {
		return nil, fmt.Errorf("skill entry is nil")
	}
	if len(entry.Pipeline) == 0 {
		return nil, fmt.Errorf("%s", FormatNoExecutableStepsMessage(entry.Name, entry, runner))
	}
	if err := CheckTrustedPipelineRunStack(runArgs, entry.Name); err != nil {
		return nil, err
	}
	params := CompleteParamsForRunner(entry.Params, nil, entry.RequiredArgs)
	entry.Params = params
	ApplyRunInputInference(entry, vars, runArgs)
	if missing := MissingRunRequiredArgs(entry.RequiredArgs, params, vars); len(missing) > 0 {
		return nil, fmt.Errorf("%s", FormatMissingRequiredArgsMessage(entry.Name, missing, entry.Description))
	}
	checkEntry := *entry
	checkEntry.Params = params
	remaining := CheckRunnerRequirements(&checkEntry, extraEnv, runner)
	errors := FilterErrors(remaining)
	if len(errors) > 0 {
		return nil, fmt.Errorf("skill %q runner requirements not satisfied: %s", entry.Name, FormatViolations(errors))
	}
	var requirementWarnings []Violation
	for _, violation := range remaining {
		if violation.Severity == "warning" {
			requirementWarnings = append(requirementWarnings, violation)
		}
	}
	return &PipelineRunnerPreparation{
		Params:                 params,
		Warnings:               FormatRunnerWarnings(requirementWarnings, nil),
		RequirementWarnings:    requirementWarnings,
		RequirementDiagnostics: remaining,
	}, nil
}

// PrepareRunnerExecution applies the shared GUI/TUI pre-execution contract for
// a skill run. Callers should normalize/hydrate the skill first, then use this
// once after any runner-specific fallback step synthesis.
func PrepareRunnerExecution(entry *corelib.NLSkillEntry, vars map[string]string, runArgs map[string]interface{}, extraEnv map[string]string, runner string) (*RunnerExecutionPreparation, error) {
	if entry == nil {
		return nil, fmt.Errorf("skill entry is nil")
	}
	selectedSteps, err := ResolveSelectedStepLabels(entry, runArgs)
	if err != nil {
		return nil, err
	}
	executionSteps := SelectedExecutableSteps(entry.Steps, selectedSteps)
	if len(selectedSteps) > 0 && len(executionSteps) == 0 {
		return nil, fmt.Errorf("skill %q step selection matched no executable steps: %s", entry.Name, strings.Join(selectedSteps, ", "))
	}

	originalParams := append([]corelib.NLSkillParam(nil), entry.Params...)
	params := CompleteParamsForRunner(originalParams, executionSteps, entry.RequiredArgs)
	entry.Params = params
	ApplyRunInputInference(entry, vars, runArgs)
	precheckSteps := PrecheckExecutableSteps(executionSteps, vars)
	for _, step := range precheckSteps {
		normalized := NormalizeStepForRunnerCopy(step, entry.SkillDir)
		if err := EnsureStepActionSupported(runner, normalized.Action); err != nil {
			return nil, err
		}
	}
	precheckRequiredArgs := RequiredArgsForRunnerPrecheck(entry.RequiredArgs, precheckSteps)
	precheckParams := CompleteParamsForRunner(originalParams, precheckSteps, precheckRequiredArgs)

	if missing := MissingRunRequiredArgs(precheckRequiredArgs, precheckParams, vars); len(missing) > 0 {
		return nil, fmt.Errorf("%s", FormatMissingRequiredArgsMessage(entry.Name, missing, entry.Description))
	}
	if implicit := DetectImplicitRunRequiredArgs(precheckSteps, vars, precheckRequiredArgs, precheckParams); len(implicit) > 0 {
		return nil, fmt.Errorf("%s", FormatImplicitRequiredArgsMessage(entry.Name, implicit, entry.Description))
	}

	resolvedPrecheckSteps, err := ResolveStepsForRunnerPrecheck(precheckSteps, vars, entry.SkillDir, precheckParams)
	if err != nil {
		return nil, fmt.Errorf("skill %q precheck parameter binding failed: %v", entry.Name, err)
	}
	fileCheckEntry := *entry
	fileCheckEntry.Steps = resolvedPrecheckSteps
	fileCheckEntry.Params = precheckParams
	remaining := CheckRunnerRequirements(&fileCheckEntry, extraEnv, runner)
	errors := FilterErrors(remaining)
	if len(errors) > 0 {
		return nil, fmt.Errorf("skill %q runner requirements not satisfied: %s", entry.Name, FormatViolations(errors))
	}
	var requirementWarnings []Violation
	for _, violation := range remaining {
		if violation.Severity == "warning" {
			requirementWarnings = append(requirementWarnings, violation)
		}
	}

	fileDiagnostics, err := CheckStepFileReferencesWithDiagnosticsAndExpectedOutputs(&fileCheckEntry, expectedOutputPathsFromVars(vars))
	if err != nil {
		return nil, err
	}
	fileWarnings := FormatStepFileDiagnostics(fileDiagnostics)
	return &RunnerExecutionPreparation{
		SelectedSteps:          selectedSteps,
		ExecutionSteps:         executionSteps,
		PrecheckSteps:          precheckSteps,
		ResolvedPrecheckSteps:  resolvedPrecheckSteps,
		Params:                 params,
		PrecheckParams:         precheckParams,
		Warnings:               FormatRunnerWarnings(requirementWarnings, fileWarnings),
		FileWarnings:           fileWarnings,
		RequirementWarnings:    requirementWarnings,
		RequirementDiagnostics: remaining,
	}, nil
}

func RequiredArgsForRunnerPrecheck(required []string, precheckSteps []corelib.NLSkillStep) []string {
	if len(required) == 0 || len(precheckSteps) == 0 {
		return nil
	}
	used := map[string]bool{}
	captured := map[string]bool{}
	for _, step := range precheckSteps {
		extractPlaceholdersFromParams(step.Params, func(key string) {
			if key = canonicalRunVarKey(key); key != "" {
				if captured[key] {
					return
				}
				used[key] = true
			}
		})
		if step.When != "" {
			for _, key := range ExtractPlaceholderKeys(step.When) {
				if key = canonicalRunVarKey(key); key != "" {
					if captured[key] {
						continue
					}
					used[key] = true
				}
			}
		}
		for key := range step.Capture {
			markKnownRunVarKey(captured, key)
		}
	}
	seen := map[string]bool{}
	var scoped []string
	for _, raw := range required {
		key := canonicalRunVarKey(raw)
		if key == "" || seen[key] || !used[key] {
			continue
		}
		seen[key] = true
		scoped = append(scoped, key)
	}
	return scoped
}

func expectedOutputPathsFromVars(vars map[string]string) []string {
	if len(vars) == 0 {
		return nil
	}
	outputKeys := []string{
		"output", "output_file", "output_path", "outfile", "out_file", "out_path",
		"dest", "destination", "target", "target_file", "result", "result_file",
	}
	seen := map[string]bool{}
	var paths []string
	for _, key := range outputKeys {
		value := strings.TrimSpace(vars[canonicalRunVarKey(key)])
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		paths = append(paths, value)
	}
	return paths
}

func quoteRunnerPrecheckValue(value string) string {
	if !needsRunnerPrecheckQuote(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func needsRunnerPrecheckQuote(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || isShellCommandSeparator(r) {
			return true
		}
		switch r {
		case '\'', '"', '`', '<', '>', '(', ')':
			return true
		}
	}
	return false
}

func knownRunVarKeys(vars map[string]string) map[string]bool {
	known := map[string]bool{}
	for key, value := range vars {
		if strings.TrimSpace(value) == "" {
			continue
		}
		markKnownRunVarKey(known, key)
	}
	return known
}

func markKnownRunVarKey(known map[string]bool, raw string) {
	key := canonicalRunVarKey(raw)
	if key == "" {
		return
	}
	known[key] = true
	for _, alias := range commonParamAliases[key] {
		if aliasKey := canonicalRunVarKey(alias); aliasKey != "" {
			known[aliasKey] = true
		}
	}
	for primary, aliases := range commonParamAliases {
		for _, alias := range aliases {
			if canonicalRunVarKey(alias) == key {
				if primaryKey := canonicalRunVarKey(primary); primaryKey != "" {
					known[primaryKey] = true
				}
			}
		}
	}
}

func shouldSkipStepForPrecheck(expr string, vars map[string]string, known map[string]bool) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	for _, rawKey := range ExtractPlaceholderKeys(expr) {
		key := canonicalRunVarKey(rawKey)
		if key == "" {
			continue
		}
		if runVarProvidedForKey(key, vars) || known[key] {
			continue
		}
		return true
	}
	resolved := substituteWhenExpression(expr, vars)
	if len(ExtractPlaceholderKeys(resolved)) > 0 {
		return false
	}
	return !EvaluateSimpleCondition(resolved)
}

func substituteWhenExpression(expr string, vars map[string]string) string {
	if expr == "" || len(vars) == 0 {
		return expr
	}
	for _, rawKey := range ExtractPlaceholderKeys(expr) {
		key := canonicalRunVarKey(rawKey)
		value := strings.TrimSpace(resolveRunVarValueForKey(key, vars))
		if key == "" || value == "" {
			continue
		}
		expr = replaceCanonicalPlaceholderLiteral(expr, key, value)
	}
	return expr
}

func StepLabelSelected(label string, selected []string) bool {
	label = strings.TrimSpace(label)
	if label == "" {
		return false
	}
	for _, item := range selected {
		item = strings.TrimSpace(item)
		if item != "" && strings.EqualFold(label, item) {
			return true
		}
	}
	return false
}

func AvailableOperationNames(operations []corelib.NLSkillOperation) string {
	if len(operations) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(operations))
	for _, op := range operations {
		if name := strings.TrimSpace(op.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

func runVarProvidedForParam(param corelib.NLSkillParam, vars map[string]string) bool {
	return runVarProvidedForParamInSchema(param, vars, nil)
}

func runVarProvidedForParamInSchema(param corelib.NLSkillParam, vars map[string]string, primaryNames map[string]bool) bool {
	for _, name := range paramBindingNamesForSchema(param, primaryNames) {
		if runVarProvidedForExactKey(name, vars) {
			return true
		}
	}
	return false
}

func runVarProvidedForKeyInSchema(key string, vars map[string]string, params []corelib.NLSkillParam) bool {
	key = canonicalRunVarKey(key)
	if key == "" {
		return false
	}
	primaryNames := schemaPrimaryParamNames(params)
	for _, param := range params {
		for _, name := range paramBindingNamesForSchema(param, primaryNames) {
			if name == key {
				return runVarProvidedForParamInSchema(param, vars, primaryNames)
			}
		}
	}
	return runVarProvidedForKey(key, vars)
}

func runVarProvidedForExactKey(key string, vars map[string]string) bool {
	if vars == nil {
		return false
	}
	value, ok := lookupCanonicalVar(vars, key)
	return ok && strings.TrimSpace(value) != ""
}

func runVarProvidedForKey(key string, vars map[string]string) bool {
	if vars == nil {
		return false
	}
	key = canonicalRunVarKey(key)
	if runVarProvidedForExactKey(key, vars) {
		return true
	}
	param := corelib.NLSkillParam{Name: key}
	if aliases, ok := commonParamAliases[key]; ok {
		param.Aliases = aliases
	}
	for _, alias := range runParamInferenceNames(param) {
		if alias != key {
			if value, ok := lookupCanonicalVar(vars, alias); ok && strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

// SubstituteVarsInString replaces {{key}}, ${key}, and {key} placeholders in s
// with values from vars. Handles all three formats that placeholderRe matches.
func SubstituteVarsInString(s string, vars map[string]string) string {
	return SubstituteVariables(s, vars)
}

func SkillParamSeconds(params map[string]interface{}, key string) (int, bool) {
	if params == nil {
		return 0, false
	}
	switch v := params[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case int32:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return int(parsed), true
		}
	}
	return 0, false
}

func RunnerStepTimeoutSeconds(params map[string]interface{}, defaultSeconds, maxSeconds int) int {
	timeout := defaultSeconds
	if timeout <= 0 {
		timeout = 120
	}
	if maxSeconds <= 0 {
		maxSeconds = timeout
	}
	if t, ok := SkillParamSeconds(params, "timeout"); ok && t > 0 {
		timeout = t
		if timeout > maxSeconds {
			timeout = maxSeconds
		}
	}
	if gt, ok := SkillParamSeconds(params, "global_timeout"); ok && gt > timeout {
		timeout = gt
	}
	return timeout
}

// EvaluateStepWhen resolves a step "when" expression with runner variables and
// then evaluates it. Missing placeholders are stripped so a condition like
// "{{enabled}}" is false when enabled was not provided.
func EvaluateStepWhen(expr string, vars map[string]string) bool {
	resolved := substituteWhenExpression(expr, vars)
	if len(ExtractPlaceholderKeys(resolved)) > 0 {
		return false
	}
	return EvaluateSimpleCondition(resolved)
}

// EvaluateSimpleCondition evaluates a simple boolean expression string.
// Supports: "true"/"false", "yes"/"no", "!expr", "a == b", "a != b",
// bare non-empty string → true, empty string → false.
func EvaluateSimpleCondition(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	lower := strings.ToLower(normalizeConditionOperand(expr))
	switch lower {
	case "true", "yes", "1":
		return true
	case "false", "no", "0":
		return false
	}
	if strings.HasPrefix(expr, "!") {
		return !EvaluateSimpleCondition(expr[1:])
	}
	if idx := strings.Index(expr, " contains "); idx > 0 {
		left := normalizeConditionOperand(expr[:idx])
		right := normalizeConditionOperand(expr[idx+len(" contains "):])
		return strings.Contains(left, right)
	}
	if strings.Contains(expr, "==") {
		parts := strings.SplitN(expr, "==", 2)
		return normalizeConditionOperand(parts[0]) == normalizeConditionOperand(parts[1])
	}
	if strings.Contains(expr, "!=") {
		parts := strings.SplitN(expr, "!=", 2)
		return normalizeConditionOperand(parts[0]) != normalizeConditionOperand(parts[1])
	}
	return true // non-empty string → true
}

func normalizeConditionOperand(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') ||
		(value[0] == '\'' && value[len(value)-1] == '\'') ||
		(value[0] == '`' && value[len(value)-1] == '`') {
		return strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

// CaptureOutputVariables extracts variables from step output using regex
// patterns defined in the step's Capture map. Each key is a variable name,
// each value is a regex pattern. The first capture group is used when present;
// otherwise the full regex match is captured.
func CaptureOutputVariables(output string, captures map[string]string) map[string]string {
	result := make(map[string]string)
	type capturedVar struct {
		key   string
		value string
	}
	var captured []capturedVar
	explicit := make(map[string]bool)
	for varName, pattern := range captures {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		m := re.FindStringSubmatch(output)
		value := ""
		if len(m) >= 2 {
			value = m[1]
		} else if len(m) == 1 {
			value = m[0]
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		rawKey := strings.TrimSpace(varName)
		key := canonicalRunVarKey(rawKey)
		if key == "" {
			continue
		}
		if rawKey != "" {
			result[rawKey] = value
		}
		if _, ok := result[key]; !ok {
			result[key] = value
		}
		explicit[key] = true
		captured = append(captured, capturedVar{key: key, value: value})
	}
	for _, item := range captured {
		mirrorCapturedRunVarAliases(result, item.key, item.value, explicit)
	}
	return result
}

func mirrorCapturedRunVarAliases(result map[string]string, key, value string, explicit map[string]bool) {
	for _, alias := range runVarEquivalentKeys(key) {
		if alias == "" || explicit[alias] {
			continue
		}
		if _, ok := result[alias]; ok {
			continue
		}
		result[alias] = value
	}
}

func runVarEquivalentKeys(key string) []string {
	seen := map[string]bool{}
	var keys []string
	add := func(raw string) {
		candidate := canonicalRunVarKey(raw)
		if candidate == "" || seen[candidate] {
			return
		}
		seen[candidate] = true
		keys = append(keys, candidate)
	}
	add(key)
	for _, alias := range commonParamAliases[canonicalRunVarKey(key)] {
		add(alias)
	}
	for primary, aliases := range commonParamAliases {
		for _, alias := range aliases {
			if canonicalRunVarKey(alias) != canonicalRunVarKey(key) {
				continue
			}
			add(primary)
			for _, sibling := range aliases {
				add(sibling)
			}
		}
	}
	return keys
}
