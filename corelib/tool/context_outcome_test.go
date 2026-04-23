package tool

import (
	"testing"
	"time"
)

func TestContextOutcomeScore_MatchingContext(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Record 10 successful ssh calls in "server monitoring" context.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"服务器", "资源", "GPU"},
			Success:     true,
			FollowUp:    "continue",
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
		})
		tracker.mu.Unlock()
	}

	// Query with overlapping tokens should get high score.
	score := tracker.ContextOutcomeScore("ssh", []string{"服务器", "GPU", "查看"})
	if score < 0.7 {
		t.Errorf("expected high score for matching context, got %.4f", score)
	}
}

func TestContextOutcomeScore_DifferentContexts(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// "server monitoring" context: 10 successes.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"服务器", "资源", "GPU"},
			Success:     true,
			FollowUp:    "continue",
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
		})
		tracker.mu.Unlock()
	}

	// "deployment" context: 3 successes, 7 failures with retries.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"部署", "应用", "docker"},
			Success:     i < 3,
			FollowUp:    "retry",
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
		})
		tracker.mu.Unlock()
	}

	monitorScore := tracker.ContextOutcomeScore("ssh", []string{"服务器", "GPU", "查看"})
	deployScore := tracker.ContextOutcomeScore("ssh", []string{"部署", "docker", "应用"})

	if monitorScore <= deployScore {
		t.Errorf("monitoring score (%.4f) should be higher than deploy score (%.4f)",
			monitorScore, deployScore)
	}
}

func TestContextOutcomeScore_FallbackToGlobal(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Record 10 successful ssh calls with specific tokens.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"服务器", "资源"},
			Success:     true,
			FollowUp:    "continue",
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
		})
		tracker.mu.Unlock()
	}

	// Query with completely different tokens — no context match.
	// Should fall back to global OutcomeScore.
	score := tracker.ContextOutcomeScore("ssh", []string{"天气", "查询"})
	globalScore := tracker.OutcomeScore("ssh")

	if score != globalScore {
		t.Errorf("expected fallback to global score %.4f, got %.4f", globalScore, score)
	}
}

func TestContextOutcomeScore_EmptyTokensFallback(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	for i := 0; i < 5; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "bash",
			QueryTokens: []string{"run"},
			Success:     true,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	// Empty query tokens should fall back to global OutcomeScore.
	score := tracker.ContextOutcomeScore("bash", nil)
	globalScore := tracker.OutcomeScore("bash")
	if score != globalScore {
		t.Errorf("nil tokens: expected global score %.4f, got %.4f", globalScore, score)
	}

	score2 := tracker.ContextOutcomeScore("bash", []string{})
	if score2 != globalScore {
		t.Errorf("empty tokens: expected global score %.4f, got %.4f", globalScore, score2)
	}
}

func TestContextOutcomeScore_NoRecords(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	score := tracker.ContextOutcomeScore("nonexistent", []string{"test"})
	if score != 0 {
		t.Errorf("expected 0 for tool with no records, got %.4f", score)
	}
}

func TestContextOutcomeScore_BelowMinRecords(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Only 2 context-matching records (below contextOutcomeMinRecords=3).
	// Plus 8 non-matching records.
	for i := 0; i < 2; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"服务器", "资源"},
			Success:     false, // failures in context
			FollowUp:    "abandon",
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}
	for i := 0; i < 8; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"部署", "docker"},
			Success:     true,
			FollowUp:    "continue",
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	// With only 2 context-matching records, should fall back to global.
	// Global has 8 success + 2 failure = 80% success rate.
	score := tracker.ContextOutcomeScore("ssh", []string{"服务器", "资源"})
	globalScore := tracker.OutcomeScore("ssh")

	if score != globalScore {
		t.Errorf("below min records: expected global score %.4f, got %.4f", globalScore, score)
	}
}

func TestContextOutcomeScore_RetryAndAbandonPenalty(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// 5 records in same context: 3 success, 1 retry, 1 abandon.
	records := []UsageRecord{
		{ToolName: "ssh", QueryTokens: []string{"服务器"}, Success: true, FollowUp: "continue", Timestamp: time.Now()},
		{ToolName: "ssh", QueryTokens: []string{"服务器"}, Success: true, FollowUp: "continue", Timestamp: time.Now()},
		{ToolName: "ssh", QueryTokens: []string{"服务器"}, Success: true, FollowUp: "continue", Timestamp: time.Now()},
		{ToolName: "ssh", QueryTokens: []string{"服务器"}, Success: false, FollowUp: "retry", Timestamp: time.Now()},
		{ToolName: "ssh", QueryTokens: []string{"服务器"}, Success: false, FollowUp: "abandon", Timestamp: time.Now()},
	}
	tracker.mu.Lock()
	tracker.records = records
	tracker.mu.Unlock()

	score := tracker.ContextOutcomeScore("ssh", []string{"服务器"})

	// successRate = 3/5 = 0.6
	// retryPenalty = 1/5 * 0.3 = 0.06
	// abandonPenalty = 1/5 * 0.5 = 0.10
	// expected = 0.6 - 0.06 - 0.10 = 0.44
	expected := 0.44
	diff := score - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 0.01 {
		t.Errorf("expected score ~%.2f, got %.4f", expected, score)
	}
}

func TestContextOutcomeScore_OldRecordsExcluded(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// 5 old records (30 days ago) — should be excluded.
	for i := 0; i < 5; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"服务器"},
			Success:     true,
			Timestamp:   time.Now().AddDate(0, 0, -30),
		})
		tracker.mu.Unlock()
	}

	score := tracker.ContextOutcomeScore("ssh", []string{"服务器"})
	if score != 0 {
		t.Errorf("expected 0 for old records only, got %.4f", score)
	}
}

// TestOutcomeScore_BackwardCompat verifies that the refactored OutcomeScore
// still produces the same results as before the ContextOutcomeScore addition.
func TestOutcomeScore_BackwardCompat(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	tracker.mu.Lock()
	tracker.records = []UsageRecord{
		{ToolName: "bash", Success: true, FollowUp: "continue", Timestamp: time.Now()},
		{ToolName: "bash", Success: true, FollowUp: "continue", Timestamp: time.Now()},
		{ToolName: "bash", Success: false, FollowUp: "retry", Timestamp: time.Now()},
		{ToolName: "bash", Success: false, FollowUp: "abandon", Timestamp: time.Now()},
	}
	tracker.mu.Unlock()

	score := tracker.OutcomeScore("bash")

	// successRate = 2/4 = 0.5
	// retryPenalty = 1/4 * 0.3 = 0.075
	// abandonPenalty = 1/4 * 0.5 = 0.125
	// expected = 0.5 - 0.075 - 0.125 = 0.3
	expected := 0.3
	diff := score - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 0.01 {
		t.Errorf("backward compat: expected score ~%.2f, got %.4f", expected, score)
	}
}
