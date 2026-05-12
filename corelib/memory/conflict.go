package memory

import (
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// ConflictResult describes the outcome of a conflict check.
type ConflictResult struct {
	HasConflict   bool   `json:"has_conflict"`
	ConflictingID string `json:"conflicting_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// ConflictDetector checks whether a new memory entry contradicts existing ones.
// It uses embedding similarity to find candidates and LLM to judge contradiction.
type ConflictDetector struct {
	store    *Store
	embedder embedding.Embedder
	llm      LLMChatCaller
}

// NewConflictDetector creates a ConflictDetector.
func NewConflictDetector(store *Store, embedder embedding.Embedder, llm LLMChatCaller) *ConflictDetector {
	return &ConflictDetector{store: store, embedder: embedder, llm: llm}
}

// Check examines whether newEntry conflicts with any existing active entry.
// Returns nil result if no conflict is detected or if dependencies are unavailable.
func (d *ConflictDetector) Check(newEntry Entry) (*ConflictResult, error) {
	if d.llm == nil || !d.llm.IsConfigured() {
		return nil, nil
	}

	// Find candidates by similarity.
	candidates := d.findCandidates(newEntry)
	if len(candidates) == 0 {
		return nil, nil
	}

	// Ask LLM to judge each candidate.
	for _, candidate := range candidates {
		isConflict, reason, err := d.judgeConflict(newEntry, candidate)
		if err != nil {
			continue
		}
		if isConflict {
			return &ConflictResult{
				HasConflict:   true,
				ConflictingID: candidate.ID,
				Reason:        reason,
			}, nil
		}
	}
	return nil, nil
}

// Supersede marks the entry with the given ID as superseded.
func (d *ConflictDetector) Supersede(id string) {
	now := time.Now()
	d.store.mu.Lock()
	defer d.store.mu.Unlock()
	if d.store.supersedeEntryLocked(id, now) {
		d.store.signalSave()
	}
}

func (d *ConflictDetector) findCandidates(newEntry Entry) []Entry {
	// Try embedding-based search first.
	if d.embedder != nil && !embedding.IsNoop(d.embedder) && len(newEntry.Embedding) > 0 {
		scores := d.store.vecIndex.score(newEntry.Embedding)
		d.store.mu.RLock()
		defer d.store.mu.RUnlock()
		var candidates []Entry
		for _, e := range d.store.entries {
			if !e.IsActive() || e.Category.IsProtected() {
				continue
			}
			if sim, ok := scores[e.ID]; ok && sim > 0.8 {
				candidates = append(candidates, e)
				if len(candidates) >= 5 {
					break
				}
			}
		}
		return candidates
	}

	// Fallback: BM25.
	bm25Scores := d.store.bm25.score(newEntry.Content)
	d.store.mu.RLock()
	defer d.store.mu.RUnlock()
	var candidates []Entry
	for _, e := range d.store.entries {
		if !e.IsActive() || e.Category.IsProtected() {
			continue
		}
		if score, ok := bm25Scores[e.ID]; ok && score > 3.0 {
			candidates = append(candidates, e)
			if len(candidates) >= 5 {
				break
			}
		}
	}
	return candidates
}

func (d *ConflictDetector) judgeConflict(newEntry, existing Entry) (bool, string, error) {
	prompt := fmt.Sprintf(`Determine if these two memory entries contradict each other.

New memory: %s
Existing memory: %s

Reply with ONLY a JSON object: {"conflict": true/false, "reason": "brief explanation"}
If they express the same fact differently (not contradicting), answer false.
Only answer true if they contain genuinely incompatible information.`,
		truncStr(newEntry.Content, 500), truncStr(existing.Content, 500))

	messages := []map[string]string{
		{"role": "user", "content": prompt},
	}

	resp, err := d.llm.ChatCall(messages)
	if err != nil {
		return false, "", err
	}

	var result struct {
		Conflict bool   `json:"conflict"`
		Reason   string `json:"reason"`
	}
	if err := extractJSONFromLLMResponse(resp, &result); err != nil {
		return false, "", fmt.Errorf("parse conflict response: %w", err)
	}
	return result.Conflict, result.Reason, nil
}
