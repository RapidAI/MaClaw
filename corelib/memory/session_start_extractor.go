package memory

import (
	"log"
	"strings"
	"sync"
	"time"
)

// SessionStartExtractor extracts knowledge from the previous conversation
// session when a new session begins. This is inspired by Codex CLI's
// memories/phase1.rs which processes old rollouts at session startup.
//
// The key difference from KnowledgeExtractor (which runs at session expiry):
// this runs at session START, ensuring knowledge from the previous session
// is available for recall in the new session — not after a 1-hour cooldown.
//
// Design principles:
//   - Async: extraction runs in a goroutine, never blocks the user's message
//   - Idempotent: tracks processed sessions to avoid duplicate extraction
//   - Lightweight: reuses KnowledgeExtractor's LLM call and dedup logic
//   - Bounded: skips trivial sessions (<6 entries) and caps input size
type SessionStartExtractor struct {
	store *Store
	llm   LLMChatCaller

	mu        sync.Mutex
	processed map[string]bool // keyed by userID+sessionHash, prevents re-extraction
}

// NewSessionStartExtractor creates an extractor that saves to the given store.
// llm is the LLM caller for knowledge extraction (same interface as KnowledgeExtractor).
func NewSessionStartExtractor(store *Store, llm LLMChatCaller) *SessionStartExtractor {
	return &SessionStartExtractor{
		store:     store,
		llm:       llm,
		processed: make(map[string]bool),
	}
}

// MaybeExtractAsync checks if the given conversation entries represent a
// previous session worth extracting, and if so, runs extraction in a
// background goroutine.
//
// "Worth extracting" means:
//   - At least 6 entries (trivial Q&A not worth the LLM call)
//   - Contains at least 1 assistant message with substantial content
//   - Not already processed in this application lifetime
//
// This should be called early in handleIMMessageWithLoop with the entries
// loaded from the previous session (before the new message is appended).
func (e *SessionStartExtractor) MaybeExtractAsync(userID string, entries []ConversationMessage) {
	if e == nil || e.llm == nil || !e.llm.IsConfigured() {
		return
	}
	if len(entries) < 6 {
		return
	}

	// Build a lightweight session fingerprint for idempotency.
	// Use first+last entry content hash to avoid re-processing.
	fingerprint := userID + "|" + sessionFingerprint(entries)

	e.mu.Lock()
	if e.processed[fingerprint] {
		e.mu.Unlock()
		return
	}
	e.processed[fingerprint] = true
	// Cap the processed map to prevent unbounded growth.
	if len(e.processed) > 200 {
		// Evict oldest entries (simple: clear all, since this is just
		// an optimization — re-extraction is harmless due to dedup in Store).
		e.processed = map[string]bool{fingerprint: true}
	}
	e.mu.Unlock()

	// Check for substantial content: at least one assistant message > 100 chars.
	hasSubstantial := false
	for _, m := range entries {
		if m.Role == "assistant" && len([]rune(m.Content)) > 100 {
			hasSubstantial = true
			break
		}
	}
	if !hasSubstantial {
		return
	}

	go e.extract(userID, entries)
}

func (e *SessionStartExtractor) extract(userID string, entries []ConversationMessage) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[session-start-extractor] panic recovered: %v", r)
		}
	}()

	startedAt := time.Now()

	// Filter: remove system/developer messages and truncate tool results.
	// Same filtering as KnowledgeExtractor.filterMessages (improvement #11).
	var filtered []ConversationMessage
	for _, m := range entries {
		switch m.Role {
		case "system", "developer":
			continue
		case "user":
			if isMemoryExcludedUserContent(m.Content) {
				continue
			}
			filtered = append(filtered, m)
		case "tool":
			msg := m
			if len([]rune(msg.Content)) > 2000 {
				runes := []rune(msg.Content)
				msg.Content = string(runes[:500]) + "\n[...truncated...]"
			}
			filtered = append(filtered, msg)
		case "assistant":
			filtered = append(filtered, m)
		}
	}

	if len(filtered) < 4 {
		return
	}

	// Build conversation text for the LLM. Cap at ~30K chars to stay
	// within typical context windows.
	var sb strings.Builder
	const maxChars = 30000
	for _, m := range filtered {
		line := "[" + m.Role + "]: " + m.Content + "\n"
		if sb.Len()+len(line) > maxChars {
			sb.WriteString("[...conversation truncated for extraction...]\n")
			break
		}
		sb.WriteString(line)
	}
	conversationText := sb.String()
	if strings.TrimSpace(conversationText) == "" {
		return
	}

	// Call LLM to extract a structured session summary.
	// This is a simpler prompt than KnowledgeExtractor's — we want a
	// single cohesive summary rather than individual knowledge points.
	// Inspired by Codex's phase1 output: raw_memory + rollout_summary.
	summary, err := e.llm.ChatCall([]map[string]string{
		{"role": "system", "content": sessionExtractionPrompt},
		{"role": "user", "content": conversationText},
	})
	if err != nil {
		log.Printf("[session-start-extractor] LLM call failed for user %s: %v", userID, err)
		return
	}

	summary = strings.TrimSpace(summary)
	if summary == "" || len(summary) < 20 {
		return
	}

	// Redact secrets before saving.
	summary = redactSecretsInMemory(summary)

	// Save as task_artifact so it's available via proactive recall.
	entry := Entry{
		Content:    summary,
		Category:   CategoryTaskArtifact,
		Tags:       []string{"session_extraction", "auto", userID},
		Scope:      ScopeProject,
		SourceType: "session_start_extraction",
		OwnerID:    userID,
	}
	if err := e.store.Save(entry); err != nil {
		log.Printf("[session-start-extractor] save failed for user %s: %v", userID, err)
		return
	}

	elapsed := time.Since(startedAt)
	log.Printf("[session-start-extractor] extracted session memory for user %s: %d entries -> %d chars summary, took %dms",
		userID, len(entries), len(summary), elapsed.Milliseconds())
}

// sessionFingerprint creates a lightweight fingerprint from the first and
// last entries to detect whether the same session has already been processed.
func sessionFingerprint(entries []ConversationMessage) string {
	if len(entries) == 0 {
		return ""
	}
	first := entries[0].Content
	last := entries[len(entries)-1].Content
	// Use rune-safe truncation to avoid splitting multi-byte UTF-8 characters.
	if r := []rune(first); len(r) > 50 {
		first = string(r[:50])
	}
	if r := []rune(last); len(r) > 50 {
		last = string(r[:50])
	}
	return computeContentHash(first + "|" + last)
}

const sessionExtractionPrompt = `你是一个会话记忆提取助手。从以下对话历史中提取一份结构化的会话摘要，用于帮助未来的对话回忆之前的工作。

摘要必须包含:
1. **任务目标**: 用户想要完成什么
2. **关键决策**: 做了哪些重要的技术/设计决策
3. **完成状态**: 哪些工作已完成，哪些未完成
4. **重要发现**: 遇到的问题、解决方案、关键配置信息
5. **文件和路径**: 涉及的重要文件路径、项目路径

要求:
- 简洁但完整，300-800 字
- 保留具体的技术细节（文件名、命令、配置值）
- 不要包含寒暄和无关内容
- 使用 Markdown 列表格式`
