package main

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// semanticClassificationNeedsTaskContext reports whether a fresh per-message
// classification landed nowhere actionable. Only then may the host retry with
// conversation context merged in — a clear new request must never be
// rewritten by an older task's intent.
func semanticClassificationNeedsTaskContext(result *intent.ClassificationResult) bool {
	if result == nil {
		return false
	}
	if result.Degraded {
		return true
	}
	primary := result.Primary
	return primary == "" || primary == intent.LabelUnknown || primary.IsNonCapabilityLabel()
}

// pendingAnswerPrefersTaskMerge reports whether a turn that answers a pending
// assistant question should still retry classification with the task context
// merged in, even though the bare verdict landed on a managed family. An
// answer takes its meaning from the question, so a short reply has no
// reliable standalone intent; only a confident bare verdict (a deliberate,
// clearly-stated task switch) overrides the merge. The threshold matches the
// cross-validation band: below 0.85 the bare verdict is not trusted to
// re-route a bound continuation on its own.
func pendingAnswerPrefersTaskMerge(result *intent.ClassificationResult, isPendingAnswer bool) bool {
	if !isPendingAnswer || result == nil {
		return false
	}
	return result.Confidence < 0.85
}

// recentUserTaskTexts returns the most recent user-role history texts,
// oldest first, each trimmed and capped. These are the only history entries
// that may carry an active task intent; assistant prose must not leak into a
// classification input.
func recentUserTaskTexts(entries []agent.ConversationEntry, limit int) []string {
	if limit <= 0 {
		limit = 2
	}
	texts := make([]string, 0, limit)
	for i := len(entries) - 1; i >= 0 && len(texts) < limit; i-- {
		if strings.ToLower(strings.TrimSpace(entries[i].Role)) != "user" {
			continue
		}
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		text = truncateRunes(strings.TrimSpace(text), 120)
		if text == "" {
			continue
		}
		// Host-injected nudges ride the user role ("[系统] 还有未关闭问题…")
		// but they are routing machinery, never task intent; merging one into
		// a classification input only adds noise.
		if strings.HasPrefix(text, "[系统]") {
			continue
		}
		texts = append(texts, text)
	}
	for i, j := 0, len(texts)-1; i < j; i, j = i+1, j-1 {
		texts[i], texts[j] = texts[j], texts[i]
	}
	return texts
}

// mergeTaskContextText composes the classification input for the
// context-merge retry: prior user task intent followed by the current
// message, so a bare continuation ("再加上照片", "发给我") is classified as
// part of the task it continues rather than as an isolated utterance.
func mergeTaskContextText(current string, priorUserTexts []string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return ""
	}
	parts := make([]string, 0, len(priorUserTexts)+1)
	for _, text := range priorUserTexts {
		if text = strings.TrimSpace(text); text != "" && text != current {
			parts = append(parts, text)
		}
	}
	parts = append(parts, current)
	return truncateRunes(strings.Join(parts, "；"), 300)
}

// classifyWithTaskContextMerge is the deterministic multi-turn intent merge:
// it runs only after the bare classification already failed (unknown,
// non-capability, or degraded), retries once with the recent user task intent
// merged into the classified text, and accepts the retry only when it lands
// on a managed capability route. One extra classification, no new durable
// state, no change to clear first-shot intents.
func (h *IMMessageHandler) classifyWithTaskContextMerge(ctx context.Context, msg IMUserMessage, history []agent.ConversationEntry, recentHistory []string) (intent.ClassificationResult, bool) {
	uic := h.getUnifiedClassifier()
	if uic == nil {
		return intent.ClassificationResult{}, false
	}
	current := strings.TrimSpace(msg.Text)
	// Long, self-contained messages gain nothing from task context; the merge
	// exists for continuation-shaped utterances.
	if utf8.RuneCountInString(current) > 120 {
		return intent.ClassificationResult{}, false
	}
	merged := mergeTaskContextText(current, recentUserTaskTexts(history, 2))
	if merged == "" || merged == current {
		return intent.ClassificationResult{}, false
	}
	result := uic.ClassifyContext(ctx, intent.MessageContext{
		Text:          semanticUserIntentText(merged),
		UserID:        msg.UserID,
		RecentHistory: recentHistory,
	})
	if result.Degraded || !imSemanticIntentIsManaged(result) {
		return intent.ClassificationResult{}, false
	}
	result.Reason = strings.TrimSpace(result.Reason + "; task-context merge")
	return result, true
}
