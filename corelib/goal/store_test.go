package goal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestUpdateObjectiveKeepsProgress(t *testing.T) {
	s := NewStore("")
	g, err := s.Set("user1", "old command")
	if err != nil {
		t.Fatal(err)
	}
	s.Pause("user1", g.GoalID)
	if !s.UpdateObjective("user1", g.GoalID, "  new command ") {
		t.Fatal("expected the command to update")
	}
	got := s.Get("user1")
	if got.GoalID != g.GoalID || got.Objective != "new command" || got.Status != StatusPaused {
		t.Fatalf("updated = %+v", got)
	}
	if s.UpdateObjective("user1", g.GoalID, " ") {
		t.Fatal("empty command must not replace the goal")
	}
	s.UpdateStatus("user1", g.GoalID, StatusComplete, "done")
	if s.UpdateObjective("user1", g.GoalID, "after done") {
		t.Fatal("a completed goal stays finished")
	}
}

func TestReleaseForContinueKeepsGoingAfterAnyStop(t *testing.T) {
	s := NewStore("")
	g, err := s.Set("user1", "ship it")
	if err != nil {
		t.Fatal(err)
	}

	s.UpdateStatus("user1", g.GoalID, StatusPaused, "Auto-paused: agent produced no tool calls for 2 consecutive turns")
	if !s.ReleaseForContinue("user1", g.GoalID) {
		t.Fatal("paused goal should continue")
	}
	got := s.Get("user1")
	if got.Status != StatusActive || got.ConsecutiveNoToolTurns != 0 || got.Summary != "" {
		t.Fatalf("paused continue = %+v", got)
	}

	s.UpdateStatus("user1", g.GoalID, StatusFailed, "目标失败")
	if !s.ReleaseForContinue("user1", g.GoalID) {
		t.Fatal("failed goal should continue")
	}
	if got = s.Get("user1"); got.Status != StatusActive {
		t.Fatalf("failed continue status = %s", got.Status)
	}

	s.UpdateStatus("user1", g.GoalID, StatusBudgetLimit, "Turn limit reached")
	s.mu.Lock()
	s.goals["user1"].MaxTurns = 4
	s.goals["user1"].TurnsUsed = 4
	s.goals["user1"].TokensUsed = 40
	s.goals["user1"].TokenBudget = 40
	usedTurns := s.goals["user1"].TurnsUsed
	s.mu.Unlock()
	if !s.ReleaseForContinue("user1", g.GoalID) {
		t.Fatal("budget stop should continue")
	}
	got = s.Get("user1")
	if got.Status != StatusActive {
		t.Fatalf("budget continue status = %s", got.Status)
	}
	if got.TurnsUsed != usedTurns || got.MaxTurns != 0 {
		t.Fatalf("turn cap = used %d max %d, want the cap lifted", got.TurnsUsed, got.MaxTurns)
	}
	if got.TokenBudget != 80 || got.TokenWindow != 40 {
		t.Fatalf("token window = budget %d window %d", got.TokenBudget, got.TokenWindow)
	}

	s.UpdateStatus("user1", g.GoalID, StatusComplete, "done")
	if s.ReleaseForContinue("user1", g.GoalID) {
		t.Fatal("a completed goal stays finished")
	}
	if got = s.Get("user1"); got.Status != StatusComplete {
		t.Fatalf("complete status = %s", got.Status)
	}
}

func TestReleaseForContinueTokenWindowIsConstant(t *testing.T) {
	s := NewStore("")
	g, err := s.Set("user1", "ship it", WithTokenBudget(100))
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.goals["user1"].TokensUsed = 100
	s.goals["user1"].Status = StatusBudgetLimit
	s.mu.Unlock()

	if !s.ReleaseForContinue("user1", g.GoalID) {
		t.Fatal("first continue")
	}
	got := s.Get("user1")
	if got.TokenWindow != 100 || got.TokenBudget != 200 || !got.ShouldContinue() {
		t.Fatalf("first window = %+v", got)
	}

	s.mu.Lock()
	s.goals["user1"].TokensUsed = 200
	s.goals["user1"].Status = StatusBudgetLimit
	s.mu.Unlock()
	if !s.ReleaseForContinue("user1", g.GoalID) {
		t.Fatal("second continue")
	}
	got = s.Get("user1")
	if got.TokenWindow != 100 || got.TokenBudget != 300 || !got.ShouldContinue() {
		t.Fatalf("second window = %+v", got)
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
		{"unlimited turns", Goal{Status: StatusActive, MaxTurns: 0, TurnsUsed: 100000}, true},
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

func TestPersistence_AutomaticTurnCapsBecomeUnlimited(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	legacy, err := s1.Set("user1", "keep going", WithMaxTurns(LegacyDefaultMaxTurns))
	if err != nil {
		t.Fatal(err)
	}
	raised, err := s1.Set("user2", "week", WithMaxTurns(RaisedDefaultMaxTurns))
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := s1.Set("user3", "short", WithMaxTurns(3))
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := s1.Set("user4", "stopped", WithMaxTurns(LegacyDefaultMaxTurns))
	if err != nil {
		t.Fatal(err)
	}
	if !s1.UpdateStatus("user4", stopped.GoalID, StatusBudgetLimit, "Turn limit reached") {
		t.Fatal("budget stop")
	}

	s2 := NewStore(dir)
	gotLegacy := s2.Get("user1")
	if gotLegacy == nil || gotLegacy.GoalID != legacy.GoalID || gotLegacy.MaxTurns != 0 || gotLegacy.Status != StatusActive {
		t.Fatalf("legacy cap = %+v, want unlimited and still active", gotLegacy)
	}
	gotRaised := s2.Get("user2")
	if gotRaised == nil || gotRaised.GoalID != raised.GoalID || gotRaised.MaxTurns != 0 {
		t.Fatalf("raised cap = %+v, want unlimited", gotRaised)
	}
	gotExplicit := s2.Get("user3")
	if gotExplicit == nil || gotExplicit.GoalID != explicit.GoalID || gotExplicit.MaxTurns != 3 {
		t.Fatalf("explicit cap = %+v, want 3", gotExplicit)
	}
	gotStopped := s2.Get("user4")
	if gotStopped == nil || gotStopped.GoalID != stopped.GoalID || gotStopped.MaxTurns != 0 || gotStopped.Status != StatusBudgetLimit {
		t.Fatalf("stopped cap = %+v, want unlimited but still budget_limited", gotStopped)
	}
	for _, name := range []string{"user1", "user2", "user4"} {
		raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"max_turns"`) {
			t.Fatalf("%s still stores a turn cap: %s", name, raw)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "user3.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"max_turns": 3`) {
		t.Fatalf("explicit cap was rewritten: %s", raw)
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

// TestStatusVocabularyCanonicalizesTheModelFacingSet pins the vocabulary a
// managed turn may set. It lives with the type for the reason IsTerminal does:
// both semantic hosts render it and parse it, and a vocabulary owned by either
// one alone would drift from the other. Two directions are pinned here --
// the enum only ever advertises lifecycle names the record actually stores,
// while the parser keeps accepting the aliases a model naturally reaches for.
func TestStatusVocabularyCanonicalizesTheModelFacingSet(t *testing.T) {
	options := StatusOptions()
	if len(options) != 2 {
		t.Fatalf("a managed turn may set complete and failed, got %v", options)
	}
	for _, option := range options {
		switch Status(option) {
		case StatusComplete, StatusFailed:
		default:
			t.Fatalf("advertised status %q is not a lifecycle name the record stores", option)
		}
		canonical, ok := StatusCanonical(option)
		if !ok || canonical != Status(option) {
			t.Fatalf("canonical option %q maps to (%q, %v)", option, canonical, ok)
		}
	}
	aliases := map[string]Status{"completed": StatusComplete, "fail": StatusFailed, "COMPLETED": StatusComplete, " failed ": StatusFailed}
	for sent, want := range aliases {
		canonical, ok := StatusCanonical(sent)
		if !ok || canonical != want {
			t.Fatalf("alias %q maps to (%q, %v), want %q", sent, canonical, ok, want)
		}
	}
	// Unlike a schedule record's optional status, an update turn parses only a
	// status it intends to set, so there is no unset reading to preserve.
	for _, refused := range []string{"", "active", "paused", "budget_limited", "done"} {
		if _, ok := StatusCanonical(refused); ok {
			t.Fatalf("status %q must not be accepted as a model-supplied update", refused)
		}
	}
	if hint := StatusHint(); !strings.Contains(hint, "complete") || !strings.Contains(hint, "failed") {
		t.Fatalf("hint %q does not name the canonical set", hint)
	}
}

func TestSetStartsInPlanPhase(t *testing.T) {
	s := NewStore("")
	g, err := s.Set("user1", "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	if g.Phase != PhasePlan || g.EffectivePhase() != PhasePlan {
		t.Fatalf("phase = %q", g.Phase)
	}
}

func TestSavePlanAndUpdateStep(t *testing.T) {
	s := NewStore("")
	g, err := s.Set("user1", "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePlan("user1", g.GoalID, "", nil, nil); err == nil {
		t.Fatal("empty plan should fail")
	}
	err = s.SavePlan("user1", g.GoalID, "先写登录接口再补测试", []Step{
		{Title: "编写接口", Detail: "JWT"},
		{Title: "补充测试"},
	}, []string{"go test 通过"})
	if err != nil {
		t.Fatal(err)
	}
	got := s.Get("user1")
	if got.EffectivePhase() != PhaseExecute || len(got.Steps) != 2 || got.Steps[0].Index != 1 || got.Steps[0].Status != StepPending {
		t.Fatalf("plan = %+v", got)
	}
	current, ok := got.CurrentStep()
	if !ok || current.Title != "编写接口" {
		t.Fatalf("current = %+v ok=%v", current, ok)
	}
	if err := s.UpdateStep("user1", g.GoalID, 1, StepDone, "接口已写"); err != nil {
		t.Fatal(err)
	}
	got = s.Get("user1")
	if got.Steps[0].Result != "接口已写" || got.ReadyToComplete() {
		t.Fatalf("after step: %+v", got.Steps)
	}
	if err := s.UpdateStep("user1", "stale", 2, StepDone, "x"); err == nil {
		t.Fatal("stale goal id should fail")
	}
	if err := s.UpdateStep("user1", g.GoalID, 2, StepProgress, "测试还没写"); err != nil {
		t.Fatal(err)
	}
	got = s.Get("user1")
	current, ok = got.CurrentStep()
	if !ok || current.Index != 2 || current.Status != StepProgress {
		t.Fatalf("progress left the step: %+v ok=%v", current, ok)
	}
	if refusal := got.StepReportRefusal(1); !strings.Contains(refusal, "步骤 2") {
		t.Fatalf("refusal = %q", refusal)
	}
	if missing := MissingCriteria("还没测", got.AcceptanceCriteria); len(missing) != 1 || missing[0] != "go test 通过" {
		t.Fatalf("missing = %#v", missing)
	}
	if missing := MissingCriteria("go test 通过", got.AcceptanceCriteria); len(missing) != 0 {
		t.Fatalf("covered criteria reported missing: %#v", missing)
	}
}

func TestPausedClockSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	g, err := s.Set("user1", "ship it")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Pause("user1", g.GoalID) {
		t.Fatal("pause")
	}
	paused := s.Get("user1")
	if paused.StoppedAt.IsZero() || paused.Status != StatusPaused {
		t.Fatalf("pause = status %s stop %s", paused.Status, paused.StoppedAt)
	}
	stopped := paused.StoppedAt.Unix()

	loaded := NewStore(dir).Get("user1")
	if loaded == nil || loaded.Status != StatusPaused || loaded.StoppedAt.Unix() != stopped || loaded.IsTerminal() || !loaded.ComposerClockStopped() {
		t.Fatalf("reloaded = %+v", loaded)
	}
}

func TestComposerClockStopsWhenTheGoalEnds(t *testing.T) {
	s := NewStore("")
	g, err := s.Set("user1", "ship it")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Pause("user1", g.GoalID) {
		t.Fatal("pause")
	}
	pausedAt := s.Get("user1").StoppedAt
	if pausedAt.IsZero() {
		t.Fatal("pause should freeze the composer clock")
	}
	if !s.UpdateStatus("user1", g.GoalID, StatusFailed, "编译失败") {
		t.Fatal("fail")
	}
	stopped := s.Get("user1").StoppedAt
	if !stopped.Equal(pausedAt) {
		t.Fatalf("failure moved the frozen clock from %s to %s", pausedAt, stopped)
	}
	if !s.UpdateObjective("user1", g.GoalID, "改一句说明") {
		t.Fatal("edit")
	}
	edited := s.Get("user1")
	if !edited.StoppedAt.Equal(stopped) {
		t.Fatalf("edit moved the stop from %s to %s", stopped, edited.StoppedAt)
	}
	s.mu.Lock()
	s.goals["user1"].CreatedAt = time.Now().Add(-70 * time.Minute)
	s.goals["user1"].StoppedAt = time.Now().Add(-60 * time.Minute)
	createdAt := s.goals["user1"].CreatedAt
	frozenElapsed := s.goals["user1"].StoppedAt.Unix() - createdAt.Unix()
	s.mu.Unlock()
	if !s.ReleaseForContinue("user1", g.GoalID) {
		t.Fatal("continue")
	}
	continued := s.Get("user1")
	if continued.Status != StatusActive || !continued.StoppedAt.IsZero() {
		t.Fatalf("continue = status %s stop %s", continued.Status, continued.StoppedAt)
	}
	if continued.ClockHoldSeconds < 59*60 || continued.ClockHoldSeconds > 61*60 {
		t.Fatalf("hold = %d", continued.ClockHoldSeconds)
	}
	resumedElapsed := time.Now().Unix() - createdAt.Unix() - continued.ClockHoldSeconds
	if delta := resumedElapsed - frozenElapsed; delta < -1 || delta > 2 {
		t.Fatalf("continue jumped by %d seconds (frozen %d resumed %d)", delta, frozenElapsed, resumedElapsed)
	}
	held := continued.ClockHoldSeconds
	if !s.Pause("user1", g.GoalID) {
		t.Fatal("pause after continue")
	}
	if got := s.Get("user1"); got.ClockHoldSeconds != held || got.StoppedAt.IsZero() {
		t.Fatalf("pause should freeze without adding hold, hold=%d stop=%s", got.ClockHoldSeconds, got.StoppedAt)
	}
	if !s.Resume("user1", g.GoalID) {
		t.Fatal("resume")
	}
	resumed := s.Get("user1")
	if !resumed.StoppedAt.IsZero() || resumed.ClockHoldSeconds < held {
		t.Fatalf("resume = stop %s hold %d, previous hold %d", resumed.StoppedAt, resumed.ClockHoldSeconds, held)
	}
	held = resumed.ClockHoldSeconds

	s.mu.Lock()
	s.goals["user1"].MaxTurns = 1
	s.goals["user1"].TurnsUsed = 0
	s.mu.Unlock()
	if !s.AccountUsage("user1", g.GoalID, 1, 1) {
		t.Fatal("account")
	}
	budget := s.Get("user1")
	if budget.Status != StatusBudgetLimit || budget.StoppedAt.IsZero() || budget.ClockHoldSeconds != held {
		t.Fatalf("budget = status %s stop %s hold %d", budget.Status, budget.StoppedAt, budget.ClockHoldSeconds)
	}

	legacyUpdate := time.Now().Add(-2 * time.Hour)
	s.mu.Lock()
	s.goals["user1"].StoppedAt = time.Time{}
	s.goals["user1"].UpdatedAt = legacyUpdate
	s.goals["user1"].Status = StatusFailed
	s.mu.Unlock()
	if !s.UpdateObjective("user1", g.GoalID, "旧记录上改一句") {
		t.Fatal("legacy edit")
	}
	if legacy := s.Get("user1"); !legacy.StoppedAt.Equal(legacyUpdate) {
		t.Fatalf("legacy stop = %s, want %s", legacy.StoppedAt, legacyUpdate)
	}
}
