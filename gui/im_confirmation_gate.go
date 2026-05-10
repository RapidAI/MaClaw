package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	confirmationApproveCommandPrefix = "__confirm_execution__"
	confirmationCancelCommandPrefix  = "__cancel_execution__"
)

type confirmationAction string

const (
	confirmationActionNone    confirmationAction = ""
	confirmationActionConfirm confirmationAction = "confirm"
	confirmationActionCancel  confirmationAction = "cancel"
)

type confirmationIntent string

const (
	confirmationIntentUnknown confirmationIntent = ""
	confirmationIntentConfirm confirmationIntent = "confirm"
	confirmationIntentCancel  confirmationIntent = "cancel"
	confirmationIntentModify  confirmationIntent = "modify"
)

func (i confirmationIntent) String() string {
	return string(i)
}

func buildConfirmationActionCommand(action confirmationAction, id string) string {
	id = strings.TrimSpace(id)
	switch action {
	case confirmationActionConfirm:
		return confirmationApproveCommandPrefix + " " + id
	case confirmationActionCancel:
		return confirmationCancelCommandPrefix + " " + id
	default:
		return ""
	}
}

func parseConfirmationActionCommand(text string) (action confirmationAction, id string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 {
		return confirmationActionNone, "", false
	}
	switch fields[0] {
	case confirmationApproveCommandPrefix:
		return confirmationActionConfirm, strings.TrimSpace(fields[1]), true
	case confirmationCancelCommandPrefix:
		return confirmationActionCancel, strings.TrimSpace(fields[1]), true
	default:
		return confirmationActionNone, "", false
	}
}

// classifyConfirmationIntent uses a lightweight LLM call to classify the
// user's typed response to an execution confirmation panel. Button clicks use
// structured confirmation commands; free-form text is interpreted with the
// pending task context so we are not depending on brittle phrase matching.
//
// Returns a typed confirmation intent. Unknown is treated as modify by the caller.
func (h *IMMessageHandler) classifyConfirmationIntent(userID, text string, pending *pendingConfirmation) confirmationIntent {
	if pending == nil {
		return confirmationIntentUnknown
	}

	// Build context from the pending confirmation.
	ctx := fmt.Sprintf("Task summary: %s\n", truncateRunes(pending.Summary, 300))
	if len(pending.PlannedActions) > 0 {
		ctx += "Planned actions:\n"
		for i, action := range pending.PlannedActions {
			if i >= 5 {
				break
			}
			ctx += fmt.Sprintf("  %d. %s\n", i+1, truncateRunes(action, 100))
		}
	}
	ctx += "The system asked the user to confirm the plan or provide changes."

	// Add the last assistant message for conversational context.
	if lastAssistant := h.getLastAssistantSnippet(userID, 300); lastAssistant != "" {
		ctx += fmt.Sprintf("\n鍔╂墜鏈€鍚庝竴鏉℃秷鎭細%s", lastAssistant)
	}

	userMessage := fmt.Sprintf("[Context]\n%s\n\n[User reply]\n%s", ctx, text)

	result, err := h.LLMClassify(context.Background(), LLMClassifyRequest{
		SystemPrompt: `You are a user intent classifier for a task execution confirmation dialog.

The user was shown a task plan and asked to confirm, cancel, or revise it. You will receive:
- The task summary and planned actions
- The assistant's last message (if available): this is what the user is directly responding to
- The user's response

IMPORTANT: Pay close attention to the assistant's last message. If the assistant asked the user to say a specific word/phrase to proceed, and the user replies with that word/phrase, it is a confirmation.

Classify the user's response into exactly one category. Reply with ONLY the category word:
- "confirm" - user approves the plan and wants to start execution. This includes any form of agreement, readiness signal, or go-ahead in the context of the pending task.
- "cancel" - user wants to abandon the task entirely.
- "modify" - user provides specific changes, corrections, or additional requirements for the plan.

When in doubt between "confirm" and "modify", prefer "confirm" if the response is short and doesn't contain specific change requests.`,
		UserMessage: userMessage,
		TimeoutSec:  8,
		Tag:         "confirmation-intent",
	})

	if err != nil {
		log.Printf("[confirmation-intent] LLM classify failed for user %s: %v", userID, err)
		return confirmationIntentUnknown
	}

	intent := normalizeConfirmationIntent(result.Text)
	log.Printf("[confirmation-intent] user=%s text=%q -> intent=%q (latency=%.1fs)",
		userID, truncateForLogGUI(text, 30), intent, result.Latency.Seconds())
	return intent
}

func normalizeConfirmationIntent(text string) confirmationIntent {
	intent := strings.ToLower(strings.TrimSpace(text))
	intent = strings.Trim(intent, " \t\r\n`\"'.,:;!?()[]{}")
	switch intent {
	case confirmationIntentConfirm.String():
		return confirmationIntentConfirm
	case confirmationIntentCancel.String():
		return confirmationIntentCancel
	case confirmationIntentModify.String():
		return confirmationIntentModify
	default:
		return confirmationIntentUnknown
	}
}

func shouldRequireExecutionConfirmation(msg IMUserMessage, pending *pendingConfirmation) bool {
	return shouldRequireExecutionConfirmationForIntent(msg, pending, classifyTaskIntent(strings.TrimSpace(msg.Text)))
}

func (h *IMMessageHandler) handleExecutionConfirmationGate(freshTask bool, msg IMUserMessage, trimmed string, httpClient *http.Client) (*IMAgentResponse, bool) {
	if !shouldConsiderExecutionConfirmation(freshTask, msg, trimmed) {
		return nil, false
	}

	intent := h.classifyTaskIntentForExecution(trimmed, msg.Attachments, httpClient)
	if !shouldRequireExecutionConfirmationForIntent(msg, nil, intent) {
		return nil, false
	}

	// Attempt LLM-based task understanding for a structured summary. On
	// failure (timeout, LLM not configured, etc.), understanding is nil and
	// buildPendingConfirmation falls back to raw-text echo.
	understanding := h.understandTaskWithLLM(msg.UserID, trimmed, intent)
	item := buildPendingConfirmation(h.app, msg.UserID, trimmed, intent, understanding)
	if h.confirmationStore != nil {
		h.confirmationStore.set(item)
	}
	return buildConfirmationResponse(item), true
}

func confirmationTaskLabel(intent taskIntent) string {
	switch intent {
	case intentCoding:
		return "coding"
	case intentSSH:
		return "ssh"
	case intentAmbiguous:
		return "ambiguous"
	default:
		return string(intent)
	}
}

func confirmationPlannedActions(intent taskIntent) []string {
	switch intent {
	case intentCoding:
		return []string{"Confirm project directory", "Confirm task goal", "Start code changes after confirmation"}
	case intentSSH:
		return []string{"Confirm target server or directory", "Confirm diagnosis goal", "Run remote operation after confirmation"}
	case intentAmbiguous:
		return []string{"Confirm whether this is code work or remote work", "Confirm workspace or target environment", "Execute after confirmation"}
	default:
		return []string{"Confirm task understanding", "Start execution after confirmation"}
	}
}

func confirmationRiskFlags(intent taskIntent) []string {
	switch intent {
	case intentCoding:
		return []string{"Executing without confirmation may modify code in the wrong directory"}
	case intentSSH:
		return []string{"Executing without confirmation may connect to the wrong server or environment"}
	case intentAmbiguous:
		return []string{"The request has multiple possible execution paths and should be clarified first"}
	default:
		return nil
	}
}

func confirmationRevisionHints(intent taskIntent) []string {
	switch intent {
	case intentAmbiguous:
		return []string{"Clarify whether this is code work or SSH/server work", "Provide the correct project directory or host information"}
	default:
		return []string{"If the directory is wrong, reply with the correct directory", "If the task understanding is wrong, reply with the correction"}
	}
}

func buildPendingConfirmation(app *App, userID, text string, result taskIntentResult, understanding *taskUnderstandingResult) *pendingConfirmation {
	now := time.Now()
	// The confirmation panel's "榛樿宸ヤ綔鐩綍" must reflect the agent's actual
	// default working directory 鈥?i.e. where bash, craft_tool, and other
	// general-purpose tools execute by default. This is the user-configured
	// working directory (AppConfig.WorkingDirectory) if set, otherwise
	// ~/.maclaw/workspace. Using EffectiveWorkspaceDir() aligns the
	// confirmation panel with the bash tool description and the actual
	// execution environment.
	projectPath := corelib.EffectiveWorkspaceDir()
	targetPaths := make([]string, 0, 1)
	if projectPath != "" {
		targetPaths = append(targetPaths, projectPath)
	}

	// --- Summary generation ---
	// If LLM understanding is available, use the structured summary.
	// Otherwise fall back to raw-text echo (previous behavior).
	var summary string
	var enhancedSummary string
	var enhancedInstruction string

	if understanding != nil && strings.TrimSpace(understanding.Summary) != "" {
		enhancedSummary = formatTaskUnderstandingSummary(understanding, projectPath)
		enhancedInstruction = formatEnhancedInstruction(understanding)
		summary = enhancedSummary
	} else {
		// Fallback: raw-text echo (previous behavior).
		summary = fmt.Sprintf("I understand you want me to handle this task: %s", strings.TrimSpace(text))
		if projectPath != "" {
			summary += fmt.Sprintf("\nDefault workspace: %s", projectPath)
		}
		if label := strings.TrimSpace(confirmationTaskLabel(result.Intent)); label != "" {
			summary += fmt.Sprintf("\nDetected task type: %s", label)
		}
		if reason := strings.TrimSpace(result.Reason); reason != "" {
			summary += fmt.Sprintf(" (reason: %s)", reason)
		} else if ev := strings.TrimSpace(formatIntentEvidence(result)); ev != "" && ev != "鏈懡涓壒寰佽瘝" {
			summary += fmt.Sprintf(" (evidence: %s)", ev)
		}
	}

	plannedActions := confirmationPlannedActions(result.Intent)
	if understanding != nil && len(understanding.ExecutionPlan) > 0 {
		plannedActions = understanding.ExecutionPlan
	}

	return &pendingConfirmation{
		ID:                  fmt.Sprintf("confirm-%d", now.UnixNano()),
		UserID:              userID,
		OriginalText:        strings.TrimSpace(text),
		ResumeText:          strings.TrimSpace(text),
		Summary:             summary,
		TaskType:            confirmationTaskLabel(result.Intent),
		TargetPaths:         targetPaths,
		PlannedActions:      plannedActions,
		RiskFlags:           confirmationRiskFlags(result.Intent),
		RevisionHints:       confirmationRevisionHints(result.Intent),
		Status:              "pending",
		CreatedAt:           now,
		UpdatedAt:           now,
		LastProjectPath:     projectPath,
		EnhancedSummary:     enhancedSummary,
		EnhancedInstruction: enhancedInstruction,
	}
}

func buildConfirmationPayload(item *pendingConfirmation) *IMResponseConfirmation {
	if item == nil {
		return nil
	}
	return &IMResponseConfirmation{
		ID:             item.ID,
		Summary:        item.Summary,
		TaskType:       item.TaskType,
		TargetPaths:    append([]string(nil), item.TargetPaths...),
		PlannedActions: append([]string(nil), item.PlannedActions...),
		RiskFlags:      append([]string(nil), item.RiskFlags...),
		RevisionHints:  append([]string(nil), item.RevisionHints...),
		Status:         item.Status,
	}
}

func buildConfirmationResponse(item *pendingConfirmation) *IMAgentResponse {
	if item == nil {
		return &IMAgentResponse{Text: "Please confirm before continuing."}
	}
	// Summary is already shown inside the Confirmation card 鈥?only keep the
	// action prompt here to avoid repeating the same content twice.
	text := "Please confirm whether my understanding is correct. After confirmation I will start execution; if anything is off, reply with the corrected directory, goal, or premise."
	return &IMAgentResponse{
		Text:         text,
		Confirmation: buildConfirmationPayload(item),
		Actions: []IMResponseAction{
			{Label: "Confirm and start", Command: buildConfirmationActionCommand(confirmationActionConfirm, item.ID), Style: "primary"},
			{Label: "Cancel", Command: buildConfirmationActionCommand(confirmationActionCancel, item.ID), Style: "secondary"},
		},
	}
}

type pendingExecutionConfirmationResult struct {
	Handled              bool
	Response             *IMAgentResponse
	ConfirmedResume      bool
	WorkflowAgentLoop    bool
	SkipWorkflowOnce     bool
	ReprocessAsFreshTask bool
	SkipExecutionConfirm bool
}

func (h *IMMessageHandler) handlePendingExecutionConfirmation(msg *IMUserMessage, trimmed *string) pendingExecutionConfirmationResult {
	if h == nil || msg == nil || trimmed == nil || h.confirmationStore == nil {
		return pendingExecutionConfirmationResult{}
	}
	action, confirmationID, hasConfirmationAction := parseConfirmationActionCommand(*trimmed)
	pending := h.confirmationStore.get(msg.UserID)
	if pending == nil {
		if hasConfirmationAction {
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: "Confirmation expired; please start again."}}
		}
		return pendingExecutionConfirmationResult{}
	}

	saveCancelContext := func() {
		if pending.OriginalText == "" || h.memory == nil {
			return
		}
		entries := h.memory.Load(msg.UserID)
		cancelNote := fmt.Sprintf("(User cancelled execution confirmation for this task. Original request: %s)", truncateRunes(pending.OriginalText, 200))
		entries = append(entries,
			agent.ConversationEntry{Role: "user", Content: pending.OriginalText},
			agent.ConversationEntry{Role: "assistant", Content: cancelNote},
		)
		h.memory.Save(msg.UserID, entries)
	}

	isWorkflowConfirmation := strings.TrimSpace(pending.WorkflowType) != ""
	approve := func() pendingExecutionConfirmationResult {
		h.confirmationStore.clear(msg.UserID)
		if isWorkflowConfirmation {
			return h.approvePendingWorkflowConfirmation(msg.UserID, pending)
		}
		msg.Text = confirmationApprovedText(pending)
		*trimmed = strings.TrimSpace(msg.Text)
		result := pendingExecutionConfirmationResult{ConfirmedResume: true}
		if h.getWorkflowEngine() != nil && !msg.IsBackground {
			if wfResp := h.handleWorkflowInterception(msg.UserID, *trimmed); wfResp != nil {
				h.pendingAskUser.Delete(msg.UserID)
				result.Handled = true
				result.Response = wfResp
				return result
			}
			if _, ok := h.workflowAgentLoopMarker.LoadAndDelete(msg.UserID); ok {
				result.WorkflowAgentLoop = true
			}
		}
		return result
	}

	switch {
	case hasConfirmationAction:
		if confirmationID != pending.ID {
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: "Confirmation expired; please start again."}}
		}
		if action == confirmationActionConfirm {
			return approve()
		}
		h.confirmationStore.clear(msg.UserID)
		if isWorkflowConfirmation {
			msg.Text = firstNonEmptyTraceText(pending.ResumeText, pending.OriginalText)
			*trimmed = strings.TrimSpace(msg.Text)
			return pendingExecutionConfirmationResult{SkipWorkflowOnce: true, SkipExecutionConfirm: true}
		}
		saveCancelContext()
		return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: "Cancelled pending confirmation."}}
	case !msg.IsBackground:
		llmIntent := h.classifyConfirmationIntent(msg.UserID, *trimmed, pending)
		switch llmIntent {
		case confirmationIntentConfirm:
			return approve()
		case confirmationIntentCancel:
			h.confirmationStore.clear(msg.UserID)
			if isWorkflowConfirmation {
				msg.Text = firstNonEmptyTraceText(pending.ResumeText, pending.OriginalText)
				*trimmed = strings.TrimSpace(msg.Text)
				return pendingExecutionConfirmationResult{SkipWorkflowOnce: true, SkipExecutionConfirm: true}
			}
			saveCancelContext()
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: "Cancelled pending confirmation."}}
		default:
			if isWorkflowConfirmation {
				h.confirmationStore.clear(msg.UserID)
				msg.Text = strings.TrimSpace(firstNonEmptyTraceText(pending.ResumeText, pending.OriginalText) + "\n\nUser clarification: " + *trimmed)
				*trimmed = strings.TrimSpace(msg.Text)
				return pendingExecutionConfirmationResult{ReprocessAsFreshTask: true, SkipExecutionConfirm: true}
			}
			updated := applyConfirmationRevision(pending, *trimmed)
			h.confirmationStore.set(updated)
			return pendingExecutionConfirmationResult{Handled: true, Response: buildConfirmationResponse(updated)}
		}
	}

	return pendingExecutionConfirmationResult{}
}

func applyConfirmationRevision(item *pendingConfirmation, revision string) *pendingConfirmation {
	if item == nil {
		return nil
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return item
	}
	clone := *item
	clone.ResumeText = strings.TrimSpace(item.OriginalText + "\n\nUser supplement/correction: " + revision)
	clone.Summary = item.Summary + "\nUser supplement/correction: " + revision
	clone.RevisionHints = append([]string(nil), item.RevisionHints...)
	clone.UpdatedAt = time.Now()
	// Clear enhanced fields 鈥?the revision changes the task, so the LLM
	// understanding is stale. confirmationApprovedText will fall back to
	// ResumeText (which includes the revision).
	clone.EnhancedSummary = ""
	clone.EnhancedInstruction = ""
	return &clone
}

func confirmationApprovedText(item *pendingConfirmation) string {
	if item == nil {
		return ""
	}
	// Prefer the LLM-generated enhanced instruction over the raw user text.
	// The enhanced instruction is a structured, actionable rewrite that gives
	// the agent a clearer directive than the user's conversational input.
	// When using the enhanced instruction, append the original text as
	// reference so the agent can cross-check if the LLM missed any details.
	base := ""
	if ei := strings.TrimSpace(item.EnhancedInstruction); ei != "" {
		original := strings.TrimSpace(firstNonEmptyTraceText(item.ResumeText, item.OriginalText))
		base = ei
		if original != "" && original != ei {
			base += "\n\n[用户原始请求]\n" + original
		}
	} else {
		base = strings.TrimSpace(firstNonEmptyTraceText(item.ResumeText, item.OriginalText))
	}
	if base == "" {
		return ""
	}

	// Extract "鈿狅笍 寰呯‘璁? items from the confirmation summary/constraints.
	// When the LLM task understanding marked items as pending confirmation
	// (e.g. SSH credentials, deployment path), the user clicking "纭骞跺紑濮?
	// confirms the PLAN but does NOT provide the missing information.
	// We must tell the agent to ask the user for these items before executing.
	pendingItems := extractPendingConfirmItems(item)
	if len(pendingItems) > 0 {
		var pendingSection strings.Builder
		pendingSection.WriteString("\n\n[执行上下文]\n用户已确认执行计划，但以下信息尚未提供，必须先获取后再执行：\n")
		for _, pi := range pendingItems {
			pendingSection.WriteString("- " + pi + "\n")
		}
		pendingSection.WriteString("先尝试使用 memory(action=recall) 查找这些值；如果记忆中没有，请向用户询问。所有必需信息齐备后再开始执行。")
		return strings.TrimSpace(base + pendingSection.String())
	}

	return strings.TrimSpace(base + "\n\n[执行上下文]\n用户已确认当前计划。直接开始执行，不要再次请求确认。如果还没有最终交付物，请说明当前动作或下一步。")
}

// extractPendingConfirmItems scans the confirmation's Summary and Constraints
// for "鈿狅笍 寰呯‘璁? markers. These indicate information the LLM flagged as
// missing during task understanding. The user confirmed the plan but did NOT
// provide these values 鈥?the agent must ask for them before executing.
func extractPendingConfirmItems(item *pendingConfirmation) []string {
	if item == nil {
		return nil
	}
	var items []string
	seen := make(map[string]bool)

	// Scan all text sources for pending confirmation markers.
	// Only scan Summary and EnhancedSummary (user-facing display text).
	// EnhancedInstruction is the execution directive 鈥?scanning it would
	// cause false positives if it mentions "寰呯‘璁? in a different context.
	sources := []string{item.Summary, item.EnhancedSummary}
	for _, c := range item.RiskFlags {
		sources = append(sources, c)
	}

	for _, src := range sources {
		for _, line := range strings.Split(src, "\n") {
			line = strings.TrimSpace(line)
			// Match "鈿狅笍 寰呯‘璁わ細xxx" or "寰呯‘璁わ細xxx" at meaningful positions.
			// Require "寰呯‘璁? to be preceded by start-of-line, bullet, or 鈿狅笍
			// to avoid matching "纭寰呯‘璁ら」" or similar false positives.
			for _, sep := range []string{"⚠️ 待确认：", "⚠️ 待确认:", "待确认：", "待确认:", "鈿狅笍 寰呯‘璁わ細", "鈿狅笍 寰呯‘璁?", "寰呯‘璁わ細", "寰呯‘璁?"} {
				if pos := strings.Index(line, sep); pos >= 0 {
					extracted := strings.TrimSpace(line[pos+len(sep):])
					// Strip trailing parenthetical notes like "锛堝缓璁?..锛?
					if extracted != "" && !seen[extracted] {
						seen[extracted] = true
						items = append(items, extracted)
					}
					break // only extract once per line
				}
			}
		}
	}
	return items
}
