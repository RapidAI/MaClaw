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
	if err := t1.Save(); err != nil {
		t.Fatal(err)
	}

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

func TestUsageTracker_RecordExperiencePersistsRichFields(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	tracker.RecordExperience(ToolExperience{
		ToolName:     "browser_click",
		QueryTokens:  []string{"browser", "click", "button", "extra", "tokens", "ignored"},
		Success:      false,
		FollowUp:     "retry",
		TaskType:     "browser_automation",
		ToolSequence: []string{"browser_navigate", "browser_click", "browser_click"},
		ErrorClass:   "element_missing",
		RetryCount:   2,
		RecoveryTool: "screenshot",
		FinalOutcome: "recovered",
	})

	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	if len(tracker.records) != 1 {
		t.Fatalf("records = %d, want 1", len(tracker.records))
	}
	record := tracker.records[0]
	if record.TaskType != "browser_automation" || record.ErrorClass != "element_missing" || record.RecoveryTool != "screenshot" {
		t.Fatalf("rich fields not preserved: %+v", record)
	}
	if len(record.QueryTokens) != 5 {
		t.Fatalf("QueryTokens length = %d, want capped at 5", len(record.QueryTokens))
	}
	if len(record.ToolSequence) != 2 {
		t.Fatalf("ToolSequence = %v, want deduplicated sequence", record.ToolSequence)
	}
}

func TestUsageTracker_DistillRoutingHints(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	now := time.Now()
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(ToolExperience{ToolName: "browser_observe", QueryTokens: []string{"browser", "button"}, TaskType: "browser_automation", Success: true, Timestamp: now})
	}
	for i := 0; i < 3; i++ {
		tracker.RecordExperience(ToolExperience{ToolName: "browser_click", QueryTokens: []string{"browser", "button"}, TaskType: "browser_automation", Success: false, RecoveryTool: "browser_observe", Timestamp: now})
	}

	hints := tracker.DistillRoutingHints(7, 3)
	if len(hints) != 1 {
		t.Fatalf("hints = %+v, want one hint", hints)
	}
	hint := hints[0]
	if hint.TaskType != "browser_automation" || hint.Evidence != 7 || hint.Confidence <= 0 {
		t.Fatalf("unexpected hint metadata: %+v", hint)
	}
	if len(hint.PreferTools) != 1 || hint.PreferTools[0] != "browser_observe" {
		t.Fatalf("PreferTools = %v, want browser_observe", hint.PreferTools)
	}
	if len(hint.AvoidTools) != 1 || hint.AvoidTools[0] != "browser_click" {
		t.Fatalf("AvoidTools = %v, want browser_click", hint.AvoidTools)
	}
	if len(hint.RecoveryTools) != 1 || hint.RecoveryTools[0] != "browser_observe" {
		t.Fatalf("RecoveryTools = %v, want browser_observe", hint.RecoveryTools)
	}
}

func TestUsageTracker_RoutingHintAdjustment(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	now := time.Now()
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(ToolExperience{ToolName: "browser_observe", QueryTokens: []string{"browser", "button"}, Success: true, Timestamp: now})
	}
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(ToolExperience{ToolName: "browser_click", QueryTokens: []string{"browser", "button"}, Success: false, RecoveryTool: "browser_observe", Timestamp: now})
	}

	prefer := tracker.RoutingHintAdjustment("browser_observe", []string{"browser", "button"})
	avoid := tracker.RoutingHintAdjustment("browser_click", []string{"browser", "button"})
	if prefer <= 0 {
		t.Fatalf("browser_observe adjustment = %.4f, want positive", prefer)
	}
	if avoid >= 0 {
		t.Fatalf("browser_click adjustment = %.4f, want negative", avoid)
	}
	if prefer > maxRoutingHintAdjustment || avoid < -maxRoutingHintAdjustment {
		t.Fatalf("adjustments out of bounds: prefer %.4f avoid %.4f", prefer, avoid)
	}
	if unrelated := tracker.RoutingHintAdjustment("browser_observe", []string{"spreadsheet"}); unrelated != 0 {
		t.Fatalf("unrelated adjustment = %.4f, want 0", unrelated)
	}
}

func TestUsageTracker_DistillSkillNudgeCandidates(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	now := time.Now()
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(ToolExperience{
			ToolName:     "browser_verify",
			QueryTokens:  []string{"browser", "checkout"},
			TaskType:     "browser_automation",
			ToolSequence: []string{"browser_observe", "browser_click", "browser_verify"},
			Success:      true,
			FinalOutcome: "completed",
			Timestamp:    now,
		})
	}
	tracker.RecordExperience(ToolExperience{
		ToolName:     "browser_click",
		QueryTokens:  []string{"browser", "checkout"},
		TaskType:     "browser_automation",
		ToolSequence: []string{"browser_observe", "browser_click", "browser_verify"},
		Success:      false,
		FinalOutcome: "failed",
		Timestamp:    now,
	})
	tracker.RecordExperience(ToolExperience{ToolName: "bash", QueryTokens: []string{"browser"}, ToolSequence: []string{"bash"}, Success: true, Timestamp: now})

	candidates := tracker.DistillSkillNudgeCandidates(14, 3)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want one", candidates)
	}
	c := candidates[0]
	if c.TaskType != "browser_automation" || c.Evidence != 5 || c.SuccessRate != 0.8 || c.Confidence <= 0 {
		t.Fatalf("unexpected candidate metadata: %+v", c)
	}
	if len(c.ToolSequence) != 3 || c.ToolSequence[0] != "browser_observe" || c.SuggestedName == "" {
		t.Fatalf("unexpected candidate sequence/name: %+v", c)
	}
}

func TestUsageTracker_DistillRecoveryPatterns(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	now := time.Now()
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(ToolExperience{
			ToolName:     "browser_click",
			QueryTokens:  []string{"browser", "button"},
			TaskType:     "browser_automation",
			ToolSequence: []string{"browser_click", "browser_observe"},
			Success:      false,
			ErrorClass:   "element_missing",
			RecoveryTool: "browser_observe",
			FinalOutcome: "recovered",
			Timestamp:    now,
		})
	}
	tracker.RecordExperience(ToolExperience{
		ToolName:     "browser_click",
		QueryTokens:  []string{"browser", "button"},
		TaskType:     "browser_automation",
		Success:      false,
		ErrorClass:   "element_missing",
		RecoveryTool: "browser_observe",
		FinalOutcome: "failed",
		Timestamp:    now,
	})

	patterns := tracker.DistillRecoveryPatterns(14, 3)
	if len(patterns) != 1 {
		t.Fatalf("patterns = %+v, want one", patterns)
	}
	p := patterns[0]
	if p.FailedTool != "browser_click" || p.RecoveryTool != "browser_observe" || p.ErrorClass != "element_missing" {
		t.Fatalf("unexpected recovery pattern identity: %+v", p)
	}
	if p.Evidence != 5 || p.SuccessRate != 0.8 || p.Confidence <= 0 || len(p.ToolSequence) < 2 {
		t.Fatalf("unexpected recovery pattern metadata: %+v", p)
	}
}
