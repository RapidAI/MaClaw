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
	"errors"
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

	mu           sync.Mutex
	cooldown     time.Duration
	lastExtract  time.Time
	lastSuccess  time.Time // set only when at least one fact was applied (ADD/UPDATE/DELETE)
	lastActivity time.Time // set whenever extraction runs (even if all NOOP)
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

// HasRecentSuccess returns true if the OnlineExtractor has successfully
// applied at least one fact (ADD/UPDATE/DELETE) within the given time window.
// This is narrower than HasRecentActivity: it only returns true when the
// pipeline actually mutated the memory store. Useful for UI indicators
// ("memory is actively learning") or metrics.
func (oe *OnlineExtractor) HasRecentSuccess(window time.Duration) bool {
	if oe == nil {
		return false
	}
	oe.mu.Lock()
	defer oe.mu.Unlock()
	return !oe.lastSuccess.IsZero() && time.Since(oe.lastSuccess) < window
}

// HasRecentActivity returns true if the OnlineExtractor has run extraction
// (regardless of outcome) within the given time window. This is broader than
// HasRecentSuccess: it includes runs where all facts were NOOP (already known).
// Used by KnowledgeExtractor to avoid redundant extraction even when the online
// pipeline is actively running but finding no new information to save.
func (oe *OnlineExtractor) HasRecentActivity(window time.Duration) bool {
	if oe == nil {
		return false
	}
	oe.mu.Lock()
	defer oe.mu.Unlock()
	return !oe.lastActivity.IsZero() && time.Since(oe.lastActivity) < window
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

	// Mark activity: extraction ran and produced facts (even if all NOOP).
	// This prevents KnowledgeExtractor from running redundantly.
	oe.mu.Lock()
	oe.lastActivity = time.Now()
	oe.mu.Unlock()

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
		oe.mu.Lock()
		oe.lastSuccess = time.Now()
		oe.mu.Unlock()
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

	systemPrompt := `You are a memory extraction assistant. Extract salient facts from the CURRENT MESSAGES that are worth remembering long-term. GROUP related facts about the same entity/topic into a single entry.

For each fact, provide:
1. "content": A concise, self-contained statement (2-4 sentences). Group ALL related information about the same entity together.
2. "category": One of "user_fact", "project_knowledge", "preference", "instruction"
3. "entities": Entity-relation triples in the format ["entity:Name", "relation:relationship", "entity:Name2"]. Extract key entities (people, places, tools, projects) and their relationships. Use only canonical relationship names from this schema: ` + semanticRelationSchemaPrompt() + `
4. "valid_at": ISO 8601 datetime if the fact has a specific start time (use the reference timestamp to resolve relative dates like "last week", "yesterday"). Leave empty if not applicable.
5. "invalid_at": ISO 8601 datetime if the fact has a known end time. Leave empty if still true or unknown.

Rules:
- GROUP all facts about the same server/tool/project/person into ONE entry (e.g. hostname + port + services + credentials = ONE entry)
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

	resp, err := chatCallWithContext(ctx, llmCaller, []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return nil, fmt.Errorf("llm extract call: %w", err)
	}

	var facts []ExtractedFact
	if err := extractJSONFromLLMResponse(resp, &facts); err != nil {
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
		if op, err := oe.saveGovernedExtractedEntry(entry); err != nil {
			return "", fmt.Errorf("save new entry: %w", err)
		} else {
			return op, nil
		}
	}

	// Similar memories exist -> LLM classifies operation.
	classified, err := oe.classifyOperation(ctx, llmCaller, content, similar)
	if err != nil {
		// Classify failure must not invent a new memory. ADD here is the
		// warehouse-corruption loop: a restated snippet becomes the next
		// task's "user fact".
		return OpNoop, nil
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
		if op, err := oe.saveGovernedExtractedEntry(entry); err != nil {
			return "", fmt.Errorf("save new entry: %w", err)
		} else {
			return op, nil
		}

	case OpUpdate:
		if classified.TargetID != "" && classified.MergedText != "" {
			targets := oe.store.SearchDirectByID(classified.TargetID)
			if len(targets) == 0 {
				entry := Entry{
					Content:  content,
					Category: cat,
					Tags:     tags,
					Entities: fact.ParsedEntities(),
					ValidAt:  validAt,
					OwnerID:  ownerID,
				}
				op, _ := oe.saveGovernedExtractedEntry(entry)
				return op, nil
			}
			updated := targets[0]
			updated.Content = classified.MergedText
			updated.Tags = mergeTags(updated.Tags, tags)
			updated.Entities = mergeStringSlice(updated.Entities, fact.ParsedEntities())
			if validAt != nil {
				updated.ValidAt = validAt
			}
			if invalidAt != nil {
				updated.InvalidAt = invalidAt
			}
			if err := oe.store.UpdateEntriesByID([]Entry{updated}); err != nil {
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
				op, _ := oe.saveGovernedExtractedEntry(entry)
				return op, nil
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
		op, _ := oe.saveGovernedExtractedEntry(entry)
		return op, nil

	case OpDelete:
		if classified.TargetID != "" {
			// Invalidate the contradicted entry using temporal invalidation
			// (Graphiti-style: set InvalidAt + mark superseded) instead of
			// hard-deleting. This preserves history for temporal reasoning.
			if targets := oe.store.SearchDirectByID(classified.TargetID); len(targets) > 0 {
				updated := targets[0]
				changed := false
				if updated.Status != StatusSuperseded {
					updated.Status = StatusSuperseded
					changed = true
				}
				if !updated.Stale {
					updated.Stale = true
					changed = true
				}
				if updated.InvalidAt == nil {
					invalid := time.Now()
					if !updated.CreatedAt.IsZero() && !invalid.After(updated.CreatedAt) {
						invalid = updated.CreatedAt.Add(time.Nanosecond)
					}
					updated.InvalidAt = &invalid
					changed = true
				}
				if changed {
					if err := oe.store.UpdateEntriesByID([]Entry{updated}); err != nil {
						return "", fmt.Errorf("supersede target: %w", err)
					}
				}
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
			_, _ = oe.saveGovernedExtractedEntry(entry)
		}
		return OpDelete, nil

	case OpNoop:
		return OpNoop, nil

	default:
		return OpNoop, nil
	}
}

func (oe *OnlineExtractor) saveGovernedExtractedEntry(entry Entry) (MemoryOperation, error) {
	decision, err := oe.store.SaveGovernedWithContext(entry, "")
	if err != nil {
		if errors.Is(err, ErrMemoryCandidateRejected) {
			log.Printf("[online_extractor] rejected memory candidate score=%d reasons=%v", decision.Score, decision.Reasons)
			return OpNoop, nil
		}
		return "", err
	}
	if decision.Action == MemoryGovernanceQuarantine {
		log.Printf("[online_extractor] quarantined memory candidate score=%d reasons=%v", decision.Score, decision.Reasons)
		return OpNoop, nil
	}
	return OpAdd, nil
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
- "add": The new fact contains genuinely new information not present in any existing memory, about a DIFFERENT topic/entity. Create a new entry.
- "update": The new fact is about the SAME topic/entity as an existing memory. Merge all information together. Provide the target_id and merged_text.
- "delete": The new fact contradicts an existing memory (e.g., user moved to a new city, changed job). Provide the target_id of the contradicted memory.
- "noop": The new fact is already fully captured by an existing memory. No action needed.

Rules:
- For "update": merged_text must preserve ALL specific facts from BOTH the new fact and the existing memory. Be concise but complete.
- For "delete": only use when there is a genuine contradiction (not just additional information).
- PREFER "update" over "add" when the same entity/topic appears in both (same server, same project, same tool, same person). Consolidating related information into one entry is better than fragmenting it across multiple entries.
- Use "add" only when the new fact is about a genuinely DIFFERENT entity/topic from all existing memories.
- When in doubt between "add" and "noop", prefer "noop" if the information is already substantially covered.

Reply with ONLY a JSON object:
{"operation": "add|update|delete|noop", "target_id": "<entry ID or empty>", "merged_text": "<merged content or empty>", "reason": "<brief explanation>"}`

	userPrompt := fmt.Sprintf("NEW FACT:\n%s\n\nEXISTING MEMORIES:\n%s", newFact, existingBuf.String())

	resp, err := chatCallWithContext(ctx, llmCaller, []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return nil, fmt.Errorf("llm classify call: %w", err)
	}

	var result ClassifiedOperation
	if err := extractJSONFromLLMResponse(resp, &result); err != nil {
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
	// BM25-only threshold is set conservatively low (1.0) to ensure same-topic
	// entries are found for UPDATE classification. The LLM classifier will
	// correctly distinguish "same topic, augment" (UPDATE) from "different topic"
	// (ADD). A high threshold (2.0) causes same-topic entries to be missed,
	// leading to fragmented duplicate entries instead of merged ones.
	hasEmbedder := len(vecScores) > 0
	threshold := 1.0 // BM25-only: moderate keyword overlap sufficient for candidate retrieval
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
			if extractionLooksLikeWarehouseRestatement(m) {
				continue
			}
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func extractionLooksLikeWarehouseRestatement(m ConversationMessage) bool {
	if m.Role != "assistant" && m.Role != "tool" {
		return false
	}
	for _, marker := range []string{
		"[知识库]",
		"[企业知识]",
		"根据知识库中的资料",
		"根据记忆中的记录",
		"知识库参考（自动检索）",
		"企业知识库参考（自动检索）",
	} {
		if strings.Contains(m.Content, marker) {
			return true
		}
	}
	return false
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
