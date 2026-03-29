package tool

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageRecord records a single tool invocation outcome.
type UsageRecord struct {
	ToolName    string    `json:"tool_name"`
	QueryTokens []string  `json:"query_tokens"`
	Success     bool      `json:"success"`
	Timestamp   time.Time `json:"timestamp"`
}

// UsageTracker maintains a rolling window of tool usage history.
type UsageTracker struct {
	mu       sync.RWMutex
	records  []UsageRecord
	path     string
	maxItems int
}

// NewUsageTracker creates or loads a UsageTracker from the given path.
func NewUsageTracker(path string) (*UsageTracker, error) {
	t := &UsageTracker{
		records:  make([]UsageRecord, 0, 256),
		path:     path,
		maxItems: 2000,
	}
	if err := t.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("usage_tracker: load: %w", err)
	}
	return t, nil
}

// DefaultUsageTrackerPath returns ~/.maclaw/data/tool_usage.json.
func DefaultUsageTrackerPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".maclaw", "data", "tool_usage.json")
}

// Record logs a tool invocation result. Safe for concurrent use.
// queryTokens should be the BM25-tokenized user message (top tokens).
func (t *UsageTracker) Record(toolName string, queryTokens []string, success bool) {
	// Copy and truncate tokens to limit storage.
	tokens := make([]string, 0, 5)
	for i, tok := range queryTokens {
		if i >= 5 {
			break
		}
		tokens = append(tokens, tok)
	}
	r := UsageRecord{
		ToolName:    toolName,
		QueryTokens: tokens,
		Timestamp:   time.Now(),
		Success:     success,
	}
	t.mu.Lock()
	t.records = append(t.records, r)
	// Ring buffer: drop oldest when over capacity.
	if len(t.records) > t.maxItems {
		excess := len(t.records) - t.maxItems
		t.records = t.records[excess:]
	}
	// Snapshot for async save to avoid data race.
	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	t.mu.Unlock()

	// Persist asynchronously to avoid blocking the hot path.
	go t.saveSnapshot(snapshot)
}

// ExperienceScore returns a [0,1] score for a tool given the current query tokens.
//
// Algorithm:
//  1. Filter records matching toolName
//  2. For each: compute Jaccard similarity with queryTokens
//  3. Weight by recency: exp(-0.01 * hours_since)
//  4. Weight by outcome: success=1.0, failure=-0.3
//  5. Aggregate weighted sum, normalize, clamp to [0,1]
func (t *UsageTracker) ExperienceScore(toolName string, queryTokens []string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	var weightedSum float64
	var count int

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	for i := len(t.records) - 1; i >= 0; i-- {
		r := t.records[i]
		if r.ToolName != toolName {
			continue
		}
		count++
		// Stop scanning after 200 matches to bound computation.
		if count > 200 {
			break
		}

		// Jaccard similarity between query tokens and record tokens.
		overlap := jaccardTokens(querySet, r.QueryTokens)
		if overlap == 0 {
			continue
		}

		// Recency decay.
		hours := now.Sub(r.Timestamp).Hours()
		if hours < 0 {
			hours = 0
		}
		recency := math.Exp(-0.01 * hours)

		// Success weight.
		successW := 1.0
		if !r.Success {
			successW = -0.3
		}

		weightedSum += overlap * recency * successW
	}

	if count == 0 {
		return 0
	}

	// Normalize by count and clamp.
	score := weightedSum / float64(count)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// jaccardTokens computes Jaccard similarity between a set and a token slice.
func jaccardTokens(querySet map[string]bool, recordTokens []string) float64 {
	if len(querySet) == 0 || len(recordTokens) == 0 {
		return 0
	}
	recSet := make(map[string]bool, len(recordTokens))
	for _, tok := range recordTokens {
		recSet[tok] = true
	}
	var intersection int
	for tok := range querySet {
		if recSet[tok] {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(querySet) + len(recSet) - intersection
	return float64(intersection) / float64(union)
}

func (t *UsageTracker) load() error {
	if t.path == "" {
		return nil
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return err
	}
	var records []UsageRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("usage_tracker: parse: %w", err)
	}
	t.mu.Lock()
	t.records = records
	t.mu.Unlock()
	return nil
}

func (t *UsageTracker) save() {
	t.mu.RLock()
	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	t.mu.RUnlock()
	t.saveSnapshot(snapshot)
}

func (t *UsageTracker) saveSnapshot(records []UsageRecord) {
	if t.path == "" {
		return
	}
	data, err := json.Marshal(records)
	if err != nil {
		return
	}
	dir := filepath.Dir(t.path)
	os.MkdirAll(dir, 0755)
	// Atomic write: temp file + rename.
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, t.path)
}
