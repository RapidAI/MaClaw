package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/session"
)

func (h *IMMessageHandler) extractSessionStartMemoryAsync(userID string, entries []agent.ConversationEntry) {
	if h == nil || h.sessionStartExtractor == nil || len(entries) < 6 {
		return
	}
	msgs := make([]memory.ConversationMessage, 0, len(entries))
	for _, e := range entries {
		text, ok := e.Content.(string)
		if !ok {
			continue
		}
		msgs = append(msgs, memory.ConversationMessage{Role: e.Role, Content: text})
	}
	h.sessionStartExtractor.MaybeExtractAsync(userID, msgs)
}

func (h *IMMessageHandler) saveConversationHistoryTimed(userID string, history []agent.ConversationEntry, resp *IMAgentResponse) {
	startedAt := time.Now()

	// Dynamic entry limit: scale MaxConversationTurns proportionally to the
	// model's effective context window. A 128K model can hold more entries
	// than the default 40 without context overflow.
	//
	// Formula: base 40 entries for 102K effective tokens (128K * 80%).
	// Scale linearly, clamped to [40, 80].
	dynamicLimit := agent.MaxConversationTurns        // 40 default
	dynamicTokenLimit := agent.MaxMemoryTokenEstimate // 60K default
	if h.app != nil {
		cfg := h.app.GetMaclawLLMConfig()
		if ect := cfg.EffectiveContextTokens(); ect > 0 {
			// Entry limit: ect / 1500, clamped to [40, 80].
			scaled := ect / 1500
			if scaled > 80 {
				scaled = 80
			}
			if scaled > dynamicLimit {
				dynamicLimit = scaled
			}
			// Token limit: match the entry limit's token equivalent.
			// This ensures the entry-based and token-based triggers are
			// consistent 鈥?no double-compression.
			tokenEquiv := dynamicLimit * 1500
			if tokenEquiv > dynamicTokenLimit {
				dynamicTokenLimit = tokenEquiv
			}
		}
	}

	// Track whether compaction will occur (for post-compaction actions).
	willCompact := len(history) > dynamicLimit ||
		(dynamicTokenLimit > 0 && estimateConversationEntryTokens(history) > dynamicTokenLimit)

	// Build optional callbacks only when trimming will actually occur.
	var summarizer func(string) string
	var memorySink func(string, []string)

	if willCompact {
		// LLM summarizer for dropped entries (Phase 7).
		if h.app != nil {
			cfg := h.app.GetMaclawLLMConfig()
			if cfg.URL != "" && cfg.Model != "" {
				summarizer = makeSummarizer(cfg, &http.Client{Timeout: 15 * time.Second})
			}
		}
		// Memory sink for substantial dropped assistant messages (Phase 1 supplement).
		if h.memoryStore != nil {
			memorySink = func(content string, tags []string) {
				// Derive title from first meaningful line of the dropped content.
				title := ""
				for _, line := range strings.SplitN(content, "\n", 10) {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						if runes := []rune(line); len(runes) > 60 {
							title = string(runes[:60]) + "..."
						} else {
							title = line
						}
						break
					}
				}
				entry := memory.Entry{
					Content:  content,
					Title:    title,
					Category: memory.CategoryTaskArtifact,
					Tags:     tags,
					Scope:    memory.ScopeProject,
					OwnerID:  userID, // multi-tenant: associate with the user whose history is being trimmed
				}
				_ = h.memoryStore.Save(entry)
			}
		}
	}

	beforeCount := len(history)
	trimmed := trimHistoryWithSummary(history, summarizer, memorySink, dynamicLimit, dynamicTokenLimit)
	h.memory.Save(userID, trimmed)
	if resp != nil {
		resp.MemorySaveNanos = time.Since(startedAt).Nanoseconds()
		h.updatePendingUserReplyFromHistory(userID, trimmed, resp)
	}

	// --- Post-compaction actions (inspired by Codex CLI) ---
	if willCompact && len(trimmed) < beforeCount {
		elapsed := time.Since(startedAt)

		// Improvement 9: Compaction analytics 鈥?log compaction stats for
		// observability and future optimization.
		log.Printf("[compaction] trigger=auto entries=%d->%d summary=%v duration=%dms user=%s",
			beforeCount, len(trimmed), summarizer != nil, elapsed.Milliseconds(), userID)

		// Improvement 7: Reset token calibration after compaction.
		// The API-reported token count from the previous iteration is stale
		// after compaction (conversation is now much shorter). Reset to 0
		// so the next LLM call re-calibrates from scratch.
		h.resetCompactionTokenCalibration(userID)

		// Improvement 8: Compaction quality warning.
		// Track compaction count per user session. Every 2 compactions,
		// warn the user that quality may degrade and suggest starting a
		// new conversation.
		count := h.incrementCompactionCount(userID)
		if count > 0 && count%2 == 0 {
			log.Printf("[compaction] user=%s compaction_count=%d 鈥?quality warning threshold reached", userID, count)
		}
	}

	// Persist transcript to FTS5 session search store (non-blocking).
	h.persistSessionTranscriptAsync(userID, history)

	// Process pending semantic dedup pairs asynchronously.
	// This piggybacks on every agent loop exit to drain the pending queue
	// without adding a separate timer. Each pair takes ~1-3s (one LLM call),
	// so this runs in a goroutine to avoid blocking the response.
	if h.memoryStore != nil && h.memoryStore.PendingDedupCount() > 0 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			merged := h.memoryStore.ProcessPendingDedup(ctx)
			if merged > 0 {
				log.Printf("[semantic_dedup] processed pending pairs after agent loop: merged %d entries", merged)
			}
		}()
	}

	// --- Task sedimentation: ensure meaningful conversations appear in "鏈€杩戜换鍔? ---
	// Many tasks (SSH ops, file processing, info queries) don't go through
	// workflow_artifact_saver or memory(action=save), so they never appear
	// in the task list. This mechanism creates a lightweight project_knowledge
	// entry at the end of every substantial agent loop, giving the task list
	// a complete picture of what the user has been working on.
	h.sedimentTaskEntry(userID, history)

	// --- Online incremental extraction (Mem0-style) ---
	// Trigger the online extractor asynchronously after each agent loop exit.
	// This extracts salient facts from the latest conversation turn and
	// integrates them via four-operation classification (ADD/UPDATE/DELETE/NOOP).
	// Runs in a goroutine to avoid blocking the response.
	h.triggerOnlineExtraction(userID, history)
}

func (h *IMMessageHandler) updatePendingUserReplyFromHistory(userID string, history []agent.ConversationEntry, resp *IMAgentResponse) {
	if h == nil || strings.TrimSpace(userID) == "" || resp == nil {
		return
	}
	if _, ok := h.suppressPendingUserReplyUpdate.Load(userID); ok {
		return
	}
	assistantText := strings.TrimSpace(firstNonEmptyTraceText(resp.Text, latestAssistantText(history)))
	if !h.classifyPendingUserReplyPrompt(assistantText) {
		h.pendingUserReply.Delete(userID)
		return
	}
	h.pendingUserReply.Store(userID, &pendingUserReplyState{Question: truncateRunes(assistantText, 500), History: cloneConversationEntries(history), Timestamp: time.Now()})
	log.Printf("[PendingUserReply] stored pending text reply context for user=%s historyLen=%d question=%q", userID, len(history), truncateRunes(assistantText, 80))
}

func (h *IMMessageHandler) classifyPendingUserReplyPrompt(assistantText string) bool {
	assistantText = strings.TrimSpace(assistantText)
	if assistantText == "" {
		return false
	}
	if h != nil && h.pendingReplyPromptClassifier != nil {
		ok, err := h.pendingReplyPromptClassifier(assistantText)
		if err == nil {
			return ok
		}
		log.Printf("[PendingUserReply] prompt test classifier failed: %v", err)
	}
	if h != nil {
		result, err := h.LLMClassify(context.Background(), LLMClassifyRequest{
			SystemPrompt: `You classify whether the assistant's last message is waiting for the user's next reply inside the same task.

Reply with exactly one word:
- pending: the assistant asks the user to choose, confirm, provide details, approve starting, or otherwise answer before continuing.
- done: the assistant is simply closing, reporting completion, or making a statement that does not require the next user message to be bound to this task.`,
			UserMessage: "Assistant message:\n" + assistantText,
			TimeoutSec:  6,
			Tag:         "pending-reply-prompt",
		})
		if err == nil {
			intent, ok := parsePendingReplyPromptIntent(result.Text)
			return ok && intent == pendingReplyPromptIntentPending
		}
		log.Printf("[PendingUserReply] prompt intent classification failed: %v", err)
	}
	return false
}

func (h *IMMessageHandler) classifyPendingUserReplyAnswer(question, answer string) (bool, bool) {
	question = strings.TrimSpace(question)
	answer = strings.TrimSpace(answer)
	if question == "" || answer == "" {
		return false, true
	}
	if h != nil && h.pendingReplyAnswerClassifier != nil {
		ok, err := h.pendingReplyAnswerClassifier(question, answer)
		if err == nil {
			return ok, true
		}
		log.Printf("[PendingUserReply] answer test classifier failed: %v", err)
	}
	if h != nil {
		result, err := h.LLMClassify(context.Background(), LLMClassifyRequest{
			SystemPrompt: `You classify whether the user's new message answers the assistant's pending question from the same task, or starts a different task.

Reply with exactly one word:
- answer: the user is confirming, choosing, approving, correcting, or supplying information requested by the assistant question.
- new: the user is asking for a separate unrelated task instead.`,
			UserMessage: fmt.Sprintf("Assistant pending question:\n%s\n\nUser new message:\n%s", question, answer),
			TimeoutSec:  6,
			Tag:         "pending-reply-answer",
		})
		if err == nil {
			intent, ok := parsePendingReplyAnswerIntent(result.Text)
			if !ok {
				log.Printf("[PendingUserReply] answer intent classification ambiguous: %q", truncateForLogGUI(result.Text, 60))
				return false, false
			}
			return intent == pendingReplyAnswerIntentAnswer, true
		}
		log.Printf("[PendingUserReply] answer intent classification failed: %v", err)
	}
	return false, false
}

// persistSessionTranscriptAsync converts the conversation history to a
// session.TranscriptEntry slice, serializes it, extracts a topic, and
// persists the document to the FTS5 session search store. Runs in a
// goroutine to avoid blocking the agent loop. Errors are logged but
// do not fail the main flow.
func (h *IMMessageHandler) persistSessionTranscriptAsync(userID string, history []agent.ConversationEntry) {
	if len(history) == 0 {
		return
	}

	// Copy history to avoid data races with the caller.
	historyCopy := make([]agent.ConversationEntry, len(history))
	copy(historyCopy, history)

	persist := func() {
		if h == nil || h.app == nil {
			return
		}
		store, err := session.NewStore(h.getSessionSearchDBPath())
		if err != nil {
			log.Printf("[session_search] failed to open store: %v", err)
			return
		}
		defer func() { _ = store.Close() }()

		entries := conversationToTranscriptEntries(historyCopy)
		if len(entries) == 0 {
			return
		}

		fullText := session.Serialize(entries)
		if strings.TrimSpace(fullText) == "" {
			return
		}

		topic := session.ExtractTopic(fullText)

		// Derive session ID from userID + current timestamp.
		sessionID := fmt.Sprintf("%s_%d", userID, time.Now().UnixNano())

		doc := session.SessionDocument{
			SessionID: sessionID,
			Timestamp: time.Now(),
			Platform:  "gui",
			Topic:     topic,
			FullText:  fullText,
		}

		if err := store.Persist(doc); err != nil {
			log.Printf("[session_search] persist failed: %v", err)
		}
	}

	if h != nil && h.app != nil && strings.TrimSpace(h.getTestHomeDir()) != "" {
		persist()
		return
	}

	go persist()
}

// conversationToTranscriptEntries converts GUI conversation entries to the
// corelib session.TranscriptEntry format for serialization and FTS5 indexing.
func conversationToTranscriptEntries(history []agent.ConversationEntry) []session.TranscriptEntry {
	var entries []session.TranscriptEntry
	for _, e := range history {
		te := session.TranscriptEntry{
			Role: e.Role,
		}

		// Extract content string.
		switch v := e.Content.(type) {
		case string:
			te.Content = v
		default:
			// For non-string content (e.g. multimodal arrays), marshal to JSON.
			if v != nil {
				b, err := json.Marshal(v)
				if err == nil {
					te.Content = string(b)
				}
			}
		}

		// Extract tool call metadata.
		if e.ToolCalls != nil {
			te.ToolCalls = extractToolCallMeta(e.ToolCalls)
		}

		// Set tool call ID for tool result entries.
		if e.ToolCallID != "" {
			te.ToolCallID = e.ToolCallID
		}

		entries = append(entries, te)
	}
	return entries
}

// extractToolCallMeta converts the raw ToolCalls interface (typically
// []interface{} from JSON) into []session.ToolCallMeta for serialization.
func extractToolCallMeta(raw interface{}) []session.ToolCallMeta {
	if raw == nil {
		return nil
	}

	// Try as []interface{} (common from JSON unmarshaling).
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	var metas []session.ToolCallMeta
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		tc := session.ToolCallMeta{}
		if id, ok := m["id"].(string); ok {
			tc.ID = id
		}
		if fn, ok := m["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				tc.Name = name
			}
			if args, ok := fn["arguments"].(string); ok {
				tc.Args = args
			}
		}
		if tc.ID != "" || tc.Name != "" {
			metas = append(metas, tc)
		}
	}
	return metas
}
