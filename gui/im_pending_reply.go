package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func shouldResumeIncompleteTask(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}

	resumePhrases := []string{
		"继续", "继续上次", "继续做完", "恢复", "接着做",
		"缁х画", "缁х画瀹屾垚", "缁х画涓婃", "鎺ョ潃瀹屾垚", "鎺ョ潃涓婃", "鎭㈠涓婃", "鍋氬畬涓婃",
		"continue", "continue it", "continue this", "resume", "resume it", "resume this", "pick up where you left off",
	}
	for _, phrase := range resumePhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func looksLikeFreshTaskRequest(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || shouldResumeIncompleteTask(trimmed) {
		return false
	}
	if countWords(trimmed) < 4 {
		return false
	}
	lower := strings.ToLower(trimmed)
	freshTaskHints := []string{
		"帮我", "现在", "移动", "复制", "写", "生成", "总结", "分析", "搜索", "导入", "放入",
		"甯垜", "璇蜂綘", "鏁寸悊", "鍒嗘瀽", "鎼滅储", "鐢熸垚", "绉诲姩", "澶嶅埗", "鏀惧叆", "瀵煎叆",
		"please", "help me", "can you", "now", "move", "copy", "write", "generate", "summarize", "analyze", "search", "import",
	}
	for _, hint := range freshTaskHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func shouldClearHistoryForIncompleteTask(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if shouldResumeIncompleteTask(trimmed) {
		return false
	}
	if looksLikeFreshTaskRequest(trimmed) {
		return true
	}
	if countWords(trimmed) < 4 {
		return false
	}
	return true
}

// pendingReplyTTL bounds how long a pending user reply can claim the next
// message as an answer to the same task.
const pendingReplyTTL = 30 * time.Minute

// pendingAskUserState tracks an ask_user question that is waiting for the
// user's response. Stored in IMMessageHandler.pendingAskUser keyed by userID.
type pendingAskUserState struct {
	Question  string
	Options   []string
	InputType string
	History   []agent.ConversationEntry
	Timestamp time.Time
}

// pendingUserReplyState binds a plain-text assistant question to the
// conversation snapshot that produced it. It covers normal prose follow-ups
// such as "which model should I deploy?" that do not use the ask_user tool.
type pendingUserReplyState struct {
	Question  string
	History   []agent.ConversationEntry
	Timestamp time.Time
}

func cloneConversationEntries(entries []agent.ConversationEntry) []agent.ConversationEntry {
	if len(entries) == 0 {
		return nil
	}
	clone := make([]agent.ConversationEntry, len(entries))
	copy(clone, entries)
	return clone
}

func latestAssistantText(entries []agent.ConversationEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role != "assistant" {
			continue
		}
		if text, ok := entries[i].Content.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func conversationEntryTextEqual(a, b agent.ConversationEntry) bool {
	if a.Role != b.Role {
		return false
	}
	textA, okA := a.Content.(string)
	textB, okB := b.Content.(string)
	return okA && okB && strings.TrimSpace(textA) == strings.TrimSpace(textB)
}

func conversationHistoryEqual(a, b []agent.ConversationEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !conversationEntryTextEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func conversationHistoryHasPrefix(current, prefix []agent.ConversationEntry) bool {
	if len(prefix) == 0 || len(current) < len(prefix) {
		return false
	}
	for i := range prefix {
		if !conversationEntryTextEqual(current[i], prefix[i]) {
			return false
		}
	}
	return true
}

func conversationExtensionHasUserMessage(current []agent.ConversationEntry, prefixLen int) bool {
	if prefixLen < 0 || prefixLen > len(current) {
		return true
	}
	for _, entry := range current[prefixLen:] {
		if entry.Role == "user" {
			return true
		}
	}
	return false
}

func isPendingReplyBoundToCurrentHistory(pending []agent.ConversationEntry, current []agent.ConversationEntry) bool {
	if len(pending) == 0 {
		return len(current) == 0
	}
	if len(current) == 0 {
		return true
	}
	if !conversationHistoryHasPrefix(current, pending) {
		return false
	}
	return !conversationExtensionHasUserMessage(current, len(pending))
}

func pendingUserReplyForCurrentHistory(raw interface{}, current []agent.ConversationEntry) (*pendingUserReplyState, bool) {
	pending, ok := raw.(*pendingUserReplyState)
	if !ok || pending == nil || time.Since(pending.Timestamp) >= pendingReplyTTL {
		return nil, false
	}
	return pending, isPendingReplyBoundToCurrentHistory(pending.History, current)
}

func pendingAskUserForCurrentHistory(raw interface{}, current []agent.ConversationEntry) (*pendingAskUserState, bool) {
	pending, ok := raw.(*pendingAskUserState)
	if !ok || pending == nil || time.Since(pending.Timestamp) >= pendingReplyTTL {
		return nil, false
	}
	return pending, isPendingReplyBoundToCurrentHistory(pending.History, current)
}

func (h *IMMessageHandler) bindPendingUserReplyAnswer(msg IMUserMessage, trimmed string, entries *[]agent.ConversationEntry, unfinishedSlot **agent.UnfinishedTaskSlot) (string, bool) {
	if h == nil || msg.IsBackground || entries == nil || unfinishedSlot == nil {
		return "", false
	}
	raw, ok := h.pendingUserReply.LoadAndDelete(msg.UserID)
	if !ok {
		return "", false
	}
	pending, pendingFresh := pendingUserReplyForCurrentHistory(raw, *entries)
	if !pendingFresh && pending != nil {
		log.Printf("[PendingUserReply] discarded stale pending reply for user=%s currentLen=%d boundLen=%d answer=%q", msg.UserID, len(*entries), len(pending.History), truncateRunes(trimmed, 80))
	}
	isPendingAnswer, classifiedPendingAnswer := false, true
	if pendingFresh {
		isPendingAnswer, classifiedPendingAnswer = h.classifyPendingUserReplyAnswer(pending.Question, trimmed)
	}
	if pendingFresh && isPendingAnswer {
		if len(pending.History) > 0 {
			current := h.memory.Load(msg.UserID)
			if len(current) == 0 {
				restored := cloneConversationEntries(pending.History)
				h.memory.Save(msg.UserID, restored)
				*entries = restored
				*unfinishedSlot = h.memory.GetUnfinishedSlot(msg.UserID)
				log.Printf("[PendingUserReply] restored bound question context for user=%s currentLen=%d restoredLen=%d answer=%q", msg.UserID, len(current), len(restored), truncateRunes(trimmed, 80))
			}
		}
		context := fmt.Sprintf("[Context hint] The user is answering the assistant question from the current task, not starting or resuming another task.\nAssistant question: %s\nUser answer: %s", pending.Question, trimmed)
		return context, true
	}
	if pendingFresh && (!classifiedPendingAnswer || trimmed == "") {
		h.pendingUserReply.Store(msg.UserID, pending)
		h.suppressPendingUserReplyUpdate.Store(msg.UserID, true)
	}
	return "", false
}

func (h *IMMessageHandler) consumePendingAskUserAnswer(userID, trimmed string, entries []agent.ConversationEntry) (string, bool) {
	if h == nil {
		return "", false
	}
	raw, ok := h.pendingAskUser.LoadAndDelete(userID)
	if !ok {
		return "", false
	}
	pending, pendingFresh := pendingAskUserForCurrentHistory(raw, entries)
	if !pendingFresh && pending != nil {
		log.Printf("[AskUser] discarded stale pending ask_user for user %s, currentLen=%d boundLen=%d answer=%q", userID, len(entries), len(pending.History), truncateRunes(trimmed, 80))
	}
	if !pendingFresh {
		return "", false
	}
	context := fmt.Sprintf(
		"[Context hint] The user is answering your previous clarification question, not starting a new request.\nAssistant question: %s\nUser answer: %s\nInterpret it as supplementary or corrective information for the current task.",
		pending.Question, trimmed,
	)
	log.Printf("[AskUser] consumed pending ask_user for user %s, question=%q, answer=%q", userID, truncateRunes(pending.Question, 50), truncateRunes(trimmed, 50))
	return context, true
}
