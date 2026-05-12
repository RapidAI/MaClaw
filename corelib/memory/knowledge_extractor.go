package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ConversationMessage represents a single message in a conversation history.
type ConversationMessage struct {
	Role    string `json:"role"`    // "user", "assistant", "tool", "system", etc.
	Content string `json:"content"` // plain text content
}

// KnowledgeExtractor extracts knowledge points from conversation history
// after a session ends and saves them to the memory store.
type KnowledgeExtractor struct {
	store        *Store
	llm          LLMChatCaller
	cooldown     time.Duration
	lastExtract  time.Time
	mu           sync.Mutex
	consolidator *Consolidator
}

// SetConsolidator wires an optional TiMem consolidator for online L1 segment creation.
func (ke *KnowledgeExtractor) SetConsolidator(c *Consolidator) {
	ke.mu.Lock()
	defer ke.mu.Unlock()
	ke.consolidator = c
}

// HasRecentMemoryWrites checks if the conversation contains assistant messages
// that explicitly wrote to memory (e.g. "I've saved this to memory", tool calls
// to memory save). When the main agent already wrote memories, the background
// extractor should skip to avoid duplicates (inspired by Claude Code's
// extractMemories mutual exclusion pattern).
func HasRecentMemoryWrites(messages []ConversationMessage) bool {
	// Signals in assistant text indicating memory was saved.
	textSignals := []string{
		"已保存到记忆", "saved to memory", "记住了", "已记录",
	}
	// Signals in tool-role messages indicating a memory save tool was called.
	toolSignals := []string{
		"memory:save", "memory_save", "save_memory", "memory:update",
	}

	start := len(messages) - 10
	if start < 0 {
		start = 0
	}
	for i := len(messages) - 1; i >= start; i-- {
		m := messages[i]
		lower := strings.ToLower(m.Content)
		switch m.Role {
		case "assistant":
			for _, signal := range textSignals {
				if strings.Contains(lower, signal) {
					return true
				}
			}
		case "tool":
			for _, signal := range toolSignals {
				if strings.Contains(lower, signal) {
					return true
				}
			}
		}
	}
	return false
}

// NewKnowledgeExtractor creates a KnowledgeExtractor with a default 10-minute cooldown.
// The cooldown was reduced from 1 hour to 10 minutes to improve data freshness.
// The online extractor (OnlineExtractor) is the primary extraction path;
// KnowledgeExtractor serves as a fallback for when the online extractor is unavailable.
func NewKnowledgeExtractor(store *Store, llm LLMChatCaller) *KnowledgeExtractor {
	return &KnowledgeExtractor{
		store:    store,
		llm:      llm,
		cooldown: 10 * time.Minute,
	}
}

// filterMessages keeps only user and assistant messages, preserving order.
// System and developer messages are excluded because they contain framework
// instructions (system prompts, steering rules, recover prompts) that are
// not user knowledge. Tool messages with overly long output are truncated
// to reduce noise in the extraction prompt.
//
// Inspired by Codex CLI's sanitize_response_item_for_memories which filters
// out developer role messages and memory-excluded contextual fragments.
func (ke *KnowledgeExtractor) filterMessages(messages []ConversationMessage) []ConversationMessage {
	var filtered []ConversationMessage
	for _, m := range messages {
		switch m.Role {
		case "system", "developer":
			// Skip framework instructions — not user knowledge.
			continue
		case "user":
			// Skip compaction recovery prompts and system notifications
			// that were injected as user-role messages.
			if isMemoryExcludedUserContent(m.Content) {
				continue
			}
			filtered = append(filtered, m)
		case "tool":
			// Truncate overly long tool results to reduce noise.
			// The extraction LLM doesn't need full file contents or
			// command outputs — it needs the gist.
			if len([]rune(m.Content)) > 2000 {
				runes := []rune(m.Content)
				m.Content = string(runes[:500]) + "\n[...tool output truncated for memory extraction...]"
			}
			filtered = append(filtered, m)
		default:
			// Only pass through known roles (assistant). Unknown roles
			// (function, etc.) are filtered out.
			if m.Role == "assistant" {
				filtered = append(filtered, m)
			}
		}
	}
	return filtered
}

// isMemoryExcludedUserContent returns true for user-role messages that are
// actually system-injected content (compaction recovery prompts, system
// notifications, etc.) and should not be extracted as user knowledge.
// Delegates to the shared SyntheticUserMessagePrefixes list in corelib.
func isMemoryExcludedUserContent(content string) bool {
	return corelib.IsSyntheticUserContent(content)
}

// preCompress uses the LLM to compress a long conversation (>20 turns)
// into a shorter summary before knowledge extraction.
func (ke *KnowledgeExtractor) preCompress(ctx context.Context, messages []ConversationMessage) (string, error) {
	var sb strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&sb, "[%s]: %s\n", m.Role, m.Content)
	}

	systemPrompt := `You are a conversation compressor. Compress the following conversation into a concise summary that preserves ALL technical details, decisions, code snippets, file paths, commands, and key facts. Remove greetings, filler, and redundant exchanges. Output ONLY the compressed text.`

	resp, err := ke.llm.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": sb.String()},
	})
	if err != nil {
		return "", fmt.Errorf("knowledge_extractor: preCompress: %w", err)
	}
	return strings.TrimSpace(resp), nil
}

// isDuplicate checks if content already exists in the store via exact match
// or substring containment.
func (ke *KnowledgeExtractor) isDuplicate(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return true
	}

	ke.store.mu.RLock()
	defer ke.store.mu.RUnlock()

	for _, e := range ke.store.entries {
		existing := strings.ToLower(strings.TrimSpace(e.Content))
		if existing == lower {
			return true
		}
		// Substring match: if either contains the other.
		if len(lower) >= minSubstringLen && len(existing) >= minSubstringLen {
			if strings.Contains(existing, lower) || strings.Contains(lower, existing) {
				return true
			}
		}
	}
	return false
}

// Extract performs post-session knowledge extraction from conversation history.
// Steps: cooldown check → mutual exclusion → filter → preCompress (if >20 turns) → LLM extract → dedup → save.
// Returns nil on cooldown, empty conversation, or unconfigured LLM.
// Skips extraction when the main agent already wrote memories (mutual exclusion).
func (ke *KnowledgeExtractor) Extract(userID string, messages []ConversationMessage) error {
	// LLM not configured: no-op.
	if ke.llm == nil || !ke.llm.IsConfigured() {
		return nil
	}

	// Mutual exclusion: if the main agent already wrote memories in this
	// conversation, skip background extraction to avoid duplicates.
	if HasRecentMemoryWrites(messages) {
		return nil
	}

	// Cooldown check.
	ke.mu.Lock()
	if !ke.lastExtract.IsZero() && time.Since(ke.lastExtract) < ke.cooldown {
		ke.mu.Unlock()
		return nil
	}
	ke.lastExtract = time.Now()
	ke.mu.Unlock()

	// Filter messages.
	filtered := ke.filterMessages(messages)
	if len(filtered) == 0 {
		return nil
	}

	ctx := context.Background()

	// Build conversation text, optionally pre-compressed.
	var conversationText string
	if len(filtered) > 20 {
		compressed, err := ke.preCompress(ctx, filtered)
		if err != nil {
			return fmt.Errorf("knowledge_extractor: %w", err)
		}
		conversationText = compressed
	} else {
		var sb strings.Builder
		for _, m := range filtered {
			fmt.Fprintf(&sb, "[%s]: %s\n", m.Role, m.Content)
		}
		conversationText = sb.String()
	}

	if strings.TrimSpace(conversationText) == "" {
		return nil
	}

	// Online L1 segment consolidation: create TMT segment nodes per turn pair.
	ke.mu.Lock()
	cons := ke.consolidator
	ke.mu.Unlock()
	if cons != nil {
		for i := 0; i+1 < len(filtered); i += 2 {
			if filtered[i].Role == "user" && filtered[i+1].Role == "assistant" {
				_, _ = cons.ConsolidateSegment(ctx, filtered[i].Content, filtered[i+1].Content, time.Now(), userID)
			}
		}
	}

	// LLM extraction call.
	knowledgePoints, err := ke.extractKnowledge(ctx, conversationText)
	if err != nil {
		return fmt.Errorf("knowledge_extractor: %w", err)
	}

	// Dedup and save.
	for _, kp := range knowledgePoints {
		content := strings.TrimSpace(kp.Content)
		if content == "" {
			continue
		}
		if ke.isDuplicate(content) {
			continue
		}

		cat := CategoryProjectKnowledge
		if kp.Category == "instruction" {
			cat = CategoryInstruction
		}

		// Extract meaningful tags from content using ExpandQuery.
		expanded := ExpandQuery(content)
		tags := []string{"extracted", userID}
		for _, entity := range expanded.Entities {
			tags = append(tags, entity)
		}

		entry := Entry{
			Content:  content,
			Category: cat,
			Tags:     tags,
			OwnerID:  userID, // multi-tenant: associate with the user who had this conversation
		}
		if err := ke.store.Save(entry); err != nil {
			return fmt.Errorf("knowledge_extractor: save: %w", err)
		}
	}

	return nil
}

// knowledgePoint represents a single extracted knowledge item from the LLM.
type knowledgePoint struct {
	Content  string `json:"content"`
	Category string `json:"category"` // "project_knowledge" or "instruction"
}

// extractKnowledge calls the LLM to extract structured knowledge points
// from the conversation text.
func (ke *KnowledgeExtractor) extractKnowledge(ctx context.Context, conversationText string) ([]knowledgePoint, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	systemPrompt := `You are a knowledge extraction assistant. Extract non-obvious technical knowledge points from the conversation below. Focus on:
- Workarounds and undocumented behaviors
- Configuration details and environment specifics
- Architecture decisions and conventions
- Important error causes and solutions
- Key technical facts discovered during the conversation

Return a JSON array of objects, each with:
  {"content": "<concise knowledge point>", "category": "project_knowledge" or "instruction"}

Rules:
- Each knowledge point should be self-contained and concise (1-3 sentences).
- Do NOT include trivial or obvious information.
- Do NOT include greetings, pleasantries, or meta-conversation.
- If no knowledge worth extracting, return an empty array: []
- Return ONLY the JSON array, no markdown, no commentary.`

	resp, err := ke.llm.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": conversationText},
	})
	if err != nil {
		return nil, fmt.Errorf("llm extract call: %w", err)
	}

	var points []knowledgePoint
	if err := extractJSONFromLLMResponse(resp, &points); err != nil {
		return nil, fmt.Errorf("parse extract response: %w", err)
	}
	return points, nil
}
