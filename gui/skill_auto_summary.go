package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
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

	// Build a map of tool_call_id => tool result content for error detection.
	toolResults := buildToolResultMap(session.Entries)

	// Extract raw steps from all assistant entries with ToolCalls.
	var rawSteps []skill.SkillYAMLStep
	for _, entry := range session.Entries {
		if entry.Role != "assistant" || entry.ToolCalls == nil {
			continue
		}
		steps := extractStepsFromToolCalls(entry.ToolCalls, toolResults)
		rawSteps = append(rawSteps, steps...)
	}

	if len(rawSteps) == 0 {
		return nil, fmt.Errorf("no tool_calls found in session")
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

// extractStepsFromToolCalls converts a ToolCalls interface{} into SkillYAMLStep
// slice, checking each tool call's result for errors. Uses the same normalizer
// as trajectory recording so live []llm.ToolCall payloads are not dropped.
func extractStepsFromToolCalls(toolCalls interface{}, toolResults map[string]string) []skill.SkillYAMLStep {
	extracted := extractTrajectoryToolCalls(toolCalls)
	if len(extracted) == 0 {
		return nil
	}
	steps := make([]skill.SkillYAMLStep, 0, len(extracted))
	for _, tc := range extracted {
		if tc.Name == "" {
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

// mergeConsecutiveSteps merges consecutive steps with the same action name
// into a single step with _repeat_count in params.
func mergeConsecutiveSteps(steps []skill.SkillYAMLStep) []skill.SkillYAMLStep {
	if len(steps) == 0 {
		return steps
	}
	var merged []skill.SkillYAMLStep
	i := 0
	for i < len(steps) {
		current := steps[i]
		count := 1
		for i+count < len(steps) && steps[i+count].Action == current.Action {
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
	// 1. Get the base skills directory.
	baseDir, err := skill.PrimarySkillsDir()
	if err != nil {
		return nil, fmt.Errorf("quality gate: get skills dir: %w", err)
	}

	// 2. Create a subdirectory named after the skill.
	skillDir := filepath.Join(baseDir, toKebabCase(draft.Name))
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, fmt.Errorf("quality gate: create skill dir: %w", err)
	}

	// 3. Format the draft and write skill.yaml.
	data, err := skill.FormatSkillYAMLFile(draft)
	if err != nil {
		return nil, fmt.Errorf("quality gate: format skill.yaml: %w", err)
	}
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := os.WriteFile(yamlPath, data, 0o644); err != nil {
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

	return &QualityGateResult{
		Status:   string(status),
		Score:    score,
		SkillDir: skillDir,
	}, nil
}

// RunAutoUpload executes the auto-upload flow for a newly created skill.
// It records the execution, packages the skill into a zip, checks upload
// conditions, and submits to SkillMarket if appropriate.
// HubCenter not configured => skip upload with warning log, return nil.
// Upload failure => log error, return error (caller preserves local skill).
func RunAutoUpload(
	ctx context.Context,
	skillName string,
	skillDir string,
	score int,
	trigger *AutoUploadTrigger,
	skillExec *SkillExecutor,
	client *SkillMarketClient,
) error {
	if trigger == nil || skillExec == nil {
		log.Printf("[auto-upload] dependencies missing, skipping upload for skill %s", skillName)
		return nil
	}

	if score < 1 {
		log.Printf("[auto-upload] quality score too low, skipping upload for skill %s", skillName)
		return nil
	}

	localHash := ""
	if strings.TrimSpace(skillDir) != "" {
		if !skillDefinitionExists(skillDir) {
			return runLegacyLearnedSkillAutoUpload(ctx, skillName, skillDir, score, trigger, skillExec, client)
		}
		_, report, err := prepareSkillDirForMarket(skillDir, true)
		if err != nil {
			return fmt.Errorf("auto-upload: portability preparation failed: %w", err)
		}
		entry, loadErr := loadMarketPackageSkillEntry(skillDir, nil)
		if loadErr == nil {
			quality := evaluateSkillQuality(entry, report, false)
			writeSkillQualityStatus(skillDir, entry, quality, "generated_upload", false)
			if !quality.MarketReady {
				return fmt.Errorf("auto-upload blocked by quality gate: score=%d reasons=%s", quality.Score, strings.Join(quality.Reasons, "; "))
			}
		}
		localHash = skillDirHash(skillDir)
	}

	// 1. Record the verified generated skill. RunAutoUpload is used after the
	// quality gate, so it uploads immediately when SkillMarket is reachable while
	// still recording the hash to avoid future duplicate uploads.
	trigger.RecordExecution(skillName, score, localHash)

	// 2. Hand off to the lifecycle queue. The queue keeps the "timely upload"
	// promise without losing good skills when HubCenter, email config, or the
	// network is temporarily unavailable.
	if client == nil || client.app == nil {
		log.Printf("[auto-upload] HubCenter client not initialized, queued upload unavailable for skill %s", skillName)
		return nil
	}
	ensureLifecycleUploadEmail(client.app, trigger)
	client.app.ensureSkillLifecycleManager()
	if client.app.skillLifecycle == nil {
		log.Printf("[auto-upload] lifecycle manager not initialized, skipping upload for skill %s", skillName)
		return nil
	}
	if _, err := client.app.skillLifecycle.EnqueueUpload(ctx, skillName, skillDir, "generated_upload", false, true); err != nil {
		return fmt.Errorf("auto-upload: enqueue skill: %w", err)
	}
	log.Printf("[auto-upload] skill %s queued for SkillMarket upload", skillName)
	return nil
}

func ensureLifecycleUploadEmail(app *App, trigger *AutoUploadTrigger) {
	if app == nil || trigger == nil || trigger.emailFn == nil {
		return
	}
	email := strings.TrimSpace(trigger.emailFn())
	if email == "" {
		return
	}
	cfg, err := app.LoadConfig()
	if err != nil || strings.TrimSpace(cfg.RemoteEmail) != "" {
		return
	}
	if _, err := app.PatchConfigFields(map[string]interface{}{"remote_email": email}); err != nil {
		log.Printf("[auto-upload] persist upload email failed: %v", err)
	}
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

func runLegacyLearnedSkillAutoUpload(ctx context.Context, skillName, skillDir string, score int, trigger *AutoUploadTrigger, skillExec *SkillExecutor, client *SkillMarketClient) error {
	// Legacy learned skills may not have a file-backed skill.yaml/SKILL.md yet.
	// Record usage history, but let the lifecycle quality gate decide whether the
	// registered skill is market-ready instead of requiring three prior runs here.
	trigger.RecordExecution(skillName, score, skillDirHash(skillDir))

	var app *App
	if client != nil && client.app != nil {
		app = client.app
	} else if skillExec != nil {
		app = skillExec.app
	}
	if app == nil {
		log.Printf("[auto-upload] app not initialized, queued upload unavailable for learned skill %s", skillName)
		return nil
	}
	ensureLifecycleUploadEmail(app, trigger)
	app.ensureSkillLifecycleManager()
	if app.skillLifecycle == nil {
		log.Printf("[auto-upload] lifecycle manager not initialized, skipping learned skill upload for %s", skillName)
		return nil
	}
	if _, err := app.skillLifecycle.EnqueueUpload(ctx, skillName, "", "generated_upload", false, true); err != nil {
		return fmt.Errorf("auto-upload: enqueue learned skill: %w", err)
	}
	log.Printf("[auto-upload] learned skill %s queued for SkillMarket upload", skillName)
	return nil
}

// SkillAutoSummaryPipeline orchestrates the end-to-end skill auto-summary
// flow: complexity analysis => draft => validate => quality gate => auto upload.
type SkillAutoSummaryPipeline struct {
	tagGen    *TagGenerator
	checker   *SecurityPolicyChecker
	trigger   *AutoUploadTrigger
	skillExec *SkillExecutor
	client    *SkillMarketClient
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
	trigger *AutoUploadTrigger,
	skillExec *SkillExecutor,
	client *SkillMarketClient,
	activity *AgentActivityStore,
) *SkillAutoSummaryPipeline {
	return &SkillAutoSummaryPipeline{
		tagGen:    tagGen,
		checker:   checker,
		trigger:   trigger,
		skillExec: skillExec,
		client:    client,
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
					if writeErr := os.WriteFile(defPath, data, 0o644); writeErr != nil {
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

	// Stage 3: ValidateSkillDraft
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

	// Stage 5: RunAutoUpload (only if approved)
	if normalizeSkillQualityGateStatus(gateResult.Status) == skillQualityGateStatusApproved {
		err := RunAutoUpload(
			context.Background(),
			draft.Name,
			gateResult.SkillDir,
			gateResult.Score,
			p.trigger,
			p.skillExec,
			p.client,
		)
		if err != nil {
			log.Printf("[skill-auto-summary] session=%s stage=RunAutoUpload error=%v", sid, err)
			// Upload failure => log error, return error (caller preserves local skill).
		} else {
			log.Printf("[skill-auto-summary] session=%s stage=RunAutoUpload result=ok", sid)
		}
	} else {
		log.Printf("[skill-auto-summary] session=%s stage=RunAutoUpload skipped (status=%s)", sid, gateResult.Status)
	}
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

// shouldUpdateSkill returns true if the new draft is better than the existing skill.
// "Better" means fewer steps (more efficient) or fewer error steps.
func shouldUpdateSkill(newDraft *skill.SkillYAMLFile, existing *corelib.NLSkillEntry) bool {
	newSteps := len(newDraft.Steps)
	oldSteps := len(existing.Steps)

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

	// New version has fewer steps (more efficient).
	if newSteps < oldSteps {
		return true
	}
	// New version has fewer error steps.
	if newErrors < oldErrors {
		return true
	}
	return false
}
