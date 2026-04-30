package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Semantic dedup: embedding candidate recall + LLM precise judgment
//
// Architecture:
//   Stage 1 (write-time, <5ms): embedding cosine similarity → candidate recall
//     High recall, some false positives. Does NOT merge — only marks candidates.
//   Stage 2 (async, ~1-3s per pair): LLM judgment → precise dedup + merge
//     High precision, no false positives. Merges or keeps based on LLM decision.
//
// Why two stages:
//   - Embedding alone can't distinguish "same fact, different wording" from
//     "related but different facts" (e.g. "PostgreSQL 性能优化" vs "PostgreSQL 备份恢复"
//     have cosine ~0.82 but are different knowledge points).
//   - LLM alone is too slow for write-time dedup (~1-3s per call).
//   - Combining them: embedding filters 500 entries to 0-3 candidates in <5ms,
//     then LLM judges only those 0-3 candidates asynchronously.
// ---------------------------------------------------------------------------

// semanticDedupCosineThreshold is the minimum cosine similarity for an
// existing entry to be considered a dedup candidate. Vectors are L2-normalized,
// so cosine = dot product.
//
// 0.85 is conservative: true paraphrased duplicates typically score 0.88-0.97,
// while related-but-different entries score 0.70-0.84.
const semanticDedupCosineThreshold = 0.85

// maxPendingDedupPairs caps the pending queue to prevent unbounded growth
// when LLM is unavailable. Oldest pairs are dropped when the cap is reached.
// Pipeline's mergeSemanticDuplicates serves as the fallback for dropped pairs.
const maxPendingDedupPairs = 100

// semanticDedupExcludedCategories lists categories excluded from semantic dedup.
// These represent temporal events where content overlap is expected.
var semanticDedupExcludedCategories = map[Category]bool{
	CategoryConversationSummary: true,
	CategorySessionCheckpoint:   true,
}

// pendingDedupPair records a (new entry, candidate entry) pair that needs
// LLM judgment to determine if they are true duplicates.
type pendingDedupPair struct {
	NewEntryID       string    `json:"new_entry_id"`
	CandidateEntryID string    `json:"candidate_entry_id"`
	CosineSimilarity float64   `json:"cosine_similarity"`
	CreatedAt        time.Time `json:"created_at"`
}

// semanticDupCandidate holds a candidate entry and its cosine similarity score.
type semanticDupCandidate struct {
	EntryID          string
	CosineSimilarity float64
}

// findSemanticDupCandidate queries the vector index for the best same-category
// entry with cosine similarity above threshold. Returns nil if no candidate.
// Caller MUST hold s.mu (read or write lock).
func (s *Store) findSemanticDupCandidate(queryEmb []float32, category Category, ownerID string) *semanticDupCandidate {
	// Skip excluded categories.
	if semanticDedupExcludedCategories[category] {
		return nil
	}
	if category.IsProtected() {
		return nil
	}

	canonicalCat := MapToCanonical(category)

	// Query vector index for cosine similarities.
	// vecIndex has its own RWMutex — safe to call while holding s.mu.
	scores := s.vecIndex.score(queryEmb)
	if len(scores) == 0 {
		return nil
	}

	// Find the best same-category candidate above threshold.
	var best *semanticDupCandidate

	for i := range s.entries {
		e := &s.entries[i]

		// Same canonical category.
		if MapToCanonical(e.Category) != canonicalCat {
			continue
		}
		// Multi-tenant isolation.
		if ownerID != "" && e.OwnerID != "" && e.OwnerID != ownerID {
			continue
		}
		// Skip pinned.
		if e.Pinned {
			continue
		}

		sim, ok := scores[e.ID]
		if !ok || sim < semanticDedupCosineThreshold {
			continue
		}
		if best == nil || sim > best.CosineSimilarity {
			best = &semanticDupCandidate{
				EntryID:          e.ID,
				CosineSimilarity: sim,
			}
		}
	}

	return best
}

// enqueuePendingDedup adds a dedup candidate pair to the pending queue.
// Caller MUST hold s.mu.Lock.
func (s *Store) enqueuePendingDedup(newEntryID string, candidate semanticDupCandidate) {
	pair := pendingDedupPair{
		NewEntryID:       newEntryID,
		CandidateEntryID: candidate.EntryID,
		CosineSimilarity: candidate.CosineSimilarity,
		CreatedAt:        time.Now(),
	}

	// Cap the queue: drop oldest pairs when full.
	if len(s.pendingDedup) >= maxPendingDedupPairs {
		s.pendingDedup = s.pendingDedup[1:] // drop oldest
	}

	s.pendingDedup = append(s.pendingDedup, pair)
	log.Printf("[semantic_dedup] enqueued candidate: new=%s candidate=%s cosine=%.4f",
		newEntryID, candidate.EntryID, candidate.CosineSimilarity)
}

// SetLLMDedup sets the LLM caller used for async precise dedup.
// Must be called before ProcessPendingDedup can do anything useful.
func (s *Store) SetLLMDedup(llm LLMChatCaller) {
	s.mu.Lock()
	s.llmDedup = llm
	s.mu.Unlock()
}

// PendingDedupCount returns the number of pending dedup pairs.
func (s *Store) PendingDedupCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pendingDedup)
}

// ProcessPendingDedup processes all pending dedup pairs using LLM judgment.
// For each pair, the LLM decides: merge (true duplicate) or keep (different).
// Returns the number of entries merged (removed).
//
// This should be called:
//   - After each agent loop iteration (piggyback on existing work)
//   - In the Pipeline's RunOnce (alongside compress/promote/reflect)
func (s *Store) ProcessPendingDedup(ctx context.Context) int {
	s.mu.Lock()
	llm := s.llmDedup
	pending := s.pendingDedup
	s.pendingDedup = nil // drain the queue
	s.mu.Unlock()

	if llm == nil || !llm.IsConfigured() || len(pending) == 0 {
		// Put back if we can't process.
		if len(pending) > 0 {
			s.mu.Lock()
			s.pendingDedup = append(s.pendingDedup, pending...)
			s.mu.Unlock()
		}
		return 0
	}

	merged := 0
	for idx, pair := range pending {
		select {
		case <-ctx.Done():
			// Put remaining UNPROCESSED pairs back (idx is the current one,
			// which was not yet processed when ctx was cancelled).
			s.mu.Lock()
			s.pendingDedup = append(s.pendingDedup, pending[idx:]...)
			s.mu.Unlock()
			return merged
		default:
		}

		// Look up both entries (they may have been deleted/merged since enqueue).
		s.mu.RLock()
		var newEntry, candEntry *Entry
		for i := range s.entries {
			if s.entries[i].ID == pair.NewEntryID {
				e := s.entries[i] // value copy — safe after lock release
				newEntry = &e
			}
			if s.entries[i].ID == pair.CandidateEntryID {
				e := s.entries[i]
				candEntry = &e
			}
		}
		s.mu.RUnlock()

		if newEntry == nil || candEntry == nil {
			continue // one or both entries no longer exist
		}

		// Ask LLM to judge (outside any lock — this is the slow part).
		decision, mergedText, err := llmJudgeDedup(ctx, llm, *newEntry, *candEntry)
		if err != nil {
			log.Printf("[semantic_dedup] LLM judge error: %v", err)
			continue
		}

		switch decision {
		case dedupDecisionMerge:
			s.mu.Lock()
			// Update the candidate (older entry) with merged content.
			for i := range s.entries {
				if s.entries[i].ID == candEntry.ID {
					if mergedText != "" {
						s.entries[i].Content = mergedText
						s.entries[i].ContentHash = computeContentHash(mergedText)
					}
					s.entries[i].Tags = mergeTags(s.entries[i].Tags, newEntry.Tags)
					s.entries[i].UpdatedAt = time.Now()
					s.entries[i].AccessCount++
					s.bm25.updateEntry(s.entries[i])
					break
				}
			}
			// Remove the new entry.
			kept := make([]Entry, 0, len(s.entries)-1)
			for _, e := range s.entries {
				if e.ID != newEntry.ID {
					kept = append(kept, e)
				}
			}
			s.entries = kept
			s.dirty = true
			s.mu.Unlock()
			s.bm25.rebuild(kept)
			if s.entityIndex != nil {
				s.entityIndex.Rebuild(kept)
			}
			s.signalSave()
			merged++
			log.Printf("[semantic_dedup] merged: %q into %q",
				truncStr(newEntry.Content, 40), truncStr(candEntry.Content, 40))

		case dedupDecisionKeep:
			// No action needed — both entries stay.
		}
	}

	return merged
}

// dedupDecision represents the LLM's judgment on a candidate pair.
type dedupDecision string

const (
	dedupDecisionMerge dedupDecision = "merge"
	dedupDecisionKeep  dedupDecision = "keep"
)

// llmDedupResponse is the expected JSON response from the LLM.
type llmDedupResponse struct {
	Decision string `json:"decision"` // "merge" or "keep"
	Merged   string `json:"merged"`   // merged text (only when decision=merge)
	Reason   string `json:"reason"`   // brief explanation
}

// llmJudgeDedup asks the LLM to judge whether two entries are true duplicates.
// This is a pure function — no Store state access, safe to call without locks.
func llmJudgeDedup(ctx context.Context, llm LLMChatCaller, a, b Entry) (dedupDecision, string, error) {
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	default:
	}

	systemPrompt := `You are a memory deduplication judge. You will receive two memory entries that have high semantic similarity.

Your job: determine if they express the SAME fact/knowledge/preference (→ merge) or DIFFERENT facts that happen to be related (→ keep both).

MERGE criteria (ALL must be true):
- Both entries describe the same specific fact, preference, or instruction
- The core information is identical, only the wording differs
- Merging would not lose any distinct information

KEEP criteria (ANY is sufficient):
- The entries describe different aspects of the same topic
- One entry contains specific details not present in the other (and vice versa)
- They are about different entities, tools, or configurations

Reply with a JSON object:
  {"decision": "merge" or "keep", "merged": "<merged text if merge>", "reason": "<brief explanation>"}

When merging, the merged text must:
- Preserve ALL specific facts, names, numbers, paths from BOTH entries
- Be concise — no redundancy
- Use the more precise/detailed phrasing when entries differ

Return ONLY the JSON object, no markdown, no commentary.`

	userPrompt := fmt.Sprintf("[A] (%s) %s\n[B] (%s) %s",
		a.Category, a.Content, b.Category, b.Content)

	resp, err := llm.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return "", "", fmt.Errorf("llm call: %w", err)
	}

	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	var result llmDedupResponse
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return "", "", fmt.Errorf("parse response: %w", err)
	}

	switch strings.ToLower(result.Decision) {
	case "merge":
		return dedupDecisionMerge, result.Merged, nil
	case "keep":
		return dedupDecisionKeep, "", nil
	default:
		return dedupDecisionKeep, "", nil // default to keep (safe)
	}
}
