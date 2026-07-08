package goal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStore_MemoryOnly(t *testing.T) {
	s := NewStore("")
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	if g := s.Get("user1"); g != nil {
		t.Fatal("expected nil for empty store")
	}
}

func TestSet_CreatesGoal(t *testing.T) {
	s := NewStore("")
	g, err := s.Set("user1", "Implement login feature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.GoalID == "" {
		t.Fatal("expected non-empty goal_id")
	}
	if g.Objective != "Implement login feature" {
		t.Fatalf("unexpected objective: %s", g.Objective)
	}
	if g.Status != StatusActive {
		t.Fatalf("expected active, got %s", g.Status)
	}
	if g.MaxTurns != DefaultMaxTurns {
		t.Fatalf("expected default max_turns=%d, got %d", DefaultMaxTurns, g.MaxTurns)
	}
}

func TestSet_EmptyObjective_Fails(t *testing.T) {
	s := NewStore("")
	_, err := s.Set("user1", "")
	if err == nil {
		t.Fatal("expected error for empty objective")
	}
}

func TestSet_Replaces_PreviousGoal(t *testing.T) {
	s := NewStore("")
	g1, _ := s.Set("user1", "Goal 1")
	g2, _ := s.Set("user1", "Goal 2")

	if g1.GoalID == g2.GoalID {
		t.Fatal("expected different goal_ids on replacement")
	}
	got := s.Get("user1")
	if got.Objective != "Goal 2" {
		t.Fatalf("expected replaced goal, got %s", got.Objective)
	}
}

func TestUpdateStatus_VersionProtection(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test goal")

	// Correct goalID → succeeds
	ok := s.UpdateStatus("user1", g.GoalID, StatusComplete, "done")
	if !ok {
		t.Fatal("expected update to succeed")
	}
	got := s.Get("user1")
	if got.Status != StatusComplete {
		t.Fatalf("expected complete, got %s", got.Status)
	}

	// Replace goal → old goalID is stale
	g2, _ := s.Set("user1", "New goal")
	ok = s.UpdateStatus("user1", g.GoalID, StatusFailed, "stale")
	if ok {
		t.Fatal("expected stale update to be ignored")
	}
	got = s.Get("user1")
	if got.GoalID != g2.GoalID || got.Status != StatusActive {
		t.Fatal("stale update should not modify new goal")
	}
}

func TestAccountUsage_IncrementAndBudgetLimit(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test", WithTokenBudget(100))

	s.AccountUsage("user1", g.GoalID, 50, 10)
	got := s.Get("user1")
	if got.TokensUsed != 50 || got.TurnsUsed != 1 {
		t.Fatalf("unexpected usage: tokens=%d turns=%d", got.TokensUsed, got.TurnsUsed)
	}

	// Push over budget
	s.AccountUsage("user1", g.GoalID, 60, 5)
	got = s.Get("user1")
	if got.Status != StatusBudgetLimit {
		t.Fatalf("expected budget_limited, got %s", got.Status)
	}
}

func TestAccountUsage_TurnsLimit(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test", WithMaxTurns(3))

	s.AccountUsage("user1", g.GoalID, 10, 1)
	s.AccountUsage("user1", g.GoalID, 10, 1)
	s.AccountUsage("user1", g.GoalID, 10, 1)
	got := s.Get("user1")
	if got.Status != StatusBudgetLimit {
		t.Fatalf("expected budget_limited after 3 turns, got %s", got.Status)
	}
}

func TestAccountUsage_StaleGoalID_Ignored(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test")
	s.Set("user1", "Replaced") // new goal_id

	ok := s.AccountUsage("user1", g.GoalID, 999, 999)
	if ok {
		t.Fatal("expected stale accountUsage to be ignored")
	}
	got := s.Get("user1")
	if got.TokensUsed != 0 {
		t.Fatal("stale accountUsage should not modify new goal")
	}
}

func TestRecordNoToolTurn_AutoPause(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test")

	s.RecordNoToolTurn("user1", g.GoalID)
	got := s.Get("user1")
	if got.Status != StatusActive {
		t.Fatal("should still be active after 1 no-tool turn")
	}

	s.RecordNoToolTurn("user1", g.GoalID)
	got = s.Get("user1")
	if got.Status != StatusPaused {
		t.Fatalf("expected paused after 2 no-tool turns, got %s", got.Status)
	}
}

func TestResetNoToolCounter(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test")

	s.RecordNoToolTurn("user1", g.GoalID)
	s.ResetNoToolCounter("user1", g.GoalID)

	got := s.Get("user1")
	if got.ConsecutiveNoToolTurns != 0 {
		t.Fatal("expected counter reset to 0")
	}
}

func TestPauseResume(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test")

	s.Pause("user1", g.GoalID)
	got := s.Get("user1")
	if got.Status != StatusPaused {
		t.Fatalf("expected paused, got %s", got.Status)
	}

	s.Resume("user1", g.GoalID)
	got = s.Get("user1")
	if got.Status != StatusActive {
		t.Fatalf("expected active after resume, got %s", got.Status)
	}
}

func TestResume_OnlyFromPaused(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test")

	// Can't resume an active goal
	ok := s.Resume("user1", g.GoalID)
	if ok {
		t.Fatal("should not resume an already-active goal")
	}

	// Complete goal can't be resumed
	s.UpdateStatus("user1", g.GoalID, StatusComplete, "done")
	ok = s.Resume("user1", g.GoalID)
	if ok {
		t.Fatal("should not resume a completed goal")
	}
}

func TestClear(t *testing.T) {
	s := NewStore("")
	s.Set("user1", "Test")

	ok := s.Clear("user1")
	if !ok {
		t.Fatal("expected clear to return true")
	}
	if g := s.Get("user1"); g != nil {
		t.Fatal("expected nil after clear")
	}

	// Clear non-existent
	ok = s.Clear("user2")
	if ok {
		t.Fatal("expected false for non-existent user")
	}
}

func TestShouldContinue(t *testing.T) {
	tests := []struct {
		name   string
		goal   Goal
		expect bool
	}{
		{"active no limits", Goal{Status: StatusActive, MaxTurns: 50}, true},
		{"paused", Goal{Status: StatusPaused}, false},
		{"complete", Goal{Status: StatusComplete}, false},
		{"budget exceeded", Goal{Status: StatusActive, TokenBudget: 100, TokensUsed: 100}, false},
		{"turns exceeded", Goal{Status: StatusActive, MaxTurns: 5, TurnsUsed: 5}, false},
		{"no-tool suppressed", Goal{Status: StatusActive, MaxTurns: 50, ConsecutiveNoToolTurns: 2}, false},
		{"no-tool 1 turn ok", Goal{Status: StatusActive, MaxTurns: 50, ConsecutiveNoToolTurns: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.goal.ShouldContinue(); got != tt.expect {
				t.Fatalf("ShouldContinue()=%v, want %v", got, tt.expect)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []Status{StatusComplete, StatusFailed, StatusBudgetLimit}
	for _, st := range terminal {
		g := Goal{Status: st}
		if !g.IsTerminal() {
			t.Fatalf("expected %s to be terminal", st)
		}
	}
	nonTerminal := []Status{StatusActive, StatusPaused}
	for _, st := range nonTerminal {
		g := Goal{Status: st}
		if g.IsTerminal() {
			t.Fatalf("expected %s to be non-terminal", st)
		}
	}
}

func TestPersistence_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)

	g, _ := s1.Set("user1", "Persistent goal", WithTokenBudget(5000))
	s1.AccountUsage("user1", g.GoalID, 100, 5)

	// Create a new store from same dir — should load the goal
	s2 := NewStore(dir)
	got := s2.Get("user1")
	if got == nil {
		t.Fatal("expected goal to be loaded from disk")
	}
	if got.Objective != "Persistent goal" {
		t.Fatalf("unexpected objective: %s", got.Objective)
	}
	if got.TokensUsed != 100 || got.TurnsUsed != 1 {
		t.Fatalf("unexpected usage after reload: tokens=%d turns=%d", got.TokensUsed, got.TurnsUsed)
	}
}

func TestPersistence_ClearRemovesFile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.Set("user1", "To be cleared")
	s.Clear("user1")

	// File should be gone
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			t.Fatalf("expected no JSON files after clear, found %s", e.Name())
		}
	}
}

func TestSetOptions(t *testing.T) {
	s := NewStore("")
	g, _ := s.Set("user1", "Test",
		WithTokenBudget(10000),
		WithMaxTurns(20),
		WithAcceptanceCriteria([]string{"tests pass", "no lint errors"}),
		WithProjectPath("/home/user/project"),
	)
	if g.TokenBudget != 10000 {
		t.Fatalf("token_budget: got %d", g.TokenBudget)
	}
	if g.MaxTurns != 20 {
		t.Fatalf("max_turns: got %d", g.MaxTurns)
	}
	if len(g.AcceptanceCriteria) != 2 {
		t.Fatalf("acceptance_criteria: got %d items", len(g.AcceptanceCriteria))
	}
	if g.ProjectPath != "/home/user/project" {
		t.Fatalf("project_path: got %s", g.ProjectPath)
	}
}
