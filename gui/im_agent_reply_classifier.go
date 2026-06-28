package main

import (
	"context"
	"strings"
)

type agentNoToolReplyIntent string

const (
	agentNoToolReplyUnknown  agentNoToolReplyIntent = "unknown"
	agentNoToolReplyAction   agentNoToolReplyIntent = "action"
	agentNoToolReplyStall    agentNoToolReplyIntent = "stall"
	agentNoToolReplyPromise  agentNoToolReplyIntent = "promise"
	agentNoToolReplyComplete agentNoToolReplyIntent = "complete"
	agentNoToolReplyFailure  agentNoToolReplyIntent = "failure"
)

func parseAgentNoToolReplyIntent(text string) (agentNoToolReplyIntent, bool) {
	s := strings.TrimSpace(strings.ToLower(text))
	s = strings.Trim(s, "` \t\r\n")
	s = strings.TrimPrefix(s, "json")
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	if len(fields) > 0 {
		s = fields[0]
	}
	s = strings.Trim(s, `"'.,:;`)
	switch agentNoToolReplyIntent(s) {
	case agentNoToolReplyAction, agentNoToolReplyStall, agentNoToolReplyPromise, agentNoToolReplyComplete, agentNoToolReplyFailure:
		return agentNoToolReplyIntent(s), true
	default:
		return agentNoToolReplyUnknown, false
	}
}

func (h *IMMessageHandler) classifyAgentNoToolReply(ctx context.Context, text string) (agentNoToolReplyIntent, bool) {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return agentNoToolReplyUnknown, false
	}
	if h != nil && h.noToolReplyClassifier != nil {
		intent, err := h.noToolReplyClassifier(trimmed)
		if err == nil && intent != agentNoToolReplyUnknown {
			return intent, true
		}
	}
	if h == nil {
		return agentNoToolReplyUnknown, false
	}
	_ = ctx
	if intent, ok := classifyAgentNoToolReplyByHeuristic(trimmed); ok {
		return intent, true
	}
	return agentNoToolReplyUnknown, false
}

func classifyAgentNoToolReplyByHeuristic(text string) (agentNoToolReplyIntent, bool) {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return agentNoToolReplyUnknown, false
	}
	promiseMarkers := []string{
		"\u9a6c\u4e0a", "\u7a0d\u540e", "\u7ee7\u7eed\u751f\u6210", "\u7ee7\u7eed\u5904\u7406", "\u4f1a\u53d1", "\u4f1a\u53d1\u7ed9\u4f60", "\u7a0d\u540e\u53d1\u7ed9\u4f60", "\u9a6c\u4e0a\u53d1\u7ed9\u4f60", "\u7a0d\u7b49", "\u9a6c\u4e0a\u7ee7\u7eed",
		"going to", "i'll", "i will", "will send", "will continue",
	}
	summaryCompleteMarkers := []string{
		"\u4ee5\u4e0b\u662f\u603b\u7ed3", "here is the summary", "final summary:", "final result:",
	}
	hasSummaryComplete := false
	for _, marker := range summaryCompleteMarkers {
		if strings.Contains(s, marker) {
			hasSummaryComplete = true
			break
		}
	}
	for _, marker := range promiseMarkers {
		if strings.Contains(s, marker) && !hasSummaryComplete {
			return agentNoToolReplyPromise, true
		}
	}
	completeMarkers := []string{
		"\u5df2\u5b8c\u6210", "\u5df2\u7ecf\u5b8c\u6210", "\u5b8c\u6210\u4e86", "\u5df2\u5904\u7406\u5b8c\u6210", "\u5904\u7406\u5b8c\u6210",
		"completed", "has been processed",
	}
	for _, marker := range summaryCompleteMarkers {
		completeMarkers = append(completeMarkers, marker)
	}
	for _, marker := range completeMarkers {
		if strings.Contains(s, marker) {
			return agentNoToolReplyComplete, true
		}
	}
	return agentNoToolReplyUnknown, false
}
