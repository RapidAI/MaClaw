package skill

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SelfRepairThreshold defines when a skill is eligible for self-repair.
const SelfRepairThreshold = 3 // minimum UsageCount before repair is considered

// SelfRepairMaxAttempts is the maximum consecutive repair attempts before
// the skill is marked as needs_review.
const SelfRepairMaxAttempts = 3

// LLMRepairer abstracts the LLM call needed for skill self-repair.
type LLMRepairer interface {
	ChatCall(messages []map[string]string) (string, error)
	IsConfigured() bool
}

// RepairContext provides detailed failure context for the LLM repairer.
// Inspired by Memento-Skills' failure attribution mechanism: the repairer
// needs to see the specific failure trace, not just the last error string.
type RepairContext struct {
	FailedStepIndex     int               `json:"failed_step_index"`
	StepOutput          string            `json:"step_output"` // truncated to 2000 chars
	ErrorClass          string            `json:"error_class"` // from classifySkillStepError
	RunArgs             map[string]string `json:"run_args"`    // actual args used in this run (ActualArgs)
	PreviousRepairCount int               `json:"previous_repair_count"`

	// Parameter contract (DeclaredParams vs ActualArgs) so the repair LLM can
	// distinguish "caller passed wrong/missing param names" from "skill logic bug".
	// Filled by EnrichRepairParamContract when empty.
	DeclaredParams      []corelib.NLSkillParam `json:"declared_params,omitempty"`
	MissingRequired     []string               `json:"missing_required,omitempty"`
	UnknownArgs         []string               `json:"unknown_args,omitempty"`
	ResolvedByAlias     map[string]string      `json:"resolved_by_alias,omitempty"` // actual→declared
	ParamContractNote   string                 `json:"param_contract_note,omitempty"`
}

// NewRepairContext builds a RepairContext with error class from LastError and
// optional run args, then enriches the parameter contract from the skill schema.
func NewRepairContext(skill *corelib.NLSkillEntry, runArgs map[string]string) *RepairContext {
	ctx := &RepairContext{}
	if skill != nil {
		ctx.ErrorClass = ExtractErrorClass(skill.LastError)
		ctx.StepOutput = skill.LastError
		ctx.PreviousRepairCount = skill.RepairAttemptCount
	}
	if len(runArgs) > 0 {
		ctx.RunArgs = make(map[string]string, len(runArgs))
		for k, v := range runArgs {
			ctx.RunArgs[k] = v
		}
	}
	EnrichRepairParamContract(skill, ctx)
	return ctx
}

// EnrichRepairParamContract fills DeclaredParams / mismatch fields when empty.
// Safe to call multiple times; does not overwrite non-empty DeclaredParams.
func EnrichRepairParamContract(skill *corelib.NLSkillEntry, ctx *RepairContext) {
	if ctx == nil || skill == nil {
		return
	}
	if len(ctx.DeclaredParams) == 0 {
		ctx.DeclaredParams = skillParamContract(skill)
	}
	if ctx.RunArgs == nil {
		ctx.RunArgs = map[string]string{}
	}
	missing, unknown, aliasHits := analyzeParamContract(ctx.DeclaredParams, ctx.RunArgs)
	ctx.MissingRequired = missing
	ctx.UnknownArgs = unknown
	ctx.ResolvedByAlias = aliasHits
	ctx.ParamContractNote = formatParamContractNote(missing, unknown, aliasHits)
}

// skillParamContract returns declared params, or synthesizes from steps when absent.
// Descriptions are enriched from SKILL.md / Content when present so repair LLM
// sees the same human contract as manage_skill(action="info").
func skillParamContract(skill *corelib.NLSkillEntry) []corelib.NLSkillParam {
	return CompleteParamsForSkill(skill)
}

func analyzeParamContract(declared []corelib.NLSkillParam, actual map[string]string) (missing, unknown []string, aliasHits map[string]string) {
	aliasHits = map[string]string{}
	if len(declared) == 0 {
		return nil, nil, nil
	}
	// canonical declared names + alias → canonical
	canonByKey := map[string]string{}
	required := map[string]bool{}
	for _, p := range declared {
		name := canonicalRunVarKey(p.Name)
		if name == "" {
			continue
		}
		canonByKey[name] = name
		for _, a := range p.Aliases {
			ak := canonicalRunVarKey(a)
			if ak != "" {
				canonByKey[ak] = name
			}
		}
		if p.Required {
			required[name] = true
		}
	}
	provided := map[string]bool{}
	for k := range actual {
		ck := canonicalRunVarKey(k)
		if ck == "" {
			continue
		}
		if canon, ok := canonByKey[ck]; ok {
			provided[canon] = true
			if canon != ck {
				aliasHits[k] = canon
			}
		} else {
			unknown = append(unknown, k)
		}
	}
	for name := range required {
		if !provided[name] {
			// Check defaulted params — treat Default as satisfied for "missing" list
			// (repair cares about unbound required placeholders more than defaults).
			hasDefault := false
			for _, p := range declared {
				if canonicalRunVarKey(p.Name) == name && strings.TrimSpace(p.Default) != "" {
					hasDefault = true
					break
				}
			}
			if !hasDefault {
				missing = append(missing, name)
			}
		}
	}
	if len(aliasHits) == 0 {
		aliasHits = nil
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	return missing, unknown, aliasHits
}

func formatParamContractNote(missing, unknown []string, aliasHits map[string]string) string {
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing required params: "+strings.Join(missing, ", "))
	}
	if len(unknown) > 0 {
		parts = append(parts, "args not in schema (possible wrong names): "+strings.Join(unknown, ", "))
	}
	if len(aliasHits) > 0 {
		pairs := make([]string, 0, len(aliasHits))
		for actual, declared := range aliasHits {
			pairs = append(pairs, fmt.Sprintf("%s→%s", actual, declared))
		}
		parts = append(parts, "resolved via alias: "+strings.Join(pairs, ", "))
	}
	if len(parts) == 0 {
		if len(missing) == 0 && len(unknown) == 0 {
			return "param contract: actual args match declared schema (or schema empty)"
		}
		return ""
	}
	return strings.Join(parts, "; ")
}

// IsRepairableError returns true if the error class is worth attempting
// automated repair. Delegates to the unified rules table in error_classifier.go
// which is the single source of truth for repairable/non-repairable classification.
//
// When the errorClass matches a known rule, returns that rule's repairable flag.
// Unknown error classes default to repairable (optimistic — worth trying).
func IsRepairableError(errorClass string) bool {
	ec := ErrorClass(errorClass)
	for _, rule := range rules {
		if rule.class == ec {
			return rule.repairable
		}
	}
	return true // unknown → optimistic
}

// ShouldAttemptRepair returns true if the skill should undergo an automated
// repair attempt. Two paths:
//
// Path 1 (newly installed hub skill): First failure is a strong signal that
// the skill is incompatible with the current environment. Repair immediately
// without waiting for the statistical threshold.
//
// Path 2 (statistical): Enough usage data to judge — success rate below 50%.
func ShouldAttemptRepair(skill *corelib.NLSkillEntry) bool {
	if !canAttemptRepairBase(skill) {
		return false
	}

	// Path 1: Newly installed hub/github skill — first failure is a strong
	// signal (environment incompatibility). Repair immediately if the error
	// class is repairable.
	if skill.UsageCount <= 2 && isHubSource(skill.Source) {
		return true
	}

	// Path 2: Statistical — enough data to judge.
	if skill.UsageCount < SelfRepairThreshold {
		return false
	}
	successRate := float64(skill.SuccessCount) / float64(skill.UsageCount)
	return successRate < 0.5
}

// CanForceAttemptRepair reports whether a manual/agent-triggered repair is
// allowed: same safety guards as auto-repair (LastError, status, file-backed,
// max attempts, repairable class) but without the usage-rate threshold.
// Used by manage_skill(action="trigger_repair", force=true).
func CanForceAttemptRepair(skill *corelib.NLSkillEntry) bool {
	return canAttemptRepairBase(skill)
}

// canAttemptRepairBase is the shared safety gate for auto and forced repair.
func canAttemptRepairBase(skill *corelib.NLSkillEntry) bool {
	if skill == nil {
		return false
	}
	if skill.LastError == "" {
		return false
	}
	if !isRepairEligibleStatus(skill.Status) {
		return false
	}
	if IsFileBackedSkill(*skill) {
		return false
	}
	if skill.RepairAttemptCount >= SelfRepairMaxAttempts {
		return false
	}
	errorClass := ExtractErrorClass(skill.LastError)
	if !IsRepairableError(errorClass) {
		return false
	}
	return true
}

// IsFileBackedSkill returns true for skills whose authoritative definition
// lives on disk and should go through a reviewed patch flow before edits.
func IsFileBackedSkill(skill corelib.NLSkillEntry) bool {
	return strings.EqualFold(strings.TrimSpace(skill.Source), "file") && strings.TrimSpace(skill.SkillDir) != ""
}

func isRepairEligibleStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "active":
		return true
	default:
		return false
	}
}

// isHubSource returns true if the skill source indicates it was installed
// from a hub or auto-discovered (not locally created).
func isHubSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "hub", "skillhub", "clawhub", "github", "auto_hub", "auto_github":
		return true
	}
	return false
}

// ExtractErrorClass extracts the error class from a formatted error string
// that contains a [class: xxx] tag (produced by FormatErrorForLLM).
// Returns the class string, or "unknown" if no tag is found.
//
// Uses errorClassPrefix/Suffix from error_classifier.go — single source of
// truth for the wire format.
func ExtractErrorClass(lastError string) string {
	idx := strings.Index(lastError, errorClassPrefix)
	if idx < 0 {
		return string(ErrUnknown)
	}
	rest := lastError[idx+len(errorClassPrefix):]
	end := strings.Index(rest, errorClassSuffix)
	if end < 0 {
		return string(ErrUnknown)
	}
	return rest[:end]
}

// RepairResult holds the outcome of a self-repair attempt.
type RepairResult struct {
	Repaired      bool            `json:"repaired"`
	NewSteps      []SkillYAMLStep `json:"new_steps,omitempty"`
	Explanation   string          `json:"explanation"`
	ShouldDisable bool            `json:"should_disable"`
}

// AttemptRepair uses an LLM to analyze the skill's last error and propose
// modified steps. Returns a RepairResult with the proposed changes.
// The caller is responsible for writing the changes back to disk.
func AttemptRepair(llm LLMRepairer, skill *corelib.NLSkillEntry) (*RepairResult, error) {
	return AttemptRepairWithContext(llm, skill, nil)
}

// AttemptRepairWithContext uses an LLM to analyze the skill's failure with
// detailed context (failed step output, error class, run args) and propose
// modified steps. When ctx is nil, falls back to basic repair using LastError.
//
// Inspired by Memento-Skills' Reflect phase: failures are training signals,
// not just reasons to retry. The LLM sees the specific failure trace and
// proposes targeted fixes rather than generic rewrites.
func AttemptRepairWithContext(llm LLMRepairer, skill *corelib.NLSkillEntry, ctx *RepairContext) (*RepairResult, error) {
	if llm == nil || !llm.IsConfigured() {
		return nil, fmt.Errorf("LLM not configured for skill repair")
	}
	if skill == nil {
		return nil, fmt.Errorf("skill is nil")
	}

	// Check if error class is repairable.
	if ctx != nil && !IsRepairableError(ctx.ErrorClass) {
		return &RepairResult{
			Repaired:    false,
			Explanation: fmt.Sprintf("error class %q is not repairable (external factor)", ctx.ErrorClass),
		}, nil
	}

	// Build the current steps as YAML-like text for the LLM.
	var stepsDesc strings.Builder
	for i, step := range skill.Steps {
		fmt.Fprintf(&stepsDesc, "  Step %d: action=%s", i, step.Action)
		if len(step.Params) > 0 {
			paramsJSON, _ := json.Marshal(step.Params)
			fmt.Fprintf(&stepsDesc, " params=%s", string(paramsJSON))
		}
		if step.OnError != "" {
			fmt.Fprintf(&stepsDesc, " on_error=%s", step.OnError)
		}
		stepsDesc.WriteString("\n")
	}

	// Ensure param contract is present for the prompt even if caller only set RunArgs.
	if ctx != nil {
		EnrichRepairParamContract(skill, ctx)
	}

	systemPrompt := `You are a skill repair assistant. A skill has been failing repeatedly.
Analyze the error, the parameter contract, and the current steps, then propose fixed steps.

Rules:
- GUI skill runner supports ONLY these step actions: bash, call_mcp_tool, craft_tool, poll
- NEVER use shell_tool, shell, run, exec, wget, curl as step actions — use action "bash" with params.command
- When craft_tool fails with "python runtime not found", rewrite download/fetch skills to a single bash step
  (e.g. curl -L -o "{{output}}" "{{url}}" or equivalent) instead of inventing unsupported actions
- Do NOT change the skill's core functionality — only fix the specific failure
- If a step consistently fails, add on_error: "skip" or "continue"
- If the error is fundamental (missing runtime that cannot be worked around, wrong tool, impossible task), set should_disable: true
- Distinguish PARAMETER CONTRACT failures from SKILL LOGIC failures:
  * If "missing required params" or "args not in schema" appear, prefer fixing
    placeholders/aliases/defaults or documenting expected arg names — do NOT rewrite
    the whole skill just because the caller used a wrong key (e.g. file vs input).
  * If params match the schema, fix the failing command/template/environment instead.
- Return ONLY a JSON object with fields: repaired (bool), new_steps (array), explanation (string), should_disable (bool)
- Each step in new_steps has: action (string), params (object), on_error (string, optional)`

	var contextSection string
	if ctx != nil {
		// Truncate step output to avoid blowing up the prompt.
		output := ctx.StepOutput
		if len([]rune(output)) > 2000 {
			output = string([]rune(output)[:2000]) + "\n... (truncated)"
		}
		argsJSON, _ := json.Marshal(ctx.RunArgs)
		declaredJSON, _ := json.Marshal(ctx.DeclaredParams)
		contextSection = fmt.Sprintf(`
Failed step index: %d
Error classification: %s
Step output:
%s

Parameter contract (declared schema): %s
Actual run arguments: %s
Param contract analysis: %s
Previous repair attempts: %d`,
			ctx.FailedStepIndex,
			ctx.ErrorClass,
			output,
			string(declaredJSON),
			string(argsJSON),
			firstNonEmptyRepairNote(ctx.ParamContractNote, "(none)"),
			ctx.PreviousRepairCount,
		)
	}

	userPrompt := fmt.Sprintf(`Skill: %s
Description: %s
Usage: %d times, %d successes (%.0f%% success rate)
Last error: %s
%s
Current steps:
%s

Propose a fix.`,
		skill.Name,
		skill.Description,
		skill.UsageCount,
		skill.SuccessCount,
		float64(skill.SuccessCount)/float64(skill.UsageCount)*100,
		skill.LastError,
		contextSection,
		stepsDesc.String(),
	)

	resp, err := llm.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM repair call failed: %w", err)
	}

	// Parse the response.
	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	var result RepairResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("parse repair response: %w", err)
	}

	// Normalize/validate proposed steps so self-repair cannot persist
	// GUI-unsupported actions (e.g. shell_tool) that later fail at start_run.
	if err := SanitizeRepairResult(skill, &result); err != nil {
		log.Printf("[skill-repair] skill=%s sanitize rejected repair: %v", skill.Name, err)
		return &RepairResult{
			Repaired:      false,
			ShouldDisable: result.ShouldDisable,
			Explanation:   fmt.Sprintf("repair rejected: %v; original: %s", err, strings.TrimSpace(result.Explanation)),
		}, nil
	}

	log.Printf("[skill-repair] skill=%s repaired=%v should_disable=%v explanation=%s",
		skill.Name, result.Repaired, result.ShouldDisable, result.Explanation)

	return &result, nil
}

// SanitizeRepairResult normalizes proposed repair steps into runner-native
// actions and rejects repairs that still cannot run on the GUI backend.
// Mutates result.NewSteps in place when repaired=true.
func SanitizeRepairResult(skill *corelib.NLSkillEntry, result *RepairResult) error {
	if result == nil || result.ShouldDisable || !result.Repaired {
		return nil
	}
	if len(result.NewSteps) == 0 {
		return fmt.Errorf("repaired=true but new_steps is empty")
	}
	skillDir := ""
	if skill != nil {
		skillDir = skill.SkillDir
	}
	out := make([]SkillYAMLStep, 0, len(result.NewSteps))
	for i, s := range result.NewSteps {
		step := NormalizeStepForRunner(corelib.NLSkillStep{
			Action:  s.Action,
			Params:  s.Params,
			OnError: s.OnError,
		}, skillDir)
		if err := EnsureStepActionSupported(RunnerBackendGUI, step.Action); err != nil {
			return fmt.Errorf("step %d action %q unsupported after normalize: %w", i, step.Action, err)
		}
		if strings.TrimSpace(step.Action) == "" {
			return fmt.Errorf("step %d has empty action after normalize", i)
		}
		out = append(out, SkillYAMLStep{
			Action:  step.Action,
			Params:  step.Params,
			OnError: step.OnError,
		})
	}
	result.NewSteps = out
	return nil
}

// ApplyRepair writes the repaired steps back to the skill entry.
// Returns true if the skill was modified, false if it was disabled or unchanged.
func ApplyRepair(skill *corelib.NLSkillEntry, result *RepairResult) bool {
	if skill == nil || result == nil {
		return false
	}
	errorClass := ExtractErrorClass(skill.LastError)
	if result.ShouldDisable {
		skill.Status = "needs_review"
		skill.LastError = fmt.Sprintf("auto-disabled: %s", result.Explanation)
		recordRepairAttempt(skill, errorClass, result.Explanation)
		log.Printf("[skill-repair] marked skill %s as needs_review: %s", skill.Name, result.Explanation)
		return false
	}

	if !result.Repaired || len(result.NewSteps) == 0 {
		return false
	}

	// Re-sanitize at apply time so callers that skip AttemptRepair still cannot
	// persist unsupported actions.
	if err := SanitizeRepairResult(skill, result); err != nil {
		log.Printf("[skill-repair] apply rejected for %s: %v", skill.Name, err)
		return false
	}

	// Convert RepairResult steps to NLSkillStep.
	newSteps := make([]corelib.NLSkillStep, len(result.NewSteps))
	for i, s := range result.NewSteps {
		newSteps[i] = corelib.NLSkillStep{
			Action:  s.Action,
			Params:  s.Params,
			OnError: s.OnError,
		}
	}

	skill.Steps = newSteps
	NormalizeSkillForRunner(skill)
	skill.LastError = fmt.Sprintf("auto-repaired: %s", result.Explanation)
	recordRepairAttempt(skill, errorClass, result.Explanation)

	log.Printf("[skill-repair] repaired skill %s with %d new steps (attempt %d)",
		skill.Name, len(newSteps), skill.RepairAttemptCount)
	return true
}

func recordRepairAttempt(skill *corelib.NLSkillEntry, errorClass, explanation string) {
	skill.RepairAttemptCount++
	skill.LastRepairAt = time.Now().Format(time.RFC3339)
	record := corelib.SkillRepairRecord{
		Timestamp:   skill.LastRepairAt,
		ErrorClass:  errorClass,
		Explanation: explanation,
		Success:     false, // caller sets to true after verify
	}
	skill.RepairHistory = append(skill.RepairHistory, record)
	if len(skill.RepairHistory) > 5 {
		skill.RepairHistory = skill.RepairHistory[len(skill.RepairHistory)-5:]
	}
}

func firstNonEmptyRepairNote(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// MarkRepairVerified marks the last repair history entry as verified (success).
// Call this after VerifyRepair succeeds.
func MarkRepairVerified(skill *corelib.NLSkillEntry) {
	if skill == nil {
		return
	}
	if len(skill.RepairHistory) > 0 {
		skill.RepairHistory[len(skill.RepairHistory)-1].Success = true
	}
}

// ResetRepairCount resets the consecutive repair attempt counter.
// Call this when the skill succeeds after a repair, indicating the fix worked.
func ResetRepairCount(skill *corelib.NLSkillEntry) {
	if skill == nil {
		return
	}
	skill.RepairAttemptCount = 0
}
