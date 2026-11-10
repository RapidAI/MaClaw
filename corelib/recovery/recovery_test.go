package recovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newEngine(t *testing.T, cfg Config) (*Engine, *clock) {
	c := &clock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	e := New(cfg, NewStore(t.TempDir()))
	e.Now = c.now
	return e, c
}

const sc = "tenant\x00alice"

func obs(goal, strategy string, ok bool, err string, facts map[string]float64) Observation {
	return Observation{Goal: goal, Surface: "site:x|publish", Method: "publish", Strategy: strategy, OK: ok, Error: err, Facts: facts, Took: time.Minute, Run: "run1"}
}

func allowAll(map[string]string) error { return nil }

func TestRepeatedSameFailureRequiresDiagnosisAndDifferentStrategy(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	v := e.Observe(sc, obs("g", "", false, "upload 3 failed after 12000ms", nil))
	if v.Kind != KindFailure || v.State != StateActive {
		t.Fatalf("first failure: %+v", v)
	}
	v = e.Observe(sc, obs("g", "", false, "upload 4 failed after 9000ms", nil))
	if v.Kind != KindRepeated || v.State != StateDiagnose || !strings.Contains(v.Evidence, "RECOVERY_REQUIRED") {
		t.Fatalf("repeat not detected: %+v", v)
	}
	if err := e.Gate(sc, "g", "publish", ""); err == nil || !strings.Contains(err.Error(), "action=recover") {
		t.Fatalf("gate let the failed way run again: %v", err)
	}
	// Same strategy text / params as what failed are refused.
	if _, err := e.Propose(sc, "g", "run1", Proposal{Hypothesis: "the upload times out on big files", Strategy: "x", Test: "t", Expect: []string{"a>=1"}}, allowAll); err != nil {
		t.Fatal(err)
	}
}

func TestRegressionFromToolFactAndFromMeasuredDrop(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	v := e.Observe(sc, obs("g", "", false, "lost", map[string]float64{"pictures_kept": 0, "regression": 1, "lost_item": 1}))
	if v.Kind != KindRegression || v.State != StateDiagnose || !strings.Contains(v.Evidence, "lost: item 1") {
		t.Fatalf("%+v", v)
	}
	e2, _ := newEngine(t, DefaultConfig())
	e2.Observe(sc, obs("h", "", false, "rows short", map[string]float64{"rows": 80}))
	v = e2.Observe(sc, obs("h", "", false, "rows short", map[string]float64{"rows": 10}))
	if v.Kind != KindRegression || !strings.Contains(v.Evidence, "rows 80→10") || !strings.Contains(v.Evidence, "best so far: rows=80") {
		t.Fatalf("%+v", v)
	}
}

func TestNoProgressStopsAfterLimit(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	for i, err := range []string{"a different error", "another one", "third kind"} {
		v := e.Observe(sc, obs("g", "", false, err, map[string]float64{"rows": 5}))
		if i < 2 && v.State != StateActive {
			t.Fatalf("attempt %d: %+v", i, v)
		}
		if i == 2 && v.Kind != KindNoProgress {
			t.Fatalf("no-progress not detected: %+v", v)
		}
	}
}

func TestHypothesisRefutedByItsTestThenSupportedThenVerifiedRecipe(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	e.Observe(sc, obs("g", "", false, "lost", map[string]float64{"pictures_kept": 0, "regression": 1, "lost_item": 1}))
	p1 := map[string]string{"insert_settle_ms": "3000"}
	if _, err := e.Propose(sc, "g", "run1", Proposal{Hypothesis: "the editor is still busy saving when the next picture comes", Strategy: "wait 3s before each picture", Params: p1, Test: "trial with 2 pictures", Expect: []string{"pictures_kept>=2"}}, allowAll); err != nil {
		t.Fatal(err)
	}
	if err := e.Gate(sc, "g", "publish", ""); err == nil {
		t.Fatal("trial state let a different strategy run")
	}
	o := obs("g", Sig(p1), true, "", map[string]float64{"pictures_kept": 1, "pictures_wanted": 2, "trial": 1})
	o.Trial = true
	v := e.Observe(sc, o)
	if v.Kind != KindRefuted || v.State != StateDiagnose || !strings.Contains(v.Evidence, "hypothesis 1 refuted") {
		t.Fatalf("refutation: %+v", v)
	}
	// A refuted strategy cannot come back.
	if _, err := e.Propose(sc, "g", "run1", Proposal{Hypothesis: "again the editor is busy saving", Strategy: "wait longer", Params: p1, Test: "trial", Expect: []string{"pictures_kept>=2"}}, allowAll); err == nil {
		t.Fatal("refuted params accepted again")
	}
	p2 := map[string]string{"insert_prep_keys": "ctrl+End,Enter"}
	if _, err := e.Propose(sc, "g", "run1", Proposal{Hypothesis: "the editor inserts at its own remembered caret, which sits on the last picture", Strategy: "move the caret with native keys first", Params: p2, Test: "trial with 2 pictures", Expect: []string{"pictures_kept>=2"}}, allowAll); err != nil {
		t.Fatal(err)
	}
	o = obs("g", Sig(p2), true, "", map[string]float64{"pictures_kept": 2, "pictures_wanted": 2, "trial": 1})
	o.Trial = true
	if v = e.Observe(sc, o); v.Kind != KindSupported || v.State != StateActive {
		t.Fatalf("support: %+v", v)
	}
	if len(e.Store.AllRecipes(sc)) != 0 {
		t.Fatal("a supported test became a recipe before verification")
	}
	o = obs("g", Sig(p2), true, "", map[string]float64{"pictures_kept": 4, "pictures_wanted": 4, "published": 1})
	o.Params, o.Complete = p2, true
	v = e.Observe(sc, o)
	if v.Kind != KindVerified || v.Recipe == nil || v.Recipe.Params["insert_prep_keys"] != "ctrl+End,Enter" {
		t.Fatalf("verify: %+v", v)
	}
	g := e.Status(sc, "g")
	if g.Hyps[0].Outcome != "refuted" || g.Hyps[1].Outcome != "verified" {
		t.Fatalf("hyps %+v", g.Hyps)
	}
	for _, r := range e.Store.AllRecipes(sc) {
		for k := range r.Params {
			if k != "insert_prep_keys" {
				t.Fatalf("recipe holds %s", k)
			}
		}
	}
}

func TestSupportedTestButRealAttemptFailsIsRefuted(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	e.Observe(sc, obs("g", "", false, "lost", map[string]float64{"regression": 1}))
	p := map[string]string{"insert_settle_ms": "2000"}
	_, _ = e.Propose(sc, "g", "r", Proposal{Hypothesis: "a slow save drops the picture", Strategy: "settle", Params: p, Test: "trial", Expect: []string{"pictures_kept>=1"}}, allowAll)
	o := obs("g", Sig(p), true, "", map[string]float64{"pictures_kept": 1, "trial": 1})
	o.Trial = true
	e.Observe(sc, o)
	v := e.Observe(sc, obs("g", Sig(p), false, "picture 3 lost", map[string]float64{"pictures_kept": 2, "regression": 1}))
	if v.Kind != KindRefuted || e.Status(sc, "g").Hyps[0].Outcome != "refuted" {
		t.Fatalf("%+v", v)
	}
}

func TestBudgetsAcrossStrategiesRunsAndResume(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAttempts = 4
	cfg.MaxWall = 10 * time.Minute
	dir := t.TempDir()
	c := &clock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	mk := func() *Engine { e := New(cfg, NewStore(dir)); e.Now = c.now; return e }
	e := mk()
	o := obs("g", "", false, "e1", map[string]float64{"rows": 1})
	o.Took = 4 * time.Minute
	e.Observe(sc, o)
	o = obs("g", "a=1;", false, "e2", map[string]float64{"rows": 2})
	o.Took = 4 * time.Minute
	e.Observe(sc, o)
	// A new process/run (resume) shares the same budget.
	c.t = c.t.Add(time.Hour)
	e = mk()
	o = obs("g", "b=2;", false, "e3", map[string]float64{"rows": 3})
	o.Took, o.Run = 3*time.Minute, "run2"
	v := e.Observe(sc, o)
	if v.Kind != KindExhausted || !strings.Contains(v.Evidence, "RECOVERY_EXHAUSTED") {
		t.Fatalf("wall budget across runs: %+v", v)
	}
	if err := e.Gate(sc, "g", "other", "c=3;"); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("switching method escaped the budget: %v", err)
	}
	// Count budget.
	e2, _ := newEngine(t, cfg)
	for i := 0; i < 4; i++ {
		e2.Observe(sc, obs("k", "", true, "", map[string]float64{"n": float64(i + 1)}))
	}
	if err := e2.Gate(sc, "k", "publish", ""); err == nil {
		t.Fatal("count budget not enforced")
	}
	// TTL: an old ledger is not resumed.
	c.t = c.t.Add(cfg.TTL + time.Hour)
	if err := e.Gate(sc, "g", "publish", ""); err != nil {
		t.Fatalf("expired ledger still blocks: %v", err)
	}
}

func TestUnconfirmedSideEffectStopsForGood(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	o := obs("g", "", false, "no evidence after submit", nil)
	o.Side = "unconfirmed"
	if v := e.Observe(sc, o); v.State != StateStopped {
		t.Fatalf("%+v", v)
	}
	if err := e.Gate(sc, "g", "publish", "x=1;"); err == nil || !strings.Contains(err.Error(), "never repeated") {
		t.Fatalf("retry allowed after unconfirmed submit: %v", err)
	}
	if _, err := e.Propose(sc, "g", "r", Proposal{Hypothesis: "maybe it did not go out", Strategy: "s", Test: "t", Expect: []string{"a>=1"}}, allowAll); err == nil {
		t.Fatal("recover allowed after unconfirmed submit")
	}
}

func TestTenantIsolationAndStaleOrFailedRecipes(t *testing.T) {
	e, c := newEngine(t, DefaultConfig())
	p := map[string]string{"insert_prep_keys": "End"}
	o := obs("g", Sig(p), true, "", map[string]float64{"published": 1})
	o.Params, o.Complete = p, true
	e.Observe(sc, o)
	if n := len(e.Store.Recipes(sc, "site:x|publish", "publish", e.Cfg.RecipeMaxAge, c.t)); n != 1 {
		t.Fatalf("recipes %d", n)
	}
	if n := len(e.Store.Recipes("tenant\x00bob", "site:x|publish", "publish", 0, c.t)); n != 0 {
		t.Fatal("another user sees the recipe")
	}
	if n := len(e.Store.Recipes("other\x00alice", "site:x|publish", "publish", 0, c.t)); n != 0 {
		t.Fatal("another tenant sees the recipe")
	}
	files, _ := filepath.Glob(filepath.Join(e.Store.Root, "*.json"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		if strings.Contains(string(raw), "alice") {
			t.Fatal("scope name written in clear")
		}
	}
	// Stale: not offered after RecipeMaxAge.
	if n := len(e.Store.Recipes(sc, "site:x|publish", "publish", e.Cfg.RecipeMaxAge, c.t.Add(e.Cfg.RecipeMaxAge+time.Hour))); n != 0 {
		t.Fatal("stale recipe offered")
	}
	// Failed when applied: invalidated.
	r := e.Store.Recipes(sc, "site:x|publish", "publish", 0, c.t)[0]
	o = obs("g2", Sig(p), false, "lost picture", map[string]float64{"regression": 1})
	o.Recipe = r.ID
	e.Observe(sc, o)
	if n := len(e.Store.Recipes(sc, "site:x|publish", "publish", 0, c.t)); n != 0 {
		t.Fatal("failed recipe still offered")
	}
	if all := e.Store.AllRecipes(sc); all[0].Invalid == "" || all[0].Failed != 1 {
		t.Fatalf("no failure provenance: %+v", all[0])
	}
}

func TestProposalValidation(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	if _, err := e.Propose(sc, "none", "r", Proposal{}, allowAll); err == nil {
		t.Fatal("proposal on a goal without failure accepted")
	}
	e.Observe(sc, obs("g", "", false, "x", map[string]float64{"regression": 1}))
	bad := []Proposal{
		{Hypothesis: "short", Strategy: "s", Test: "t", Expect: []string{"a>=1"}},
		{Hypothesis: "a real hypothesis here", Strategy: "s", Test: "t"},
		{Hypothesis: "a real hypothesis here", Strategy: "s", Test: "t", Expect: []string{"looks good"}},
		{Hypothesis: "a real hypothesis here", Strategy: "s", Test: "t", Expect: []string{"a>=1"}, Params: map[string]string{"cmd": "rm -rf /"}},
	}
	for i, p := range bad {
		_, err := e.Propose(sc, "g", "r", p, func(m map[string]string) error {
			if _, ok := m["cmd"]; ok {
				return os.ErrPermission
			}
			return nil
		})
		if err == nil {
			t.Fatalf("bad proposal %d accepted", i)
		}
	}
	if _, err := e.Pin(sc, "c", "rows>=100"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pin(sc, "c", "rows>=10"); err == nil {
		t.Fatal("criteria relaxed")
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{"MACLAW_RECOVERY_ATTEMPTS": "6", "MACLAW_RECOVERY_WALL_MIN": "0", "MACLAW_RECOVERY_SAME_FAILURES": "x"}
	c := ConfigFromEnv(func(k string) string { return env[k] })
	if c.MaxAttempts != 6 || c.MaxWall != 25*time.Minute || c.SameFailureLimit != 2 {
		t.Fatalf("%+v", c)
	}
}
