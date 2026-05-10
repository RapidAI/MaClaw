package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type skillInstallExecutionResult struct {
	Text    string
	Success bool
}

func buildNoToolActionPrompt(preferSkill bool, skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return "[Execution requirement]\nThis task requires real execution. Do not stop at explanation or planning. " + buildSkillProgressGuidance(skillName, runID) + "\n[/Execution requirement]"
	}
	if preferSkill && skillName != "" {
		return fmt.Sprintf("[Execution requirement]\nThis task requires real execution. Prefer manage_skill(action=\"run\", name=\"%s\"). If a run_id exists, call get_skill_run(run_id=...) until success or failure. If the Skill fails, switch to another real tool path.\n[/Execution requirement]", skillName)
	}
	return "[Execution requirement]\nThis task requires real execution. Choose the best real tool and start executing. For document/file delivery, prefer file generation, editing, or sending tools.\n[/Execution requirement]"
}

func buildNoToolStallRecoverPrompt(consecutive int, preferSkill bool, skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return fmt.Sprintf("[Recover]\nNo real tool was called for %d consecutive rounds. Do not continue with explanation only. %s\n[/Recover]", consecutive, buildSkillProgressGuidance(skillName, runID))
	}
	if preferSkill && skillName != "" {
		return fmt.Sprintf("[Recover]\nNo real tool was called for %d consecutive rounds. Prefer manage_skill(action=\"run\", name=\"%s\"). If that Skill fails, use another real tool path.\n[/Recover]", consecutive, skillName)
	}
	return fmt.Sprintf("[Recover]\nNo real tool was called for %d consecutive rounds. Choose the best real tool now; for document/file delivery, prefer file generation or sending tools.\n[/Recover]", consecutive)
}

func didSkillToolFail(toolCalls []llm.ToolCall, toolOutcomes []toolOutcome) bool {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolOutcomes) {
		return false
	}
	for i, tc := range toolCalls {
		switch classifyAgentToolKind(tc.Function.Name) {
		case agentToolKindRunSkill, agentToolKindGetSkillRun, agentToolKindSearchAndInstallSkill:
			if toolOutcomes[i] == toolOutcomeFailed {
				return true
			}
		}
	}
	return false
}

// extractFailedSkillInfo extracts the skill name and error message from a
// failed run_skill or manage_skill(action=run) tool call. Returns ("", "")
// if no skill failure is found. This is used for workaround detection:
// when a skill fails but the LLM resolves the task through alternative
// tool calls, the outcome is classified as "workaround".
func extractFailedSkillInfo(toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome) (skillName, lastError string) {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolResults) || len(toolCalls) != len(toolOutcomes) {
		return "", ""
	}
	for i, tc := range toolCalls {
		kind := classifyAgentToolKind(tc.Function.Name)
		if kind != agentToolKindRunSkill && kind != agentToolKindManageSkill {
			continue
		}
		if kind == agentToolKindManageSkill {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err != nil {
				continue
			}
			action, _ := parsed["action"].(string)
			if classifyManageSkillAction(action) != manageSkillActionRun {
				continue
			}
		}
		if toolOutcomes[i] != toolOutcomeFailed {
			continue
		}
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
		errMsg := strings.TrimSpace(toolResults[i])
		if len(errMsg) > 300 {
			errMsg = errMsg[:300]
		}
		return sn, errMsg
	}
	return "", ""
}
