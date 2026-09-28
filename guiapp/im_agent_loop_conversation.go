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

// assistantClaimedConversationQuota is the reply the model writes after a
// closed plan: this conversation's tool quota is gone, send /new. An API
// note such as "这个接口的配额用完后会返回 429" is not that claim.
func assistantClaimedConversationQuota(text string) bool {
	exhausted := strings.Contains(text, "耗尽") || strings.Contains(text, "用完") || strings.Contains(text, "用尽")
	if !exhausted {
		return false
	}
	// "当前工具调用配额已用完" has neither 对话 nor /new. An API note
	// ("这个接口的配额用完后会返回 429") has neither 工具调用 nor a chat scope.
	if strings.Contains(text, "工具调用") || strings.Contains(text, "工具配额") || strings.Contains(text, "调用额度") || strings.Contains(text, "调用配额") {
		return true
	}
	if !strings.Contains(text, "额度") && !strings.Contains(text, "配额") {
		return false
	}
	return strings.Contains(text, "对话") || strings.Contains(text, "本轮") || strings.Contains(text, "/new")
}

// neutralizeAssistantQuotaClaim rewrites that reply only in the next model
// request. The stored chat is left as the user saw it. Leaving the claim, or
// an assistant quote of "Planned invocations for this session are complete",
// makes the following turn copy it (production 2026-09-27, the video turn).
func neutralizeAssistantQuotaClaim(entry agent.ConversationEntry) (agent.ConversationEntry, bool) {
	if !strings.EqualFold(strings.TrimSpace(entry.Role), "assistant") {
		return entry, false
	}
	text := assistantEntryText(entry)
	if text == "" || (!assistantClaimedConversationQuota(text) && !historicalSessionCeiling(text)) {
		return entry, false
	}
	entry.Content = "[system] The previous reply wrongly told the user this chat could not continue and to start another chat. That referred to a finished plan. Do not repeat it. Continue the unfinished work with the tools listed now."
	// reasoning_content is sent back to the model on the next turn. Leaving
	// the old chain of thought there replays the closed-plan conclusion.
	entry.ReasoningContent = ""
	return entry, true
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

func semanticShortConsent(text string) bool {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	compact = strings.Trim(compact, "。.!！?？~～啊呀吧呢")
	switch compact {
	case "要", "好", "好的", "可以", "行", "同意", "允许", "允许使用", "可以用", "用吧", "做吧":
		return true
	}
	return false
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
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "bash") && !strings.Contains(lower, "curl") {
		return false
	}
	if strings.Contains(text, "额度") || strings.Contains(text, "配额") {
		return true
	}
	if strings.Contains(text, "不允许") {
		return false
	}
	return strings.Contains(text, "允许我") || strings.Contains(text, "允许使用")
}

// semanticConsentLeavesSpentWave is a short yes after that blocked reply.
// "可爱风" is not one. "要" after "再改一版吗" is not one either: the assistant
// did not say the shell was blocked.
func semanticConsentLeavesSpentWave(userText string, history []agent.ConversationEntry) bool {
	return semanticShortConsent(userText) && assistantBlockedOnShell(lastAssistantText(history))
}

// semanticConsentShellClassification is the plan for that yes: local shell,
// not another copy of the spent knowledge-write grant. Petitioning bash from
// knowledge_write is outside that label, so the call never starts.
func semanticConsentShellClassification(userText string, history []agent.ConversationEntry) *intent.ClassificationResult {
	if !semanticConsentLeavesSpentWave(userText, history) {
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
	if semanticUtteranceIsSourceEdit(userText) || semanticUtteranceIsKnowledgeSave(userText) || !semanticUtteranceWantsRemoteCall(userText) {
		return false
	}
	switch current.Primary {
	case intent.LabelKnowledgeWrite:
	case intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance:
		if current.Confidence >= 0.85 {
			return false
		}
	default:
		return false
	}
	return semanticTextHasCallTarget(userText) || semanticHistoryHasCallTarget(history)
}

func semanticUtteranceIsKnowledgeSave(text string) bool {
	return strings.Contains(text, "保存") || strings.Contains(text, "知识库") || strings.Contains(text, "记下来")
}

func semanticUtteranceIsSourceEdit(text string) bool {
	compact := strings.ToLower(text)
	for _, cue := range []string{"函数", "代码", "文件", "bug", "编译", "重构", "改一下", "修复", ".go", ".py", ".ts", ".java"} {
		if strings.Contains(compact, cue) {
			return true
		}
	}
	return false
}

func semanticUtteranceWantsRemoteCall(text string) bool {
	compact := strings.ToLower(strings.ReplaceAll(text, " ", ""))
	for _, cue := range []string{"视频", "curl", "接口", "http"} {
		if strings.Contains(compact, cue) {
			return true
		}
	}
	return strings.Contains(compact, "api")
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

// shortConsentContinuesHere is a one-word yes after the assistant asked the
// user to open a new chat because tools were "used up". The yes continues
// the unfinished work here.
func shortConsentContinuesHere(userText string, history []agent.ConversationEntry) string {
	if !semanticShortConsent(userText) {
		return ""
	}
	text := lastAssistantText(history)
	if strings.Contains(text, "/new") && (strings.Contains(text, "额度") || strings.Contains(text, "配额")) {
		return "[系统] 用户这句是同意在当前对话里把未做完的事做完，不是同意开启新对话。不要让用户另开对话。当前列出的工具可以用；需要 bash 时直接调用。"
	}
	return ""
}

func (h *IMMessageHandler) buildAgentLoopConversationStart(loopID, userID, userText, systemPrompt, platform string, attachments []MessageAttachment, cfg corelib.MaclawLLMConfig, history []agent.ConversationEntry, priorReplanCount int, recorder *TrajectoryRecorder, tools []map[string]interface{}, onProgress func(string), allowLocalAttachmentStaging bool) agentLoopConversationStart {
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
	for _, entry := range history {
		if replaced, ok := neutralizeHistoricalSessionCeiling(entry); ok {
			entry = replaced
		} else if replaced, ok := neutralizeAssistantQuotaClaim(entry); ok {
			entry = replaced
		}
		conversation = append(conversation, stripHistoryAttachments(entry.ToMessage()))
	}

	userContent := buildUserContentWithPreparedLocalAttachments(userText, attachments, cfg.Protocol, cfg.SupportsVision, h.app, onProgress, allowLocalAttachmentStaging, true, cfg.EffectiveContextTokens())
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})
	history = append(history, agent.ConversationEntry{Role: "user", Content: userContent})
	consentNote := shortConsentContinuesHere(userText, history[:len(history)-1])
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
