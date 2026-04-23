package tool

import (
	"fmt"
	"sort"
	"strings"
)

// SkillMemory aggregates signals from UsageTracker to produce context-aware
// capability summaries and failure warnings for injection into the system prompt.
//
// Inspired by Memento-Skills' SRDP formalism: the agent's policy π(a|s, M_t)
// is conditioned on both the current state and the accumulated skill memory.
// SkillMemory makes this memory explicit and queryable.
//
// SkillMemory is a read-only aggregation layer — it does not own or modify
// the underlying UsageTracker data.
type SkillMemory struct {
	tracker *UsageTracker
}

// NewSkillMemory creates a SkillMemory backed by the given UsageTracker.
// Returns nil if tracker is nil.
func NewSkillMemory(tracker *UsageTracker) *SkillMemory {
	if tracker == nil {
		return nil
	}
	return &SkillMemory{tracker: tracker}
}

// maxCapabilitySummaryItems limits the number of tools in the capability summary.
const maxCapabilitySummaryItems = 5

// maxFailureWarningItems limits the number of tools in the failure warnings.
const maxFailureWarningItems = 3

// BuildCapabilitySummary generates a concise summary of the agent's proven
// capabilities in the context of the current query. Returns empty string
// when there's nothing useful to report.
//
// The summary is injected into the system prompt to help the LLM make
// informed tool selection decisions based on historical success patterns.
func (sm *SkillMemory) BuildCapabilitySummary(queryTokens []string) string {
	if sm == nil || sm.tracker == nil {
		return ""
	}

	patterns := sm.tracker.ExtractPatterns(7)
	if len(patterns) == 0 {
		return ""
	}

	// Score patterns by relevance to current query.
	type scoredPattern struct {
		pattern   UsagePattern
		relevance float64
	}
	var scored []scoredPattern

	querySet := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		querySet[tok] = true
	}

	for _, p := range patterns {
		if len(queryTokens) == 0 {
			continue
		}
		relevance := jaccardTokens(querySet, p.TopTokens)
		if relevance > 0 {
			scored = append(scored, scoredPattern{p, relevance})
		}
	}

	if len(scored) == 0 {
		return ""
	}

	// Sort by relevance (context-matching first), then by success rate.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].relevance != scored[j].relevance {
			return scored[i].relevance > scored[j].relevance
		}
		return scored[i].pattern.SuccessRate > scored[j].pattern.SuccessRate
	})

	// Take top N.
	n := maxCapabilitySummaryItems
	if len(scored) < n {
		n = len(scored)
	}

	var sb strings.Builder
	sb.WriteString("[能力记忆] 基于近 7 天的工具使用数据：\n")
	for i := 0; i < n; i++ {
		p := scored[i].pattern
		sb.WriteString(fmt.Sprintf("- %s\n", p.Description))
	}
	return sb.String()
}

// BuildFailureWarnings generates warnings about tools that have high failure
// rates in the context of the current query. Returns empty string when there
// are no relevant warnings.
//
// These warnings help the LLM avoid known failure patterns and choose
// alternative approaches proactively.
func (sm *SkillMemory) BuildFailureWarnings(queryTokens []string) string {
	if sm == nil || sm.tracker == nil || len(queryTokens) == 0 {
		return ""
	}

	stats := sm.tracker.ContextFailureStats(queryTokens, 3)

	var warnings []ToolContextStats
	for _, s := range stats {
		if s.FailureRate >= 0.5 {
			warnings = append(warnings, s)
		}
	}

	if len(warnings) == 0 {
		return ""
	}

	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i].FailureRate > warnings[j].FailureRate
	})

	n := maxFailureWarningItems
	if len(warnings) < n {
		n = len(warnings)
	}

	var sb strings.Builder
	sb.WriteString("[失败警告] 以下工具在类似场景下失败率较高：\n")
	for i := 0; i < n; i++ {
		w := warnings[i]
		sb.WriteString(fmt.Sprintf("- %s：近期 %d 次调用中 %d 次失败（%.0f%%）\n",
			w.ToolName, w.Total, w.Failures, w.FailureRate*100))
	}
	return sb.String()
}

// SuggestAlternatives returns tool names that have succeeded in similar
// contexts where the given tool has failed. Useful for drift recovery prompts.
//
// Instead of the generic "please try a different approach", the drift recovery
// can now say "in similar tasks, {alternative} succeeded — try that instead".
func (sm *SkillMemory) SuggestAlternatives(failedTool string, queryTokens []string) []string {
	if sm == nil || sm.tracker == nil || len(queryTokens) == 0 {
		return nil
	}

	stats := sm.tracker.ContextSuccessAlternatives(queryTokens, failedTool, 2, 0.7)

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].SuccessRate > stats[j].SuccessRate
	})

	n := 3
	if len(stats) < n {
		n = len(stats)
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = stats[i].ToolName
	}
	return result
}
