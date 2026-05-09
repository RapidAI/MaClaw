package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ReflectResult holds the outcome of a reflection run.
type ReflectResult struct {
	InsightsGenerated int    `json:"insights_generated"`
	Error             string `json:"error,omitempty"`
}

// Reflector analyses recent episodic memories and generates high-level
// insights (preferences, habits, decision patterns) stored as semantic
// memories. Inspired by the Generative Agents "reflection" mechanism.
type Reflector struct {
	mu         sync.RWMutex
	store      *Store
	llm        LLMChatCaller
	minEntries int
	lastRun    time.Time
}

// NewReflector creates a Reflector.
func NewReflector(store *Store, llm LLMChatCaller) *Reflector {
	return &Reflector{store: store, llm: llm, minEntries: 50}
}

// SetLLM rewires the LLM used by reflection without rebuilding the pipeline.
func (r *Reflector) SetLLM(llm LLMChatCaller) {
	r.mu.Lock()
	r.llm = llm
	r.mu.Unlock()
}

// Reflect runs one reflection cycle. It skips if:
//   - fewer than minEntries total entries
//   - less than 24h since last reflection
//   - LLM is not configured
func (r *Reflector) Reflect(ctx context.Context) (*ReflectResult, error) {
	r.mu.RLock()
	llm := r.llm
	r.mu.RUnlock()
	if llm == nil || !llm.IsConfigured() {
		return &ReflectResult{}, nil
	}

	r.store.mu.RLock()
	total := len(r.store.entries)
	r.store.mu.RUnlock()

	if total < r.minEntries {
		return &ReflectResult{}, nil
	}
	if !r.lastRun.IsZero() && time.Since(r.lastRun) < 24*time.Hour {
		return &ReflectResult{}, nil
	}

	// Collect recent episodic memories.
	r.store.mu.RLock()
	var episodic []Entry
	for _, e := range r.store.entries {
		if e.Category.Tier() == TierEpisodic && e.IsActive() {
			episodic = append(episodic, e)
		}
	}
	r.store.mu.RUnlock()

	if len(episodic) < 5 {
		return &ReflectResult{}, nil
	}

	// Take the most recent 30.
	if len(episodic) > 30 {
		episodic = episodic[len(episodic)-30:]
	}

	// Build prompt.
	var sb strings.Builder
	for i, e := range episodic {
		sb.WriteString(formatExperiencePromptEntry(i, e, 300))
	}

	systemPrompt := `You are a memory reflection assistant. Analyze the following episodic memories (conversation summaries and session checkpoints) and extract high-level insights about the user.

Focus on:
- User preferences (tools, languages, frameworks, coding style)
- Decision patterns (how they approach problems, what they prioritize)
- Recurring habits (workflows, naming conventions, project structure)
- Important facts that appear repeatedly

Return a JSON array of insights:
[{"type": "preference|instruction|fact", "content": "concise insight text"}]

Rules:
- Each insight must be a single, actionable statement
- When an entry includes experience_protection, keep concrete evidence and caveats visible instead of smoothing them away
- Treat A2A/tool/swarm traces as experience evidence; do not turn one isolated trace into a broad user fact
- Maximum 10 insights per reflection
- Skip trivial or one-time observations
- Return ONLY the JSON array, no commentary
- If no meaningful insights can be extracted, return []`

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
		return &ReflectResult{Error: err.Error()}, nil
	}

	// Parse insights.
	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	type insight struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	var insights []insight
	if err := json.Unmarshal([]byte(body), &insights); err != nil {
		return &ReflectResult{Error: fmt.Sprintf("parse reflection: %v", err)}, nil
	}

	result := &ReflectResult{}
	for _, ins := range insights {
		if strings.TrimSpace(ins.Content) == "" {
			continue
		}
		cat := CategoryPreference
		if ins.Type == "instruction" {
			cat = CategoryInstruction
		} else if ins.Type == "fact" {
			cat = CategoryUserFact
		}
		entry := Entry{
			Content:  ins.Content,
			Category: cat,
			Tags:     []string{"reflection", "auto_generated"},
		}
		if err := r.store.Save(entry); err != nil {
			continue // skip failed saves
		}
		result.InsightsGenerated++
	}

	r.lastRun = time.Now()
	return result, nil
}
