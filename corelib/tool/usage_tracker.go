package tool

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// UsageRecord records a single tool invocation outcome.
type UsageRecord struct {
	ToolName    string    `json:"tool_name"`
	QueryTokens []string  `json:"query_tokens"`
	Success     bool      `json:"success"`
	FollowUp    string    `json:"follow_up,omitempty"` // "continue", "retry", "abandon"
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
	t.RecordOutcome(toolName, queryTokens, success, "")
}

// RecordOutcome records a tool invocation with richer outcome information.
// followUp indicates what the model did next: "continue" (moved on), "retry"
// (called same tool again), or "abandon" (gave up on the approach).
func (t *UsageTracker) RecordOutcome(toolName string, queryTokens []string, success bool, followUp string) {
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
		FollowUp:    followUp,
	}
	t.mu.Lock()
	t.records = append(t.records, r)
	if len(t.records) > t.maxItems {
		excess := len(t.records) - t.maxItems
		t.records = t.records[excess:]
	}
	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	t.mu.Unlock()
	go t.saveSnapshot(snapshot)
}

// OutcomeScore returns a [0,1] quality score for a tool based on recent outcomes.
// Considers success rate, retry frequency, and abandon rate over the last 7 days.
// A tool with high success and low retry/abandon rates scores close to 1.0.
func (t *UsageTracker) OutcomeScore(toolName string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	score, _ := t.outcomeScoreWithCount(toolName, nil)
	return score
}

// ContextOutcomeScore returns a [0,1] quality score for a tool in the context
// of the current query. Unlike OutcomeScore which computes a global score,
// this method only considers records whose QueryTokens overlap with the given
// queryTokens (Jaccard similarity > contextOutcomeMinJaccard). This allows the
// Router to distinguish "ssh is great for server monitoring" from "ssh is
// mediocre for deployment" based on historical outcomes in similar contexts.
//
// When no context-matching records are found, falls back to the global
// OutcomeScore for backward compatibility.
//
// Inspired by Memento-Skills' behavioral utility routing: the value of a tool
// depends on the task context, not just its global success rate.
func (t *UsageTracker) ContextOutcomeScore(toolName string, queryTokens []string) float64 {
	if len(queryTokens) == 0 {
		return t.OutcomeScore(toolName)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	// Try context-specific score first.
	contextScore, contextTotal := t.outcomeScoreWithCount(toolName, querySet)

	// Not enough context-matching records — fall back to global score.
	if contextTotal < contextOutcomeMinRecords {
		score, _ := t.outcomeScoreWithCount(toolName, nil)
		return score
	}

	return contextScore
}

// contextOutcomeMinJaccard is the minimum Jaccard similarity between the
// current query tokens and a record's tokens for the record to be considered
// "same context". 0.2 is deliberately low — even a single overlapping token
// out of 5 (Jaccard=0.2) is a meaningful signal.
const contextOutcomeMinJaccard = 0.2

// contextOutcomeMinRecords is the minimum number of context-matching records
// required before ContextOutcomeScore uses context-specific stats. Below this
// threshold, the global OutcomeScore is used to avoid noisy estimates.
const contextOutcomeMinRecords = 3

// outcomeScoreWithCount computes the outcome score and record count from
// records matching toolName, optionally filtered by querySet (Jaccard > threshold).
// When querySet is nil, all records for the tool are considered (global score).
// Returns (score, totalRecords). Caller must hold t.mu.RLock.
func (t *UsageTracker) outcomeScoreWithCount(toolName string, querySet map[string]bool) (float64, int) {
	cutoff := time.Now().AddDate(0, 0, -7)
	var total, successes, retries, abandons int

	for _, r := range t.records {
		if r.ToolName != toolName || r.Timestamp.Before(cutoff) {
			continue
		}
		if querySet != nil {
			sim := jaccardTokens(querySet, r.QueryTokens)
			if sim < contextOutcomeMinJaccard {
				continue
			}
		}
		total++
		if r.Success {
			successes++
		}
		switch r.FollowUp {
		case "retry":
			retries++
		case "abandon":
			abandons++
		}
	}

	if total == 0 {
		return 0, 0
	}

	successRate := float64(successes) / float64(total)
	retryPenalty := float64(retries) / float64(total) * 0.3
	abandonPenalty := float64(abandons) / float64(total) * 0.5

	score := successRate - retryPenalty - abandonPenalty
	return clampFloat(score, 0, 1), total
}

// ExperienceScore returns a [0,1] score for a tool given the current query tokens.
//
// The score estimates contextual utility, not global popularity. Only records
// with query-token overlap contribute evidence, each record is weighted by
// similarity and recency, and the final utility is shrunk by evidence volume so
// one lucky call cannot dominate routing.
func (t *UsageTracker) ExperienceScore(toolName string, queryTokens []string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	var utilitySum float64
	var evidenceWeight float64
	var matched int
	var scanned int

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	for i := len(t.records) - 1; i >= 0; i-- {
		r := t.records[i]
		if r.ToolName != toolName {
			continue
		}
		scanned++
		// Stop scanning after 200 matches to bound computation.
		if scanned > 200 {
			break
		}

		// Jaccard similarity between query tokens and record tokens.
		overlap := jaccardTokens(querySet, r.QueryTokens)
		if overlap == 0 {
			continue
		}
		matched++

		// Recency decay.
		hours := now.Sub(r.Timestamp).Hours()
		if hours < 0 {
			hours = 0
		}
		recency := math.Exp(-0.01 * hours)

		evidence := overlap * recency
		evidenceWeight += evidence
		utilitySum += evidence * usageOutcomeWeight(r)
	}

	if matched == 0 || evidenceWeight == 0 {
		return 0
	}

	utility := utilitySum / evidenceWeight
	if utility <= 0 {
		return 0
	}

	// Confidence rises with corroborating, similar, recent evidence. The curve is
	// intentionally conservative: one exact recent success scores about 0.28,
	// while repeated consistent successes approach 1.0.
	confidence := 1 - math.Exp(-evidenceWeight/3.0)
	return clampFloat(utility*confidence, 0, 1)
}

func usageOutcomeWeight(r UsageRecord) float64 {
	if r.Success {
		switch r.FollowUp {
		case "retry":
			return 0.6
		case "abandon":
			return 0.2
		default:
			return 1.0
		}
	}
	switch r.FollowUp {
	case "retry":
		return -0.45
	case "abandon":
		return -0.6
	default:
		return -0.3
	}
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
	_ = t.saveSnapshot(snapshot)
}

// Save persists the current usage records to disk. Safe for concurrent use.
func (t *UsageTracker) Save() error {
	t.mu.RLock()
	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	t.mu.RUnlock()
	return t.saveSnapshot(snapshot)
}

func (t *UsageTracker) saveSnapshot(records []UsageRecord) error {
	if t.path == "" {
		return nil
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	dir := filepath.Dir(t.path)
	os.MkdirAll(dir, 0755)
	// Atomic write: temp file + rename.
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, t.path)
}

// UsagePattern describes a high-frequency successful tool usage pattern.
type UsagePattern struct {
	ToolName    string   `json:"tool_name"`
	TopTokens   []string `json:"top_tokens"`
	SuccessRate float64  `json:"success_rate"`
	Count       int      `json:"count"`
	Description string   `json:"description"`
}

// contextToolStats scans records whose QueryTokens overlap with queryTokens
// (Jaccard > contextOutcomeMinJaccard) and returns per-tool success/failure
// counts. excludeTool is excluded from results (pass "" to include all).
// Caller must NOT hold t.mu — this method acquires the lock internally.
func (t *UsageTracker) contextToolStats(queryTokens []string, excludeTool string) map[string]*toolCtxAccum {
	t.mu.RLock()
	defer t.mu.RUnlock()

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	m := make(map[string]*toolCtxAccum)
	for _, r := range t.records {
		if r.ToolName == excludeTool {
			continue
		}
		sim := jaccardTokens(querySet, r.QueryTokens)
		if sim < contextOutcomeMinJaccard {
			continue
		}
		s, ok := m[r.ToolName]
		if !ok {
			s = &toolCtxAccum{}
			m[r.ToolName] = s
		}
		s.total++
		if r.Success {
			s.successes++
		} else {
			s.failures++
		}
	}
	return m
}

// toolCtxAccum accumulates per-tool stats during a context scan.
type toolCtxAccum struct {
	total     int
	successes int
	failures  int
}

// ContextFailureStats returns per-tool failure statistics for records whose
// QueryTokens overlap with the given queryTokens (Jaccard > contextOutcomeMinJaccard).
// Only tools with at least minRecords matching records are included.
func (t *UsageTracker) ContextFailureStats(queryTokens []string, minRecords int) []ToolContextStats {
	if len(queryTokens) == 0 {
		return nil
	}
	m := t.contextToolStats(queryTokens, "")
	var result []ToolContextStats
	for name, s := range m {
		if s.total < minRecords {
			continue
		}
		result = append(result, ToolContextStats{
			ToolName:    name,
			Total:       s.total,
			Failures:    s.failures,
			FailureRate: float64(s.failures) / float64(s.total),
		})
	}
	return result
}

// ContextSuccessAlternatives returns tools that have succeeded in contexts
// similar to queryTokens, excluding excludeTool. Only tools with at least
// minRecords matching records and successRate >= minSuccessRate are included.
func (t *UsageTracker) ContextSuccessAlternatives(queryTokens []string, excludeTool string, minRecords int, minSuccessRate float64) []ToolContextStats {
	if len(queryTokens) == 0 {
		return nil
	}
	m := t.contextToolStats(queryTokens, excludeTool)
	var result []ToolContextStats
	for name, s := range m {
		if s.total < minRecords {
			continue
		}
		rate := float64(s.successes) / float64(s.total)
		if rate < minSuccessRate {
			continue
		}
		result = append(result, ToolContextStats{
			ToolName:    name,
			Total:       s.total,
			Successes:   s.successes,
			SuccessRate: rate,
		})
	}
	return result
}

// ToolContextStats holds per-tool statistics in a specific query context.
type ToolContextStats struct {
	ToolName    string  `json:"tool_name"`
	Total       int     `json:"total"`
	Successes   int     `json:"successes,omitempty"`
	Failures    int     `json:"failures,omitempty"`
	SuccessRate float64 `json:"success_rate,omitempty"`
	FailureRate float64 `json:"failure_rate,omitempty"`
}

// ExtractPatterns scans records from the last windowDays and returns
// patterns for tools with success rate > 80% and count > 5.
func (t *UsageTracker) ExtractPatterns(windowDays int) []UsagePattern {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -windowDays)

	// Group by tool name.
	type toolStats struct {
		total   int
		success int
		tokens  map[string]int // token → frequency
	}
	stats := make(map[string]*toolStats)

	for _, r := range t.records {
		if r.Timestamp.Before(cutoff) {
			continue
		}
		s, ok := stats[r.ToolName]
		if !ok {
			s = &toolStats{tokens: make(map[string]int)}
			stats[r.ToolName] = s
		}
		s.total++
		if r.Success {
			s.success++
		}
		for _, tok := range r.QueryTokens {
			s.tokens[tok]++
		}
	}

	var patterns []UsagePattern
	for toolName, s := range stats {
		if s.total < 5 {
			continue
		}
		rate := float64(s.success) / float64(s.total)
		if rate < 0.8 {
			continue
		}

		// Extract top-5 tokens by frequency.
		type tokenFreq struct {
			token string
			freq  int
		}
		var tfs []tokenFreq
		for tok, freq := range s.tokens {
			tfs = append(tfs, tokenFreq{tok, freq})
		}
		sort.Slice(tfs, func(i, j int) bool { return tfs[i].freq > tfs[j].freq })
		topN := 5
		if len(tfs) < topN {
			topN = len(tfs)
		}
		topTokens := make([]string, topN)
		for i := 0; i < topN; i++ {
			topTokens[i] = tfs[i].token
		}

		desc := fmt.Sprintf("工具 %s 在 [%s] 类任务中表现稳定（成功率 %.0f%%，近%d天 %d 次）",
			toolName, strings.Join(topTokens, ", "), rate*100, windowDays, s.total)

		patterns = append(patterns, UsagePattern{
			ToolName:    toolName,
			TopTokens:   topTokens,
			SuccessRate: rate,
			Count:       s.total,
			Description: desc,
		})
	}

	return patterns
}
