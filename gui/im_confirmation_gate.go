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
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

const (
	confirmationApproveCommandPrefix = "__confirm_execution__"
	confirmationCancelCommandPrefix  = "__cancel_execution__"
)

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
		ctx += fmt.Sprintf("\n\u52a9\u624b\u6700\u540e\u4e00\u6761\u6d88\u606f\uff1a%s", lastAssistant)
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
	return buildConfirmationResponse(item, h.getWorkflowLang()), true
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

func confirmationPlannedActions(intent taskIntent, lang string) []string {
	switch intent {
	case intentCoding:
		return []string{i18n.T(i18n.MsgExecPlanCoding1, lang), i18n.T(i18n.MsgExecPlanCoding2, lang), i18n.T(i18n.MsgExecPlanCoding3, lang)}
	case intentSSH:
		return []string{i18n.T(i18n.MsgExecPlanSSH1, lang), i18n.T(i18n.MsgExecPlanSSH2, lang), i18n.T(i18n.MsgExecPlanSSH3, lang)}
	case intentAmbiguous:
		return []string{i18n.T(i18n.MsgExecPlanAmbig1, lang), i18n.T(i18n.MsgExecPlanAmbig2, lang), i18n.T(i18n.MsgExecPlanAmbig3, lang)}
	default:
		return []string{i18n.T(i18n.MsgExecPlanDefault1, lang), i18n.T(i18n.MsgExecPlanDefault2, lang)}
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
	// The confirmation panel target path must reflect the agent's actual
	// default working directory, i.e. where bash, craft_tool, and other
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
		} else if ev := strings.TrimSpace(formatIntentEvidence(result)); ev != "" {
			summary += fmt.Sprintf(" (evidence: %s)", ev)
		}
	}

	lang := ""
	if app != nil {
		lang = app.CurrentLanguage
	}
	plannedActions := confirmationPlannedActions(result.Intent, lang)
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
		Status:              confirmationStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
		LastProjectPath:     projectPath,
		EnhancedSummary:     enhancedSummary,
		EnhancedInstruction: enhancedInstruction,
	}
}

func buildConfirmationPayload(item *pendingConfirmation, lang string) *IMResponseConfirmation {
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
		Status:         item.Status.String(),
		Labels:         buildConfirmationLabels(lang),
	}
}

// buildConfirmationLabels returns localized section titles for the
// confirmation card. This is the single source of truth for all
// confirmation panel labels; the frontend renders these directly.
func buildConfirmationLabels(lang string) *IMResponseConfirmLabels {
	return &IMResponseConfirmLabels{
		Title:          i18n.T(i18n.MsgConfirmLabelTitle, lang),
		Status:         i18n.T(i18n.MsgConfirmLabelStatus, lang),
		TargetPaths:    i18n.T(i18n.MsgConfirmLabelTargetPaths, lang),
		PlannedActions: i18n.T(i18n.MsgConfirmLabelPlannedActions, lang),
		RiskFlags:      i18n.T(i18n.MsgConfirmLabelRiskFlags, lang),
		RevisionHints:  i18n.T(i18n.MsgConfirmLabelRevisionHints, lang),
	}
}

func buildConfirmationResponse(item *pendingConfirmation, lang string) *IMAgentResponse {
	if item == nil {
		return &IMAgentResponse{Text: i18n.T(i18n.MsgExecConfirmNilText, lang)}
	}
	return &IMAgentResponse{
		Text:         i18n.T(i18n.MsgExecConfirmText, lang),
		Confirmation: buildConfirmationPayload(item, lang),
		Actions: []IMResponseAction{
			{Label: i18n.T(i18n.MsgExecConfirmBtnConfirm, lang), Command: buildConfirmationActionCommand(confirmationActionConfirm, item.ID), Style: "primary"},
			{Label: i18n.T(i18n.MsgExecConfirmBtnCancel, lang), Command: buildConfirmationActionCommand(confirmationActionCancel, item.ID), Style: "secondary"},
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
	lang := h.getWorkflowLang()
	if pending == nil {
		if hasConfirmationAction {
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgExecConfirmExpired, lang)}}
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
			return h.approvePendingWorkflowConfirmation(msg.UserID, pending, msg.Platform)
		}
		msg.Text = confirmationApprovedText(pending)
		*trimmed = strings.TrimSpace(msg.Text)
		result := pendingExecutionConfirmationResult{ConfirmedResume: true}
		if h.getWorkflowEngine() != nil && !msg.IsBackground {
			if wfResp := h.handleWorkflowInterception(msg.UserID, *trimmed, msg.Platform); wfResp != nil {
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
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgExecConfirmExpired, lang)}}
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
		return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgExecConfirmCancelled, lang)}}
	case !msg.IsBackground:
		if isWorkflowConfirmation {
			switch classifyWorkflowConfirmationReply(*trimmed) {
			case confirmationIntentConfirm:
				return approve()
			case confirmationIntentCancel:
				h.confirmationStore.clear(msg.UserID)
				msg.Text = firstNonEmptyTraceText(pending.ResumeText, pending.OriginalText)
				*trimmed = strings.TrimSpace(msg.Text)
				return pendingExecutionConfirmationResult{SkipWorkflowOnce: true, SkipExecutionConfirm: true}
			default:
				h.confirmationStore.clear(msg.UserID)
				msg.Text = strings.TrimSpace(firstNonEmptyTraceText(pending.ResumeText, pending.OriginalText) + "\n\nUser clarification: " + *trimmed)
				*trimmed = strings.TrimSpace(msg.Text)
				return pendingExecutionConfirmationResult{ReprocessAsFreshTask: true, SkipExecutionConfirm: true}
			}
		}
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
			return pendingExecutionConfirmationResult{Handled: true, Response: &IMAgentResponse{Text: i18n.T(i18n.MsgExecConfirmCancelled, lang)}}
		default:
			if isWorkflowConfirmation {
				h.confirmationStore.clear(msg.UserID)
				msg.Text = strings.TrimSpace(firstNonEmptyTraceText(pending.ResumeText, pending.OriginalText) + "\n\nUser clarification: " + *trimmed)
				*trimmed = strings.TrimSpace(msg.Text)
				return pendingExecutionConfirmationResult{ReprocessAsFreshTask: true, SkipExecutionConfirm: true}
			}
			updated := applyConfirmationRevision(pending, *trimmed)
			h.confirmationStore.set(updated)
			return pendingExecutionConfirmationResult{Handled: true, Response: buildConfirmationResponse(updated, h.getWorkflowLang())}
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
	// Clear enhanced fields; the revision changes the task, so the LLM
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

	var base string
	if ei := strings.TrimSpace(item.EnhancedInstruction); ei != "" {
		original := strings.TrimSpace(firstNonEmptyTraceText(item.ResumeText, item.OriginalText))
		base = ei
		if original != "" && original != ei {
			base += "\n\n[\u7528\u6237\u539f\u59cb\u8bf7\u6c42]\n" + original
		}
	} else {
		base = strings.TrimSpace(firstNonEmptyTraceText(item.ResumeText, item.OriginalText))
	}
	if base == "" {
		return ""
	}

	pendingItems := extractPendingConfirmItems(item)
	if len(pendingItems) > 0 {
		var pendingSection strings.Builder
		pendingSection.WriteString("\n\n[\u6267\u884c\u4e0a\u4e0b\u6587]\n\u7528\u6237\u5df2\u786e\u8ba4\u6267\u884c\u8ba1\u5212\uff0c\u4f46\u4ee5\u4e0b\u4fe1\u606f\u5c1a\u672a\u63d0\u4f9b\uff0c\u5fc5\u987b\u5148\u83b7\u53d6\u540e\u518d\u6267\u884c\uff1a\n")
		for _, pi := range pendingItems {
			pendingSection.WriteString("- " + pi + "\n")
		}
		pendingSection.WriteString("\u5148\u5c1d\u8bd5\u4f7f\u7528 memory(action=recall) \u67e5\u627e\u8fd9\u4e9b\u503c\uff1b\u5982\u679c\u8bb0\u5fc6\u4e2d\u6ca1\u6709\uff0c\u8bf7\u5411\u7528\u6237\u8be2\u95ee\u3002\u6240\u6709\u5fc5\u9700\u4fe1\u606f\u9f50\u5907\u540e\u518d\u5f00\u59cb\u6267\u884c\u3002")
		return strings.TrimSpace(base + pendingSection.String())
	}

	return strings.TrimSpace(base + "\n\n[\u6267\u884c\u4e0a\u4e0b\u6587]\n\u7528\u6237\u5df2\u786e\u8ba4\u5f53\u524d\u8ba1\u5212\u3002\u76f4\u63a5\u5f00\u59cb\u6267\u884c\uff0c\u4e0d\u8981\u518d\u6b21\u8bf7\u6c42\u786e\u8ba4\u3002\u5982\u679c\u8fd8\u6ca1\u6709\u6700\u7ec8\u4ea4\u4ed8\u7269\uff0c\u8bf7\u8bf4\u660e\u5f53\u524d\u52a8\u4f5c\u6216\u4e0b\u4e00\u6b65\u3002")
}

func extractPendingConfirmItems(item *pendingConfirmation) []string {
	if item == nil {
		return nil
	}
	seen := map[string]bool{}
	var items []string

	sources := []string{item.Summary, item.EnhancedSummary}
	for _, c := range item.RiskFlags {
		sources = append(sources, c)
	}
	for _, c := range item.RevisionHints {
		sources = append(sources, c)
	}

	markers := []string{
		"\u26a0\ufe0f \u5f85\u786e\u8ba4\uff1a",
		"\u26a0\ufe0f \u5f85\u786e\u8ba4:",
		"\u5f85\u786e\u8ba4\uff1a",
		"\u5f85\u786e\u8ba4:",
	}
	for _, src := range sources {
		for _, line := range strings.Split(src, "\n") {
			line = strings.TrimSpace(line)
			for _, marker := range markers {
				if pos := strings.Index(line, marker); pos >= 0 {
					extracted := strings.TrimSpace(line[pos+len(marker):])
					if extracted != "" && !seen[extracted] {
						seen[extracted] = true
						items = append(items, extracted)
					}
				}
			}
		}
	}
	return items
}
