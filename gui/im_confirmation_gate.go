package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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
	// A free-form reply arrives on the foreground interaction path. Use the
	// fast model and a bounded deadline so a slow semantic convenience check
	// cannot make the confirmation UI feel unresponsive. Button commands remain
	// fully deterministic and bypass this call.
	llmCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	llmCtx = llm.WithRequestTrace(llmCtx, llm.RequestTrace{Caller: "confirmation-intent-fast", OwnerID: userID})
	result, err := h.LLMClassify(llmCtx, LLMClassifyRequest{
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
		UserMessage:       userMessage,
		TimeoutSec:        2,
		Tag:               "confirmation-intent",
		PreferLightweight: true,
	})

	if err != nil {
		log.Printf("[confirmation-intent] LLM classify failed for user %s: %v", userID, err)
		return classifyConfirmationIntentFallback(text)
	}

	intent := normalizeConfirmationIntent(result.Text)
	log.Printf("[confirmation-intent] user=%s text_len=%d -> intent=%q (latency=%.1fs)",
		userID, len([]rune(text)), intent, result.Latency.Seconds())
	return intent
}

func shouldRequireExecutionConfirmation(msg IMUserMessage, pending *pendingConfirmation) bool {
	return shouldRequireExecutionConfirmationForIntent(msg, pending, classifyTaskIntent(strings.TrimSpace(msg.Text)))
}

func (h *IMMessageHandler) handleExecutionConfirmationGate(freshTask bool, msg IMUserMessage, trimmed string, httpClient *http.Client) (*IMAgentResponse, bool) {
	if !shouldConsiderExecutionConfirmation(freshTask, msg, trimmed) {
		return nil, false
	}

	intent := h.classifyTaskIntentForExecution(msg.UserID, trimmed, msg.Attachments, httpClient)
	if !shouldRequireExecutionConfirmationForIntent(msg, nil, intent) {
		return nil, false
	}

	// A confirmation is useful only when it gives the user a meaningful,
	// reviewable interpretation of the task.  In IM channels, echoing the
	// incoming text inside a confirmation card is both redundant and confusing.
	// If task understanding is unavailable, let the normal agent path handle the
	// request instead of sending a no-op confirmation.
	understanding := h.understandTaskWithLLM(msg.UserID, trimmed, intent)
	if !hasMeaningfulTaskUnderstanding(understanding, trimmed) {
		return nil, false
	}
	item := buildPendingConfirmation(h.app, msg.UserID, trimmed, intent, understanding)
	if h.confirmationStore != nil {
		h.confirmationStore.set(item)
	}
	return buildConfirmationResponse(item, h.getWorkflowLang()), true
}

// hasMeaningfulTaskUnderstanding reports whether the confirmation card adds
// information beyond simply restating the user's message. A summary, plan, or
// rewritten instruction must contain substantive content that differs from
// the original request.
func hasMeaningfulTaskUnderstanding(understanding *taskUnderstandingResult, original string) bool {
	if understanding == nil {
		return false
	}
	original = normalizeTaskUnderstandingText(original)
	for _, candidate := range []string{understanding.Summary, understanding.EnhancedInstruction} {
		if taskUnderstandingAddsDetail(candidate, original) {
			return true
		}
	}
	return hasMeaningfulTaskUnderstandingItems(understanding.Goals, original) ||
		hasMeaningfulTaskUnderstandingItems(understanding.Constraints, original) ||
		hasMeaningfulTaskUnderstandingItems(understanding.ExecutionPlan, original)
}

func taskUnderstandingAddsDetail(candidate, normalizedOriginal string) bool {
	normalizedCandidate := normalizeTaskUnderstandingText(candidate)
	if normalizedCandidate == "" || normalizedCandidate == normalizedOriginal {
		return false
	}
	// A shortened extract of the original request is not an expanded
	// understanding either. Confirmations should introduce reviewable detail,
	// not merely restate or discard part of what the user already supplied.
	if strings.Contains(normalizedOriginal, normalizedCandidate) {
		return false
	}
	if !strings.Contains(normalizedCandidate, normalizedOriginal) {
		return true
	}

	// A useful expansion can include the original request (for example, add
	// verification steps after it). Ignore generic labels before deciding
	// whether the remaining text adds enough substance to review.
	extra := strings.ReplaceAll(normalizedCandidate, normalizedOriginal, "")
	for _, label := range []string{
		"task", "request", "yourtaskis", "please", "couldyou", "canyou", "helpme",
		"任务", "用户请求", "任务理解", "执行指令", "请", "请帮我", "帮我", "帮忙",
	} {
		extra = strings.ReplaceAll(extra, label, "")
	}
	return len([]rune(extra)) >= 3
}

func hasMeaningfulTaskUnderstandingItems(items []string, normalizedOriginal string) bool {
	for _, item := range items {
		if taskUnderstandingAddsDetail(item, normalizedOriginal) {
			return true
		}
	}
	return false
}

// normalizeTaskUnderstandingText removes cosmetic differences that do not
// make a task understanding more useful, such as whitespace, casing, and
// trailing punctuation. It deliberately preserves words and CJK characters.
func normalizeTaskUnderstandingText(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsSpace(r) || strings.ContainsRune(".,;:!?，。；：！？、'\"“”‘’()（）[]【】", r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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

func confirmationRiskFlags(intent taskIntent, lang string) []string {
	switch intent {
	case intentCoding:
		return []string{i18n.T(i18n.MsgExecRiskCoding1, lang)}
	case intentSSH:
		return []string{i18n.T(i18n.MsgExecRiskSSH1, lang)}
	case intentAmbiguous:
		return []string{i18n.T(i18n.MsgExecRiskAmbig1, lang)}
	default:
		return nil
	}
}

func confirmationRevisionHints(intent taskIntent, lang string) []string {
	switch intent {
	case intentAmbiguous:
		return []string{i18n.T(i18n.MsgExecRevisionAmbig1, lang), i18n.T(i18n.MsgExecRevisionAmbig2, lang)}
	default:
		return []string{i18n.T(i18n.MsgExecRevisionDefault1, lang), i18n.T(i18n.MsgExecRevisionDefault2, lang)}
	}
}

func buildPendingConfirmation(app *App, userID, text string, result taskIntentResult, understanding *taskUnderstandingResult) *pendingConfirmation {
	now := time.Now()
	// Confirmation panel target path must match ProjectDirBar / tools / workflow
	// "项目路径" for this session owner (not the Projects list).
	projectPath := ""
	if app != nil {
		projectPath = strings.TrimSpace(app.EffectiveWorkingDirForOwner(userID))
	}
	if projectPath == "" {
		projectPath = strings.TrimSpace(corelib.EffectiveWorkspaceDir())
	}
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
	lang := "zh-Hans"
	if app != nil && strings.TrimSpace(app.CurrentLanguage) != "" {
		lang = app.CurrentLanguage
	}

	if understanding != nil && hasDisplayableTaskUnderstanding(understanding) {
		// Do not render or later execute a field that only paraphrases the raw
		// request. Retain the independently useful structured parts (such as a
		// plan), which are what make the confirmation worthwhile.
		displayUnderstanding := taskUnderstandingForConfirmation(understanding, text)
		enhancedSummary = formatTaskUnderstandingSummary(displayUnderstanding, projectPath)
		enhancedInstruction = formatEnhancedInstruction(displayUnderstanding)
		summary = enhancedSummary
	} else {
		// This fallback is retained for non-IM callers that construct a pending
		// confirmation directly. The IM gate only creates confirmation cards when
		// hasMeaningfulTaskUnderstanding has already accepted the understanding.
		if strings.HasPrefix(strings.ToLower(lang), "en") {
			summary = fmt.Sprintf("I understand you want me to handle this task: %s", strings.TrimSpace(text))
		} else {
			summary = fmt.Sprintf("我理解你想让我处理这项任务：%s", strings.TrimSpace(text))
		}
		if projectPath != "" {
			if strings.HasPrefix(strings.ToLower(lang), "en") {
				summary += fmt.Sprintf("\nProject directory: %s", projectPath)
			} else {
				summary += fmt.Sprintf("\n项目目录：%s", projectPath)
			}
		}
		if label := strings.TrimSpace(confirmationTaskLabel(result.Intent)); label != "" {
			if strings.HasPrefix(strings.ToLower(lang), "en") {
				summary += fmt.Sprintf("\nDetected task type: %s", label)
			} else {
				summary += fmt.Sprintf("\n识别任务类型：%s", label)
			}
		}
		if reason := strings.TrimSpace(result.Reason); reason != "" {
			summary += fmt.Sprintf(" (reason: %s)", reason)
		} else if ev := strings.TrimSpace(formatIntentEvidence(result)); ev != "" {
			summary += fmt.Sprintf(" (evidence: %s)", ev)
		}
	}

	plannedActions := confirmationPlannedActions(result.Intent, lang)
	if understanding != nil {
		if meaningfulPlan := meaningfulTaskUnderstandingItems(understanding.ExecutionPlan, text); len(meaningfulPlan) > 0 {
			plannedActions = meaningfulPlan
		}
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
		RiskFlags:           confirmationRiskFlags(result.Intent, lang),
		RevisionHints:       confirmationRevisionHints(result.Intent, lang),
		Status:              confirmationStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
		LastProjectPath:     projectPath,
		EnhancedSummary:     enhancedSummary,
		EnhancedInstruction: enhancedInstruction,
	}
}

func taskUnderstandingForConfirmation(understanding *taskUnderstandingResult, original string) *taskUnderstandingResult {
	if understanding == nil {
		return nil
	}
	clone := *understanding
	normalizedOriginal := normalizeTaskUnderstandingText(original)
	if !taskUnderstandingAddsDetail(clone.Summary, normalizedOriginal) {
		clone.Summary = ""
	}
	if !taskUnderstandingAddsDetail(clone.EnhancedInstruction, normalizedOriginal) {
		clone.EnhancedInstruction = ""
	}
	clone.Goals = meaningfulTaskUnderstandingItems(clone.Goals, original)
	clone.Constraints = meaningfulTaskUnderstandingItems(clone.Constraints, original)
	clone.ExecutionPlan = meaningfulTaskUnderstandingItems(clone.ExecutionPlan, original)
	return &clone
}

func meaningfulTaskUnderstandingItems(items []string, original string) []string {
	normalizedOriginal := normalizeTaskUnderstandingText(original)
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		key := normalizeTaskUnderstandingText(item)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if taskUnderstandingAddsDetail(item, normalizedOriginal) {
			result = append(result, item)
		}
	}
	return result
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
		// Eager embedding warmup: the approved text will be used as proactive
		// recall query later in this same handleIMMessageWithLoop call. Start
		// embedding inference now so it completes during entry_context/serialization
		// processing (~30ms+) before proactive recall needs the result.
		if h.memoryStore != nil && msg.Text != "" {
			h.memoryStore.WarmQueryEmbedding(agent.CompactQueryForEmbedding(msg.Text))
		}
		result := pendingExecutionConfirmationResult{ConfirmedResume: true}
		// Legacy workflow interception removed — routing handled in im_entry_context.
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
		pendingSection.WriteString("\u5148\u68c0\u67e5\u4e0a\u65b9\u7cfb\u7edf\u63d0\u793a\u4e2d\u7684\u300c\u76f8\u5173\u8bb0\u5fc6\uff08\u81ea\u52a8\u53ec\u56de\uff09\u300d\u90e8\u5206\uff0c\u5982\u679c\u5df2\u5305\u542b\u8fd9\u4e9b\u4fe1\u606f\u5219\u76f4\u63a5\u4f7f\u7528\uff1b\u5426\u5219\u5c1d\u8bd5 memory(action=recall) \u67e5\u627e\uff1b\u8bb0\u5fc6\u4e2d\u4e5f\u6ca1\u6709\u65f6\u518d\u5411\u7528\u6237\u8be2\u95ee\u3002")
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

	// Match "待确认：" with or without a legacy leading warning pictograph.
	markers := []string{
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
