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
	// SilentFailure indicates the failure was already communicated to the user
	// through another channel (e.g., the confirmation UI showed rejection/timeout
	// feedback inline). When true, the caller should NOT emit additional failure
	// notifications to avoid duplicate or confusing messages.
	SilentFailure bool
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

func buildCodingWorkflowImplementationNoToolActionPrompt() string {
	return "[Execution requirement]\nCoding workflow implementation requires real execution through the internal CodingSubAgent. Do not write code directly from the main workflow agent and do not claim there are no coding tools. Call delegate_task(agent=\"coding_workflow\", request=\"...\") with a concise request referencing the approved task IDs and existing workflow context. If delegate_task is unavailable, report a workflow tooling error.\n[/Execution requirement]"
}

func buildCodingWorkflowImplementationEmptyResultRecoverPrompt(pendingTaskHint string) string {
	base := "[Recover]\nThe previous coding workflow implementation round returned no visible result. The main workflow agent must not write code directly or claim there are no coding tools. Call delegate_task(agent=\"coding_workflow\", request=\"...\") now to hand off to the internal CodingSubAgent with approved task IDs and current workflow context, or report a workflow tooling error if delegate_task is unavailable."
	if pendingTaskHint != "" {
		base += "\n" + strings.TrimSpace(pendingTaskHint)
	}
	base += "\n[/Recover]"
	return base
}

func buildCodingWorkflowImplementationToolAvailabilityCorrection() string {
	return "[system correction] delegate_task is available in the current tool list. Coding workflow implementation must be handed off with delegate_task(agent=\"coding_workflow\", request=\"...\") to the internal CodingSubAgent, using approved task IDs and current workflow context."
}

func buildCodingWorkflowImplementationToolingFailureText(reason string, pendingTaskHint string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "the main workflow agent did not call delegate_task(agent=\"coding_workflow\")"
	}
	text := "Workflow tooling error: coding implementation could not proceed because " + reason + ". This phase requires delegate_task(agent=\"coding_workflow\", request=\"...\") to hand project mutation to the internal CodingSubAgent; the main workflow agent must not write project files directly."
	if strings.TrimSpace(pendingTaskHint) != "" {
		text = appendPendingBackgroundTaskFinalHint(text, pendingTaskHint)
	}
	return text
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

func buildCodingWorkflowImplementationNoToolStallRecoverPrompt(consecutive int) string {
	return fmt.Sprintf("[Recover]\nNo real tool was called for %d consecutive rounds in coding workflow implementation. The only project-mutation path for the main workflow agent is delegate_task(agent=\"coding_workflow\"). Call that handoff with a concise request, or report a workflow tooling error if the tool is unavailable.\n[/Recover]", consecutive)
}

func didSkillToolFail(toolCalls []llm.ToolCall, toolOutcomes []toolOutcome) bool {
	return didSkillToolExecutionFail(buildToolExecutionResults(toolCalls, nil, toolOutcomes))
}

func didSkillToolExecutionFail(toolExecResults []toolExecutionResult) bool {
	if len(toolExecResults) == 0 {
		return false
	}
	for _, result := range toolExecResults {
		switch result.ToolKind {
		case agentToolKindRunSkill, agentToolKindGetSkillRun, agentToolKindSearchAndInstallSkill:
			if result.Outcome == toolOutcomeFailed {
				return true
			}
		}
	}
	return false
}

func isSkillRunStarterToolCall(tc llm.ToolCall) bool {
	switch classifyAgentToolKind(tc.Function.Name) {
	case agentToolKindRunSkill:
		return true
	case agentToolKindManageSkill:
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err != nil {
			return false
		}
		action, _ := parsed["action"].(string)
		return classifyManageSkillAction(action) == manageSkillActionRun
	default:
		return false
	}
}

func buildToolExecutionResults(toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome) []toolExecutionResult {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolOutcomes) {
		return nil
	}
	results := make([]toolExecutionResult, 0, len(toolCalls))
	for i, tc := range toolCalls {
		text := ""
		if i < len(toolResults) {
			text = toolResults[i]
		}
		kind := classifyAgentToolKind(tc.Function.Name)
		results = append(results, toolExecutionResult{
			Text:        text,
			ToolName:    tc.Function.Name,
			ToolKind:    kind,
			Outcome:     toolOutcomes[i],
			FailureKind: failureKindForOutcome(toolOutcomes[i]),
			Metadata:    inferToolResultMetadata(kind, text),
		})
	}
	return results
}

// extractFailedSkillInfo extracts the skill name and error message from a
// failed run_skill or manage_skill(action=run) tool call. Returns ("", "")
// if no skill failure is found. This is used for workaround detection:
// when a skill fails but the LLM resolves the task through alternative
// tool calls, the outcome is classified as "workaround".
func extractFailedSkillInfo(toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome) (skillName, lastError string) {
	return extractFailedSkillInfoFromExecutions(toolCalls, buildToolExecutionResults(toolCalls, toolResults, toolOutcomes))
}

func extractFailedSkillInfoFromExecutions(toolCalls []llm.ToolCall, toolExecResults []toolExecutionResult) (skillName, lastError string) {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolExecResults) {
		return "", ""
	}
	for i, tc := range toolCalls {
		kind := toolExecResults[i].ToolKind
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
		if toolExecResults[i].Outcome != toolOutcomeFailed {
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
		errMsg := strings.TrimSpace(toolExecResults[i].Text)
		if len(errMsg) > 300 {
			errMsg = errMsg[:300]
		}
		return sn, errMsg
	}
	return "", ""
}
