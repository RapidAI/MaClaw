package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// ComplexityResult holds the complexity analysis result for a TrajectorySession.
type ComplexityResult struct {
	Score         string // "worth_summarizing" | "too_simple"
	StepCount     int    // role="assistant" entries with non-nil ToolCalls
	ToolKindCount int    // unique tool names across all ToolCalls
	TurnCount     int    // total number of Entries
}

// AnalyzeComplexity analyzes the complexity of a TrajectorySession.
// Thresholds: StepCount >= 3 && ToolKindCount >= 2 && TurnCount >= 5 => "worth_summarizing",
// otherwise => "too_simple". nil/empty session returns "too_simple".
func AnalyzeComplexity(session *TrajectorySession) ComplexityResult {
	if session == nil || len(session.Entries) == 0 {
		return ComplexityResult{Score: "too_simple"}
	}

	stepCount := 0
	toolKinds := make(map[string]bool)
	turnCount := len(session.Entries)

	for _, entry := range session.Entries {
		if entry.Role == "assistant" && entry.ToolCalls != nil {
			stepCount++
			collectToolNameSet(entry.ToolCalls, toolKinds)
		}
	}

	result := ComplexityResult{
		StepCount:     stepCount,
		ToolKindCount: len(toolKinds),
		TurnCount:     turnCount,
	}

	if stepCount >= 3 && len(toolKinds) >= 2 && turnCount >= 5 {
		result.Score = "worth_summarizing"
	} else {
		result.Score = "too_simple"
	}

	return result
}

// collectToolNameSet extracts unique tool names from a ToolCalls interface{} value
// into the provided set. Accepts live []llm.ToolCall, JSON []interface{} maps,
// and other shapes handled by extractTrajectoryToolCalls.
func collectToolNameSet(toolCalls interface{}, names map[string]bool) {
	if names == nil {
		return
	}
	for _, tc := range extractTrajectoryToolCalls(toolCalls) {
		if tc.Name != "" {
			names[tc.Name] = true
		}
	}
}

// DraftSkill generates a Skill draft from a TrajectorySession by extracting
// tool_calls in time order, merging consecutive identical calls, and marking
// error steps. Returns error if no tool_calls are found.
func DraftSkill(session *TrajectorySession) (*skill.SkillYAMLFile, error) {
	if session == nil || len(session.Entries) == 0 {
		return nil, fmt.Errorf("no tool_calls found in session")
	}

	// Build result maps before extracting steps. A tool call is evidence for a
	// reusable recipe only when it completed successfully (or is a legacy entry
	// with no outcome metadata); failed/cancelled calls are observations for
	// diagnosis, not instructions to replay.
	toolResults := buildToolResultMap(session.Entries)
	toolOutcomes := buildAutoSummaryToolOutcomeMap(session.Entries)

	// Extract raw steps from all assistant entries with ToolCalls.
	var rawSteps []skill.SkillYAMLStep
	for _, entry := range session.Entries {
		if entry.Role != "assistant" || entry.ToolCalls == nil {
			continue
		}
		steps := extractStepsFromToolCalls(entry.ToolCalls, toolResults, toolOutcomes)
		rawSteps = append(rawSteps, steps...)
	}

	if len(rawSteps) == 0 {
		return nil, fmt.Errorf("no tool_calls found in session")
	}

	// A trajectory includes both the task work and the agent framework used to
	// orchestrate that work.  The latter must never become part of a learned
	// skill: e.g. recording manage_skill(run, name=X) inside X creates a
	// recursive definition and cannot run on the GUI skill runner.
	rawSteps = filterAutoSummaryOrchestrationSteps(rawSteps)
	if len(rawSteps) == 0 {
		return nil, fmt.Errorf("no reusable task steps found after excluding agent orchestration calls")
	}

	// Merge consecutive identical tool calls.
	mergedSteps := mergeConsecutiveSteps(rawSteps)

	// Extract description from first user message.
	description := extractUserDescription(session)
	if description == "" {
		description = session.SessionID
	}

	name := tool.GenerateSkillName(description)
	triggers := tool.ExtractTriggerKeywords(description)

	return &skill.SkillYAMLFile{
		Name:        name,
		Description: description,
		Triggers:    triggers,
		Steps:       mergedSteps,
		Status:      "active",
		Source:      "learned",
	}, nil
}

// buildToolResultMap builds a map from tool_call_id to the content string
// of the corresponding tool result entry. Trajectory uses role="tool_result"
// (legacy also used role="tool" for results in early drafts).
func buildToolResultMap(entries []TrajectoryEntry) map[string]string {
	results := make(map[string]string)
	for _, entry := range entries {
		if entry.ToolCallID == "" {
			continue
		}
		// role=tool with map content is a tool *call* (name+args), not a result.
		if entry.Role != "tool_result" && entry.Role != "tool" {
			continue
		}
		if entry.Role == "tool" {
			if _, isCall := entry.Content.(map[string]interface{}); isCall {
				continue
			}
		}
		switch s := entry.Content.(type) {
		case string:
			results[entry.ToolCallID] = s
		default:
			// Non-string results still useful for error-string detection via fmt.
			if entry.Content != nil {
				results[entry.ToolCallID] = fmt.Sprint(entry.Content)
			}
		}
	}
	return results
}

// buildAutoSummaryToolOutcomeMap records explicit terminal outcomes for tool
// results. Older trajectory files have no outcome, so callers retain their
// legacy content-based behavior for backward compatibility.
func buildAutoSummaryToolOutcomeMap(entries []TrajectoryEntry) map[string]string {
	outcomes := make(map[string]string)
	for _, entry := range entries {
		if entry.ToolCallID == "" || !trajectoryEntryIsToolResult(entry) {
			continue
		}
		if outcome := normalizeTrajectoryToolOutcome(entry.ToolOutcome); outcome != "" {
			outcomes[entry.ToolCallID] = outcome
		}
	}
	return outcomes
}

// extractStepsFromToolCalls converts a ToolCalls interface{} into SkillYAMLStep
// slice, checking each tool call's result for errors. Uses the same normalizer
// as trajectory recording so live []llm.ToolCall payloads are not dropped.
func extractStepsFromToolCalls(toolCalls interface{}, toolResults, toolOutcomes map[string]string) []skill.SkillYAMLStep {
	extracted := extractTrajectoryToolCalls(toolCalls)
	if len(extracted) == 0 {
		return nil
	}
	steps := make([]skill.SkillYAMLStep, 0, len(extracted))
	for _, tc := range extracted {
		if tc.Name == "" {
			continue
		}
		if outcome, found := toolOutcomes[tc.ID]; found && outcome != "succeeded" {
			continue
		}
		step := skill.SkillYAMLStep{
			Action: tc.Name,
			Params: parseToolArguments(tc.Args),
		}
		// Check if the tool result indicates an error.
		if tc.ID != "" {
			if content, found := toolResults[tc.ID]; found {
				if strings.HasPrefix(content, "[error]") || strings.HasPrefix(content, "[stderr]") {
					step.OnError = "skip"
				}
			}
		}
		steps = append(steps, step)
	}
	return steps
}

// parseToolArguments parses the "arguments" field from a tool call.
// It can be a JSON string or already a map.
func parseToolArguments(args interface{}) map[string]interface{} {
	if args == nil {
		return map[string]interface{}{}
	}
	// Already a map (e.g. from in-memory construction).
	if m, ok := args.(map[string]interface{}); ok {
		return m
	}
	// JSON string (typical from LLM responses).
	if s, ok := args.(string); ok && s != "" {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m
		}
	}
	return map[string]interface{}{}
}

// mergeConsecutiveSteps merges only consecutive calls with the same executable
// contract into a single step with _repeat_count in params. Calls such as
// ssh_read_file({path:a}) followed by ssh_read_file({path:b}) are distinct
// work, not retries: merging them would silently replay only the first path.
func mergeConsecutiveSteps(steps []skill.SkillYAMLStep) []skill.SkillYAMLStep {
	if len(steps) == 0 {
		return steps
	}
	var merged []skill.SkillYAMLStep
	i := 0
	for i < len(steps) {
		current := steps[i]
		count := 1
		for i+count < len(steps) && sameAutoSummaryMergeableStep(current, steps[i+count]) {
			// Propagate on_error="skip" if any in the group has it.
			if steps[i+count].OnError == "skip" {
				current.OnError = "skip"
			}
			count++
		}
		if count > 1 {
			if current.Params == nil {
				current.Params = make(map[string]interface{})
			}
			current.Params["_repeat_count"] = count
		}
		merged = append(merged, current)
		i += count
	}
	return merged
}

func sameAutoSummaryMergeableStep(left, right skill.SkillYAMLStep) bool {
	return skill.NormalizeStepActionName(left.Action) == skill.NormalizeStepActionName(right.Action) &&
		sameAutoSummaryParams(left.Params, right.Params)
}

// extractUserDescription returns the Content string from the first role="user"
// entry, or empty string if none found.
func extractUserDescription(session *TrajectorySession) string {
	for _, entry := range session.Entries {
		if entry.Role != "user" {
			continue
		}
		if s := trajectoryUserContentText(entry.Content); s != "" {
			return s
		}
	}
	return ""
}

// trajectoryUserContentText extracts plain text from user content that may be a
// string or multimodal content-block array (OpenAI/Anthropic style).
func trajectoryUserContentText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var parts []string
		for _, raw := range v {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			switch typ {
			case "text", "input_text":
				if t, ok := m["text"].(string); ok {
					if t = strings.TrimSpace(t); t != "" {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case []map[string]interface{}:
		var parts []string
		for _, m := range v {
			typ, _ := m["type"].(string)
			if typ == "text" || typ == "input_text" {
				if t, ok := m["text"].(string); ok {
					if t = strings.TrimSpace(t); t != "" {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

// filterAutoSummaryOrchestrationSteps removes control-plane actions that start,
// install, or route skills rather than perform the user's task.  A learned
// skill is an executable task recipe, not a recording of how the agent chose
// that recipe. Keep this list deliberately small and explicit; normal task
// actions continue through the runner compatibility gate below.
func filterAutoSummaryOrchestrationSteps(steps []skill.SkillYAMLStep) []skill.SkillYAMLStep {
	if len(steps) == 0 {
		return nil
	}
	filtered := make([]skill.SkillYAMLStep, 0, len(steps))
	for _, step := range steps {
		if isAutoSummaryOrchestrationAction(step.Action) {
			continue
		}
		filtered = append(filtered, step)
	}
	return filtered
}

func isAutoSummaryOrchestrationAction(action string) bool {
	switch skill.NormalizeStepActionName(action) {
	case "manage_skill", "run_skill", "search_skill_hub", "install_skill_hub", "search_and_install_skill":
		return true
	default:
		return false
	}
}

// validateAutoSummaryRunnerCompatibility stops invalid generated drafts before
// they can replace a similar learned skill or be registered. Auto summaries are
// produced by the GUI application, so GUI is the execution contract to verify.
func validateAutoSummaryRunnerCompatibility(draft *skill.SkillYAMLFile) error {
	if draft == nil {
		return &ValidationError{Reasons: []string{"draft must not be nil"}}
	}
	var reasons []string
	for i, step := range draft.Steps {
		action := skill.NormalizeStepActionName(step.Action)
		if isAutoSummaryOrchestrationAction(action) {
			reasons = append(reasons, fmt.Sprintf("step[%d] action %q is an agent orchestration action and cannot appear in a learned skill", i, action))
			continue
		}
		if err := skill.EnsureStepActionSupported(skill.RunnerBackendGUI, action); err != nil {
			reasons = append(reasons, fmt.Sprintf("step[%d] %v", i, err))
		}
	}
	if len(reasons) > 0 {
		return &ValidationError{Reasons: reasons}
	}
	return nil
}

// ValidationError holds all validation failure reasons.
type ValidationError struct {
	Reasons []string
}

// Error returns a joined string of all validation failure reasons.
func (e *ValidationError) Error() string {
	return strings.Join(e.Reasons, "; ")
}

// actionTokenMatch reports whether action equals needle or contains it as a
// snake/kebab token (avoids "run" matching "runtime", "api" matching "capital").
func actionTokenMatch(action, needle string) bool {
	if action == needle {
		return true
	}
	if strings.Contains(action, "_"+needle+"_") ||
		strings.HasPrefix(action, needle+"_") ||
		strings.HasSuffix(action, "_"+needle) {
		return true
	}
	if strings.Contains(action, "-"+needle+"-") ||
		strings.HasPrefix(action, needle+"-") ||
		strings.HasSuffix(action, "-"+needle) {
		return true
	}
	return false
}

// labelsForAction returns security labels for a single step action name.
func labelsForAction(action string) []string {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return nil
	}
	var labels []string
	// Token match covers shell_exec / exec_cmd / run_terminal / run_shell via separators.
	if actionTokenMatch(action, "exec") || actionTokenMatch(action, "cmd") ||
		actionTokenMatch(action, "shell") || actionTokenMatch(action, "bash") ||
		actionTokenMatch(action, "powershell") || actionTokenMatch(action, "terminal") ||
		actionTokenMatch(action, "script") || actionTokenMatch(action, "run") {
		labels = append(labels, "shell_exec")
	}
	if actionTokenMatch(action, "http") || actionTokenMatch(action, "https") ||
		actionTokenMatch(action, "api") || actionTokenMatch(action, "fetch") ||
		actionTokenMatch(action, "request") || actionTokenMatch(action, "network") ||
		actionTokenMatch(action, "url") || actionTokenMatch(action, "curl") ||
		actionTokenMatch(action, "wget") {
		labels = append(labels, "network_access")
	}
	if actionTokenMatch(action, "file") || actionTokenMatch(action, "read") ||
		actionTokenMatch(action, "write") || actionTokenMatch(action, "open") ||
		actionTokenMatch(action, "save") || actionTokenMatch(action, "path") {
		labels = append(labels, "file_system_access")
	}
	// "query" alone is too broad (search_query); require db/sql context or exact.
	if actionTokenMatch(action, "db") || actionTokenMatch(action, "sql") ||
		actionTokenMatch(action, "database") || action == "query" {
		labels = append(labels, "database_access")
	}
	return labels
}

// inferSecurityLabels maps step actions to security labels.
// Matching is token-aware so benign names like "runtime_info" / "capital" are not
// mis-tagged as shell_exec / network_access. Result order is sorted for stable logs.
func inferSecurityLabels(steps []skill.SkillYAMLStep) []string {
	labelSet := make(map[string]bool)
	for _, step := range steps {
		for _, l := range labelsForAction(step.Action) {
			labelSet[l] = true
		}
	}
	if len(labelSet) == 0 {
		return nil
	}
	labels := make([]string, 0, len(labelSet))
	for l := range labelSet {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	return labels
}

// maxSkillDescriptionBytes is the hard limit enforced by skill.yaml schema.
const maxSkillDescriptionBytes = 500

// maxAutoSummarySkillNameBytes mirrors ValidateSkillDraft's persisted name
// contract. Keep suffixing below this bound so a collision can be resolved
// rather than turning a valid generated draft into a later validation failure.
const maxAutoSummarySkillNameBytes = 60

// truncateSkillDescription shortens description to at most maxBytes, cutting on a
// UTF-8 rune boundary and appending an ellipsis when room allows.
func truncateSkillDescription(desc string, maxBytes int) string {
	if maxBytes <= 0 || len(desc) <= maxBytes {
		return desc
	}
	ellipsis := "…"
	limit := maxBytes
	if maxBytes > len(ellipsis) {
		limit = maxBytes - len(ellipsis)
	}
	n := 0
	for _, r := range desc {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 1
		}
		if n+size > limit {
			break
		}
		n += size
	}
	if n == 0 {
		// First rune alone exceeds the ellipsis budget: take as many full runes
		// as fit in maxBytes (never split a multi-byte code point).
		n = 0
		for _, r := range desc {
			size := utf8.RuneLen(r)
			if size < 0 {
				size = 1
			}
			if n+size > maxBytes {
				break
			}
			n += size
		}
		if n == 0 {
			return ""
		}
		return desc[:n]
	}
	if maxBytes-n >= len(ellipsis) {
		return desc[:n] + ellipsis
	}
	return desc[:n]
}

func stripStepsRequiringLabels(steps []skill.SkillYAMLStep, blocked []string) (kept []skill.SkillYAMLStep, removed int) {
	if len(blocked) == 0 {
		return steps, 0
	}
	blockSet := make(map[string]bool, len(blocked))
	for _, b := range blocked {
		blockSet[b] = true
	}
	kept = make([]skill.SkillYAMLStep, 0, len(steps))
	for _, step := range steps {
		deny := false
		for _, l := range labelsForAction(step.Action) {
			if blockSet[l] {
				deny = true
				break
			}
		}
		if deny {
			removed++
			continue
		}
		kept = append(kept, step)
	}
	return kept, removed
}

// deniedSecurityLabels returns labels the policy would reject for this skill.
// Evaluates every label (unlike CheckLabels fail-fast) so callers can sanitize.
// Mirrors CheckLabels semantics: Deny always denies; Ask denies only when askFn
// is set and returns false; nil askFn on Ask means allow.
func deniedSecurityLabels(checker *SecurityPolicyChecker, skillName string, labels []string) []string {
	if checker == nil || len(labels) == 0 {
		return nil
	}
	var denied []string
	for _, label := range labels {
		switch checker.getModeForLabel(label) {
		case SecurityDeny:
			denied = append(denied, label)
		case SecurityAsk:
			if checker.askFn != nil && !checker.askFn(label, skillName) {
				denied = append(denied, label)
			}
		}
	}
	return denied
}

// ValidateSkillDraft validates a Skill draft against structural rules and security policy.
// It collects ALL failure reasons and returns a ValidationError if any rule is violated.
// Soft fixes applied before hard failure:
//   - description longer than 500 bytes is truncated (not rejected)
//   - steps requiring security labels the user/policy denies are stripped first; only if
//     zero steps remain is validation failed (background auto-summary cannot pop UI)
//
// If the name conflicts with existingNames, a timestamp suffix is appended.
// Returns the (possibly modified) draft and nil error on success.
func ValidateSkillDraft(
	draft *skill.SkillYAMLFile,
	checker *SecurityPolicyChecker,
	existingNames map[string]bool,
) (*skill.SkillYAMLFile, error) {
	if draft == nil {
		return nil, &ValidationError{Reasons: []string{"draft must not be nil"}}
	}
	var reasons []string

	// Validate name.
	if strings.TrimSpace(draft.Name) == "" {
		reasons = append(reasons, "name must not be empty")
	} else if len(draft.Name) > 60 {
		reasons = append(reasons, "name must be <= 60 characters")
	}

	// Validate description — soft-truncate oversize drafts from LLM.
	if strings.TrimSpace(draft.Description) == "" {
		reasons = append(reasons, "description must not be empty")
	} else if len(draft.Description) > maxSkillDescriptionBytes {
		before := len(draft.Description)
		draft.Description = truncateSkillDescription(draft.Description, maxSkillDescriptionBytes)
		log.Printf("[skill-auto-summary] truncated description from %d to %d bytes for skill=%s",
			before, len(draft.Description), draft.Name)
	}

	// Validate triggers.
	if len(draft.Triggers) == 0 {
		reasons = append(reasons, "at least 1 trigger is required")
	}

	// Security policy FIRST: strip blocked-capability steps before counting /
	// validating actions so empty-action errors don't refer to steps we drop.
	if checker != nil && len(draft.Steps) > 0 {
		labels := inferSecurityLabels(draft.Steps)
		if len(labels) > 0 {
			denied := deniedSecurityLabels(checker, draft.Name, labels)
			if len(denied) > 0 {
				kept, removed := stripStepsRequiringLabels(draft.Steps, denied)
				if removed > 0 {
					log.Printf("[skill-auto-summary] stripped %d step(s) blocked by security labels %v for skill=%s (kept=%d)",
						removed, denied, draft.Name, len(kept))
					draft.Steps = kept
				}
				if len(draft.Steps) == 0 {
					reasons = append(reasons, fmt.Sprintf(
						"security policy: all steps require denied capabilities %v", denied))
				}
			}
		}
	}

	// Validate remaining steps (after security strip).
	if len(draft.Steps) == 0 {
		// Avoid duplicate "at least 1 step" when security already explained why.
		hasSecurityEmpty := false
		for _, r := range reasons {
			if strings.Contains(r, "security policy") {
				hasSecurityEmpty = true
				break
			}
		}
		if !hasSecurityEmpty {
			reasons = append(reasons, "at least 1 step is required")
		}
	} else {
		for i, step := range draft.Steps {
			if strings.TrimSpace(step.Action) == "" {
				reasons = append(reasons, fmt.Sprintf("step[%d] action must not be empty", i))
			}
		}
	}

	// If there are validation failures, return them all.
	if len(reasons) > 0 {
		return draft, &ValidationError{Reasons: reasons}
	}

	// Name dedup: if name conflicts with existing, append timestamp suffix.
	if existingNames != nil && existingNames[draft.Name] {
		draft.Name = draft.Name + "_" + time.Now().Format("20060102150405")
	}

	return draft, nil
}

// QualityGateResult holds the result of the quality gate evaluation.
type QualityGateResult struct {
	Status   string
	Score    int
	SkillDir string
}

// RunQualityGate writes the skill draft to the local skills directory,
// generates tags metadata, evaluates the skill, and returns the gate result.
// Tags generation failure is non-fatal (logged as warning).
// Disk write failures are fatal and return an error.
func RunQualityGate(draft *skill.SkillYAMLFile, tagGen *TagGenerator) (*QualityGateResult, error) {
	if draft == nil {
		return nil, fmt.Errorf("quality gate: draft is required")
	}
	// 1. Get the base skills directory.
	baseDir, err := skill.PrimarySkillsDir()
	if err != nil {
		return nil, fmt.Errorf("quality gate: get skills dir: %w", err)
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("quality gate: create skills root: %w", err)
	}

	// 2. Reserve a new subdirectory named after the skill. A failed or colliding
	// draft must not leave a directory behind: future auto-summaries use the
	// directory name as part of their uniqueness contract.
	skillDir := filepath.Join(baseDir, toKebabCase(draft.Name))
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("quality gate: skill directory already exists at %s", skillDir)
		}
		return nil, fmt.Errorf("quality gate: create skill dir: %w", err)
	}
	createdSkillDir := true
	defer func() {
		if createdSkillDir {
			if removeErr := os.RemoveAll(skillDir); removeErr != nil {
				log.Printf("[quality-gate] cleanup incomplete skill dir %s: %v", skillDir, removeErr)
			}
		}
	}()

	// 3. Format the draft and write skill.yaml.
	data, err := skill.FormatSkillYAMLFile(draft)
	if err != nil {
		return nil, fmt.Errorf("quality gate: format skill.yaml: %w", err)
	}
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := fileutil.AtomicWriteFile(yamlPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("quality gate: write skill.yaml: %w", err)
	}

	// 4-6. Generate tags (optional; failure is logged but not fatal).
	if tagGen != nil {
		meta, err := tagGen.GenerateTags(skillDir)
		if err != nil {
			log.Printf("[quality-gate] warning: GenerateTags failed for %s: %v", skillDir, err)
		} else {
			if err := tagGen.WriteBackToYAML(skillDir, meta); err != nil {
				log.Printf("[quality-gate] warning: WriteBackToYAML failed for %s: %v", skillDir, err)
			}
		}
	}

	// 7. Simulate a successful execution result for scoring.
	result := SkillExecutionResult{
		Success:       true,
		OutputQuality: "basic",
	}

	// 8. Evaluate the skill execution to get a score.
	score := EvaluateSkillExecution(&result)

	// 9. Determine status based on score threshold.
	status := skillQualityGateStatusForScore(score)
	createdSkillDir = false

	return &QualityGateResult{
		Status:   string(status),
		Score:    score,
		SkillDir: skillDir,
	}, nil
}

func skillDefinitionExists(skillDir string) bool {
	if strings.TrimSpace(skillDir) == "" {
		return false
	}
	if defPath, _ := findSkillDefinitionFile(skillDir); defPath != "" {
		return true
	}
	return findSkillMarkdownDocPath(skillDir) != ""
}

// SkillAutoSummaryPipeline orchestrates the end-to-end skill auto-summary
// flow: complexity analysis => draft => validate => quality gate => register.
type SkillAutoSummaryPipeline struct {
	tagGen    *TagGenerator
	checker   *SecurityPolicyChecker
	skillExec *SkillExecutor
	activity  *AgentActivityStore

	// namingLLM, when set and configured, generates a semantic skill name
	// from the task description and tool sequence. nil => heuristic naming.
	namingLLM skillNamingLLM

	mu        sync.Mutex
	processed map[string]bool // session_id => already processed (idempotent)
}

// NewSkillAutoSummaryPipeline creates a new pipeline with all required dependencies.
func NewSkillAutoSummaryPipeline(
	tagGen *TagGenerator,
	checker *SecurityPolicyChecker,
	skillExec *SkillExecutor,
	activity *AgentActivityStore,
) *SkillAutoSummaryPipeline {
	return &SkillAutoSummaryPipeline{
		tagGen:    tagGen,
		checker:   checker,
		skillExec: skillExec,
		activity:  activity,
		processed: make(map[string]bool),
	}
}

// WithNamingLLM attaches an optional LLM used to generate semantic skill
// names. Returns the pipeline for chaining.
func (p *SkillAutoSummaryPipeline) WithNamingLLM(l skillNamingLLM) *SkillAutoSummaryPipeline {
	p.namingLLM = l
	return p
}

// RunPipeline executes the full skill auto-summary pipeline for a session.
// It is idempotent: repeated calls with the same session_id are skipped.
// Each stage is run sequentially; any failure aborts subsequent stages.
func (p *SkillAutoSummaryPipeline) RunPipeline(session *TrajectorySession) {
	if session == nil {
		return
	}
	sid := session.SessionID
	if !isAutoSummaryEligibleSession(session) {
		log.Printf("[skill-auto-summary] session=%s skipped due to terminal status=%q", sid, session.Status)
		return
	}

	// Idempotency check must check and mark in the same critical section
	// to prevent two goroutines from both passing the check.
	p.mu.Lock()
	if p.processed[sid] {
		p.mu.Unlock()
		log.Printf("[skill-auto-summary] session %s already processed, skipping", sid)
		return
	}
	p.processed[sid] = true
	p.mu.Unlock()

	// Ensure activity is cleared when pipeline exits (success or failure).
	defer p.clearActivity()

	// Update activity store to indicate summarizing is in progress.
	if p.activity != nil {
		p.activity.Update(&AgentActivity{
			Source:      "skill_summarizing",
			Task:        "Auto summarize Skill",
			LastSummary: "pipeline started",
		})
	}

	// Stage 1: AnalyzeComplexity
	complexity := AnalyzeComplexity(session)
	log.Printf("[skill-auto-summary] session=%s stage=AnalyzeComplexity result=%s steps=%d tools=%d turns=%d",
		sid, complexity.Score, complexity.StepCount, complexity.ToolKindCount, complexity.TurnCount)
	if complexity.Score == "too_simple" {
		log.Printf("[skill-auto-summary] session=%s too_simple, skipping", sid)
		return
	}

	// Stage 2: DraftSkill
	draft, err := DraftSkill(session)
	if err != nil {
		log.Printf("[skill-auto-summary] session=%s stage=DraftSkill error=%v", sid, err)
		return
	}
	log.Printf("[skill-auto-summary] session=%s stage=DraftSkill result=ok name=%s steps=%d",
		sid, draft.Name, len(draft.Steps))

	// Validate before similarity matching or disk mutation. Previously a draft
	// could reach VersionedUpdate before its structure and security policy were
	// checked, allowing an invalid trajectory to replace a healthy learned skill.
	draft, err = ValidateSkillDraft(draft, p.checker, nil)
	if err != nil {
		log.Printf("[skill-auto-summary] session=%s stage=ValidateSkillDraft error=%v", sid, err)
		return
	}
	log.Printf("[skill-auto-summary] session=%s stage=ValidateSkillDraft result=ok name=%s",
		sid, draft.Name)

	// Reject incompatible drafts before similarity matching. In particular, this
	// must happen before VersionedUpdate so an invalid new trajectory can never
	// overwrite a previously runnable learned skill.
	if err := validateAutoSummaryRunnerCompatibility(draft); err != nil {
		log.Printf("[skill-auto-summary] session=%s stage=RunnerCompatibility error=%v", sid, err)
		return
	}
	log.Printf("[skill-auto-summary] session=%s stage=RunnerCompatibility result=ok runner=%s",
		sid, skill.RunnerBackendGUI)

	// Stage 2.5: FindSimilarSkill checks if an existing skill should be updated.
	existing, simScore := skill.FindSimilarSkill(draft.Description, 0.6)
	if existing != nil {
		log.Printf("[skill-auto-summary] session=%s stage=FindSimilarSkill result=matched score=%.2f existing=%s",
			sid, simScore, existing.Name)
		if shouldUpdateSkill(draft, existing) {
			versioner := &skill.Versioner{}
			ver, backupErr := versioner.BackupCurrent(existing.SkillDir)
			if backupErr != nil {
				log.Printf("[skill-auto-summary] session=%s stage=VersionedUpdate backup_error=%v", sid, backupErr)
			} else {
				log.Printf("[skill-auto-summary] session=%s stage=VersionedUpdate backed_up=v%d", sid, ver)
				// A versioned update must keep the existing skill's identity:
				// writing the draft's fresh name into the old directory would
				// orphan the previously registered name and duplicate the
				// skill in the registry.
				draft.Name = existing.Name
				// Write new version.
				defPath, defFormat := findSkillDefinitionFile(existing.SkillDir)
				if defPath == "" {
					defPath = filepath.Join(existing.SkillDir, "skill.yaml")
					defFormat = "yaml"
				}
				// Ensure rewritten YAML keeps self-learned classification.
				if strings.TrimSpace(draft.Source) == "" {
					draft.Source = "learned"
				}
				data, fmtErr := skill.FormatSkillDefinitionFile(draft, defFormat)
				if fmtErr == nil {
					if writeErr := fileutil.AtomicWriteFile(defPath, data, 0o644); writeErr != nil {
						log.Printf("[skill-auto-summary] session=%s stage=VersionedUpdate write_error=%v", sid, writeErr)
					} else {
						log.Printf("[skill-auto-summary] session=%s stage=VersionedUpdate result=ok", sid)
						_ = versioner.CleanOldVersions(existing.SkillDir, 5)
						if p.skillExec != nil {
							if regErr := registerAutoSummaryLearnedSkill(p.skillExec, draft, existing.SkillDir); regErr != nil {
								log.Printf("[skill-auto-summary] session=%s stage=RegisterLearned error=%v", sid, regErr)
							}
						}
					}
				}
			}
		} else {
			log.Printf("[skill-auto-summary] session=%s stage=FindSimilarSkill existing skill is better, skipping iteration", sid)
		}
		return // Skip new skill creation; either updated or skipped.
	}
	log.Printf("[skill-auto-summary] session=%s stage=FindSimilarSkill result=unmatched score=%.2f", sid, simScore)

	// Stage 2.6: NameSkill — ask the LLM for a semantic name based on the
	// task description and tool sequence. Falls back to the heuristic name
	// from DraftSkill when the LLM is unavailable or returns junk.
	// Runs only on the new-skill path: versioned updates keep the existing
	// skill's name, so naming there would waste the LLM call.
	if p.namingLLM != nil {
		var existingNames map[string]bool
		if p.skillExec != nil {
			skills := p.skillExec.loadSkills()
			existingNames = make(map[string]bool, len(skills)*2)
			for _, s := range skills {
				existingNames[s.Name] = true
				// Directory names are derived via toKebabCase, so include the
				// kebab form too — a _ vs - difference must still count as a
				// collision.
				existingNames[toKebabCase(s.Name)] = true
			}
		}
		name, usedLLM := GenerateSkillNameWithLLM(p.namingLLM, draft.Description, draft.Steps, existingNames)
		source := "fallback"
		if usedLLM {
			source = "llm"
		}
		log.Printf("[skill-auto-summary] session=%s stage=NameSkill result=%s name=%s", sid, source, name)
		draft.Name = name
	}

	// The heuristic fallback does not receive existing names, and the directory
	// on disk is normalized to kebab case. Reserve a unique name for every new
	// learned skill here; VersionedUpdate returned above intentionally retains
	// the matched skill identity instead.
	draft.Name = uniqueAutoSummarySkillName(draft.Name, p.autoSummaryExistingSkillNames())

	// Stage 3: ValidateSkillDraft again after optional LLM naming. The first
	// validation above protects existing skills from a bad draft; this final pass
	// verifies that an LLM-supplied name still satisfies the same contract before
	// a new skill reaches the quality gate.
	draft, err = ValidateSkillDraft(draft, p.checker, nil)
	if err != nil {
		log.Printf("[skill-auto-summary] session=%s stage=ValidateSkillDraft error=%v", sid, err)
		return
	}
	log.Printf("[skill-auto-summary] session=%s stage=ValidateSkillDraft result=ok name=%s",
		sid, draft.Name)

	// Stage 4: RunQualityGate
	gateResult, err := RunQualityGate(draft, p.tagGen)
	if err != nil {
		log.Printf("[skill-auto-summary] session=%s stage=RunQualityGate error=%v", sid, err)
		return
	}
	log.Printf("[skill-auto-summary] session=%s stage=RunQualityGate result=%s score=%d dir=%s",
		sid, gateResult.Status, gateResult.Score, gateResult.SkillDir)

	// Stage 4.5: Register as a learned skill so UI "自学习" filters and
	// config-backed lifecycle paths see Source=learned immediately
	// (scanner also resolves craft_* / source: learned from disk).
	if p.skillExec != nil {
		if regErr := registerAutoSummaryLearnedSkill(p.skillExec, draft, gateResult.SkillDir); regErr != nil {
			log.Printf("[skill-auto-summary] session=%s stage=RegisterLearned error=%v", sid, regErr)
		} else {
			log.Printf("[skill-auto-summary] session=%s stage=RegisterLearned result=ok name=%s", sid, draft.Name)
		}
	}

	// Stage 5: Registration only — generation never uploads. The skill is
	// auto-uploaded only after it reaches skill_auto_upload_min_successes
	// successful real runs, via the run path (SkillRunner.tryAutoUpload).
	if normalizeSkillQualityGateStatus(gateResult.Status) == skillQualityGateStatusApproved {
		log.Printf("[skill-auto-summary] session=%s stage=Register result=ok name=%s (auto upload deferred until the success-run threshold is met)", sid, draft.Name)
	} else {
		log.Printf("[skill-auto-summary] session=%s stage=Register skipped (status=%s)", sid, gateResult.Status)
	}
}

// isAutoSummaryEligibleSession keeps failed or interrupted trajectories from
// teaching the system a recipe. Empty status is accepted for older on-disk
// trajectories that predate outcome recording.
func isAutoSummaryEligibleSession(session *TrajectorySession) bool {
	if session == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(session.Status)) {
	case "", "success", "succeeded":
		return true
	default:
		return false
	}
}

func (p *SkillAutoSummaryPipeline) autoSummaryExistingSkillNames() map[string]bool {
	if p == nil || p.skillExec == nil {
		return nil
	}
	skills := p.skillExec.loadSkills()
	if len(skills) == 0 {
		return nil
	}
	names := make(map[string]bool, len(skills)*2)
	for _, entry := range skills {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		names[name] = true
		names[toKebabCase(name)] = true
	}
	return names
}

func uniqueAutoSummarySkillName(name string, existing map[string]bool) string {
	if len(existing) == 0 {
		return name
	}
	base := strings.TrimSpace(name)
	if base == "" || (!existing[base] && !existing[toKebabCase(base)]) {
		return name
	}
	for suffix := 2; ; suffix++ {
		suffixText := "_" + strconv.Itoa(suffix)
		candidateBase := truncateUTF8Bytes(base, maxAutoSummarySkillNameBytes-len(suffixText))
		candidateBase = strings.TrimRight(candidateBase, "_-")
		if candidateBase == "" {
			// The original name has already passed ValidateSkillDraft, so this is
			// only reachable for an unusually small future name limit.
			candidateBase = "skill"
		}
		candidate := candidateBase + suffixText
		if !existing[candidate] && !existing[toKebabCase(candidate)] {
			return candidate
		}
	}
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := 0
	for _, r := range value {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 1
		}
		if end+size > maxBytes {
			break
		}
		end += size
	}
	return value[:end]
}

// clearActivity clears the skill_summarizing activity from the store.
func (p *SkillAutoSummaryPipeline) clearActivity() {
	if p.activity != nil {
		p.activity.Clear("skill_summarizing")
	}
}

// registerAutoSummaryLearnedSkill persists a thin Source=learned overlay into
// config (name + source + SkillDir), matching skill-recorder semantics.
// Steps remain on disk under skill.yaml and are hydrated by loadSkills.
//
// Uses UpdateLearnedSource (config-only upsert) instead of Register(loadSkills)
// so we never rewrite the entire merged skill list into config.json.
func registerAutoSummaryLearnedSkill(exec *SkillExecutor, draft *skill.SkillYAMLFile, skillDir string) error {
	if exec == nil || draft == nil {
		return fmt.Errorf("skill executor and draft are required")
	}
	name := strings.TrimSpace(draft.Name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	source := strings.TrimSpace(draft.Source)
	if !corelib.IsLearnedSource(source) {
		source = "learned"
	} else {
		source = strings.ToLower(source)
	}
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return fmt.Errorf("skill dir is required for learned overlay")
	}
	entry := corelib.NLSkillEntry{
		Name:     name,
		Source:   source,
		SkillDir: skillDir,
		Status:   "active",
	}
	// UpdateLearnedSource → saveSkills already invalidates skill list + scanner cache.
	return exec.UpdateLearnedSource(entry)
}

// shouldUpdateSkill permits automatic replacement only when the draft preserves
// the executable action plan and demonstrably removes recorded failures. A
// shorter trace is not evidence of an improvement: it may have omitted a
// required validation or deployment action. Structural optimizations that alter
// the plan should be proposed separately, not silently applied by auto-summary.
func shouldUpdateSkill(newDraft *skill.SkillYAMLFile, existing *corelib.NLSkillEntry) bool {
	if newDraft == nil || existing == nil || !sameAutoSummaryActionPlan(newDraft.Steps, existing.Steps) {
		return false
	}

	newSteps := len(newDraft.Steps)

	newErrors := 0
	for _, s := range newDraft.Steps {
		if s.OnError == "skip" {
			newErrors++
		}
	}
	oldErrors := 0
	for _, s := range existing.Steps {
		if s.OnError == "skip" || s.OnError == "continue" {
			oldErrors++
		}
	}

	return newSteps == len(existing.Steps) && newErrors < oldErrors
}

func sameAutoSummaryActionPlan(draftSteps []skill.SkillYAMLStep, existingSteps []corelib.NLSkillStep) bool {
	if len(draftSteps) == 0 || len(draftSteps) != len(existingSteps) {
		return false
	}
	for i := range draftSteps {
		if !sameAutoSummaryStepContract(draftSteps[i], existingSteps[i]) {
			return false
		}
	}
	return true
}

// sameAutoSummaryStepContract compares every execution-affecting field. An
// action name alone is insufficient: changing a command, path, repeat count,
// condition, timeout, poll, or loop can materially change a skill while still
// appearing to be a lower-error run. OnError is intentionally excluded: it is
// the observed-failure signal that this function is comparing. Names are
// display-only and deliberately excluded. Existing fallback steps are not
// generated by this pipeline, so refuse to auto-replace them rather than
// discard their recovery behavior.
func sameAutoSummaryStepContract(draft skill.SkillYAMLStep, existing corelib.NLSkillStep) bool {
	if skill.NormalizeStepActionName(draft.Action) != skill.NormalizeStepActionName(existing.Action) ||
		draft.Condition != existing.Condition ||
		draft.When != existing.When ||
		draft.Label != existing.Label ||
		existing.FallbackStep != nil ||
		!sameAutoSummaryParams(normalizeAutoSummaryParams(draft.Params, draft.TimeoutSeconds), normalizeAutoSummaryParams(existing.Params, 0)) ||
		!reflect.DeepEqual(normalizeAutoSummaryCapture(draft.Capture), normalizeAutoSummaryCapture(existing.Capture)) ||
		!sameAutoSummaryPoll(draft.Poll, existing.Poll) ||
		!sameAutoSummaryLoop(draft.Loop, existing.Loop) {
		return false
	}
	return true
}

// sameAutoSummaryParams compares JSON/YAML-derived values semantically. A
// live tool call represents numbers as float64, while a YAML round-trip may
// preserve them as int or uint; reflect.DeepEqual would reject an otherwise
// identical execution contract and prevent safe failure-only updates.
func sameAutoSummaryParams(left, right map[string]interface{}) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == 0 && len(right) == 0
	}
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		rightValue, ok := right[key]
		if !ok || !sameAutoSummaryValue(leftValue, rightValue) {
			return false
		}
	}
	return true
}

func sameAutoSummaryValue(left, right interface{}) bool {
	if leftNumber, ok := autoSummaryNumber(left); ok {
		rightNumber, rightOK := autoSummaryNumber(right)
		return rightOK && sameAutoSummaryNumber(leftNumber, rightNumber)
	}
	switch leftValue := left.(type) {
	case map[string]interface{}:
		rightValue, ok := right.(map[string]interface{})
		return ok && sameAutoSummaryParams(leftValue, rightValue)
	case []interface{}:
		rightValue, ok := right.([]interface{})
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for i := range leftValue {
			if !sameAutoSummaryValue(leftValue[i], rightValue[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(left, right)
	}
}

type autoSummaryNumericValue struct {
	isFloat  bool
	isSigned bool
	float    float64
	signed   int64
	unsigned uint64
}

func sameAutoSummaryNumber(left, right autoSummaryNumericValue) bool {
	if left.isFloat || right.isFloat {
		leftFloat, leftOK := left.asExactFloat()
		rightFloat, rightOK := right.asExactFloat()
		return leftOK && rightOK && leftFloat == rightFloat
	}
	if left.isSigned && left.signed < 0 || right.isSigned && right.signed < 0 {
		return left.signed == right.signed
	}
	return left.asUnsigned() == right.asUnsigned()
}

func (value autoSummaryNumericValue) asUnsigned() uint64 {
	if value.isSigned {
		return uint64(value.signed)
	}
	return value.unsigned
}

func (value autoSummaryNumericValue) asExactFloat() (float64, bool) {
	if value.isFloat {
		return value.float, !math.IsNaN(value.float) && !math.IsInf(value.float, 0)
	}
	const maxExactlyRepresentableInteger = uint64(1 << 53)
	if value.isSigned && value.signed < 0 {
		magnitude := uint64(-(value.signed + 1)) + 1
		if magnitude > maxExactlyRepresentableInteger {
			return 0, false
		}
		return float64(value.signed), true
	}
	unsigned := value.asUnsigned()
	if unsigned > maxExactlyRepresentableInteger {
		return 0, false
	}
	return float64(unsigned), true
}

func autoSummaryNumber(value interface{}) (autoSummaryNumericValue, bool) {
	switch number := value.(type) {
	case int:
		return autoSummaryNumericValue{isSigned: true, signed: int64(number)}, true
	case int8:
		return autoSummaryNumericValue{isSigned: true, signed: int64(number)}, true
	case int16:
		return autoSummaryNumericValue{isSigned: true, signed: int64(number)}, true
	case int32:
		return autoSummaryNumericValue{isSigned: true, signed: int64(number)}, true
	case int64:
		return autoSummaryNumericValue{isSigned: true, signed: number}, true
	case uint:
		return autoSummaryNumericValue{unsigned: uint64(number)}, true
	case uint8:
		return autoSummaryNumericValue{unsigned: uint64(number)}, true
	case uint16:
		return autoSummaryNumericValue{unsigned: uint64(number)}, true
	case uint32:
		return autoSummaryNumericValue{unsigned: uint64(number)}, true
	case uint64:
		return autoSummaryNumericValue{unsigned: number}, true
	case float32:
		return autoSummaryNumericValue{isFloat: true, float: float64(number)}, true
	case float64:
		return autoSummaryNumericValue{isFloat: true, float: number}, true
	default:
		return autoSummaryNumericValue{}, false
	}
}

func normalizeAutoSummaryParams(params map[string]interface{}, timeoutSeconds int) map[string]interface{} {
	if len(params) == 0 && timeoutSeconds <= 0 {
		return nil
	}
	normalized := make(map[string]interface{}, len(params)+1)
	for key, value := range params {
		normalized[key] = value
	}
	if timeoutSeconds > 0 {
		normalized["timeout"] = float64(timeoutSeconds)
	}
	return normalized
}

func normalizeAutoSummaryCapture(capture map[string]string) map[string]string {
	if len(capture) == 0 {
		return nil
	}
	return capture
}

func sameAutoSummaryPoll(draft *skill.SkillYAMLStepPoll, existing *corelib.StepPollConfig) bool {
	if draft == nil || existing == nil {
		return draft == nil && existing == nil
	}
	return draft.Interval == existing.Interval && draft.MaxAttempts == existing.MaxAttempts &&
		draft.UntilMatch == existing.UntilMatch && draft.UntilStatus == existing.UntilStatus
}

func sameAutoSummaryLoop(draft *skill.SkillYAMLStepLoop, existing *corelib.StepLoopConfig) bool {
	if draft == nil || existing == nil {
		return draft == nil && existing == nil
	}
	return draft.MaxIterations == existing.MaxIterations && draft.UntilStep == existing.UntilStep &&
		draft.UntilMatch == existing.UntilMatch && draft.OnFailStep == existing.OnFailStep
}
