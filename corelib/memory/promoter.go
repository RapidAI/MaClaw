package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// PromoteResult holds the outcome of an episodic→semantic promotion run.
type PromoteResult struct {
	Promoted int    `json:"promoted"`
	Error    string `json:"error,omitempty"`
}

// Promoter scans episodic memories for recurring facts and promotes them
// to semantic memories (preference/instruction) when they appear ≥ threshold
// times. This implements the MemGPT-style episodic→semantic transition.
type Promoter struct {
	mu        sync.RWMutex
	store     *Store
	llm       LLMChatCaller
	threshold int // minimum occurrences to trigger promotion (default 3)
}

// NewPromoter creates a Promoter.
func NewPromoter(store *Store, llm LLMChatCaller) *Promoter {
	return &Promoter{store: store, llm: llm, threshold: 3}
}

// SetLLM rewires the LLM used by the promotion cycle. The App constructs the
// memory pipeline before model configuration is always available, so evolution
// components must be able to receive the caller later without being recreated.
func (p *Promoter) SetLLM(llm LLMChatCaller) {
	p.mu.Lock()
	p.llm = llm
	p.mu.Unlock()
}

// Promote runs one promotion cycle. It groups episodic memories by
// content similarity, identifies recurring themes, and asks the LLM
// to confirm promotion to semantic memory.
func (p *Promoter) Promote(ctx context.Context) (*PromoteResult, error) {
	p.mu.RLock()
	llm := p.llm
	p.mu.RUnlock()
	if llm == nil || !llm.IsConfigured() {
		return &PromoteResult{}, nil
	}

	// Collect episodic entries.
	p.store.mu.RLock()
	var episodic []Entry
	for _, e := range p.store.entries {
		if e.Category.Tier() == TierEpisodic && e.IsActive() {
			episodic = append(episodic, e)
		}
	}
	p.store.mu.RUnlock()

	if len(episodic) < p.threshold*2 {
		return &PromoteResult{}, nil
	}

	// Take the most recent 50 episodic entries for analysis.
	if len(episodic) > 50 {
		episodic = episodic[len(episodic)-50:]
	}

	var sb strings.Builder
	for i, e := range episodic {
		fmt.Fprintf(&sb, "[%d] %s\n", i, truncStr(e.Content, 200))
	}

	systemPrompt := fmt.Sprintf(`You are a memory promotion assistant. Analyze the following episodic memories and identify facts, preferences, or patterns that appear in %d or more separate entries.

For each recurring theme, output a promotion candidate:
[{"content": "concise fact/preference", "category": "preference|instruction|user_fact", "evidence_count": N}]

Rules:
- Only include themes that genuinely recur across multiple entries
- "content" must be a single actionable statement
- Maximum 5 promotions per run
- Return ONLY the JSON array
- If nothing qualifies, return []`, p.threshold)

	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": sb.String()},
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	resp, err := llm.ChatCall(messages)
	if err != nil {
		return &PromoteResult{Error: err.Error()}, nil
	}

	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	type candidate struct {
		Content       string `json:"content"`
		Category      string `json:"category"`
		EvidenceCount int    `json:"evidence_count"`
	}
	var candidates []candidate
	if err := json.Unmarshal([]byte(body), &candidates); err != nil {
		return &PromoteResult{Error: fmt.Sprintf("parse promotion: %v", err)}, nil
	}

	result := &PromoteResult{}
	for _, c := range candidates {
		if c.EvidenceCount < p.threshold || strings.TrimSpace(c.Content) == "" {
			continue
		}
		cat := CategoryPreference
		switch c.Category {
		case "instruction":
			cat = CategoryInstruction
		case "user_fact":
			cat = CategoryUserFact
		}
		entry := Entry{
			Content:  c.Content,
			Category: cat,
			Tags:     []string{"promoted", "auto_generated"},
			Scope:    ScopeGlobal,
		}
		_ = p.store.Save(entry)
		result.Promoted++
	}
	return result, nil
}
