package main

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/tui/commands"
)

// extractFailedSkillInfoTUI extracts the skill name and error message from a
// failed run_skill or manage_skill(action=run) tool call. Returns ("", "")
// if no skill failure is found. This mirrors the GUI's extractFailedSkillInfo
// in gui/im_message_handler.go for workaround detection: when a skill fails
// but the LLM resolves the task through alternative tool calls, the outcome
// is classified as "workaround".
func extractFailedSkillInfoTUI(toolCalls []llmToolCall, toolResults []string) (skillName, lastError string) {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolResults) {
		return "", ""
	}
	for i, tc := range toolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		if name != "run_skill" && name != "manage_skill" {
			continue
		}
		// For manage_skill, only consider action=run
		if name == "manage_skill" {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err != nil {
				continue
			}
			action, _ := parsed["action"].(string)
			if action != "run" {
				continue
			}
		}
		// Check if the tool result indicates failure.
		if !isSkillResultFailedTUI(toolResults[i]) {
			continue
		}
		// Extract skill name from the tool call arguments.
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err != nil {
			continue
		}
		sn, _ := parsed["name"].(string)
		if sn == "" {
			sn, _ = parsed["skill_name"].(string)
		}
		if sn == "" {
			continue
		}
		// Use the tool result as the error message (truncated).
		errMsg := strings.TrimSpace(toolResults[i])
		if len(errMsg) > 300 {
			errMsg = errMsg[:300]
		}
		return sn, errMsg
	}
	return "", ""
}

// isSkillResultFailedTUI checks whether a tool result string indicates a
// skill execution failure. Mirrors the GUI's classifySkillRunOutcome logic.
func isSkillResultFailedTUI(result string) bool {
	lower := strings.ToLower(strings.TrimSpace(result))
	if lower == "" {
		return false
	}
	// Explicit failure markers from skill runner output
	if strings.Contains(lower, "status: failed") ||
		strings.Contains(lower, "status: cancelled") ||
		strings.Contains(lower, "status: canceled") {
		return true
	}
	// TUI skill runner uses ❌ prefix for failures
	if strings.HasPrefix(strings.TrimSpace(result), "❌") {
		return true
	}
	// Generic error prefixes
	if strings.HasPrefix(strings.TrimSpace(result), "错误:") ||
		strings.HasPrefix(strings.TrimSpace(result), "错误：") {
		return true
	}
	return false
}

// recordSkillOutcome records an execution outcome for a skill by name.
// outcome must be one of "success", "failure", or "workaround".
//
// NOTE: This method is no longer called from the agent loop to avoid
// double-counting with toolRunSkill(). For workaround recording,
// use recordSkillWorkaround() instead. Retained for backward compatibility.
func (h *TUIAgentHandler) recordSkillOutcome(skillName, outcome, lastError string) {
	if skillName == "" {
		return
	}
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		log.Printf("[skill-outcome] failed to load config for outcome recording: %v", err)
		return
	}
	for i, s := range cfg.NLSkills {
		if s.MatchesName(skillName) && s.Source != "file" {
			switch outcome {
			case "success":
				cfg.NLSkills[i].SuccessCount++
				cfg.NLSkills[i].LastError = ""
			case "failure":
				cfg.NLSkills[i].FailureCount++
				if lastError != "" {
					cfg.NLSkills[i].LastError = lastError
				}
			case "workaround":
				cfg.NLSkills[i].WorkaroundCount++
				if lastError != "" {
					cfg.NLSkills[i].LastError = lastError
				}
			default:
				return // unknown outcome, skip
			}
			cfg.NLSkills[i].UsageCount++
			cfg.NLSkills[i].LastUsedAt = time.Now().Format(time.RFC3339)
			if saveErr := store.SaveConfig(cfg); saveErr != nil {
				log.Printf("[skill-outcome] failed to save config after outcome recording: %v", saveErr)
				return
			}
			log.Printf("[skill-outcome] outcome recorded for %q: outcome=%s usage=%d success=%d failure=%d workaround=%d",
				skillName, outcome, cfg.NLSkills[i].UsageCount, cfg.NLSkills[i].SuccessCount,
				cfg.NLSkills[i].FailureCount, cfg.NLSkills[i].WorkaroundCount)
			break
		}
	}
}

// recordSkillWorkaround records a workaround outcome for a skill without
// incrementing UsageCount. This is called from the agent loop when a skill
// failed but the LLM resolved the task through alternative tools. The
// UsageCount and FailureCount were already incremented by toolRunSkill()
// when the skill execution completed, so we only need to bump WorkaroundCount.
func (h *TUIAgentHandler) recordSkillWorkaround(skillName, lastError string) {
	if skillName == "" {
		return
	}
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		log.Printf("[skill-outcome] failed to load config for workaround recording: %v", err)
		return
	}
	for i, s := range cfg.NLSkills {
		if s.MatchesName(skillName) && s.Source != "file" {
			cfg.NLSkills[i].WorkaroundCount++
			if lastError != "" {
				cfg.NLSkills[i].LastError = lastError
			}
			if saveErr := store.SaveConfig(cfg); saveErr != nil {
				log.Printf("[skill-outcome] failed to save config after workaround recording: %v", saveErr)
				return
			}
			log.Printf("[skill-outcome] workaround recorded for %q: workaround=%d (usage unchanged at %d)",
				skillName, cfg.NLSkills[i].WorkaroundCount, cfg.NLSkills[i].UsageCount)
			break
		}
	}
}

// truncateForLog truncates a string for log output, preserving rune boundaries.
func truncateForLog(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
