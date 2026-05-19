package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// SynthesizeResult holds the combined outcome of a synthesis run
// (merged Promoter + Reflector functionality).
type SynthesizeResult struct {
	Promoted          int    `json:"promoted"`
	InsightsGenerated int    `json:"insights_generated"`
	Error             string `json:"error,omitempty"`
}

// Synthesizer combines the functionality of Promoter (recurring pattern
// detection) and Reflector (high-level insight extraction) into a single
// LLM call. This eliminates redundant episodic entry collection and
// reduces Pipeline LLM calls by one per cycle.
type Synthesizer struct {
	mu                   sync.RWMutex
	store                *Store
	llm                  LLMChatCaller
	experienceProtection string
	threshold            int // minimum occurrences for promotion (default 3)
	minEntries           int // minimum total entries to run (default 50)
}

// NewSynthesizer creates a Synthesizer.
func NewSynthesizer(store *Store, llm LLMChatCaller) *Synthesizer {
	return &Synthesizer{
		store:      store,
		llm:        llm,
		threshold:  3,
		minEntries: 50,
	}
}

// SetMinEntries overrides the minimum total entry count for testing.
func (s *Synthesizer) SetMinEntries(n int) {
	s.mu.Lock()
	s.minEntries = n
	s.mu.Unlock()
}

// SetLLM rewires the LLM used by synthesis.
func (s *Synthesizer) SetLLM(llm LLMChatCaller) {
	s.mu.Lock()
	s.llm = llm
	s.mu.Unlock()
}

// SetExperienceProtectionSamples installs read-only retention anchors.
func (s *Synthesizer) SetExperienceProtectionSamples(samples []ProtectedExperienceCandidate) {
	s.mu.Lock()
	s.experienceProtection = FormatExperienceProtectionPrompt(samples)
	s.mu.Unlock()
}

// Synthesize runs one combined promotion + reflection cycle. It skips if:
//   - fewer than minEntries total entries
//   - LLM is not configured
//
// Note: no internal cooldown — the Pipeline's 6h cycle is the rate limiter.
func (s *Synthesizer) Synthesize(ctx context.Context) (*SynthesizeResult, error) {
	s.mu.RLock()
	llm := s.llm
	experienceProtection := s.experienceProtection
	minEntries := s.minEntries
	s.mu.RUnlock()
	if llm == nil || !llm.IsConfigured() {
		return &SynthesizeResult{}, nil
	}

	// Check total entry count and collect episodic entries in one lock.
	s.store.mu.RLock()
	total := len(s.store.entries)
	var episodic []Entry
	if total >= minEntries {
		for _, e := range s.store.entries {
			if e.Category.Tier() == TierEpisodic && e.IsActive() {
				episodic = append(episodic, e)
			}
		}
	}
	s.store.mu.RUnlock()
	if total < minEntries {
		return &SynthesizeResult{}, nil
	}

	if len(episodic) < s.threshold*2 {
		return &SynthesizeResult{}, nil
	}

	// Take the most recent 50 episodic entries.
	if len(episodic) > 50 {
		episodic = episodic[len(episodic)-50:]
	}

	var sb strings.Builder
	for i, e := range episodic {
		sb.WriteString(formatExperiencePromptEntry(i, e, 250))
	}

	systemPrompt := fmt.Sprintf(`You are a memory synthesis assistant. Analyze the following episodic memories and perform TWO tasks:

## Task 1: Recurring Pattern Promotion
Identify facts, preferences, or patterns that appear in %d or more separate entries.
For each, output: {"source": "recurring", "content": "concise fact/preference", "category": "preference|instruction|user_fact", "evidence_count": N, "evidence_ids": ["entry-id", "..."]}

## Task 2: High-Level Insight Extraction
Extract user preferences, decision patterns, recurring habits, and important facts.
For each, output: {"source": "insight", "content": "concise insight text", "category": "preference|instruction|user_fact", "evidence_count": N, "evidence_ids": ["entry-id", "..."]}

Return a single JSON array combining both tasks:
[{"source": "recurring|insight", "content": "...", "category": "...", "evidence_count": N, "evidence_ids": ["..."]}]

Rules:
- Each item must be a single, actionable statement about ONE topic
- For "recurring" items: only include themes that genuinely recur across %d+ entries
- For "insight" items: skip trivial or one-time observations
- When an entry includes experience_protection, preserve concrete evidence and caveats
- A2A/tool/swarm traces require repeated evidence; do not turn one isolated trace into a broad fact
- Maximum 5 "recurring" items + 8 "insight" items = 13 total maximum
- Return ONLY the JSON array, no commentary
- If nothing qualifies, return []

CRITICAL category rules:
- "user_fact" = ONLY personal information about the user (name, family, location, job, personal habits, relationships)
- "preference" = user's tool/language/style preferences
- "instruction" = how the user wants things done
- NEVER classify technical environment details (software versions, server configs, Docker settings, project architecture, API endpoints, deployment configs) as "user_fact". These are project knowledge, not user facts.
- Use only IDs shown in the episodic memory labels for evidence_ids
- Each item must be about ONE topic. NEVER combine personal info with technical info.`, s.threshold, s.threshold)

	userPrompt := sb.String()
	if experienceProtection != "" {
		userPrompt = experienceProtection + "\n\nEpisodic memories:\n" + userPrompt
	}

	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	resp, err := llm.ChatCall(messages)
	if err != nil {
		return &SynthesizeResult{Error: err.Error()}, nil
	}

	// Parse combined results.
	type synthesisItem struct {
		Source        string   `json:"source"`
		Content       string   `json:"content"`
		Category      string   `json:"category"`
		EvidenceCount int      `json:"evidence_count"`
		EvidenceIDs   []string `json:"evidence_ids"`
	}
	var items []synthesisItem
	if err := extractJSONFromLLMResponse(resp, &items); err != nil {
		return &SynthesizeResult{Error: fmt.Sprintf("parse synthesis: %v", err)}, nil
	}

	result := &SynthesizeResult{}
	for _, item := range items {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		evidence := selectSynthesisEvidence(episodic, item.EvidenceIDs)
		if len(evidence) == 0 && item.EvidenceCount > 0 {
			evidence = syntheticEvidencePlaceholders(item.EvidenceCount)
		}
		gate := AssessConsolidationGate(evidence, ConsolidationGateOptions{MinEvidence: s.threshold})
		if !gate.Allowed {
			continue
		}

		cat := CategoryPreference
		switch item.Category {
		case "instruction":
			cat = CategoryInstruction
		case "user_fact", "fact":
			cat = CategoryUserFact
		}

		tag := "reflection"
		if item.Source == "recurring" {
			tag = "promoted"
		}

		evidenceIDs := synthesisEvidenceIDs(evidence)
		_, err := s.store.UpsertEntryByTags(UpsertByTagsOptions{
			Title:            "Schema consolidation",
			Content:          item.Content,
			Category:         cat,
			Tags:             []string{tag, "auto_generated", "schema:" + item.Source},
			IdentityTagCount: 3,
			Scope:            ScopeGlobal,
			SourceType:       "schema_consolidation",
			EvidenceIDs:      evidenceIDs,
			RelatedIDs:       evidenceIDs,
			DerivedKind:      "schema:" + item.Source,
			Boundary:         &gate.Boundary,
		})
		if err != nil {
			continue
		}

		if item.Source == "recurring" {
			result.Promoted++
		} else {
			result.InsightsGenerated++
		}
	}

	return result, nil
}

func selectSynthesisEvidence(entries []Entry, ids []string) []Entry {
	if len(ids) == 0 {
		return nil
	}
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	if len(want) == 0 {
		return nil
	}
	selected := make([]Entry, 0, len(want))
	for _, entry := range entries {
		if _, ok := want[entry.ID]; ok {
			selected = append(selected, entry)
		}
	}
	return selected
}

func syntheticEvidencePlaceholders(count int) []Entry {
	if count <= 0 {
		return nil
	}
	evidence := make([]Entry, count)
	for i := range evidence {
		evidence[i] = Entry{SourceType: "llm_evidence_count"}
	}
	return evidence
}

func synthesisEvidenceIDs(evidence []Entry) []string {
	ids := make([]string, 0, len(evidence))
	seen := map[string]struct{}{}
	for _, entry := range evidence {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
