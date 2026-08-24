package main

import (
	"context"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Conversation lookup facts satisfy the generate after-edge. The planner
// lookup-to-generate Requires means generate needs facts, not that this
// turn must call search again.
type semanticConversationHistoryKey struct{}

func withSemanticConversationHistory(ctx context.Context, history []agent.ConversationEntry) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(history) == 0 {
		return ctx
	}
	return context.WithValue(ctx, semanticConversationHistoryKey{}, history)
}

func semanticConversationHistory(ctx context.Context) []agent.ConversationEntry {
	if ctx == nil {
		return nil
	}
	history, _ := ctx.Value(semanticConversationHistoryKey{}).([]agent.ConversationEntry)
	return history
}

func semanticNeedIsLookup(need tool.CapabilityNeed) bool {
	capability := strings.TrimSpace(string(need.Capability))
	return strings.HasPrefix(capability, "information.search.") || capability == "information.current_time"
}

func semanticNeedsHaveLookup(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if semanticNeedIsLookup(need) {
			return true
		}
	}
	return false
}

func semanticNeedsHaveGenerate(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if strings.TrimSpace(string(need.Capability)) == "document.generate.file" {
			return true
		}
	}
	return false
}

// A live-data image renderer consumes only evidence recorded by this turn's
// host-owned lookup. Unlike PDF generation, it has no safe assistant-text
// fallback: treating conversation prose as fresh renderer input would turn
// unproven history into a visual fact. Keep the lookup whenever this producer
// is present until a typed, provenance-preserving history handoff exists.
func semanticNeedsHaveLiveDataVisual(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if strings.TrimSpace(string(need.Capability)) == "visual.render.live_data" {
			return true
		}
	}
	return false
}

func semanticNeedsForReusableConversationLookup(needs []tool.CapabilityNeed, ctx context.Context, userText string) []tool.CapabilityNeed {
	if !semanticNeedsHaveLookup(needs) || !semanticNeedsHaveGenerate(needs) || semanticNeedsHaveLiveDataVisual(needs) {
		return needs
	}
	if !conversationHasReusableLookupFacts(semanticConversationHistory(ctx), userText) {
		return needs
	}
	kept := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if semanticNeedIsLookup(need) {
			continue
		}
		kept = append(kept, need)
	}
	if len(kept) == len(needs) || len(kept) == 0 {
		return needs
	}
	log.Printf("[semantic] omit this-turn lookup; conversation already has same-topic facts")
	return kept
}

func conversationHasReusableLookupFacts(history []agent.ConversationEntry, userText string) bool {
	if lexicalWebSearchRequest(userText) || lexicalFreshLookupRequest(userText) {
		return false
	}
	topic := lookupTopicKey(userText)
	if topic == "" {
		return false
	}
	lastUserTopic := ""
	for _, entry := range history {
		switch strings.ToLower(strings.TrimSpace(entry.Role)) {
		case "user":
			lastUserTopic = lookupTopicKey(entryContentToString(entry.Content))
		case "tool":
			if !lookupTopicsAlign(topic, lastUserTopic) || !conversationEntryIsLookupResult(entry) {
				continue
			}
			if trustedHostLookupEvidence(entryContentToString(entry.Content)) != "" {
				return true
			}
		case "assistant":
			if !lookupTopicsAlign(topic, lastUserTopic) {
				continue
			}
			cleaned := strings.TrimSpace(llm.StripXMLToolCalls(stripDeferredPDFPromise(entryContentToString(entry.Content))))
			if substantialHostPDFReportText(cleaned) {
				return true
			}
		}
	}
	return false
}

func conversationEntryIsLookupResult(entry agent.ConversationEntry) bool {
	name := strings.ToLower(strings.TrimSpace(entry.ToolName))
	if name == "" {
		return false
	}
	if name == "web_search" || name == semanticTrustedWebSearchAdapter {
		return true
	}
	return strings.Contains(name, "search")
}

func lexicalFreshLookupRequest(text string) bool {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return false
	}
	for _, marker := range lexicalFreshLookupMarkers() {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func lookupTopicKey(text string) string {
	text = strings.ToLower(strings.TrimSpace(hostOwnedPDFReportTitle(text)))
	if text == "" {
		return ""
	}
	for _, noise := range lookupTopicNoise() {
		text = strings.ReplaceAll(text, noise, " ")
	}
	text = strings.Map(func(r rune) rune {
		if strings.ContainsRune(" \t,.;:!?，。、；：！？", r) {
			return ' '
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), "")
}

func lookupTopicsAlign(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return left == right || strings.Contains(left, right) || strings.Contains(right, left)
}

func lexicalFreshLookupMarkers() []string {
	return []string{
		"refresh", "latest", "look up again", "search again",
		"\u91cd\u65b0\u67e5", "\u518d\u67e5\u4e00\u904d", "\u518d\u67e5\u4e00\u6b21", "\u518d\u67e5\u4e00\u4e0b", "\u518d\u67e5",
		"\u91cd\u65b0\u641c\u7d22", "\u518d\u641c", "\u5237\u65b0", "\u6700\u65b0", "\u5b9e\u65f6",
	}
}

func lookupTopicNoise() []string {
	return []string{
		"\u67e5\u8be2", "\u8bf7\u5e2e\u6211", "\u5e2e\u6211", "\u8bf7", "\u4e00\u4e0b",
		"\u751f\u6210", "generate", "\u4e00\u4efd", "\u7248\u672c",
		"pdf", "\u62a5\u544a", "report",
		"\u5929\u6c14", "weather",
		"\u80a1\u4ef7", "\u6c47\u7387", "stock price", "exchange rate", "\u822a\u73ed",
	}
}
