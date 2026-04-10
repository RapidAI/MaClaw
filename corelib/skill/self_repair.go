package skill

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SelfRepairThreshold defines when a skill is eligible for self-repair.
const SelfRepairThreshold = 3 // minimum UsageCount before repair is considered

// SelfRepairMaxAttempts is the maximum consecutive repair attempts before
// the skill is disabled.
const SelfRepairMaxAttempts = 3

// LLMRepairer abstracts the LLM call needed for skill self-repair.
type LLMRepairer interface {
	ChatCall(messages []map[string]string) (string, error)
	IsConfigured() bool
}

// ShouldAttemptRepair returns true if the skill has enough usage data and
// a low enough success rate to warrant an automated repair attempt.
func ShouldAttemptRepair(skill *corelib.NLSkillEntry) bool {
	if skill == nil {
		return false
	}
	if skill.UsageCount < SelfRepairThreshold {
		return false
	}
	if skill.LastError == "" {
		return false
	}
	successRate := float64(skill.SuccessCount) / float64(skill.UsageCount)
	return successRate < 0.5
}

// RepairResult holds the outcome of a self-repair attempt.
type RepairResult struct {
	Repaired    bool              `json:"repaired"`
	NewSteps    []SkillYAMLStep   `json:"new_steps,omitempty"`
	Explanation string            `json:"explanation"`
	ShouldDisable bool           `json:"should_disable"`
}

// AttemptRepair uses an LLM to analyze the skill's last error and propose
// modified steps. Returns a RepairResult with the proposed changes.
// The caller is responsible for writing the changes back to disk.
func AttemptRepair(llm LLMRepairer, skill *corelib.NLSkillEntry) (*RepairResult, error) {
	if llm == nil || !llm.IsConfigured() {
		return nil, fmt.Errorf("LLM not configured for skill repair")
	}
	if skill == nil {
		return nil, fmt.Errorf("skill is nil")
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

	systemPrompt := `You are a skill repair assistant. A skill has been failing repeatedly.
Analyze the error and the current steps, then propose fixed steps.

Rules:
- Keep the same action names (tool names) — only modify params or step order
- If a step consistently fails, add on_error: "skip" or "continue"
- If the error is fundamental (wrong tool, impossible task), set should_disable: true
- Return ONLY a JSON object with fields: repaired (bool), new_steps (array), explanation (string), should_disable (bool)
- Each step in new_steps has: action (string), params (object), on_error (string, optional)`

	userPrompt := fmt.Sprintf(`Skill: %s
Description: %s
Usage: %d times, %d successes (%.0f%% success rate)
Last error: %s

Current steps:
%s

Propose a fix.`,
		skill.Name,
		skill.Description,
		skill.UsageCount,
		skill.SuccessCount,
		float64(skill.SuccessCount)/float64(skill.UsageCount)*100,
		skill.LastError,
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

	log.Printf("[skill-repair] skill=%s repaired=%v should_disable=%v explanation=%s",
		skill.Name, result.Repaired, result.ShouldDisable, result.Explanation)

	return &result, nil
}

// ApplyRepair writes the repaired steps back to the skill entry.
// Returns true if the skill was modified, false if it was disabled.
func ApplyRepair(skill *corelib.NLSkillEntry, result *RepairResult) bool {
	if result.ShouldDisable {
		skill.Status = "disabled"
		skill.LastError = fmt.Sprintf("auto-disabled: %s", result.Explanation)
		log.Printf("[skill-repair] disabled skill %s: %s", skill.Name, result.Explanation)
		return false
	}

	if !result.Repaired || len(result.NewSteps) == 0 {
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
	skill.LastError = fmt.Sprintf("auto-repaired: %s", result.Explanation)
	log.Printf("[skill-repair] repaired skill %s with %d new steps", skill.Name, len(newSteps))
	return true
}
