package recovery

import (
	"strings"
	"testing"
	"time"
)

// run_854d0758 (2026-11-10): a CDP Page.navigate timeout during a
// hypothesis' test refuted the hypothesis, and a test that did not report the
// expected fact refuted it too. Neither is evidence about the strategy.

func toTrial(t *testing.T, e *Engine, goal string, expect string) {
	t.Helper()
	e.Observe(sc, obs(goal, "", false, "pictures lost", map[string]float64{"pictures_kept": 0, "regression": 1, "lost_item": 1}))
	if g := e.Status(sc, goal); g == nil || g.State != StateDiagnose {
		t.Fatalf("setup: %+v", g)
	}
	if _, err := e.Propose(sc, goal, "run1", Proposal{Hypothesis: "uploads race the autosave", Uncertainty: "timing", Strategy: "wait longer between pictures", Params: map[string]string{"insert_settle_ms": "12000"}, Test: "trial_pictures=3", Expect: []string{expect}}, allowAll); err != nil {
		t.Fatal(err)
	}
	if g := e.Status(sc, goal); g.State != StateTrial {
		t.Fatalf("not in trial: %+v", g)
	}
}

func TestInfraTimeoutNeverRefutesAHypothesisAndBlocksAfterLimit(t *testing.T) {
	e, c := newEngine(t, DefaultConfig())
	toTrial(t, e, "g", "pictures_kept>=3")
	o := obs("g", "insert_settle_ms=12000;", false, "publish step observe failed: could not open the editor: cdp timeout: Page.navigate", nil)
	o.Trial, o.Infra = true, true
	v := e.Observe(sc, o)
	g := e.Status(sc, "g")
	if v.Kind != KindInfra || g.State != StateTrial || g.Hyps[0].Outcome != "pending" || len(g.Failed) != 1 {
		t.Fatalf("infra failure changed the diagnosis: %+v %+v", v, g)
	}
	if !strings.Contains(v.Evidence, "nothing was refuted") {
		t.Fatalf("evidence: %s", v.Evidence)
	}
	countBefore := g.Count
	v = e.Observe(sc, o)
	g = e.Status(sc, "g")
	if v.Kind != KindInfra || g.State != StateBlocked || g.Hyps[0].Outcome != "pending" || !strings.Contains(v.Evidence, "RECOVERY_BLOCKED") {
		t.Fatalf("second infra failure did not block: %+v %+v", v, g)
	}
	if g.Count != countBefore {
		t.Fatalf("infra attempts were counted as strategy attempts: %d -> %d", countBefore, g.Count)
	}
	if err := e.Gate(sc, "g", "publish", "insert_settle_ms=12000;"); err == nil || !strings.Contains(err.Error(), "BLOCKED") || !strings.Contains(err.Error(), "request_takeover") {
		t.Fatalf("blocked goal admitted an attempt: %v", err)
	}
	c.t = c.t.Add(11 * time.Minute)
	if err := e.Gate(sc, "g", "publish", "insert_settle_ms=12000;"); err != nil {
		t.Fatalf("after the cooldown the pending test must be allowed again: %v", err)
	}
	if g = e.Status(sc, "g"); g.State != StateTrial || g.InfraFails != 0 {
		t.Fatalf("did not resume the trial: %+v", g)
	}
}

func TestTestWithoutTheExpectedFactIsInconclusiveNotRefuted(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	toTrial(t, e, "g", "pictures_kept>=3")
	o := obs("g", "insert_settle_ms=12000;", true, "", map[string]float64{"trial": 1})
	o.Trial = true
	v := e.Observe(sc, o)
	g := e.Status(sc, "g")
	if v.Kind != KindInconclusive || g.Hyps[0].Outcome != "inconclusive" || g.State != StateDiagnose {
		t.Fatalf("%+v %+v", v, g)
	}
	if _, failed := g.Failed["insert_settle_ms=12000;"]; failed {
		t.Fatalf("an unmeasured strategy was marked failed: %v", g.Failed)
	}
	if !strings.Contains(g.Reason, "trial=1") {
		t.Fatalf("reason should name what the test reported: %s", g.Reason)
	}
	// A measured miss still refutes.
	e2, _ := newEngine(t, DefaultConfig())
	toTrial(t, e2, "h", "pictures_kept>=3")
	o = obs("h", "insert_settle_ms=12000;", true, "", map[string]float64{"trial": 1, "pictures_kept": 1})
	o.Trial = true
	if v := e2.Observe(sc, o); v.Kind != KindRefuted {
		t.Fatalf("measured miss: %+v", v)
	}
}

func TestInfraFailuresDoNotCountAsTheSameFailureOrNoProgress(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	e.Observe(sc, obs("g", "", false, "upload 3 failed", nil))
	inf := obs("g", "", false, "cdp timeout: Page.navigate", nil)
	inf.Infra = true
	e.Observe(sc, inf)
	if g := e.Status(sc, "g"); g.State != StateActive {
		t.Fatalf("one real failure + one infra failure must not require a diagnosis: %+v", g)
	}
	// A real result resets the infra streak.
	e.Observe(sc, obs("g", "", true, "", map[string]float64{"pictures_kept": 1}))
	e.Observe(sc, inf)
	if g := e.Status(sc, "g"); g.State == StateBlocked || g.InfraFails != 1 {
		t.Fatalf("streak not reset: %+v", g)
	}
}
