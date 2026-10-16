package guiapp

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

type agentLoopConversationStart struct {
	Conversation []interface{}
	History      []agent.ConversationEntry
	UserContent  interface{}
	TaskAnchor   *taskIdentityAnchor
	StartedAt    time.Time
	Elapsed      time.Duration
}

// neutralizeHistoricalSessionCeiling rewrites a previous turn's closed-plan
// tool result before it is shown again. Left as-is, "Planned invocations for
// this session are complete" makes the next reply tell the user the
// conversation quota is gone and to send /new (production 2026-09-27, user
// answered "要").
func neutralizeHistoricalSessionCeiling(entry agent.ConversationEntry) (agent.ConversationEntry, bool) {
	if entry.Role != "tool" && entry.Role != "tool_result" {
		return entry, false
	}
	text, ok := entry.Content.(string)
	if !ok || !historicalSessionCeiling(text) {
		return entry, false
	}
	entry.Content = "[system] That earlier call did not run because the previous plan was finished. It does not apply to this turn. The tools listed now are usable. Continue in this chat. Do not hand the user a command."
	return entry, true
}

func historicalSessionCeiling(text string) bool {
	return strings.Contains(text, "Planned invocations for this session are complete") ||
		strings.Contains(text, "this turn did not open a new tool plan")
}

// neutralizeAssistantQuotaClaim rewrites an assistant quote of the host's
// closed-plan sentence. The assistant's own wording is not read. A turn the
// host marked plan-closed rewrites the last assistant entry separately.
func neutralizeAssistantQuotaClaim(entry agent.ConversationEntry) (agent.ConversationEntry, bool) {
	if !strings.EqualFold(strings.TrimSpace(entry.Role), "assistant") {
		return entry, false
	}
	text := assistantEntryText(entry)
	if text == "" || !historicalSessionCeiling(text) {
		return entry, false
	}
	return rewriteClosedPlanAssistant(entry), true
}

func rewriteClosedPlanAssistant(entry agent.ConversationEntry) agent.ConversationEntry {
	entry.Content = "[system] The previous reply wrongly told the user this chat could not continue and to start another chat. That referred to a finished plan. Do not repeat it. Continue the unfinished work with the tools listed now."
	entry.ReasoningContent = ""
	return entry
}

func assistantEntryText(entry agent.ConversationEntry) string {
	text := strings.TrimSpace(conversationEntryText(entry.Content))
	reasoning := strings.TrimSpace(entry.ReasoningContent)
	if reasoning == "" {
		return text
	}
	if text == "" {
		return reasoning
	}
	return text + "\n" + reasoning
}

func lastAssistantText(history []agent.ConversationEntry) string {
	for i := len(history) - 1; i >= 0; i-- {
		if !strings.EqualFold(strings.TrimSpace(history[i].Role), "assistant") {
			continue
		}
		return assistantEntryText(history[i])
	}
	return ""
}

// assistantBlockedOnShell reports that the assistant stopped because it could
// not run bash or curl, and told the user the turn was out of quota or asked
// to be allowed to run that command. "不允许" contains "允许" and must not match.
func assistantBlockedOnShell(text string) bool {
	return historicalSessionCeiling(text)
}

// semanticConsentLeavesSpentWave is a continuation label after the host
// closed the previous plan, or after the assistant quoted that host sentence.
// A content label is an answer, not a yes. The assistant's own wording is not read.
func semanticConsentLeavesSpentWave(current intent.ClassificationResult, history []agent.ConversationEntry, planClosed bool) bool {
	if !isGenericContinuationPrimary(current) {
		return false
	}
	return planClosed || assistantBlockedOnShell(lastAssistantText(history))
}

// semanticConsentShellClassification is the plan for that continuation: local
// shell, not another copy of the spent knowledge-write grant.
func semanticConsentShellClassification(current intent.ClassificationResult, history []agent.ConversationEntry, planClosed bool) *intent.ClassificationResult {
	if !semanticConsentLeavesSpentWave(current, history, planClosed) {
		return nil
	}
	return semanticShellClassification("spent-wave consent for bash")
}

func semanticShellClassification(reason string) *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelShellCommand,
		Confidence: 0.95,
		Layer:      3,
		Reason:     reason,
	}
}

// semanticReleasedRequestPlansShell is a new request that left a finished
// wave with a weak coding or knowledge label. Those surfaces cannot run the
// HTTP call the user just asked for, and a coding grant withholds shell on
// purpose (production 2026-09-27: "生成一段猫和老鼠游戏的视频" stayed on
// knowledge_write, then on coding, and bash was never started). A source
// edit keeps its own plan.
func semanticReleasedRequestPlansShell(current intent.ClassificationResult, userText string, history []agent.ConversationEntry) bool {
	switch current.Primary {
	case intent.LabelKnowledgeWrite:
		// The sentence itself is the knowledge save. A URL in the note, or in
		// an earlier turn, is that text's evidence. Rewriting the save into
		// shell lists bash, and the spent-wave note then tells the model to
		// call bash again.
		if intent.ExplicitCapabilityRequestTrigger(userText) {
			return false
		}
	case intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance:
		if current.Confidence >= 0.85 {
			return false
		}
	default:
		return false
	}
	return semanticTextHasCallTarget(userText) || semanticHistoryHasCallTarget(history)
}

// semanticExplicitKnowledgeWrite is the sentence's own capability when it
// asks to persist text into the knowledge base. The tree, a spent shell wave,
// or task-context merge may have labeled it shell because the previous turn
// was a server session. That label publishes bash as the required need, so
// the model can only narrate knowledge_save_text inside a shell command.
// A link inside the note is content being saved. The workspace shell floor
// remains available for a sentence that also names an endpoint.
// The trigger does not execute the tool; it selects the knowledge-ingest
// plan, which is the normal grant path. Runner-up SSH and a protocol-failure
// flag are cleared: the planner would otherwise promote the server session
// back into a required shell, or reject the turn before that plan runs.
func semanticExplicitKnowledgeWrite(current *intent.ClassificationResult, userText string) *intent.ClassificationResult {
	if !intent.ExplicitCapabilityRequestTrigger(userText) {
		return nil
	}
	base := intent.ClassificationResult{}
	if current != nil {
		base = *current
	}
	if semanticKnowledgeWriteAlreadySelected(base) {
		return nil
	}
	base.Primary = intent.LabelKnowledgeWrite
	base.Secondary = nil
	base.ToolNames = nil
	base.WorkflowType = ""
	base.Degraded = false
	base.ControlPlaneFailure = false
	base.CreationOriented = false
	base.RunnerUp = ""
	base.RunnerUpScore = 0
	base.Confidence = 0.95
	if base.Layer < 3 {
		base.Layer = 3
	}
	const note = "explicit knowledge-write request"
	if base.Reason == "" {
		base.Reason = note
	} else if !strings.Contains(base.Reason, note) {
		base.Reason = strings.TrimSpace(base.Reason + "; " + note)
	}
	return &base
}

// semanticKnowledgeWriteAlreadySelected reports a classification that already
// publishes only the knowledge-write plan. Escalation evidence left on an
// otherwise clean label is not selected: the planner promotes runner-up SSH
// into a required remote shell.
func semanticKnowledgeWriteAlreadySelected(result intent.ClassificationResult) bool {
	return result.Primary == intent.LabelKnowledgeWrite &&
		len(result.Secondary) == 0 &&
		len(result.ToolNames) == 0 &&
		result.WorkflowType == "" &&
		result.RunnerUp == "" &&
		!result.Degraded &&
		!result.ControlPlaneFailure &&
		!result.CreationOriented
}

func semanticTextHasCallTarget(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "curl") || strings.Contains(lower, "https://") || strings.Contains(lower, "http://")
}

func semanticHistoryHasCallTarget(history []agent.ConversationEntry) bool {
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < 12; i-- {
		role := strings.ToLower(strings.TrimSpace(history[i].Role))
		if role != "user" && role != "assistant" {
			continue
		}
		seen++
		if semanticTextHasCallTarget(assistantEntryText(history[i])) {
			return true
		}
	}
	return false
}

// shortConsentContinuesHere is a continuation label after the assistant said
// this chat was out of tools. The label continues the unfinished work here.
// A content label is a new request and does not get the note.
func shortConsentContinuesHere(current *intent.ClassificationResult, history []agent.ConversationEntry, planClosed bool) string {
	if current == nil || !isGenericContinuationPrimary(*current) {
		return ""
	}
	if !planClosed && !assistantBlockedOnShell(lastAssistantText(history)) {
		return ""
	}
	return "[系统] 用户这句是同意在当前对话里把未做完的事做完，不是同意开启新对话。不要让用户另开对话。当前列出的工具可以用；需要 bash 时直接调用。"
}

func (h *IMMessageHandler) buildAgentLoopConversationStart(loopID, userID, userText, systemPrompt, platform string, attachments []MessageAttachment, cfg corelib.MaclawLLMConfig, history []agent.ConversationEntry, priorReplanCount int, recorder *TrajectoryRecorder, tools []map[string]interface{}, onProgress func(string), allowLocalAttachmentStaging bool, semantic *intent.ClassificationResult, planClosed bool) agentLoopConversationStart {
	startedAt := time.Now()
	// Capture identity/source before historical attachment stripping and before
	// any compaction can discard the document body.
	anchorPrompt := ""
	var anchorSnapshot *taskIdentityAnchor
	if anchor, ok := h.taskIdentityAnchorForTurn(userID, userText); ok {
		copy := anchor
		anchorSnapshot = &copy
		anchorPrompt = taskIdentityAnchorPrompt(anchor)
		if anchorPrompt != "" {
			// The first system message is preserved by every compactor. Keep the
			// anchor there rather than as a later synthetic message that a long
			// tool loop could discard.
			systemPrompt = appendTaskIdentityAnchorPrompt(systemPrompt, anchorSnapshot)
		}
	}
	conversation := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
	}
	lastAssistant := -1
	if planClosed {
		for i := range history {
			if strings.EqualFold(strings.TrimSpace(history[i].Role), "assistant") {
				lastAssistant = i
			}
		}
	}
	for i, entry := range history {
		if i == lastAssistant {
			entry = rewriteClosedPlanAssistant(entry)
		} else if replaced, ok := neutralizeHistoricalSessionCeiling(entry); ok {
			entry = replaced
		} else if replaced, ok := neutralizeAssistantQuotaClaim(entry); ok {
			entry = replaced
		}
		conversation = append(conversation, stripHistoryAttachments(entry.ToMessage()))
	}

	userContent := buildUserContentWithPreparedLocalAttachments(userText, attachments, cfg.Protocol, cfg.SupportsVision, h.app, onProgress, allowLocalAttachmentStaging, true, cfg.EffectiveContextTokens())
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})
	history = append(history, agent.ConversationEntry{Role: "user", Content: userContent})
	consentNote := shortConsentContinuesHere(semantic, history[:len(history)-1], planClosed)
	if consentNote != "" {
		conversation = append(conversation, map[string]string{"role": "system", "content": consentNote})
	}

	var driftCtx string
	if driftTool, ok := h.sessionDriftTool.LoadAndDelete(userID); ok {
		toolName, _ := driftTool.(string)
		driftCtx = fmt.Sprintf(
			"[System notice] The previous turn stopped after repeated failures calling %s. "+
				"Do not use the same approach again. "+
				"If no alternative is available, explain the current limitation and recommendation to the user.",
			toolName,
		)
		conversation = append(conversation, map[string]string{
			"role": "system", "content": driftCtx,
		})
		log.Printf("[DriftContext] injected drift warning for user=%s tool=%s priorReplanCount=%d", userID, toolName, priorReplanCount)
	}

	if recorder != nil {
		provider := ""
		if h != nil {
			provider = h.getMaclawLLMProviders().Current
		}
		// Kind defaults to main; shared/subagent paths overwrite via SetKind / their own sessions.
		recorder.StartSessionWithMeta(loopID, provider, cfg.Model, cfg.Protocol, userID, platform, "main", "", tools)
		recorder.SetExperienceDomain(h.resolveTrajectoryExperienceDomain("main", userID))
		recorder.Record("system", systemPrompt, nil, "", "")
		// Prior multi-turn context the model will see (before current user turn).
		if prior := history[:len(history)-1]; len(prior) > 0 {
			recorder.RecordHistory(prior)
		}
		recorder.Record("user", userContent, nil, "", "")
		if consentNote != "" {
			recorder.Record("system", consentNote, nil, "", "")
		}
		if driftCtx != "" {
			recorder.Record("system", driftCtx, nil, "", "")
		}
	}

	return agentLoopConversationStart{
		Conversation: conversation,
		History:      history,
		UserContent:  userContent,
		TaskAnchor:   anchorSnapshot,
		StartedAt:    startedAt,
		Elapsed:      time.Since(startedAt),
	}
}
