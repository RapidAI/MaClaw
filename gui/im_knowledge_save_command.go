package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func (h *IMMessageHandler) handleImmediateKnowledgeSaveText(msg IMUserMessage, trimmed string) (*IMAgentResponse, bool) {
	text, ok := parseImmediateKnowledgeSaveText(trimmed)
	if !ok {
		return nil, false
	}
	if h.app == nil {
		return &IMAgentResponse{Error: "Knowledge base is not available in this mode."}, true
	}
	source, err := h.app.KnowledgeSaveText(knowledge.TextSaveRequest{
		Text:       text,
		Title:      immediateKnowledgeSaveTitle(text),
		Kind:       string(knowledge.SourceKindConversation),
		SaveScope:  knowledge.SaveScopeProject,
		TopicHint:  "AI assistant saved note",
		AutoLabels: true,
	})
	if err != nil {
		return &IMAgentResponse{Error: fmt.Sprintf("\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93\u5931\u8d25: %v", err)}, true
	}
	return &IMAgentResponse{Text: fmt.Sprintf("\u5df2\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93\u3002Source ID: %s", source.ID)}, true
}

func parseImmediateKnowledgeSaveText(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
	for {
		before := trimmed
		for _, prefix := range []string{
			"\u8bf7\u5e2e\u6211",
			"\u9ebb\u70e6\u4f60",
			"\u9ebb\u70e6",
			"\u5e2e\u6211",
			"\u5e2e\u5fd9",
			"\u8bf7",
		} {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
		if trimmed == before {
			break
		}
	}
	if trimmed == "" {
		return "", false
	}
	payloadAfter := func(marker string, requireDelimiter bool) (string, bool) {
		if !strings.HasPrefix(trimmed, marker) {
			return "", false
		}
		rawRest := trimmed[len(marker):]
		if requireDelimiter && rawRest != "" {
			first := []rune(rawRest)[0]
			if !strings.ContainsRune(":\uff1a;\uff1b\u3002\n\r\t ", first) {
				return "", false
			}
		}
		rest := strings.TrimSpace(rawRest)
		rest = strings.TrimLeft(rest, ":\uff1a;\uff1b\u3002\n\r\t ")
		if len([]rune(strings.TrimSpace(rest))) >= 2 {
			return strings.TrimSpace(rest), true
		}
		return "", false
	}
	contextualMarkers := []string{
		"\u5c06\u4ee5\u4e0b\u5185\u5bb9\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93",
		"\u628a\u4ee5\u4e0b\u5185\u5bb9\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93",
		"\u5c06\u4e0b\u9762\u5185\u5bb9\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93",
		"\u628a\u4e0b\u9762\u5185\u5bb9\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93",
		"\u4fdd\u5b58\u4ee5\u4e0b\u5185\u5bb9\u5230\u77e5\u8bc6\u5e93",
		"\u4fdd\u5b58\u4e0b\u9762\u5185\u5bb9\u5230\u77e5\u8bc6\u5e93",
	}
	for _, marker := range contextualMarkers {
		if rest, ok := payloadAfter(marker, false); ok {
			return rest, true
		}
	}
	genericMarkers := []string{
		"\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93",
		"\u4fdd\u5b58\u8fdb\u77e5\u8bc6\u5e93",
		"\u52a0\u5165\u77e5\u8bc6\u5e93",
		"\u5199\u5165\u77e5\u8bc6\u5e93",
		"\u5b58\u5165\u77e5\u8bc6\u5e93",
	}
	for _, marker := range genericMarkers {
		if rest, ok := payloadAfter(marker, true); ok {
			return rest, true
		}
	}
	return "", false
}

func immediateKnowledgeSaveTitle(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "AI assistant saved note"
	}
	runes := []rune(strings.Join(strings.Fields(trimmed), " "))
	if len(runes) > 40 {
		runes = runes[:40]
	}
	return "AI assistant: " + string(runes)
}
