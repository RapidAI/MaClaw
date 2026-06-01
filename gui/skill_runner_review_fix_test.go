package main

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// TestGetRunStatus_DeepCopiesSession verifies the P0 fix: GetRunStatus must
// return a Session pointer distinct from the live run.status.Session so that
// post-lock hydrate/summarize mutations (and concurrent callers) don't race on
// or corrupt the shared meta.
func TestGetRunStatus_DeepCopiesSession(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	live := &SkillRunSessionMeta{SessionID: "sess-1", JobID: "job-1"}
	runner.runs["run-1"] = &skillRun{status: SkillRunStatus{
		RunID:   "run-1",
		Skill:   "demo",
		Status:  skillRunStatusRunning,
		Steps:   []StepResult{{Index: 0, Action: "bash", Status: skillStepStatusSuccess}},
		Session: live,
	}}

	status, err := runner.GetRunStatus("run-1")
	if err != nil {
		t.Fatalf("GetRunStatus() error = %v", err)
	}
	if status.Session == nil {
		t.Fatal("expected Session in snapshot")
	}
	if status.Session == live {
		t.Fatal("snapshot Session must not alias the live run.status.Session pointer")
	}
	// Mutating the snapshot must not leak back into the stored run.
	status.Session.JobID = "mutated"
	if live.JobID != "job-1" {
		t.Fatalf("live Session.JobID was mutated through snapshot: %q", live.JobID)
	}
}

// TestGetRunStatus_ConcurrentNoRace exercises the P0 race path: many concurrent
// GetRunStatus calls (each hydrating/summarizing the Session outside the lock)
// must not race. Run with -race to validate.
func TestGetRunStatus_ConcurrentNoRace(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	runner.runs["run-1"] = &skillRun{status: SkillRunStatus{
		RunID:   "run-1",
		Skill:   "demo",
		Status:  skillRunStatusRunning,
		Steps:   []StepResult{{Index: 0, Action: "bash", Status: skillStepStatusSuccess}},
		Session: &SkillRunSessionMeta{SessionID: "sess-1"},
		SessionProgress: &SessionProgressInfo{
			SessionStatus:   SessionRunning,
			LastOutputLines: []string{"line-1", "line-2"},
		},
	}}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := runner.GetRunStatus("run-1"); err != nil {
				t.Errorf("GetRunStatus() error = %v", err)
			}
			_ = runner.ListRuns()
		}()
	}
	wg.Wait()
}

// TestListRuns_DeepCopiesSessionAndProgress verifies ListRuns shares the same
// deep-copy path as GetRunStatus (the original race came from ListRuns missing
// the SessionProgress/Session deep copy that GetRunStatus had).
func TestListRuns_DeepCopiesSessionAndProgress(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	liveSess := &SkillRunSessionMeta{SessionID: "sess-1", JobID: "job-1"}
	liveLines := []string{"a", "b"}
	runner.runs["run-1"] = &skillRun{status: SkillRunStatus{
		RunID:           "run-1",
		Skill:           "demo",
		Status:          skillRunStatusRunning,
		Steps:           []StepResult{{Index: 0, Action: "bash", Status: skillStepStatusSuccess}},
		Session:         liveSess,
		SessionProgress: &SessionProgressInfo{SessionStatus: SessionRunning, LastOutputLines: liveLines},
	}}

	runs := runner.ListRuns()
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Session == liveSess {
		t.Fatal("ListRuns Session aliases live pointer")
	}
	if len(runs[0].SessionProgress.LastOutputLines) > 0 &&
		&runs[0].SessionProgress.LastOutputLines[0] == &liveLines[0] {
		t.Fatal("ListRuns LastOutputLines aliases live slice backing array")
	}
	// Mutating the snapshot must not affect the stored run.
	runs[0].Session.JobID = "mutated"
	if liveSess.JobID != "job-1" {
		t.Fatalf("live Session mutated through ListRuns snapshot: %q", liveSess.JobID)
	}
}

// helper never splits a multi-byte (Chinese) rune into invalid UTF-8.
func TestTruncateRunesWithEllipsis_NoInvalidUTF8(t *testing.T) {
	// 200 Chinese runes (3 bytes each); truncating at a byte boundary would
	// split the rune straddling the cut point.
	long := strings.Repeat("中", 200)
	out := truncateRunesWithEllipsis(long, 160)
	if !utf8.ValidString(out) {
		t.Fatal("truncated output is not valid UTF-8")
	}
	if !strings.HasSuffix(out, "...") {
		t.Fatalf("expected ellipsis suffix, got %q", out)
	}
	if got := utf8.RuneCountInString(strings.TrimSuffix(out, "...")); got != 160 {
		t.Fatalf("expected 160 runes before ellipsis, got %d", got)
	}
}

func TestTruncateRunesWithEllipsis_ShortStringUnchanged(t *testing.T) {
	if out := truncateRunesWithEllipsis("短文本", 160); out != "短文本" {
		t.Fatalf("short string should be unchanged, got %q", out)
	}
	if out := truncateRunesWithEllipsis("", 160); out != "" {
		t.Fatalf("empty string should stay empty, got %q", out)
	}
}

func TestTruncateSkillRunSnippet_RuneSafe(t *testing.T) {
	long := strings.Repeat("数", 300)
	out := truncateSkillRunSnippet(long)
	if !utf8.ValidString(out) {
		t.Fatal("snippet is not valid UTF-8")
	}
}

// TestTruncateRunesMarker_BoundaryAndMarker verifies the single-pass
// byte-offset truncation: exact-length input is unchanged (no marker), and
// over-length input is cut on a rune boundary with the supplied marker.
func TestTruncateRunesMarker_BoundaryAndMarker(t *testing.T) {
	// Exactly maxRunes: no truncation, no marker.
	if got := truncateRunesMarker("中文", 2, "X"); got != "中文" {
		t.Fatalf("exact-length input mutated: %q", got)
	}
	// One over: truncate to maxRunes runes + marker, still valid UTF-8.
	got := truncateRunesMarker("中文字", 2, "...")
	if got != "中文..." {
		t.Fatalf("expected 中文..., got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncated output not valid UTF-8")
	}
	// Custom marker preserved.
	if got := truncateRunesMarker(strings.Repeat("a", 10), 4, "[cut]"); got != "aaaa[cut]" {
		t.Fatalf("custom marker not applied: %q", got)
	}
	// maxRunes <= 0 yields empty.
	if got := truncateRunesMarker("abc", 0, "..."); got != "" {
		t.Fatalf("maxRunes=0 should yield empty, got %q", got)
	}
}

// TestPollActionDoesNotUseManagedProcessEnv verifies the P1 fix: poll steps
// must not pin the global os.Setenv mutex across the poll loop.
func TestPollActionDoesNotUseManagedProcessEnv(t *testing.T) {
	if skillStepActionPoll.UsesManagedProcessEnv() {
		t.Fatal("poll action must not use managed (global os.Setenv) process env")
	}
	// installSkillStepProcessEnv must be a no-op for poll even with env present.
	restore := installSkillStepProcessEnv("poll", map[string]string{"FOO": "bar"})
	if restore == nil {
		t.Fatal("expected non-nil restore func")
	}
	restore()
}
