package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

// OptimizationTrigger defines when a skill should be considered for optimization.
type OptimizationTrigger struct {
	// MinUsageCount is the minimum total uses before optimization is considered.
	MinUsageCount int // default: 8

	// SuccessRateLow is the lower bound of the "working but not great" range.
	SuccessRateLow float64 // default: 0.50

	// SuccessRateHigh is the upper bound — above this, the skill is good enough.
	SuccessRateHigh float64 // default: 0.85

	// MinRetryEvidence is the minimum number of retry/abandon records in
	// the recent window to justify optimization effort.
	MinRetryEvidence int // default: 2

	// CooldownHours prevents re-optimization within this period.
	CooldownHours int // default: 24
}

func defaultOptimizationTrigger(t OptimizationTrigger) OptimizationTrigger {
	if t.MinUsageCount <= 0 {
		t.MinUsageCount = 8
	}
	if t.SuccessRateLow <= 0 {
		t.SuccessRateLow = 0.50
	}
	if t.SuccessRateHigh <= 0 {
		t.SuccessRateHigh = 0.85
	}
	if t.MinRetryEvidence <= 0 {
		t.MinRetryEvidence = 2
	}
	if t.CooldownHours <= 0 {
		t.CooldownHours = 24
	}
	return t
}

// TextEdit describes a single bounded edit to a skill's text (params, description, etc.)
// Modeled after Microsoft SkillOpt's "text gradient" edits.
type TextEdit struct {
	Type   string `json:"type"`   // "add" | "delete" | "replace"
	Target string `json:"target"` // "step_params" | "description" | "on_error" | "step_N_params"
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// OptimizationResult holds the outcome of a skill optimization attempt.
type OptimizationResult struct {
	Optimized   bool                  `json:"optimized"`
	Changes     []TextEdit            `json:"changes,omitempty"`
	NewSteps    []corelib.NLSkillStep `json:"new_steps,omitempty"`
	NewDesc     string                `json:"new_description,omitempty"`
	Explanation string                `json:"explanation"`
	GateResult  *GateResult           `json:"gate_result,omitempty"`
}

// SkillUsageRecord is a simplified view of a usage record for the optimizer.
// The optimizer doesn't import corelib/tool directly to avoid circular deps.
type SkillUsageRecord struct {
	Success    bool      `json:"success"`
	FollowUp   string    `json:"follow_up"` // "continue" | "retry" | "abandon"
	ErrorClass string    `json:"error_class"`
	Timestamp  time.Time `json:"timestamp"`
}

// SkillOptimizer improves working-but-suboptimal skills by analyzing failure
// patterns and proposing bounded text edits, verified through RepairGate.
type SkillOptimizer struct {
	Trigger   OptimizationTrigger
	LLM       LLMRepairer
	Gate      *RepairGate
	Versioner *Versioner
}

// NewSkillOptimizer creates a SkillOptimizer with default trigger settings.
func NewSkillOptimizer(llm LLMRepairer, gate *RepairGate, versioner *Versioner) *SkillOptimizer {
	return &SkillOptimizer{
		Trigger:   defaultOptimizationTrigger(OptimizationTrigger{}),
		LLM:       llm,
		Gate:      gate,
		Versioner: versioner,
	}
}

// ShouldOptimize determines if a skill is in the "working but not great" zone
// and has enough evidence of suboptimal behavior to warrant optimization.
func (o *SkillOptimizer) ShouldOptimize(skill *corelib.NLSkillEntry, recentRecords []SkillUsageRecord) bool {
	if skill == nil || o == nil {
		return false
	}
	trigger := defaultOptimizationTrigger(o.Trigger)

	// Not enough usage data.
	if skill.UsageCount < trigger.MinUsageCount {
		return false
	}

	// File-backed skills need reviewed patch flow.
	if IsFileBackedSkill(*skill) {
		return false
	}
	// Imported Markdown project workflows are guidance for an interactive agent,
	// not a recipe an optimizer may safely rewrite as one craft_tool step.
	if IsAgentGuidedWorkflowSkill(skill) {
		return false
	}

	// Guard against division by zero (should not happen given MinUsageCount check).
	if skill.UsageCount == 0 {
		return false
	}

	// Success rate check.
	successRate := float64(skill.SuccessCount) / float64(skill.UsageCount)
	if successRate < trigger.SuccessRateLow || successRate > trigger.SuccessRateHigh {
		return false
	}

	// Cooldown check.
	if skill.LastOptimizedAt != "" {
		if lastOpt, err := time.Parse(time.RFC3339, skill.LastOptimizedAt); err == nil {
			if time.Since(lastOpt).Hours() < float64(trigger.CooldownHours) {
				return false
			}
		}
	}

	// Enough retry/abandon evidence?
	var retryAbandonCount int
	for _, r := range recentRecords {
		if r.FollowUp == "retry" || r.FollowUp == "abandon" {
			retryAbandonCount++
		}
	}
	return retryAbandonCount >= trigger.MinRetryEvidence
}

// Optimize analyzes failure traces and proposes parameter adjustments.
// If RepairGate is configured, the proposed changes are verified in sandbox.
// Returns the optimization result — caller applies changes if Optimized=true.
func (o *SkillOptimizer) Optimize(ctx context.Context, skill *corelib.NLSkillEntry, recentRecords []SkillUsageRecord, historicalArgs []map[string]string) (*OptimizationResult, error) {
	if o == nil || o.LLM == nil || !o.LLM.IsConfigured() {
		return nil, fmt.Errorf("LLM not configured for skill optimization")
	}
	if skill == nil {
		return nil, fmt.Errorf("skill is nil")
	}

	log.Printf("[skill-optimizer] starting optimization for skill=%s usage=%d success_rate=%.0f%%",
		skill.Name, skill.UsageCount, float64(skill.SuccessCount)/float64(skill.UsageCount)*100)

	// Build context for LLM.
	prompt := buildOptimizationPrompt(skill, recentRecords)

	resp, err := o.LLM.ChatCall([]map[string]string{
		{"role": "system", "content": optimizerSystemPrompt},
		{"role": "user", "content": prompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM optimization call: %w", err)
	}

	// Parse response.
	result, err := parseOptimizationResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("parse optimization response: %w", err)
	}

	if !result.Optimized || len(result.NewSteps) == 0 {
		log.Printf("[skill-optimizer] no optimization proposed for skill=%s: %s", skill.Name, result.Explanation)
		return result, nil
	}

	// Verify through RepairGate if available.
	if o.Gate != nil {
		gateResult, gateErr := o.Gate.Verify(ctx, skill, result.NewSteps, historicalArgs)
		if gateErr != nil {
			return nil, fmt.Errorf("optimization gate verification: %w", gateErr)
		}
		result.GateResult = gateResult
		if !gateResult.IsRealPass() {
			result.Optimized = false
			reason := "gate did not provide real passing evidence"
			if gateResult != nil && gateResult.Reason != "" {
				reason = gateResult.Reason
			}
			result.Explanation = "optimization proposed but failed gate verification: " + reason
			log.Printf("[skill-optimizer] gate REJECTED for skill=%s: %s", skill.Name, gateResult.Reason)
			return result, nil
		}
		log.Printf("[skill-optimizer] gate PASSED for skill=%s: %s", skill.Name, gateResult.Reason)
	}

	return result, nil
}

// ApplyOptimization writes the optimization result to the skill entry.
// Caller must persist the skill after this call.
func ApplyOptimization(skill *corelib.NLSkillEntry, result *OptimizationResult, versioner *Versioner) bool {
	if skill == nil || result == nil || !result.Optimized {
		return false
	}

	// Backup current version before modifying.
	if versioner != nil && skill.SkillDir != "" {
		if ver, err := versioner.BackupCurrent(skill.SkillDir); err == nil {
			log.Printf("[skill-optimizer] backed up skill=%s to v%d before optimization", skill.Name, ver)
		}
	}

	// Apply new steps.
	if len(result.NewSteps) > 0 {
		skill.Steps = result.NewSteps
	}

	// Apply new description.
	if result.NewDesc != "" {
		skill.Description = result.NewDesc
	}

	// Update metadata.
	skill.OptimizationCount++
	skill.LastOptimizedAt = time.Now().Format(time.RFC3339)

	log.Printf("[skill-optimizer] applied optimization to skill=%s (count=%d): %s",
		skill.Name, skill.OptimizationCount, result.Explanation)

	return true
}

// --- LLM Prompt ---

const optimizerSystemPrompt = `You are a skill optimization assistant. A skill is working but has suboptimal success rate.
Analyze the execution history and propose targeted parameter adjustments.

Rules:
- Do NOT change the skill's core functionality or tool sequence
- Only adjust: step params (timeout, format, flags), description, on_error strategies
- Focus on patterns in the failure/retry/abandon records
- Keep changes minimal and bounded — one or two targeted edits
- Return a JSON object with fields:
  - optimized (bool): true if you have a concrete improvement
  - new_steps (array): the complete steps array with your adjustments applied
  - new_description (string, optional): improved description if needed
  - changes (array): list of TextEdit objects describing what you changed
  - explanation (string): why this change should improve success rate
- Each TextEdit has: type ("add"|"delete"|"replace"), target ("step_N_params"|"description"|"on_error"), before, after
- Each step in new_steps has: action (string), params (object), on_error (string, optional), label (string, optional), when (string, optional)`

func buildOptimizationPrompt(skill *corelib.NLSkillEntry, records []SkillUsageRecord) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Skill: %s\n", skill.Name)
	fmt.Fprintf(&b, "Description: %s\n", skill.Description)
	fmt.Fprintf(&b, "Usage: %d times, %d successes (%.0f%% success rate)\n",
		skill.UsageCount, skill.SuccessCount,
		float64(skill.SuccessCount)/float64(skill.UsageCount)*100)
	fmt.Fprintf(&b, "Optimization attempts so far: %d\n\n", skill.OptimizationCount)

	b.WriteString("Current steps:\n")
	for i, step := range skill.Steps {
		fmt.Fprintf(&b, "  Step %d: action=%s", i, step.Action)
		if len(step.Params) > 0 {
			paramsJSON, _ := json.Marshal(step.Params)
			fmt.Fprintf(&b, " params=%s", string(paramsJSON))
		}
		if step.OnError != "" {
			fmt.Fprintf(&b, " on_error=%s", step.OnError)
		}
		b.WriteString("\n")
	}

	b.WriteString("\nRecent execution history (last 10 relevant records):\n")
	shown := 0
	for _, r := range records {
		if shown >= 10 {
			break
		}
		status := "success"
		if !r.Success {
			status = "failure"
		}
		fmt.Fprintf(&b, "  [%s] %s follow_up=%s", r.Timestamp.Format("01-02 15:04"), status, r.FollowUp)
		if r.ErrorClass != "" {
			fmt.Fprintf(&b, " error_class=%s", r.ErrorClass)
		}
		b.WriteString("\n")
		shown++
	}

	b.WriteString("\nPropose targeted parameter adjustments to improve success rate.\n")
	return b.String()
}

func parseOptimizationResponse(resp string) (*OptimizationResult, error) {
	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	// Try to find JSON object in the response.
	start := strings.Index(body, "{")
	if start >= 0 {
		end := strings.LastIndex(body, "}")
		if end > start {
			body = body[start : end+1]
		}
	}

	// Local struct with json tags to properly parse LLM JSON output.
	// SkillYAMLStep only has yaml tags — json.Unmarshal won't match
	// snake_case keys like "on_error" to Go field "OnError".
	type jsonStep struct {
		Action  string                 `json:"action"`
		Params  map[string]interface{} `json:"params"`
		OnError string                 `json:"on_error"`
		Label   string                 `json:"label"`
		When    string                 `json:"when"`
	}
	var result struct {
		Optimized   bool       `json:"optimized"`
		NewSteps    []jsonStep `json:"new_steps"`
		NewDesc     string     `json:"new_description"`
		Changes     []TextEdit `json:"changes"`
		Explanation string     `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (body: %.200s)", err, body)
	}

	// Convert to NLSkillStep.
	var nlSteps []corelib.NLSkillStep
	for _, s := range result.NewSteps {
		nlSteps = append(nlSteps, corelib.NLSkillStep{
			Action:  s.Action,
			Params:  s.Params,
			OnError: s.OnError,
			Label:   s.Label,
			When:    s.When,
		})
	}

	return &OptimizationResult{
		Optimized:   result.Optimized,
		Changes:     result.Changes,
		NewSteps:    nlSteps,
		NewDesc:     result.NewDesc,
		Explanation: result.Explanation,
	}, nil
}

// WriteBackOptimizedSteps updates the on-disk skill.yaml with the entry's
// current Steps. This is necessary because loadSkills treats skill.yaml as
// the source of truth for Steps — if only config.json is updated, a restart
// would revert the optimization.
//
// The function reads the existing YAML, replaces the steps section, and writes
// it back atomically. It preserves all other YAML fields (triggers, params, etc.)
func WriteBackOptimizedSteps(entry *corelib.NLSkillEntry) error {
	if entry == nil || entry.SkillDir == "" {
		return nil
	}

	yamlPath := ""
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		p := filepath.Join(entry.SkillDir, name)
		if _, err := os.Stat(p); err == nil {
			yamlPath = p
			break
		}
	}
	if yamlPath == "" {
		// No skill.yaml on disk — nothing to write back (config-only skill).
		return nil
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", yamlPath, err)
	}

	// Parse existing YAML to preserve all fields.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", yamlPath, err)
	}

	// Convert current Steps to YAML-compatible format. poll/loop are NOT
	// written back: a repair/optimization draft only rewrites the flat step
	// fields, and round-tripping the nested poll/loop configs is out of scope
	// (steps carrying them are not eligible for automatic repair anyway).
	yamlSteps := make([]map[string]interface{}, len(entry.Steps))
	for i, step := range entry.Steps {
		s := map[string]interface{}{
			"action": step.Action,
		}
		if len(step.Params) > 0 {
			s["params"] = step.Params
		}
		if step.OnError != "" {
			s["on_error"] = step.OnError
		}
		if step.Name != "" {
			s["name"] = step.Name
		}
		if step.Condition != "" {
			s["condition"] = step.Condition
		}
		if step.Label != "" {
			s["label"] = step.Label
		}
		if step.When != "" {
			s["when"] = step.When
		}
		if len(step.Capture) > 0 {
			s["capture"] = step.Capture
		}
		yamlSteps[i] = s
	}
	raw["steps"] = yamlSteps

	// Update description if changed.
	if entry.Description != "" {
		raw["description"] = entry.Description
	}

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal updated YAML: %w", err)
	}

	// Atomic write: temp file + rename.
	tmpPath := yamlPath + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmpPath, yamlPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	log.Printf("[skill-optimizer] wrote back optimized steps to %s", yamlPath)
	return nil
}
