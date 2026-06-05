package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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
	// Store the prepared messages for deferred extraction. The actual LLM call
	// is triggered AFTER the agent loop completes (in saveConversationHistoryTimed),
	// ensuring it never competes with the main LLM call for API bandwidth.
	// The extraction results are only needed for the NEXT session anyway.
	h.deferredSessionExtraction.Store(userID, msgs)
}

func (h *IMMessageHandler) saveConversationHistoryTimed(userID string, history []agent.ConversationEntry, resp *IMAgentResponse) {
	startedAt := time.Now()
	history = sanitizeVEGroupExecutorHistory(userID, history)

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
			// consistent - no double-compression.
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
	var memorySink func(string, []string)

	if willCompact {
		// Memory sink for substantial dropped assistant messages (Phase 1 supplement).
		if h.memoryStore != nil {
			memorySink = func(content string, tags []string) {
				preview := memoryRefPreview(content)
				if preview == "" {
					return
				}

				// Derive title from first meaningful line of the dropped content.
				title := ""
				for _, line := range strings.SplitN(preview, "\n", 10) {
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

				refPath, err := writeMemoryRefFile(h.memoryStore.Path(), userID, "conversation_trim", content, time.Now())
				if err != nil {
					log.Printf("[compaction] failed to write memory ref for user=%s: %v", userID, err)
				}
				entryTags := append([]string{}, tags...)
				if refPath != "" {
					entryTags = append(entryTags, "source_ref")
				}
				identityTagCount := len(entryTags)
				if refPath != "" {
					entryTags = append(entryTags, "ref:"+refPath)
					identityTagCount = len(entryTags)
				}
				_, err = h.memoryStore.UpsertTaskArtifact(memory.TaskArtifactUpsertOptions{
					Title:            title,
					Content:          preview,
					Tags:             entryTags,
					IdentityTagCount: identityTagCount,
					OwnerID:          userID,
					SourceType:       "conversation_trim_ref",
					SourceURL:        refPath,
				})
				if err == nil && h.app != nil {
					h.app.triggerMemoryPipelineSoon(45 * time.Second)
				}
			}
		}
	}

	beforeCount := len(history)
	// Compaction: trim without LLM summarizer (fast, <1ms). Uses static
	// placeholder for dropped entries. The LLM summary was previously done
	// synchronously (6.5s blocking), but its value is marginal - the static
	// placeholder is functionally equivalent for the LLM's next turn.
	// Removing the summarizer from the critical path saves 6.5s per compaction.
	trimmed := trimHistoryWithSummary(history, nil, memorySink, dynamicLimit, dynamicTokenLimit)
	h.memory.Save(userID, trimmed)
	if resp != nil {
		resp.MemorySaveNanos = time.Since(startedAt).Nanoseconds()
		h.updatePendingUserReplyFromHistory(userID, trimmed, resp)
	}

	// --- Post-compaction actions (inspired by Codex CLI) ---
	if willCompact && len(trimmed) < beforeCount {
		elapsed := time.Since(startedAt)

		// Improvement 9: Compaction analytics - log compaction stats for
		// observability and future optimization.
		log.Printf("[compaction] trigger=auto entries=%d->%d duration=%dms user=%s",
			beforeCount, len(trimmed), elapsed.Milliseconds(), userID)

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
			log.Printf("[compaction] user=%s compaction_count=%d - quality warning threshold reached", userID, count)
		}
	}

	requestID := ""
	if resp != nil {
		requestID = strings.TrimSpace(resp.RequestID)
	}
	if requestID == "" {
		requestID = h.activePostConversationRequestID(userID)
	}
	h.schedulePostConversationProcessingWithRequestID(userID, requestID, trimmed)
}

func (h *IMMessageHandler) activePostConversationRequestID(userID string) string {
	if h == nil {
		return ""
	}
	if ctx := h.getSessionLoopCtx(userID); ctx != nil {
		return strings.TrimSpace(ctx.Runtime.RequestID)
	}
	ctx, _, ok := h.legacyLoopSnapshotForUser(userID)
	if ok && ctx != nil {
		return strings.TrimSpace(ctx.Runtime.RequestID)
	}
	return ""
}

func (h *IMMessageHandler) schedulePostConversationProcessing(userID string, history []agent.ConversationEntry) {
	h.schedulePostConversationProcessingWithRequestID(userID, "", history)
}

func (h *IMMessageHandler) schedulePostConversationProcessingWithRequestID(userID, requestID string, history []agent.ConversationEntry) {
	if h == nil {
		return
	}
	if h.app != nil && strings.TrimSpace(h.app.testHomeDir) != "" {
		log.Printf("[post-conversation] skip async post-processing in test mode user=%s request_id=%q history_len=%d", userID, requestID, len(history))
		return
	}
	var deferredMessages []memory.ConversationMessage
	if raw, ok := h.deferredSessionExtraction.LoadAndDelete(userID); ok {
		if msgs, ok := raw.([]memory.ConversationMessage); ok {
			deferredMessages = msgs
		}
	}
	historySnapshot := append([]agent.ConversationEntry(nil), history...)
	h.ensurePostConversationScheduler().Enqueue(postConversationTask{
		UserID:           userID,
		RequestID:        strings.TrimSpace(requestID),
		History:          historySnapshot,
		DeferredMessages: deferredMessages,
	})
}

func (h *IMMessageHandler) ensurePostConversationScheduler() *postConversationScheduler {
	h.postConversationSchedulerMu.Lock()
	defer h.postConversationSchedulerMu.Unlock()
	if h.postConversationScheduler != nil {
		return h.postConversationScheduler
	}
	h.postConversationScheduler = newPostConversationScheduler(h)
	return h.postConversationScheduler
}

func (h *IMMessageHandler) runPostConversationProcessing(bgCtx context.Context, userID, requestID string, history []agent.ConversationEntry, deferredMessages []memory.ConversationMessage) {
	startedAt := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[post-conversation] panic user=%s request_id=%q panic=%v", userID, requestID, r)
		}
		log.Printf("[post-conversation] done user=%s request_id=%q duration=%s cancelled=%v history_len=%d", userID, requestID, time.Since(startedAt).Round(time.Millisecond), bgCtx.Err() != nil, len(history))
		imPerfLog("post_conversation", startedAt, requestID, userID, "cancelled", bgCtx.Err() != nil, "history_len", len(history), "deferred", len(deferredMessages))
	}()
	log.Printf("[post-conversation] start user=%s request_id=%q history_len=%d", userID, requestID, len(history))

	// Persist transcript to FTS5 session search store (non-blocking).
	stageStartedAt := time.Now()
	h.persistSessionTranscriptAsync(userID, history)
	log.Printf("[post-conversation] stage=transcript user=%s request_id=%q duration=%s cancelled=%v", userID, requestID, time.Since(stageStartedAt).Round(time.Millisecond), bgCtx.Err() != nil)
	if h.app != nil && !h.app.waitForForegroundAgentIdle(bgCtx, "post-conversation-llm", userID) {
		return
	}

	// Process pending semantic dedup pairs in the background. This can make LLM
	// calls, so it uses the owner-scoped cancellable context.
	if h.memoryStore != nil && h.memoryStore.PendingDedupCount() > 0 {
		stageStartedAt = time.Now()
		ctx, cancel := context.WithTimeout(bgCtx, 30*time.Second)
		merged := h.memoryStore.ProcessPendingDedup(ctx)
		cancel()
		log.Printf("[post-conversation] stage=semantic_dedup user=%s request_id=%q merged=%d duration=%s cancelled=%v", userID, requestID, merged, time.Since(stageStartedAt).Round(time.Millisecond), bgCtx.Err() != nil)
		if merged > 0 {
			log.Printf("[semantic_dedup] processed pending pairs after agent loop: merged %d entries", merged)
		}
	}

	// Recent-task sedimentation is useful, but not required before the visible
	// answer is delivered to the user.
	stageStartedAt = time.Now()
	h.sedimentTaskEntry(userID, history)
	log.Printf("[post-conversation] stage=sediment user=%s request_id=%q duration=%s cancelled=%v", userID, requestID, time.Since(stageStartedAt).Round(time.Millisecond), bgCtx.Err() != nil)
	if h.app != nil && !h.app.waitForForegroundAgentIdle(bgCtx, "online-extraction", userID) {
		return
	}

	// Online incremental extraction uses the shared bgCtx and exits early if the
	// user sends a new foreground message for the same owner.
	stageStartedAt = time.Now()
	h.triggerOnlineExtractionWithContext(bgCtx, userID, history)
	log.Printf("[post-conversation] stage=online_extraction user=%s request_id=%q duration=%s cancelled=%v", userID, requestID, time.Since(stageStartedAt).Round(time.Millisecond), bgCtx.Err() != nil)

	if len(deferredMessages) > 0 && bgCtx.Err() == nil && h.sessionStartExtractor != nil {
		stageStartedAt = time.Now()
		extractCtx := llm.WithRequestTrace(bgCtx, llm.RequestTrace{Caller: "session-start-extraction", OwnerID: userID, RequestID: requestID})
		h.sessionStartExtractor.MaybeExtract(extractCtx, userID, deferredMessages)
		log.Printf("[post-conversation] stage=session_start_extraction user=%s request_id=%q messages=%d duration=%s cancelled=%v", userID, requestID, len(deferredMessages), time.Since(stageStartedAt).Round(time.Millisecond), bgCtx.Err() != nil)
	}
}

func (h *IMMessageHandler) storeBackgroundLLMCancelForOwner(userID string, cancel context.CancelFunc) {
	if h == nil || cancel == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		h.backgroundLLMMu.Lock()
		previous := h.backgroundLLMCancel
		h.backgroundLLMCancel = cancel
		h.backgroundLLMMu.Unlock()
		if previous != nil {
			previous()
		}
		return
	}
	if previous, loaded := h.backgroundLLMCancelByUser.LoadOrStore(userID, cancel); loaded {
		if prevCancel, ok := previous.(context.CancelFunc); ok && prevCancel != nil {
			prevCancel()
		}
		h.backgroundLLMCancelByUser.Store(userID, cancel)
	}
}

func (h *IMMessageHandler) cancelBackgroundLLMForOwner(userID string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		h.backgroundLLMMu.Lock()
		cancel := h.backgroundLLMCancel
		h.backgroundLLMCancel = nil
		h.backgroundLLMMu.Unlock()
		if cancel == nil {
			return false
		}
		cancel()
		return true
	}
	if previous, loaded := h.backgroundLLMCancelByUser.LoadAndDelete(userID); loaded {
		if cancel, ok := previous.(context.CancelFunc); ok && cancel != nil {
			cancel()
			return true
		}
	}
	return h.ensurePostConversationScheduler().CancelOwner(userID, "foreground")
}

func (h *IMMessageHandler) cancelAllBackgroundLLM(reason string) int {
	if h == nil {
		return 0
	}
	cancelled := 0
	h.backgroundLLMMu.Lock()
	legacyCancel := h.backgroundLLMCancel
	h.backgroundLLMCancel = nil
	h.backgroundLLMMu.Unlock()
	if legacyCancel != nil {
		legacyCancel()
		cancelled++
	}
	h.backgroundLLMCancelByUser.Range(func(key, value any) bool {
		if cancel, ok := value.(context.CancelFunc); ok && cancel != nil {
			cancel()
			cancelled++
		}
		h.backgroundLLMCancelByUser.Delete(key)
		return true
	})
	h.postConversationSchedulerMu.Lock()
	postScheduler := h.postConversationScheduler
	h.postConversationSchedulerMu.Unlock()
	if postScheduler != nil {
		cancelled += postScheduler.CancelAll(reason)
	}
	if cancelled > 0 {
		log.Printf("[background-llm] cancel_all reason=%s cancelled=%d", strings.TrimSpace(reason), cancelled)
	}
	return cancelled
}

func sanitizeVEGroupExecutorHistory(userID string, history []agent.ConversationEntry) []agent.ConversationEntry {
	if !strings.HasPrefix(strings.TrimSpace(userID), "ve-group-executor:") || len(history) == 0 {
		return history
	}
	out := make([]agent.ConversationEntry, 0, len(history))
	for _, entry := range history {
		if strings.EqualFold(strings.TrimSpace(entry.Role), "assistant") {
			entry.ReasoningContent = ""
		}
		out = append(out, entry)
	}
	return out
}

func (h *IMMessageHandler) updatePendingUserReplyFromHistory(userID string, history []agent.ConversationEntry, resp *IMAgentResponse) {
	if h == nil || strings.TrimSpace(userID) == "" || resp == nil {
		return
	}
	if _, ok := h.suppressPendingUserReplyUpdate.Load(userID); ok {
		return
	}
	assistantText := sanitizePendingUserReplyQuestion(firstNonEmptyTraceText(resp.Text, latestAssistantText(history)))
	if assistantText == "" {
		h.pendingUserReply.Delete(userID)
		return
	}
	if looksLikeBrowserDebugInstruction(assistantText) {
		h.pendingUserReply.Delete(userID)
		return
	}
	// Run the LLM classification in a background goroutine to avoid blocking
	// the response return. The pending reply state is consumed on the NEXT
	// user message, so there's no urgency - it just needs to be ready before
	// the next message arrives (typically seconds to minutes later).
	historyCopy := cloneConversationEntries(history)
	go func() {
		if !h.classifyPendingUserReplyPrompt(userID, assistantText) {
			h.pendingUserReply.Delete(userID)
			return
		}
		h.pendingUserReply.Store(userID, &pendingUserReplyState{Question: truncateRunes(assistantText, 500), History: historyCopy, Timestamp: time.Now()})
		log.Printf("[PendingUserReply] stored pending text reply context for user=%s historyLen=%d question_len=%d", userID, len(historyCopy), len([]rune(assistantText)))
	}()
}

func sanitizePendingUserReplyQuestion(text string) string {
	text = strings.TrimSpace(agent.StripRolePrefixHallucination(text))
	return strings.TrimSpace(text)
}

func looksLikeBrowserDebugInstruction(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "chrome://inspect/#remote-debugging") ||
		strings.Contains(normalized, "edge://inspect/#remote-debugging") ||
		strings.Contains(normalized, "allow remote debugging") ||
		(strings.Contains(normalized, "remote debugging") && strings.Contains(normalized, "chrome")) ||
		(strings.Contains(normalized, "远程调试") && strings.Contains(normalized, "chrome"))
}

func (h *IMMessageHandler) classifyPendingUserReplyPrompt(userID, assistantText string) bool {
	assistantText = sanitizePendingUserReplyQuestion(assistantText)
	if assistantText == "" {
		return false
	}
	if looksLikeBrowserDebugInstruction(assistantText) {
		return false
	}
	if !looksLikePendingUserReplyPromptCandidate(assistantText) {
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
		ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "pending-reply-prompt", OwnerID: userID})
		result, err := h.LLMClassify(ctx, LLMClassifyRequest{
			SystemPrompt: `You classify whether the assistant's last message is waiting for the user's next reply inside the same task.

Reply with exactly one word:
- pending: the assistant asks the user to choose, confirm, provide details, approve starting, or otherwise answer before continuing.
- done: the assistant is simply closing, reporting completion, or making a statement that does not require the next user message to be bound to this task.`,
			UserMessage: "Assistant message:\n" + assistantText,
			TimeoutSec:  30,
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

func (h *IMMessageHandler) classifyPendingUserReplyAnswer(userID, question, answer string) (bool, bool) {
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
		ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "pending-reply-answer", OwnerID: userID})
		result, err := h.LLMClassify(ctx, LLMClassifyRequest{
			SystemPrompt: `You classify whether the user's next message answers the assistant's pending question or starts a new task.

Reply with exactly one word:
- answer: the user is choosing, confirming, approving, refusing, or otherwise answering the pending question.
- new: the user is asking for a different task, especially deployment, coding, server work, file work, or a new request.`,
			UserMessage: fmt.Sprintf("Assistant question:\n%s\n\nUser message:\n%s", question, answer),
			TimeoutSec:  30,
			Tag:         "pending-reply-answer",
		})
		if err == nil {
			intent, ok := parsePendingReplyAnswerIntent(result.Text)
			if !ok {
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
