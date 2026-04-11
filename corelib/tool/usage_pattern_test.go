package tool

import (
	"testing"
	"time"
)

func TestExtractPatterns_HighSuccessRate(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Record 10 successful bash calls.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "bash",
			QueryTokens: []string{"run", "command", "deploy"},
			Success:     true,
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
		})
		tracker.mu.Unlock()
	}

	patterns := tracker.ExtractPatterns(7)
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	p := patterns[0]
	if p.ToolName != "bash" {
		t.Errorf("ToolName: got %q, want %q", p.ToolName, "bash")
	}
	if p.SuccessRate != 1.0 {
		t.Errorf("SuccessRate: got %.2f, want 1.0", p.SuccessRate)
	}
	if p.Count != 10 {
		t.Errorf("Count: got %d, want 10", p.Count)
	}
	if len(p.TopTokens) == 0 {
		t.Error("TopTokens should not be empty")
	}
	if p.Description == "" {
		t.Error("Description should not be empty")
	}
}

func TestExtractPatterns_LowSuccessRate(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// 3 success + 7 failure = 30% success rate — below threshold.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "bad_tool",
			QueryTokens: []string{"test"},
			Success:     i < 3,
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
		})
		tracker.mu.Unlock()
	}

	patterns := tracker.ExtractPatterns(7)
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns for low success rate, got %d", len(patterns))
	}
}

func TestExtractPatterns_TooFewCalls(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Only 3 calls — below count threshold of 5.
	for i := 0; i < 3; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "rare_tool",
			QueryTokens: []string{"test"},
			Success:     true,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	patterns := tracker.ExtractPatterns(7)
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns for too few calls, got %d", len(patterns))
	}
}

func TestExtractPatterns_OldRecordsExcluded(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Records from 30 days ago — outside 7-day window.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "old_tool",
			QueryTokens: []string{"test"},
			Success:     true,
			Timestamp:   time.Now().AddDate(0, 0, -30),
		})
		tracker.mu.Unlock()
	}

	patterns := tracker.ExtractPatterns(7)
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns for old records, got %d", len(patterns))
	}
}

func TestExtractPatterns_EmptyTracker(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	patterns := tracker.ExtractPatterns(7)
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns for empty tracker, got %d", len(patterns))
	}
}

func TestExtractPatterns_MultipleTools(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// bash: 8 success, 2 failure = 80% — meets threshold.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "bash",
			QueryTokens: []string{"run"},
			Success:     i < 8,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	// read_file: 6 success, 0 failure = 100% — meets threshold.
	for i := 0; i < 6; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "read_file",
			QueryTokens: []string{"read"},
			Success:     true,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	patterns := tracker.ExtractPatterns(7)
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patterns))
	}
}
