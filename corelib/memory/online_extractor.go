package memory

// online_extractor.go implements the Mem0-style online incremental extraction
// pipeline. Instead of waiting for session expiry (1h cooldown), this pipeline
// triggers after each agent loop iteration, extracting facts from the latest
// conversation turn and integrating them via four-operation classification
// (ADD/UPDATE/DELETE/NOOP).
//
// Architecture (inspired by Mem0 paper, Section 2.1):
//   1. Extraction phase: LLM extracts salient facts from the latest messages,
//      with conversation summary + recent messages as context.
//   2. Update phase: For each extracted fact, retrieve top-5 similar memories,
//      then LLM classifies the operation (ADD/UPDATE/DELETE/NOOP).
//   3. Execute: Apply the classified operation to the store.
//
// Temporal extraction (inspired by Graphiti, Section 2.2.3):
//   The extraction prompt also asks for temporal information (valid_at/invalid_at)
//   when facts have time-related context.
//
// Entity-relation extraction (inspired by Mem0^g, Section 2.2):
//   The extraction prompt also asks for entity-relation triples to enable
//   multi-hop reasoning via tag-based recall.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// OnlineExtractor performs real-time incremental memory extraction from
// conversation turns. It replaces the passive 1-hour cooldown extraction
// with an active, per-turn pipeline.
type OnlineExtractor struct {
	store *Store
	llm   LLMChatCaller

	mu          sync.Mutex
	cooldown    time.Duration
	lastExtract time.Time
}

// NewOnlineExtractor creates an OnlineExtractor with a 3-minute cooldown.
// The cooldown prevents excessive LLM calls during rapid-fire conversations.
func NewOnlineExtractor(store *Store, llm LLMChatCaller) *OnlineExtractor {
	return &OnlineExtractor{
		store:    store,
		llm:      llm,
		cooldown: 3 * time.Minute,
	}
}

// SetCooldown overrides the default cooldown for testing.
func (oe *OnlineExtractor) SetCooldown(d time.Duration) {
	oe.mu.Lock()
	oe.cooldown = d
	oe.mu.Unlock()
}

// SetLLM sets or replaces the LLM caller. This is called when the LLM
// config becomes available after initial construction (the OnlineExtractor
// is created with nil LLM in ensureMemoryStore, then wired later in
// activateEmbedderAsync).
func (oe *OnlineExtractor) SetLLM(llm LLMChatCaller) {
	oe.mu.Lock()
	oe.llm = llm
	oe.mu.Unlock()
}

// ExtractAndIntegrate is the main entry point. It extracts facts from the
// latest conversation messages and integrates them into the memory store
// using four-operation classification.
//
// Parameters:
//   - ctx: cancellation context
//   - recentMessages: the last N messages from the conversation (typically 10)
//   - conversationSummary: a brief summary of the full conversation so far
//   - referenceTime: the timestamp of the current message (for temporal extraction)
//   - ownerID: user ID for multi-tenant isolation (empty for single-user)
//
// This method is designed to be called asynchronously (in a goroutine) after
// each agent loop iteration. It is safe for concurrent use.
func (oe *OnlineExtractor) ExtractAndIntegrate(
	ctx context.Context,
	recentMessages []ConversationMessage,
	conversationSummary string,
	referenceTime time.Time,
	ownerID string,
) *OnlineExtractionResult {
	result := &OnlineExtractionResult{}

	// Read LLM and check cooldown in a single lock acquisition.
	oe.mu.Lock()
	llmCaller := oe.llm
	if llmCaller == nil || !llmCaller.IsConfigured() {
		oe.mu.Unlock()
		return result
	}
	if !oe.lastExtract.IsZero() && time.Since(oe.lastExtract) < oe.cooldown {
		oe.mu.Unlock()
		return result
	}
	oe.lastExtract = time.Now()
	oe.mu.Unlock()

	// Mutual exclusion: skip if the agent already wrote memories in this turn.
	if HasRecentMemoryWrites(recentMessages) {
		return result
	}

	// Filter messages: keep only user/assistant/tool, exclude system.
	filtered := filterMessagesForExtraction(recentMessages)
	if len(filtered) < 2 {
		return result
	}

	// Phase 1: Extract facts with temporal and entity annotations.
	facts, err := oe.extractFacts(ctx, llmCaller, filtered, conversationSummary, referenceTime)
	if err != nil {
		log.Printf("[online_extractor] extraction failed: %v", err)
		result.Errors++
		return result
	}
	result.ExtractedFacts = len(facts)

	if len(facts) == 0 {
		return result
	}

	// Phase 2: For each fact, classify operation and execute.
	for _, fact := range facts {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		content := strings.TrimSpace(fact.Content)
		if content == "" {
			continue
		}

		op, err := oe.classifyAndApply(ctx, llmCaller, fact, ownerID)
		if err != nil {
			log.Printf("[online_extractor] classify/apply failed: %v", err)
			result.Errors++
			continue
		}

		switch op {
		case OpAdd:
			result.Added++
		case OpUpdate:
			result.Updated++
		case OpDelete:
			result.Deleted++
		case OpNoop:
			result.Noops++
		}
	}

	if result.Added > 0 || result.Updated > 0 || result.Deleted > 0 {
		log.Printf("[online_extractor] completed: extracted=%d added=%d updated=%d deleted=%d noop=%d",
			result.ExtractedFacts, result.Added, result.Updated, result.Deleted, result.Noops)
	}

	return result
}

// extractFacts calls the LLM to extract structured facts from conversation.
// The prompt includes temporal extraction (Graphiti-style) and entity-relation
// extraction (Mem0^g-style).
func (oe *OnlineExtractor) extractFacts(
	ctx context.Context,
	llmCaller LLMChatCaller,
	messages []ConversationMessage,
	summary string,
	refTime time.Time,
) ([]ExtractedFact, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Build conversation text from recent messages.
	var msgText strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&msgText, "[%s]: %s\n", m.Role, m.Content)
	}

	refTimeStr := refTime.Format(time.RFC3339)

	systemPrompt := `You are a memory extraction assistant. Extract salient facts from the CURRENT MESSAGES that are worth remembering long-term.

For each fact, provide:
1. "content": A concise, self-contained statement (1-2 sentences)
2. "category": One of "user_fact", "project_knowledge", "preference", "instruction"
3. "entities": Entity-relation triples in the format ["entity:Name", "relation:relationship", "entity:Name2"]. Extract key entities (people, places, tools, projects) and their relationships. Use only canonical relationship names from this schema: ` + semanticRelationSchemaPrompt() + `
4. "valid_at": ISO 8601 datetime if the fact has a specific start time (use the reference timestamp to resolve relative dates like "last week", "yesterday"). Leave empty if not applicable.
5. "invalid_at": ISO 8601 datetime if the fact has a known end time. Leave empty if still true or unknown.

Rules:
- Extract ONLY non-trivial information worth remembering across sessions
- DO NOT extract greetings, filler, or meta-conversation
- DO NOT extract information that is purely about the current task execution (tool calls, file edits)
- DO extract: user preferences, decisions, facts about the user/project, configuration details, important conclusions
- For temporal extraction: use the reference timestamp to resolve relative dates
- If no facts worth extracting, return an empty array: []
- Return ONLY a JSON array, no markdown, no commentary

CRITICAL category rules:
- "user_fact" = ONLY personal information about the user (name, family members, location, job title, personal habits, relationships between people)
- "project_knowledge" = technical environment (software versions, server configs, Docker settings, project architecture, API endpoints, deployment configs, tool configurations, file paths, commands)
- "preference" = user's tool/language/style preferences
- "instruction" = how the user wants things done (coding style, workflow rules)
- NEVER mix personal info and technical info in a single fact. If a conversation mentions both family members and Docker configs, extract them as SEPARATE facts with DIFFERENT categories.

Reference timestamp: ` + refTimeStr

	userPrompt := ""
	if summary != "" {
		userPrompt = "CONVERSATION SUMMARY:\n" + summary + "\n\nCURRENT MESSAGES:\n" + msgText.String()
	} else {
		userPrompt = "CURRENT MESSAGES:\n" + msgText.String()
	}

	resp, err := llmCaller.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return nil, fmt.Errorf("llm extract call: %w", err)
	}

	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	var facts []ExtractedFact
	if err := json.Unmarshal([]byte(body), &facts); err != nil {
		return nil, fmt.Errorf("parse extract response: %w", err)
	}
	return facts, nil
}

// classifyAndApply determines the correct operation for a fact and executes it.
// Steps:
//  1. Find top-5 similar existing memories (vector + BM25)
//  2. If no similar memories -> ADD
//  3. If similar memories exist -> LLM classifies ADD/UPDATE/DELETE/NOOP
//  4. Execute the operation
//
// All mutations go through Store's public API (Save/Update/Delete) to ensure
// all indices (BM25, vector, graph, entity, project) are updated consistently.
func (oe *OnlineExtractor) classifyAndApply(
	ctx context.Context,
	llmCaller LLMChatCaller,
	fact ExtractedFact,
	ownerID string,
) (MemoryOperation, error) {
	content := strings.TrimSpace(fact.Content)

	// Build the entry for similarity search.
	cat := categoryFromString(fact.Category)
	tags := buildFactTags(fact)

	// Parse temporal fields.
	var validAt, invalidAt *time.Time
	if fact.ValidAt != "" {
		if t, err := time.Parse(time.RFC3339, fact.ValidAt); err == nil {
			validAt = &t
		}
	}
	if fact.InvalidAt != "" {
		if t, err := time.Parse(time.RFC3339, fact.InvalidAt); err == nil {
			invalidAt = &t
		}
	}

	// Find similar existing memories.
	similar := oe.findSimilarMemories(content, cat, ownerID, 5)

	// No similar memories -> direct ADD.
	if len(similar) == 0 {
		entry := Entry{
			Content:   content,
			Category:  cat,
			Tags:      tags,
			Entities:  fact.ParsedEntities(),
			ValidAt:   validAt,
			InvalidAt: invalidAt,
			OwnerID:   ownerID,
		}
		if err := oe.store.Save(entry); err != nil {
			return "", fmt.Errorf("save new entry: %w", err)
		}
		return OpAdd, nil
	}

	// Similar memories exist -> LLM classifies operation.
	classified, err := oe.classifyOperation(ctx, llmCaller, content, similar)
	if err != nil {
		// On LLM failure, default to ADD (safe: may create a near-duplicate,
		// but the async semantic dedup will clean it up later).
		entry := Entry{
			Content:   content,
			Category:  cat,
			Tags:      tags,
			Entities:  fact.ParsedEntities(),
			ValidAt:   validAt,
			InvalidAt: invalidAt,
			OwnerID:   ownerID,
		}
		_ = oe.store.Save(entry)
		return OpAdd, nil
	}

	// Execute the classified operation.
	switch classified.Operation {
	case OpAdd:
		entry := Entry{
			Content:   content,
			Category:  cat,
			Tags:      tags,
			Entities:  fact.ParsedEntities(),
			ValidAt:   validAt,
			InvalidAt: invalidAt,
			OwnerID:   ownerID,
		}
		if err := oe.store.Save(entry); err != nil {
			return "", fmt.Errorf("save new entry: %w", err)
		}
		return OpAdd, nil

	case OpUpdate:
		if classified.TargetID != "" && classified.MergedText != "" {
			// Use Store.Update to ensure all indices are updated consistently.
			// Merge tags from the new fact into the existing entry's tags.
			// Preserve the target entry's original category — UPDATE means
			// "augment existing memory", not "reclassify it".
			mergedTags := tags
			targetCat := cat // fallback to new fact's category if target not found
			oe.store.mu.RLock()
			for i := range oe.store.entries {
				if oe.store.entries[i].ID == classified.TargetID {
					mergedTags = mergeTags(oe.store.entries[i].Tags, tags)
					targetCat = oe.store.entries[i].Category
					break
				}
			}
			oe.store.mu.RUnlock()

			if err := oe.store.Update(classified.TargetID, classified.MergedText, targetCat, mergedTags); err != nil {
				log.Printf("[online_extractor] update failed for %s: %v", classified.TargetID, err)
				// Fallback to ADD.
				entry := Entry{
					Content:  content,
					Category: cat,
					Tags:     tags,
					Entities: fact.ParsedEntities(),
					ValidAt:  validAt,
					OwnerID:  ownerID,
				}
				_ = oe.store.Save(entry)
				return OpAdd, nil
			}

			// Update entities on the target entry (Store.Update doesn't handle Entities).
			if parsedEnts := fact.ParsedEntities(); len(parsedEnts) > 0 || validAt != nil {
				changed := false
				oe.store.mu.Lock()
				for i := range oe.store.entries {
					if oe.store.entries[i].ID == classified.TargetID {
						if len(parsedEnts) > 0 {
							oe.store.entries[i].Entities = mergeStringSlice(oe.store.entries[i].Entities, parsedEnts)
						}
						if validAt != nil {
							oe.store.entries[i].ValidAt = validAt
						}
						oe.store.rebuildDerivedIndexesLocked(false)
						oe.store.dirty = true
						changed = true
						break
					}
				}
				oe.store.mu.Unlock()
				if changed {
					oe.store.signalSave()
				}
			}
			return OpUpdate, nil
		}
		// Fallback: if no target or merged text, treat as ADD.
		entry := Entry{
			Content:  content,
			Category: cat,
			Tags:     tags,
			Entities: fact.ParsedEntities(),
			ValidAt:  validAt,
			OwnerID:  ownerID,
		}
		_ = oe.store.Save(entry)
		return OpAdd, nil

	case OpDelete:
		if classified.TargetID != "" {
			// Invalidate the contradicted entry using temporal invalidation
			// (Graphiti-style: set InvalidAt + mark superseded) instead of
			// hard-deleting. This preserves history for temporal reasoning.
			now := time.Now()
			oe.store.mu.Lock()
			changed := oe.store.supersedeEntryLocked(classified.TargetID, now)
			oe.store.mu.Unlock()
			if changed {
				oe.store.signalSave()
			}

			// Also ADD the new fact (the contradicting information).
			entry := Entry{
				Content:   content,
				Category:  cat,
				Tags:      tags,
				Entities:  fact.ParsedEntities(),
				ValidAt:   validAt,
				InvalidAt: invalidAt,
				OwnerID:   ownerID,
			}
			_ = oe.store.Save(entry)
		}
		return OpDelete, nil

	case OpNoop:
		return OpNoop, nil

	default:
		return OpNoop, nil
	}
}

// classifyOperation asks the LLM to determine the correct operation for a
// new fact given similar existing memories.
func (oe *OnlineExtractor) classifyOperation(
	ctx context.Context,
	llmCaller LLMChatCaller,
	newFact string,
	similar []Entry,
) (*ClassifiedOperation, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Build the existing memories context.
	var existingBuf strings.Builder
	for _, e := range similar {
		fmt.Fprintf(&existingBuf, "- ID=%s [%s] %s\n", e.ID, e.Category, e.Content)
	}

	systemPrompt := `You are a memory management assistant. Given a NEW FACT extracted from conversation and a list of EXISTING MEMORIES that are semantically similar, determine the correct operation.

Operations:
- "add": The new fact contains genuinely new information not present in any existing memory. Create a new entry.
- "update": The new fact augments or refines an existing memory. Merge the information. Provide the target_id and merged_text.
- "delete": The new fact contradicts an existing memory (e.g., user moved to a new city, changed job). Provide the target_id of the contradicted memory.
- "noop": The new fact is already fully captured by an existing memory. No action needed.

Rules:
- For "update": merged_text must preserve ALL specific facts from BOTH the new fact and the existing memory. Be concise.
- For "delete": only use when there is a genuine contradiction (not just additional information).
- When in doubt between "add" and "noop", prefer "add" (safe: dedup will clean up later).
- When in doubt between "update" and "add", prefer "update" if the topic is clearly the same.

Reply with ONLY a JSON object:
{"operation": "add|update|delete|noop", "target_id": "<entry ID or empty>", "merged_text": "<merged content or empty>", "reason": "<brief explanation>"}`

	userPrompt := fmt.Sprintf("NEW FACT:\n%s\n\nEXISTING MEMORIES:\n%s", newFact, existingBuf.String())

	resp, err := llmCaller.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return nil, fmt.Errorf("llm classify call: %w", err)
	}

	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	var result ClassifiedOperation
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("parse classify response: %w", err)
	}

	// Normalize operation.
	result.Operation = MemoryOperation(strings.ToLower(string(result.Operation)))

	return &result, nil
}

// findSimilarMemories retrieves the top-k most similar active memories
// using both vector similarity and BM25.
// Category isolation: only returns entries whose canonical category matches
// the new fact's category. This prevents cross-category UPDATE merges
// (e.g. project_knowledge content being merged into a user_fact entry).
func (oe *OnlineExtractor) findSimilarMemories(content string, category Category, ownerID string, topK int) []Entry {
	// BM25 scores.
	bm25Scores := oe.store.bm25.score(content)

	// Vector scores (if embedder available).
	var vecScores map[string]float64
	oe.store.mu.RLock()
	emb := oe.store.embedder
	oe.store.mu.RUnlock()
	if emb != nil {
		vec, err := emb.Embed(content)
		if err == nil && len(vec) > 0 {
			vecScores = oe.store.vecIndex.score(vec)
		}
	}

	// Combine scores and find top-k.
	// Use a higher threshold when only BM25 is available (no embedder),
	// because BM25 scores for unrelated entries can exceed 0.5 on common
	// words like "user", "project". With embedder, vector similarity
	// provides a strong signal so a lower threshold is acceptable.
	hasEmbedder := len(vecScores) > 0
	threshold := 2.0 // BM25-only: require strong keyword overlap
	if hasEmbedder {
		threshold = 0.5 // hybrid: vector similarity provides strong signal
	}

	// Canonical category for isolation check.
	canonicalCat := MapToCanonical(category)

	type scored struct {
		entry Entry
		score float64
	}
	var candidates []scored

	oe.store.mu.RLock()
	for _, e := range oe.store.entries {
		if !e.IsActive() {
			continue
		}
		if e.Category.IsProtected() {
			continue
		}
		// Multi-tenant isolation.
		if ownerID != "" && e.OwnerID != "" && e.OwnerID != ownerID {
			continue
		}
		// Category isolation: only match entries in the same canonical category.
		// This prevents cross-category merges (e.g. a project_knowledge fact
		// being UPDATE-merged into a user_fact entry about family members).
		if category != "" && MapToCanonical(e.Category) != canonicalCat {
			continue
		}

		score := 0.0
		if s, ok := bm25Scores[e.ID]; ok {
			score += s
		}
		if s, ok := vecScores[e.ID]; ok {
			score += s * 5.0 // weight vector similarity higher
		}
		if score > threshold {
			candidates = append(candidates, scored{entry: e, score: score})
		}
	}
	oe.store.mu.RUnlock()

	// Sort by score descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Return top-k.
	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	result := make([]Entry, len(candidates))
	for i, c := range candidates {
		result[i] = c.entry
	}
	return result
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// filterMessagesForExtraction keeps user/assistant/tool messages, excludes system.
func filterMessagesForExtraction(messages []ConversationMessage) []ConversationMessage {
	var filtered []ConversationMessage
	for _, m := range messages {
		switch m.Role {
		case "system", "developer":
			continue
		case "tool":
			// Truncate long tool outputs.
			if len([]rune(m.Content)) > 1000 {
				runes := []rune(m.Content)
				m.Content = string(runes[:500]) + "\n[...truncated...]"
			}
			filtered = append(filtered, m)
		default:
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// categoryFromString converts a string category to the Category type.
func categoryFromString(s string) Category {
	switch strings.ToLower(s) {
	case "user_fact", "user":
		return CategoryUserFact
	case "preference":
		return CategoryPreference
	case "instruction", "feedback":
		return CategoryInstruction
	case "project_knowledge", "project":
		return CategoryProjectKnowledge
	default:
		return CategoryProjectKnowledge
	}
}

// buildFactTags builds tags from an extracted fact's entities and metadata.
func buildFactTags(fact ExtractedFact) []string {
	tags := []string{"online_extracted"}
	// Add entity names as tags (strip "entity:" prefix for BM25 matching).
	for _, e := range fact.ParsedEntities() {
		if name, ok := semanticEntityTokenName(e); ok {
			tags = append(tags, name)
		}
	}
	return tags
}

// mergeStringSlice merges two string slices, deduplicating entries.
func mergeStringSlice(existing, incoming []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, s := range existing {
		seen[s] = true
	}
	result := make([]string, len(existing))
	copy(result, existing)
	for _, s := range incoming {
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result
}
