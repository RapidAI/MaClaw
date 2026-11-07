package guiapp

// im_online_extraction.go wires conversation turns into the durable online
// extraction outbox after each agent loop exit.

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func (h *IMMessageHandler) triggerOnlineExtraction(userID string, history []agent.ConversationEntry) {
	if isIsolatedAssistantSessionUserID(userID) || h.currentMemoryStore() == nil {
		return
	}
	oe := h.currentMemoryStore().OnlineExtractor()
	if oe == nil || len(history) < 4 {
		return
	}
	messages := convertHistoryToMessages(history, 10)
	if len(messages) < 2 {
		return
	}
	summary := extractConversationSummary(history)
	go h.enqueueOnlineExtraction(context.Background(), oe, userID, messages, summary, "")
}

func (h *IMMessageHandler) triggerOnlineExtractionDeferred(userID string, history []agent.ConversationEntry) {
	if isIsolatedAssistantSessionUserID(userID) || h.currentMemoryStore() == nil {
		return
	}
	oe := h.currentMemoryStore().OnlineExtractor()
	if oe == nil || len(history) < 4 {
		return
	}
	messages := convertHistoryToMessages(history, 10)
	if len(messages) < 2 {
		return
	}
	summary := extractConversationSummary(history)
	go func() {
		state := h.getSessionLoop(userID)
		state.mu.Lock()
		state.mu.Unlock()
		h.enqueueOnlineExtraction(context.Background(), oe, userID, messages, summary, "")
	}()
}

func (h *IMMessageHandler) enqueueOnlineExtraction(ctx context.Context, extractor *memory.OnlineExtractor, userID string, messages []memory.ConversationMessage, summary, requestID string) {
	writeContext := h.memoryWriteContextForUser(userID, "online_extraction")
	writeContext.TurnID = strings.TrimSpace(requestID)
	if err := extractor.EnqueueExtraction(ctx, messages, summary, time.Now(), userID, writeContext); err != nil {
		log.Printf("[online_extraction_queue] enqueue failed user=%s request_id=%q err=%v", userID, requestID, err)
	}
}

// convertHistoryToMessages converts the last N conversation entries to
// the ConversationMessage format expected by OnlineExtractor.
func convertHistoryToMessages(history []agent.ConversationEntry, maxEntries int) []memory.ConversationMessage {
	start := len(history) - maxEntries
	if start < 0 {
		start = 0
	}

	var messages []memory.ConversationMessage
	for i, e := range history[start:] {
		role := e.Role
		if role == "" {
			continue
		}

		content := ""
		switch v := e.Content.(type) {
		case string:
			content = v
		default:
			continue
		}

		if strings.TrimSpace(content) == "" {
			continue
		}

		// Truncate very long entries to keep the extraction prompt manageable.
		if runes := []rune(content); len(runes) > 2000 {
			content = string(runes[:2000]) + "\n[...truncated...]"
		}

		messages = append(messages, memory.ConversationMessage{
			ID:      extractionConversationMessageID(e, content, start+i),
			Role:    role,
			Content: content,
		})
	}
	return messages
}

func extractionConversationMessageID(entry agent.ConversationEntry, content string, index int) string {
	return memory.ConversationMessageID(entry.ID, entry.Timestamp, index, entry.Role, content)
}

// extractConversationSummary builds a brief summary from the first and last
// user messages in the history, providing context for the extraction LLM.
// Using both captures the initial topic and the current focus.
func extractConversationSummary(history []agent.ConversationEntry) string {
	var first, last string
	for _, e := range history {
		if e.Role == "user" {
			if content, ok := e.Content.(string); ok && strings.TrimSpace(content) != "" {
				if first == "" {
					first = content
				}
				last = content
			}
		}
	}
	if first == "" {
		return ""
	}

	truncate := func(s string, maxRunes int) string {
		runes := []rune(s)
		if len(runes) > maxRunes {
			return string(runes[:maxRunes]) + "..."
		}
		return s
	}

	if first == last {
		return truncate(first, 300)
	}
	return truncate(first, 150) + "\n[...]\n" + truncate(last, 150)
}

// triggerOnlineExtractionWithContext enqueues the latest history snapshot.
// Cancelling the foreground-scoped context no longer drops durable work.
func (h *IMMessageHandler) triggerOnlineExtractionWithContext(bgCtx context.Context, userID string, history []agent.ConversationEntry, requestIDs ...string) {
	if isIsolatedAssistantSessionUserID(userID) || h.currentMemoryStore() == nil {
		return
	}
	oe := h.currentMemoryStore().OnlineExtractor()
	if oe == nil || len(history) < 4 {
		return
	}
	messages := convertHistoryToMessages(history, 10)
	if len(messages) < 2 {
		return
	}
	requestID := ""
	if len(requestIDs) > 0 {
		requestID = requestIDs[0]
	}
	h.enqueueOnlineExtraction(bgCtx, oe, userID, messages, extractConversationSummary(history), requestID)
}
