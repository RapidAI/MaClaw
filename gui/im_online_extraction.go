package main

// im_online_extraction.go wires the Mem0-style online incremental extraction
// pipeline into the GUI agent loop. The OnlineExtractor is triggered
// asynchronously after each agent loop exit via saveConversationHistoryTimed.

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// triggerOnlineExtraction asynchronously triggers the Mem0-style online
// extraction pipeline after each agent loop exit. It converts the conversation
// history to the format expected by OnlineExtractor and runs in a goroutine.
func (h *IMMessageHandler) triggerOnlineExtraction(userID string, history []agent.ConversationEntry) {
	if h.memoryStore == nil {
		return
	}

	oe := h.memoryStore.OnlineExtractor()
	if oe == nil {
		return
	}

	// Only extract from conversations with enough substance.
	if len(history) < 4 {
		return
	}

	// Convert the last 10 entries to ConversationMessage format.
	messages := convertHistoryToMessages(history, 10)
	if len(messages) < 2 {
		return
	}

	// Build a brief conversation summary from the first user message.
	summary := extractConversationSummary(history)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result := oe.ExtractAndIntegrate(ctx, messages, summary, time.Now(), userID)
		if result.Added > 0 || result.Updated > 0 || result.Deleted > 0 {
			log.Printf("[online_extraction] user=%s extracted=%d added=%d updated=%d deleted=%d noop=%d errors=%d",
				userID, result.ExtractedFacts, result.Added, result.Updated, result.Deleted, result.Noops, result.Errors)
		}
	}()
}

// triggerOnlineExtractionDeferred is like triggerOnlineExtraction but waits
// for chatLoopMu before starting the LLM call. This ensures the extraction
// yields to any new user message that arrives before it begins, preventing
// background LLM calls from competing with the main agent loop for API bandwidth.
func (h *IMMessageHandler) triggerOnlineExtractionDeferred(userID string, history []agent.ConversationEntry) {
	if h.memoryStore == nil {
		return
	}

	oe := h.memoryStore.OnlineExtractor()
	if oe == nil {
		return
	}

	if len(history) < 4 {
		return
	}

	messages := convertHistoryToMessages(history, 10)
	if len(messages) < 2 {
		return
	}

	summary := extractConversationSummary(history)

	go func() {
		// Wait until no agent loop is running for this user. If the user sends
		// a new message before we acquire the lock, their agent loop runs first.
		state := h.getSessionLoop(userID)
		state.mu.Lock()
		state.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result := oe.ExtractAndIntegrate(ctx, messages, summary, time.Now(), userID)
		if result.Added > 0 || result.Updated > 0 || result.Deleted > 0 {
			log.Printf("[online_extraction] user=%s extracted=%d added=%d updated=%d deleted=%d noop=%d errors=%d",
				userID, result.ExtractedFacts, result.Added, result.Updated, result.Deleted, result.Noops, result.Errors)
		}
	}()
}

// convertHistoryToMessages converts the last N conversation entries to
// the ConversationMessage format expected by OnlineExtractor.
func convertHistoryToMessages(history []agent.ConversationEntry, maxEntries int) []memory.ConversationMessage {
	start := len(history) - maxEntries
	if start < 0 {
		start = 0
	}

	var messages []memory.ConversationMessage
	for _, e := range history[start:] {
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
			Role:    role,
			Content: content,
		})
	}
	return messages
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

// triggerOnlineExtractionWithContext is like triggerOnlineExtraction but uses
// the provided context for cancellation. When the user sends a new message,
// the context is cancelled, aborting the in-flight LLM call and freeing API
// bandwidth for the new agent loop.
func (h *IMMessageHandler) triggerOnlineExtractionWithContext(bgCtx context.Context, userID string, history []agent.ConversationEntry) {
	if h.memoryStore == nil {
		return
	}

	oe := h.memoryStore.OnlineExtractor()
	if oe == nil {
		return
	}

	if len(history) < 4 {
		return
	}

	messages := convertHistoryToMessages(history, 10)
	if len(messages) < 2 {
		return
	}

	summary := extractConversationSummary(history)

	ctx, cancel := context.WithTimeout(bgCtx, 30*time.Second)
	defer cancel()

	result := oe.ExtractAndIntegrate(ctx, messages, summary, time.Now(), userID)
	if result.Added > 0 || result.Updated > 0 || result.Deleted > 0 || result.Errors > 0 || ctx.Err() != nil {
		log.Printf("[online_extraction] user=%s extracted=%d added=%d updated=%d deleted=%d noop=%d errors=%d cancelled=%v",
			userID, result.ExtractedFacts, result.Added, result.Updated, result.Deleted, result.Noops, result.Errors, ctx.Err() != nil)
	}
}
