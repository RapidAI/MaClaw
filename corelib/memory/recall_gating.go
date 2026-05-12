package memory

import (
	"fmt"
	"strings"
	"sync"
)

// recallGatingThreshold is the minimum candidate count to trigger LLM gating.
// Below this threshold, all candidates pass through without filtering.
const recallGatingThreshold = 15

// RecallGating performs TiMem-style post-retrieval filtering using an LLM
// to judge whether each candidate memory is genuinely relevant to the query.
// This sits between the RRF scoring phase and the final result assembly.
//
// Gating is only activated when the candidate count exceeds
// recallGatingThreshold to avoid unnecessary LLM calls on small result sets.
type RecallGating struct {
	mu  sync.RWMutex
	llm LLMChatCaller
}

// NewRecallGating creates a RecallGating instance.
func NewRecallGating(llm LLMChatCaller) *RecallGating {
	return &RecallGating{llm: llm}
}

// SetLLM rewires the LLM used by post-retrieval recall gating.
func (rg *RecallGating) SetLLM(llm LLMChatCaller) {
	rg.mu.Lock()
	rg.llm = llm
	rg.mu.Unlock()
}

// gatingDecision represents the LLM's judgment on a single candidate.
type gatingDecision struct {
	ID     string `json:"id"`
	Keep   bool   `json:"keep"`
	Reason string `json:"reason,omitempty"`
}

// Filter takes a query and a set of scored recall candidates, asks the LLM
// to judge relevance, and returns only the retained entries in their original
// score order.
//
// If the LLM is not configured, returns an error, or the candidate count is
// below the threshold, the original candidates are returned unchanged.
func (rg *RecallGating) Filter(query string, candidates []recallScored) []recallScored {
	rg.mu.RLock()
	llm := rg.llm
	rg.mu.RUnlock()
	if llm == nil || !llm.IsConfigured() {
		return candidates
	}
	if len(candidates) <= recallGatingThreshold {
		return candidates
	}

	// Build compact candidate descriptions for the LLM.
	var sb strings.Builder
	sb.WriteString("Query: ")
	sb.WriteString(truncStr(query, 300))
	sb.WriteString("\n\nMemory candidates:\n")

	for _, c := range candidates {
		desc := c.entry.CompactForm
		if desc == "" {
			desc = c.entry.Content
		}
		fmt.Fprintf(&sb, "- id=%s [%s] %s\n", c.entry.ID, c.entry.Category, truncStr(desc, 150))
	}

	systemPrompt := `You are a memory recall gating filter. For each memory candidate, decide if it is genuinely relevant to the user's query.

Rules:
- Keep memories that directly answer, contextualize, or are prerequisites for the query
- Discard memories that are topically unrelated despite keyword overlap
- Discard memories that are redundant (covered by a more specific candidate)
- When in doubt, KEEP the memory (prefer recall over precision)
- Return ONLY a JSON array: [{"id":"...","keep":true/false}]
- Do NOT add any commentary outside the JSON`

	resp, err := llm.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": sb.String()},
	})
	if err != nil {
		return candidates // graceful degradation
	}

	// Parse LLM response.
	var decisions []gatingDecision
	if err := extractJSONFromLLMResponse(resp, &decisions); err != nil {
		return candidates // parse failure → keep all
	}

	// Build keep set.
	keepSet := make(map[string]bool, len(decisions))
	for _, d := range decisions {
		if d.Keep {
			keepSet[d.ID] = true
		}
	}

	// If LLM kept nothing or fewer than 3, something went wrong — return all.
	if len(keepSet) < 3 {
		return candidates
	}

	// Filter while preserving original score order.
	var result []recallScored
	for _, c := range candidates {
		if keepSet[c.entry.ID] {
			result = append(result, c)
		}
	}

	// Safety: if we filtered too aggressively (>80% removed), return original.
	if len(result) < len(candidates)/5 {
		return candidates
	}

	return result
}
