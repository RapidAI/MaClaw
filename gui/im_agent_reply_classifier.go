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
	return agentNoToolReplyUnknown, false
}
