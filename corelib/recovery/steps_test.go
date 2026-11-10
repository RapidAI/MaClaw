package recovery

import (
	"sync"
	"testing"
	"time"
)

func pinned(t *testing.T, e *Engine, scope, key string) {
	t.Helper()
	if _, err := e.Pin(scope, key, `[{"kind":"exists","path":"/x"}]`); err != nil {
		t.Fatal(err)
	}
}

// A step's reservation is in the ledger before the step runs: a process that
// dies (or restarts) before the next measurement loses nothing, and the
// reload sees the same attempt time left — never a reset.
func TestStepReservationSurvivesACrashAndReload(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}
	cfg := DefaultConfig()
	mk := func() *Engine { e := New(cfg, NewStore(dir)); e.Now = c.now; return e }
	e := mk()
	pinned(t, e, sc, "check|L|orders")
	id, budget, err := e.BeginStep(sc, "check|L|orders", "run1", "", 2*time.Minute)
	if err != nil || id == "" || budget != 2*time.Minute {
		t.Fatalf("begin: id=%q budget=%s err=%v", id, budget, err)
	}
	leftBefore := e.WallLeft(e.Status(sc, "check|L|orders"))
	// "Crash": the step never ends. A new process reads the same ledger.
	c.t = c.t.Add(30 * time.Second)
	r := mk()
	g := r.Status(sc, "check|L|orders")
	if g.Count != 1 || !g.Open || g.SpentMS != (2*time.Minute).Milliseconds() || len(g.Steps) != 1 {
		t.Fatalf("after reload: count=%d open=%v spent=%d steps=%d", g.Count, g.Open, g.SpentMS, len(g.Steps))
	}
	if left := r.WallLeft(g); left > leftBefore {
		t.Fatalf("attempt time grew after a restart: %s > %s", left, leftBefore)
	}
	// The dead step's deadline passes: it is pruned, its charge stays.
	c.t = c.t.Add(2*time.Minute + cfg.LeaseTTL)
	id2, _, err := r.BeginStep(sc, "check|L|orders", "run2", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	g = r.Status(sc, "check|L|orders")
	if len(g.Steps) != 1 || g.Steps[0].ID != id2 || g.SpentMS != (3*time.Minute).Milliseconds() || g.Count != 1 {
		t.Fatalf("after prune: steps=%v spent=%d count=%d", g.Steps, g.SpentMS, g.Count)
	}
	// A late settle of the pruned step adds only its overrun.
	r.EndStep(sc, "check|L|orders", id, 2*time.Minute, 2*time.Minute+10*time.Second)
	if g = r.Status(sc, "check|L|orders"); g.SpentMS != (3*time.Minute + 10*time.Second).Milliseconds() {
		t.Fatalf("late settle of a pruned step: spent=%d", g.SpentMS)
	}
}

// Settling refunds the unused reservation and charges an overrun; the next
// measurement closes the attempt without counting it twice.
func TestStepSettlesAndCountsOneAttemptPerMeasurement(t *testing.T) {
	e, c := newEngine(t, DefaultConfig())
	key := "check|L|csv"
	pinned(t, e, sc, key)
	for i := 0; i < 3; i++ { // three steps, one attempt
		id, budget, err := e.BeginStep(sc, key, "run1", "", 90*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(2 * time.Second)
		e.EndStep(sc, key, id, budget, 2*time.Second)
	}
	g := e.Status(sc, key)
	if g.Count != 1 || !g.Open || g.SpentMS != 6000 || len(g.Steps) != 0 {
		t.Fatalf("count=%d open=%v spent=%d steps=%d", g.Count, g.Open, g.SpentMS, len(g.Steps))
	}
	v := e.Observe(sc, Observation{Goal: key, Method: "check", Facts: map[string]float64{"rows": 1}, Error: "not met"})
	g = e.Status(sc, key)
	if g.Count != 1 || g.Open || g.SpentMS != 6000 {
		t.Fatalf("measurement double counted: count=%d open=%v spent=%d (%s)", g.Count, g.Open, g.SpentMS, v.Kind)
	}
	// A timed-out step whose result arrives late is charged its real time.
	id, budget, err := e.BeginStep(sc, key, "run1", "", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	e.EndStep(sc, key, id, budget, 45*time.Second)
	e.EndStep(sc, key, "unknown", 0, time.Hour) // nothing reserved: only an overrun counts
	g = e.Status(sc, key)
	if g.Count != 2 || g.SpentMS != 6000+45000+3600000 {
		t.Fatalf("late result: count=%d spent=%d", g.Count, g.SpentMS)
	}
	// Cancelled step that did nothing: settling at 0 refunds it.
	e2, _ := newEngine(t, DefaultConfig())
	pinned(t, e2, sc, key)
	id, budget, _ = e2.BeginStep(sc, key, "r", "", time.Minute)
	e2.EndStep(sc, key, id, budget, 0)
	if g = e2.Status(sc, key); g.SpentMS != 0 || len(g.Steps) != 0 {
		t.Fatalf("cancel: spent=%d steps=%d", g.SpentMS, len(g.Steps))
	}
}

// The step deadline is the attempt time left; out of time (or out of
// attempts after the last measurement) refuses the step.
func TestStepBudgetBoundsAndRefuses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAttempts, cfg.MaxWall, cfg.MinLeft = 2, 3*time.Minute, 10*time.Second
	e, _ := newEngine(t, cfg)
	key := "check|L|f"
	pinned(t, e, sc, key)
	id, budget, err := e.BeginStep(sc, key, "r", "", 15*time.Minute)
	if err != nil || budget != 3*time.Minute {
		t.Fatalf("budget %s err %v", budget, err)
	}
	if _, _, err := e.BeginStep(sc, key, "r", "", time.Minute); err == nil {
		t.Fatal("a second step started with no attempt time left")
	}
	e.EndStep(sc, key, id, budget, time.Minute)
	if _, b, err := e.BeginStep(sc, key, "r", "", 15*time.Minute); err != nil || b != 2*time.Minute {
		t.Fatalf("after settle: budget %s err %v", b, err)
	} else {
		e.Observe(sc, Observation{Goal: key, Method: "check", Error: "not met"})
	}
	// Count is 1 (one attempt, measured). Attempt 2 may take several steps.
	g := e.Status(sc, key)
	if g.Count != 1 {
		t.Fatalf("count %d", g.Count)
	}
}

func TestStepAttemptLimitAllowsTheLastAttemptsSteps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAttempts = 2
	e, _ := newEngine(t, cfg)
	key := "check|L|g"
	pinned(t, e, sc, key)
	step := func() error {
		id, b, err := e.BeginStep(sc, key, "r", "", time.Second)
		e.EndStep(sc, key, id, b, time.Millisecond)
		return err
	}
	for round := 0; round < 2; round++ {
		for i := 0; i < 3; i++ {
			if err := step(); err != nil {
				t.Fatalf("round %d step %d: %v", round, i, err)
			}
		}
		e.Observe(sc, Observation{Goal: key, Method: "check", Facts: map[string]float64{"rows": float64(round)}, Error: "not met"})
	}
	if err := step(); err == nil {
		t.Fatal("a step ran after the last attempt was measured")
	}
	if g := e.Status(sc, key); g.Count != 2 || g.State != StateExhausted {
		t.Fatalf("count=%d state=%s", g.Count, g.State)
	}
}

// Separate stores on one root (separate processes) charging steps of one
// goal concurrently: every millisecond is charged exactly once, one attempt.
// Another tenant's goal of the same key is untouched.
func TestStepsAreChargedOnceAcrossStores(t *testing.T) {
	root := t.TempDir()
	key := "check|L|h"
	other := "tenant\x00bob"
	e0 := New(DefaultConfig(), NewStore(root))
	pinned(t, e0, sc, key)
	pinned(t, e0, other, key)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := New(DefaultConfig(), NewStore(root))
			for j := 0; j < 5; j++ {
				id, b, err := e.BeginStep(sc, key, "r", "", time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				e.EndStep(sc, key, id, b, 100*time.Millisecond)
			}
		}()
	}
	wg.Wait()
	g := New(DefaultConfig(), NewStore(root)).Status(sc, key)
	if g.SpentMS != 40*100 || g.Count != 1 || len(g.Steps) != 0 {
		t.Fatalf("spent=%d count=%d steps=%d", g.SpentMS, g.Count, len(g.Steps))
	}
	if b := e0.Status(other, key); b.SpentMS != 0 || b.Count != 0 {
		t.Fatalf("other tenant charged: %+v", b)
	}
}

// A goal with a step in flight does not expire under it.
func TestGoalWithAStepInFlightDoesNotExpire(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTL = time.Hour
	e, c := newEngine(t, cfg)
	key := "check|L|i"
	pinned(t, e, sc, key)
	if _, _, err := e.BeginStep(sc, key, "r", "", time.Minute); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(2 * time.Hour)
	if g := e.Status(sc, key); g == nil || g.SpentMS != 60000 {
		t.Fatalf("goal with a step in flight expired: %+v", g)
	}
}
