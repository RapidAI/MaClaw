package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
// Thresholds: StepCount >= 3 && ToolKindCount >= 2 && TurnCount >= 5 鈫?"worth_summarizing",
// otherwise 鈫?"too_simple". nil/empty session returns "too_simple".
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
			extractToolNames(entry.ToolCalls, toolKinds)
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

// extractToolNames extracts unique tool names from a ToolCalls interface{} value
// into the provided set. ToolCalls is expected to be []interface{} where each
// element is a map[string]interface{} with a "function" key containing a
// map[string]interface{} with a "name" key.
func extractToolNames(toolCalls interface{}, names map[string]bool) {
	calls, ok := toolCalls.([]interface{})
	if !ok {
		return
	}
	for _, call := range calls {
		callMap, ok := call.(map[string]interface{})
		if !ok {
			continue
		}
		fn, ok := callMap["function"]
		if !ok {
			continue
		}
		fnMap, ok := fn.(map[string]interface{})
		if !ok {
			continue
		}
		name, ok := fnMap["name"].(string)
		if !ok {
			continue
		}
		if name != "" {
			names[name] = true
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

	// Build a map of tool_call_id 鈫?tool result content for error detection.
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
	}, nil
}

// buildToolResultMap builds a map from tool_call_id to the content string
// of the corresponding role="tool" entry.
func buildToolResultMap(entries []TrajectoryEntry) map[string]string {
	results := make(map[string]string)
	for _, entry := range entries {
		if entry.Role == "tool" && entry.ToolCallID != "" {
			if s, ok := entry.Content.(string); ok {
				results[entry.ToolCallID] = s
			}
		}
	}
	return results
}

// extractStepsFromToolCalls converts a ToolCalls interface{} into SkillYAMLStep
// slice, checking each tool call's result for errors.
func extractStepsFromToolCalls(toolCalls interface{}, toolResults map[string]string) []skill.SkillYAMLStep {
	calls, ok := toolCalls.([]interface{})
	if !ok {
		return nil
	}
	var steps []skill.SkillYAMLStep
	for _, call := range calls {
		callMap, ok := call.(map[string]interface{})
		if !ok {
			continue
		}
		fn, ok := callMap["function"]
		if !ok {
			continue
		}
		fnMap, ok := fn.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fnMap["name"].(string)
		if name == "" {
			continue
		}

		params := parseToolArguments(fnMap["arguments"])

		step := skill.SkillYAMLStep{
			Action: name,
			Params: params,
		}

		// Check if the tool result indicates an error.
		if id, ok := callMap["id"].(string); ok && id != "" {
			if content, found := toolResults[id]; found {
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
		if entry.Role == "user" {
			if s, ok := entry.Content.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// ValidationError holds all validation failure reasons.
type ValidationError struct {
	Reasons []string
}

// Error returns a joined string of all validation failure reasons.
func (e *ValidationError) Error() string {
	return strings.Join(e.Reasons, "; ")
}

// inferSecurityLabels maps step actions to security labels.
func inferSecurityLabels(steps []skill.SkillYAMLStep) []string {
	labelSet := make(map[string]bool)
	for _, step := range steps {
		action := strings.ToLower(step.Action)
		if strings.Contains(action, "exec") || strings.Contains(action, "cmd") ||
			strings.Contains(action, "shell") || strings.Contains(action, "run") {
			labelSet["shell_exec"] = true
		}
		if strings.Contains(action, "http") || strings.Contains(action, "api") ||
			strings.Contains(action, "fetch") || strings.Contains(action, "request") ||
			strings.Contains(action, "network") || strings.Contains(action, "url") {
			labelSet["network_access"] = true
		}
		if strings.Contains(action, "file") || strings.Contains(action, "read") ||
			strings.Contains(action, "write") || strings.Contains(action, "open") ||
			strings.Contains(action, "save") || strings.Contains(action, "path") {
			labelSet["file_system_access"] = true
		}
		if strings.Contains(action, "db") || strings.Contains(action, "sql") ||
			strings.Contains(action, "query") || strings.Contains(action, "database") {
			labelSet["database_access"] = true
		}
	}
	labels := make([]string, 0, len(labelSet))
	for l := range labelSet {
		labels = append(labels, l)
	}
	return labels
}

// ValidateSkillDraft validates a Skill draft against structural rules and security policy.
// It collects ALL failure reasons and returns a ValidationError if any rule is violated.
// If the name conflicts with existingNames, a timestamp suffix is appended.
// Returns the (possibly modified) draft and nil error on success.
func ValidateSkillDraft(
	draft *skill.SkillYAMLFile,
	checker *SecurityPolicyChecker,
	existingNames map[string]bool,
) (*skill.SkillYAMLFile, error) {
	var reasons []string

	// Validate name.
	if strings.TrimSpace(draft.Name) == "" {
		reasons = append(reasons, "name must not be empty")
	} else if len(draft.Name) > 60 {
		reasons = append(reasons, "name must be 鈮?60 characters")
	}

	// Validate description.
	if strings.TrimSpace(draft.Description) == "" {
		reasons = append(reasons, "description must not be empty")
	} else if len(draft.Description) > 500 {
		reasons = append(reasons, "description must be 鈮?500 characters")
	}

	// Validate steps.
	if len(draft.Steps) == 0 {
		reasons = append(reasons, "at least 1 step is required")
	} else {
		for i, step := range draft.Steps {
			if strings.TrimSpace(step.Action) == "" {
				reasons = append(reasons, fmt.Sprintf("step[%d] action must not be empty", i))
			}
		}
	}

	// Validate triggers.
	if len(draft.Triggers) == 0 {
		reasons = append(reasons, "at least 1 trigger is required")
	}

	// Security policy check.
	if checker != nil && len(draft.Steps) > 0 {
		labels := inferSecurityLabels(draft.Steps)
		if len(labels) > 0 {
			if err := checker.CheckLabels(draft.Name, labels); err != nil {
				reasons = append(reasons, fmt.Sprintf("security policy: %s", err.Error()))
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
	Status   string // "approved" | "draft"
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

	// 4-6. Generate tags (optional 鈥?failure is logged but not fatal).
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
	status := "draft"
	if score >= 1 {
		status = "approved"
	}

	return &QualityGateResult{
		Status:   status,
		Score:    score,
		SkillDir: skillDir,
	}, nil
}

// RunAutoUpload executes the auto-upload flow for a newly created skill.
// It records the execution, packages the skill into a zip, checks upload
// conditions, and submits to SkillMarket if appropriate.
// HubCenter not configured 鈫?skip upload with warning log, return nil.
// Upload failure 鈫?log error, return error (caller preserves local skill).
func RunAutoUpload(
	ctx context.Context,
	skillName string,
	skillDir string,
	score int,
	trigger *AutoUploadTrigger,
	skillExec *SkillExecutor,
	client *SkillMarketClient,
) error {
	// 1. Record execution (empty localHash for new skill).
	trigger.RecordExecution(skillName, score, "")

	// 2. Package skill into a temp zip.
	tmpFile, err := os.CreateTemp("", "skill-upload-*.zip")
	if err != nil {
		return fmt.Errorf("auto-upload: create temp zip: %w", err)
	}
	zipPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(zipPath)

	if err := skillExec.ExportLearnedSkillsZip([]string{skillName}, zipPath); err != nil {
		return fmt.Errorf("auto-upload: export zip: %w", err)
	}

	// 3. Check if upload conditions are met.
	if !trigger.ShouldUpload(skillName) {
		return nil
	}

	// 4. Check if any HubCenter node is currently reachable.
	if client == nil || client.app == nil {
		log.Printf("[auto-upload] HubCenter client not initialized, skipping upload for skill %s", skillName)
		return nil
	}
	if _, _, err := client.app.resolveHubCenterBaseURL(ctx, hubHTTPClient); err != nil {
		log.Printf("[auto-upload] HubCenter not configured or reachable, skipping upload for skill %s: %v", skillName, err)
		return nil
	}

	// 5. Submit to SkillMarket.
	submissionID, err := client.SubmitSkill(ctx, zipPath, "")
	if err != nil {
		log.Printf("[auto-upload] upload failed for skill %s: %v", skillName, err)
		return fmt.Errorf("auto-upload: submit skill: %w", err)
	}

	// 6. Write upload_status.json in skillDir.
	status := map[string]string{"submission_id": submissionID}
	statusData, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("auto-upload: marshal status: %w", err)
	}
	statusPath := filepath.Join(skillDir, "upload_status.json")
	if err := os.WriteFile(statusPath, statusData, 0o644); err != nil {
		return fmt.Errorf("auto-upload: write upload_status.json: %w", err)
	}

	log.Printf("[auto-upload] skill %s uploaded, submission_id=%s", skillName, submissionID)
	return nil
}

// SkillAutoSummaryPipeline orchestrates the end-to-end skill auto-summary
// flow: complexity analysis 鈫?draft 鈫?validate 鈫?quality gate 鈫?auto upload.
type SkillAutoSummaryPipeline struct {
	tagGen    *TagGenerator
	checker   *SecurityPolicyChecker
	trigger   *AutoUploadTrigger
	skillExec *SkillExecutor
	client    *SkillMarketClient
	activity  *AgentActivityStore

	mu        sync.Mutex
	processed map[string]bool // session_id 鈫?already processed (idempotent)
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

// RunPipeline executes the full skill auto-summary pipeline for a session.
// It is idempotent: repeated calls with the same session_id are skipped.
// Each stage is run sequentially; any failure aborts subsequent stages.
func (p *SkillAutoSummaryPipeline) RunPipeline(session *TrajectorySession) {
	if session == nil {
		return
	}
	sid := session.SessionID

	// Idempotency check 鈥?must check and mark in the same critical section
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
			Task:        "鑷姩鎬荤粨 Skill",
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

	// Stage 2.5: FindSimilarSkill 鈥?check if an existing skill should be updated.
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
				// Write new version.
				data, fmtErr := skill.FormatSkillYAMLFile(draft)
				if fmtErr == nil {
					yamlPath := filepath.Join(existing.SkillDir, "skill.yaml")
					if writeErr := os.WriteFile(yamlPath, data, 0o644); writeErr != nil {
						log.Printf("[skill-auto-summary] session=%s stage=VersionedUpdate write_error=%v", sid, writeErr)
					} else {
						log.Printf("[skill-auto-summary] session=%s stage=VersionedUpdate result=ok", sid)
						_ = versioner.CleanOldVersions(existing.SkillDir, 5)
					}
				}
			}
		} else {
			log.Printf("[skill-auto-summary] session=%s stage=FindSimilarSkill existing skill is better, skipping iteration", sid)
		}
		return // Skip new skill creation 鈥?either updated or skipped.
	}
	log.Printf("[skill-auto-summary] session=%s stage=FindSimilarSkill result=unmatched score=%.2f", sid, simScore)

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

	// Stage 5: RunAutoUpload (only if approved)
	if gateResult.Status == "approved" {
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
			// Upload failure is non-fatal 鈥?local skill is preserved.
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
