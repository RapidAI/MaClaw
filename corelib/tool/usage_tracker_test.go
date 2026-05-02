package tool

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUsageTracker_RecordAndScore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	tracker, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}

	// Record a successful use of "bash" with tokens ["run", "command"].
	tracker.Record("bash", []string{"run", "command"}, true)
	// Give async save a moment.
	time.Sleep(50 * time.Millisecond)

	// Score should be positive for matching query.
	score := tracker.ExperienceScore("bash", []string{"run", "command"})
	if score <= 0 {
		t.Errorf("expected positive score for matching query, got %.4f", score)
	}

	// Score should be zero for unrelated tool.
	score2 := tracker.ExperienceScore("unknown_tool", []string{"run", "command"})
	if score2 != 0 {
		t.Errorf("expected zero score for unknown tool, got %.4f", score2)
	}

	// Score should be zero for completely different tokens.
	score3 := tracker.ExperienceScore("bash", []string{"database", "query"})
	if score3 != 0 {
		t.Errorf("expected zero score for non-overlapping tokens, got %.4f", score3)
	}
}

func TestUsageTracker_FailurePenalty(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Record failures.
	for i := 0; i < 5; i++ {
		tracker.Record("bad_tool", []string{"test", "query"}, false)
	}

	score := tracker.ExperienceScore("bad_tool", []string{"test", "query"})
	if score > 0 {
		t.Errorf("expected zero or negative (clamped to 0) score for all-failure tool, got %.4f", score)
	}
}

func TestUsageTracker_IgnoresUnrelatedSameToolHistory(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	now := time.Now()

	tracker.mu.Lock()
	for i := 0; i < 50; i++ {
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"database", "query"},
			Success:     true,
			Timestamp:   now,
		})
	}
	for i := 0; i < 5; i++ {
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"server", "logs"},
			Success:     true,
			Timestamp:   now,
		})
	}
	tracker.mu.Unlock()

	scoreWithNoise := tracker.ExperienceScore("ssh", []string{"server", "logs"})

	clean, _ := NewUsageTracker("")
	clean.mu.Lock()
	for i := 0; i < 5; i++ {
		clean.records = append(clean.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"server", "logs"},
			Success:     true,
			Timestamp:   now,
		})
	}
	clean.mu.Unlock()
	scoreClean := clean.ExperienceScore("ssh", []string{"server", "logs"})

	if diff := scoreWithNoise - scoreClean; diff < -0.001 || diff > 0.001 {
		t.Errorf("unrelated same-tool history should not dilute contextual score: noisy %.4f clean %.4f", scoreWithNoise, scoreClean)
	}
}

func TestUsageTracker_EvidenceConfidenceShrinksSingleSuccess(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	tracker.mu.Lock()
	tracker.records = append(tracker.records, UsageRecord{
		ToolName:    "bash",
		QueryTokens: []string{"run", "test"},
		Success:     true,
		Timestamp:   time.Now(),
	})
	tracker.mu.Unlock()

	single := tracker.ExperienceScore("bash", []string{"run", "test"})
	if single <= 0 || single >= 0.5 {
		t.Fatalf("single success should help but stay conservative, got %.4f", single)
	}

	for i := 0; i < 9; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "bash",
			QueryTokens: []string{"run", "test"},
			Success:     true,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	repeated := tracker.ExperienceScore("bash", []string{"run", "test"})
	if repeated <= single {
		t.Errorf("repeated successes should build confidence: single %.4f repeated %.4f", single, repeated)
	}
}

func TestUsageTracker_FollowUpAffectsExperience(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	tracker.mu.Lock()
	for i := 0; i < 6; i++ {
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "fragile_tool",
			QueryTokens: []string{"deploy"},
			Success:     false,
			FollowUp:    "abandon",
			Timestamp:   time.Now(),
		})
	}
	tracker.mu.Unlock()

	score := tracker.ExperienceScore("fragile_tool", []string{"deploy"})
	if score != 0 {
		t.Errorf("abandoned failures should clamp experience to zero, got %.4f", score)
	}
}

func TestUsageTracker_RingBuffer(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	tracker.maxItems = 10

	for i := 0; i < 20; i++ {
		tracker.Record("tool", []string{"tok"}, true)
	}

	tracker.mu.RLock()
	n := len(tracker.records)
	tracker.mu.RUnlock()
	if n > 10 {
		t.Errorf("expected max 10 records, got %d", n)
	}
}

func TestUsageTracker_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")

	t1, _ := NewUsageTracker(path)
	t1.Record("bash", []string{"hello"}, true)
	time.Sleep(100 * time.Millisecond) // wait for async save

	t2, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	t2.mu.RLock()
	n := len(t2.records)
	t2.mu.RUnlock()
	if n != 1 {
		t.Errorf("expected 1 persisted record, got %d", n)
	}
}

func TestUsageTracker_EmptyTokens(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	tracker.Record("bash", []string{"a"}, true)

	score := tracker.ExperienceScore("bash", nil)
	if score != 0 {
		t.Errorf("expected 0 for nil query tokens, got %.4f", score)
	}

	score2 := tracker.ExperienceScore("bash", []string{})
	if score2 != 0 {
		t.Errorf("expected 0 for empty query tokens, got %.4f", score2)
	}
}

func TestUsageTracker_LoadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	tracker, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal("should not error on missing file")
	}
	if tracker == nil {
		t.Fatal("tracker should not be nil")
	}
}

func TestJaccardTokens(t *testing.T) {
	tests := []struct {
		query  map[string]bool
		record []string
		want   float64
	}{
		{map[string]bool{"a": true, "b": true}, []string{"a", "b"}, 1.0},
		{map[string]bool{"a": true, "b": true}, []string{"a", "c"}, 1.0 / 3.0},
		{map[string]bool{"a": true}, []string{"b"}, 0},
		{map[string]bool{}, []string{"a"}, 0},
		{map[string]bool{"a": true}, nil, 0},
	}
	for i, tt := range tests {
		got := jaccardTokens(tt.query, tt.record)
		diff := got - tt.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.001 {
			t.Errorf("case %d: jaccardTokens = %.4f, want %.4f", i, got, tt.want)
		}
	}
}
