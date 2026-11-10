// Package recovery corrects a run's own interaction failures at runtime
// without changing any program: it watches the measured facts of every
// attempt at one goal, notices a failure that repeats, a result that got
// worse than before (regression) or attempts that stop improving, and then
// stops that way of working until the model states a hypothesis, a
// meaningfully different strategy, a minimal test and the facts the test
// must show. The test result, not the model's words, decides whether the
// hypothesis is supported or refuted; only a strategy that then reaches a
// verified outcome becomes a recipe (declarative parameters, never code).
//
// Budgets (attempt count and wall-clock) belong to the goal, not the run, so
// switching methods, renaming files or resuming the task in a new run spend
// the same budget. Everything is persisted per tenant+user.
package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config holds the limits; DefaultConfig is conservative.
type Config struct {
	SameFailureLimit int           // identical failures of one method+strategy before a diagnosis is required
	NoProgressLimit  int           // attempts that do not improve the best facts before a diagnosis is required
	MaxDiagnoses     int           // hypotheses a goal may try
	MaxAttempts      int           // attempts at one goal across strategies, runs and resumes
	MaxWall          time.Duration // time spent in attempts at one goal, across strategies, runs and resumes
	TTL              time.Duration // a goal's ledger is kept this long after its last change (resume)
	RecipeMaxAge     time.Duration // a verified recipe not re-verified for this long is no longer offered
}

func DefaultConfig() Config {
	return Config{
		SameFailureLimit: 2,
		NoProgressLimit:  3,
		MaxDiagnoses:     3,
		MaxAttempts:      10,
		MaxWall:          25 * time.Minute,
		TTL:              12 * time.Hour,
		RecipeMaxAge:     30 * 24 * time.Hour,
	}
}

// ConfigFromEnv overrides DefaultConfig with MACLAW_RECOVERY_* values
// (SAME_FAILURES, NO_PROGRESS, DIAGNOSES, ATTEMPTS, WALL_MIN, TTL_HOURS);
// out-of-range values keep the default.
func ConfigFromEnv(getenv func(string) string) Config {
	c := DefaultConfig()
	num := func(name string, def, min, max int) int {
		v, err := strconv.Atoi(strings.TrimSpace(getenv("MACLAW_RECOVERY_" + name)))
		if err != nil || v < min || v > max {
			return def
		}
		return v
	}
	c.SameFailureLimit = num("SAME_FAILURES", c.SameFailureLimit, 1, 10)
	c.NoProgressLimit = num("NO_PROGRESS", c.NoProgressLimit, 2, 20)
	c.MaxDiagnoses = num("DIAGNOSES", c.MaxDiagnoses, 1, 10)
	c.MaxAttempts = num("ATTEMPTS", c.MaxAttempts, 2, 100)
	c.MaxWall = time.Duration(num("WALL_MIN", int(c.MaxWall/time.Minute), 1, 240)) * time.Minute
	c.TTL = time.Duration(num("TTL_HOURS", int(c.TTL/time.Hour), 1, 24*14)) * time.Hour
	return c
}

// Goal states.
const (
	StateActive    = "active"    // attempts allowed
	StateDiagnose  = "diagnose"  // a hypothesis with a different strategy is required first
	StateTrial     = "trial"     // the registered hypothesis' test is the only allowed attempt
	StateExhausted = "exhausted" // budget or hypotheses used up: stop and report
	StateStopped   = "stopped"   // a consequential side effect is unconfirmed: never retry
	StateDone      = "done"      // verified complete
)

// Verdict kinds of one observation.
const (
	KindProgress   = "progress"
	KindFailure    = "failure"
	KindRepeated   = "repeated_failure"
	KindRegression = "regression"
	KindNoProgress = "no_progress"
	KindSupported  = "hypothesis_supported"
	KindRefuted    = "hypothesis_refuted"
	KindVerified   = "verified"
	KindExhausted  = "budget_exhausted"
	KindStopped    = "stopped_unconfirmed"
)

// Observation is one finished attempt and what it measurably left behind.
type Observation struct {
	Goal     string            // the user's goal key (task + target)
	Surface  string            // where a recipe applies (site/editor/tool), no paths or ids
	Method   string            // tool/method class that made the attempt
	Strategy string            // canonical strategy signature ("" = default way)
	Params   map[string]string // validated declarative strategy parameters, if any
	Recipe   string            // id of the verified recipe the attempt applied, if any
	Run      string
	OK       bool   // the tool itself reported success
	Complete bool   // facts prove the goal is met (independent verification)
	Trial    bool   // a bounded test, not the real attempt
	Side     string // "unconfirmed": a consequential write may have happened without proof
	Error    string // error text (fingerprinted, clipped)
	Facts    map[string]float64
	Took     time.Duration
	At       time.Time
}

// Expectation is one fact the test must show, e.g. pictures_kept>=2.
type Expectation struct {
	Fact  string  `json:"fact"`
	Op    string  `json:"op"`
	Value float64 `json:"value"`
}

var expectRe = regexp.MustCompile(`^\s*([a-zA-Z][a-zA-Z0-9_.@:/-]{0,80})\s*(>=|<=|==|=|>|<)\s*(-?[0-9]+(?:\.[0-9]+)?)\s*$`)

// ParseExpectation parses "name>=2".
func ParseExpectation(s string) (Expectation, error) {
	m := expectRe.FindStringSubmatch(s)
	if m == nil {
		return Expectation{}, fmt.Errorf("expectation %q is not fact<op>number (e.g. pictures_kept>=2)", clip(s, 60))
	}
	v, _ := strconv.ParseFloat(m[3], 64)
	op := m[2]
	if op == "=" {
		op = "=="
	}
	return Expectation{Fact: m[1], Op: op, Value: v}, nil
}

func (e Expectation) String() string { return fmt.Sprintf("%s%s%g", e.Fact, e.Op, e.Value) }

// Met reports whether facts satisfy e (a missing fact never does).
func (e Expectation) Met(f map[string]float64) bool {
	v, ok := f[e.Fact]
	if !ok {
		return false
	}
	switch e.Op {
	case ">=":
		return v >= e.Value
	case "<=":
		return v <= e.Value
	case ">":
		return v > e.Value
	case "<":
		return v < e.Value
	}
	return v == e.Value
}

// Proposal is the model's diagnosis and the strategy it wants to test.
type Proposal struct {
	Hypothesis  string
	Uncertainty string
	Strategy    string            // what will be done differently
	Params      map[string]string // declarative strategy parameters (validated by the caller)
	Test        string            // the minimal safe test
	Expect      []string          // facts the test must show
}

// Hypothesis is a proposal on the goal's record with its tested outcome.
type Hypothesis struct {
	N           int               `json:"n"`
	Text        string            `json:"text"`
	Uncertainty string            `json:"uncertainty,omitempty"`
	Strategy    string            `json:"strategy"`
	Sig         string            `json:"sig,omitempty"` // bound at registration (params) or by the first trial attempt
	Params      map[string]string `json:"params,omitempty"`
	Test        string            `json:"test"`
	Expect      []Expectation     `json:"expect"`
	Outcome     string            `json:"outcome"` // pending, supported, refuted, verified, abandoned
	Evidence    string            `json:"evidence,omitempty"`
	Run         string            `json:"run,omitempty"`
}

// Attempt is the compact record of one observation.
type Attempt struct {
	At       time.Time          `json:"at"`
	Run      string             `json:"run,omitempty"`
	Method   string             `json:"method"`
	Strategy string             `json:"strategy,omitempty"`
	OK       bool               `json:"ok"`
	Complete bool               `json:"complete,omitempty"`
	Trial    bool               `json:"trial,omitempty"`
	Finger   string             `json:"finger,omitempty"`
	Error    string             `json:"error,omitempty"`
	Facts    map[string]float64 `json:"facts,omitempty"`
	TookMS   int64              `json:"took_ms"`
	Verdict  string             `json:"verdict"`
}

// Goal is the persisted ledger of one goal.
type Goal struct {
	Key      string             `json:"key"`
	Scope    string             `json:"-"`
	Surface  string             `json:"surface,omitempty"`
	Created  time.Time          `json:"created"`
	Updated  time.Time          `json:"updated"`
	Runs     []string           `json:"runs,omitempty"`
	State    string             `json:"state"`
	Reason   string             `json:"reason,omitempty"`
	Count    int                `json:"count"`
	SpentMS  int64              `json:"spent_ms"`
	Best     map[string]float64 `json:"best,omitempty"`
	Attempts []Attempt          `json:"attempts,omitempty"`
	Hyps     []Hypothesis       `json:"hyps,omitempty"`
	Failed   map[string]string  `json:"failed,omitempty"` // strategy sig -> why it failed
	LastKind string             `json:"last_kind,omitempty"`
	Evidence string             `json:"evidence,omitempty"` // last compact evidence
	Spec     string             `json:"spec,omitempty"`     // pinned success criteria (never relaxed)
}

const maxAttemptsKept = 24

// Verdict is the engine's answer to one observation.
type Verdict struct {
	Kind     string
	State    string
	Evidence string // compact evidence for the model (and the run log)
	Recipe   *Recipe
}

// Engine applies Config to goals in a Store.
type Engine struct {
	Cfg   Config
	Store *Store
	Now   func() time.Time
}

func New(cfg Config, store *Store) *Engine {
	return &Engine{Cfg: cfg, Store: store, Now: time.Now}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Fingerprint reduces an error to its stable shape (numbers, ids, urls out).
func Fingerprint(method, strategy, errText string) string {
	s := strings.ToLower(errText)
	s = urlRe.ReplaceAllString(s, "<url>")
	s = numRe.ReplaceAllString(s, "#")
	s = strings.Join(strings.Fields(s), " ")
	s = clip(s, 160)
	sum := sha256.Sum256([]byte(method + "\x00" + strategy + "\x00" + s))
	return hex.EncodeToString(sum[:6])
}

var (
	urlRe = regexp.MustCompile(`https?://\S+`)
	numRe = regexp.MustCompile(`[0-9a-f]{6,}|[0-9]+`)
)

// Sig is the canonical signature of declarative strategy parameters.
func Sig(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + params[k] + ";")
	}
	return b.String()
}

// Gate is asked before an attempt at goal with strategy sig: nil allows it.
func (e *Engine) Gate(scope, goal, method, strategy string) error {
	g := e.Store.Goal(scope, goal, e.Cfg.TTL, e.now())
	if g == nil {
		return nil
	}
	if err := e.budgetLeft(g); err != nil {
		return err
	}
	switch g.State {
	case StateStopped:
		return fmt.Errorf("recovery: refused: %s; the earlier attempt may already have taken effect, so it is never repeated — observe the real state and report it", g.Reason)
	case StateExhausted:
		return fmt.Errorf("recovery: refused: this goal is out of recovery budget (%s); stop and report what is done, what is missing and the evidence", g.Reason)
	case StateDiagnose:
		return fmt.Errorf("recovery: refused: %s. Before another attempt call action=recover with hypothesis, uncertainty, strategy (meaningfully different from: %s), test and expect. %s", g.Reason, e.failedList(g), e.budgetText(g))
	case StateTrial:
		h := &g.Hyps[len(g.Hyps)-1]
		if h.Sig != "" && h.Sig != strategy {
			return fmt.Errorf("recovery: refused: hypothesis %d is being tested with strategy %q; run its test (%s) with exactly that strategy first", h.N, h.Sig, h.Test)
		}
		if h.Sig == "" {
			if why, failed := g.Failed[strategy]; failed {
				return fmt.Errorf("recovery: refused: that is the strategy that already failed (%s); hypothesis %d needs a different one", why, h.N)
			}
		}
		return nil
	}
	if why, failed := g.Failed[strategy]; failed && len(g.Hyps) > 0 {
		return fmt.Errorf("recovery: refused: strategy %q already failed for this goal (%s)", strategyLabel(strategy), why)
	}
	return nil
}

func (e *Engine) budgetLeft(g *Goal) error {
	if g.State == StateDone || g.State == StateStopped {
		return nil
	}
	why := ""
	switch {
	case g.Count >= e.Cfg.MaxAttempts:
		why = fmt.Sprintf("%d of %d attempts used", g.Count, e.Cfg.MaxAttempts)
	case time.Duration(g.SpentMS)*time.Millisecond >= e.Cfg.MaxWall:
		why = fmt.Sprintf("%s of %s attempt time used", (time.Duration(g.SpentMS) * time.Millisecond).Round(time.Second), e.Cfg.MaxWall)
	}
	if why == "" {
		return nil
	}
	if g.State != StateExhausted {
		g.State, g.Reason = StateExhausted, why
		e.Store.Put(g)
	}
	return fmt.Errorf("recovery: refused: this goal is out of recovery budget (%s); stop and report what is done, what is missing and the evidence", why)
}

func (e *Engine) budgetText(g *Goal) string {
	left := time.Duration(e.Cfg.MaxWall.Milliseconds()-g.SpentMS) * time.Millisecond
	if left < 0 {
		left = 0
	}
	return fmt.Sprintf("Budget left for this goal: %d attempt(s), %s, %d hypothesis(es).", e.Cfg.MaxAttempts-g.Count, left.Round(time.Second), e.Cfg.MaxDiagnoses-len(g.Hyps))
}

func (e *Engine) failedList(g *Goal) string {
	if len(g.Failed) == 0 {
		return "the default way"
	}
	var parts []string
	for sig, why := range g.Failed {
		parts = append(parts, strategyLabel(sig)+" ("+clip(why, 80)+")")
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func strategyLabel(sig string) string {
	if sig == "" {
		return "default"
	}
	return clip(sig, 80)
}

// Observe records one attempt and decides what it means.
func (e *Engine) Observe(scope string, o Observation) Verdict {
	now := o.At
	if now.IsZero() {
		now = e.now()
	}
	g := e.Store.Goal(scope, o.Goal, e.Cfg.TTL, now)
	if g == nil {
		g = &Goal{Key: o.Goal, Scope: scope, Created: now, State: StateActive}
	}
	g.Scope = scope
	if o.Surface != "" {
		g.Surface = o.Surface
	}
	g.Updated = now
	if o.Run != "" && (len(g.Runs) == 0 || g.Runs[len(g.Runs)-1] != o.Run) {
		g.Runs = append(g.Runs, o.Run)
	}
	g.Count++
	g.SpentMS += o.Took.Milliseconds()
	att := Attempt{At: now, Run: o.Run, Method: o.Method, Strategy: o.Strategy, OK: o.OK, Complete: o.Complete, Trial: o.Trial, Facts: o.Facts, TookMS: o.Took.Milliseconds(), Error: clip(o.Error, 300)}
	if !o.Complete {
		att.Finger = Fingerprint(o.Method, o.Strategy, o.Error)
	}
	prev := lastComparable(g, o.Trial)
	v := Verdict{}
	var hyp *Hypothesis
	if g.State == StateTrial && len(g.Hyps) > 0 {
		hyp = &g.Hyps[len(g.Hyps)-1]
		if hyp.Sig == "" {
			hyp.Sig = o.Strategy
		}
		if hyp.Sig != o.Strategy {
			hyp = nil
		}
	}
	regressed, lost := regression(prev, o)
	switch {
	case o.Side == "unconfirmed":
		g.State, g.Reason = StateStopped, "a consequential write went out without proof of its result"
		v.Kind = KindStopped
	case o.Complete:
		v.Kind = KindVerified
		g.State, g.Reason = StateDone, ""
		if hyp != nil || (o.Strategy != "" && supported(g, o.Strategy)) {
			for i := range g.Hyps {
				if g.Hyps[i].Sig == o.Strategy && g.Hyps[i].Outcome != "refuted" {
					g.Hyps[i].Outcome = "verified"
					g.Hyps[i].Evidence = "goal verified: " + factText(o.Facts)
				}
			}
		}
		if !o.Trial && len(o.Params) > 0 && o.Surface != "" {
			v.Recipe = e.Store.VerifyRecipe(scope, o, now)
		}
	case hyp != nil:
		// The hypothesis' test: its expected facts decide, nothing else.
		var missed []string
		for _, ex := range hyp.Expect {
			if !ex.Met(o.Facts) {
				got := "missing"
				if val, ok := o.Facts[ex.Fact]; ok {
					got = strconv.FormatFloat(val, 'g', -1, 64)
				}
				missed = append(missed, fmt.Sprintf("%s (observed %s)", ex, got))
			}
		}
		if len(missed) == 0 && !regressed {
			hyp.Outcome, hyp.Evidence = "supported", "test showed "+expectText(hyp.Expect)+": "+factText(o.Facts)
			g.State, g.Reason = StateActive, ""
			v.Kind = KindSupported
		} else {
			why := strings.Join(missed, ", ")
			if regressed {
				why = strings.TrimPrefix(why+"; lost "+lost, "; ")
			}
			hyp.Outcome, hyp.Evidence = "refuted", "not shown: "+why
			g.markFailed(o.Strategy, "hypothesis "+strconv.Itoa(hyp.N)+" refuted: "+why)
			g.State, g.Reason = StateDiagnose, "hypothesis "+strconv.Itoa(hyp.N)+" was refuted by its test ("+clip(why, 160)+")"
			v.Kind = KindRefuted
		}
	case !o.OK && o.Strategy != "" && supported(g, o.Strategy):
		// A test can pass at small scale and the real attempt still fail:
		// the hypothesis did not hold, whatever its test showed.
		why := clip(o.Error, 120)
		if regressed {
			why = "lost " + lost
		}
		for i := range g.Hyps {
			if g.Hyps[i].Sig == o.Strategy && g.Hyps[i].Outcome == "supported" {
				g.Hyps[i].Outcome, g.Hyps[i].Evidence = "refuted", "test passed but the real attempt failed: "+why
			}
		}
		g.markFailed(o.Strategy, "real attempt failed after a passing test: "+why)
		g.State, g.Reason = StateDiagnose, "the supported strategy failed in the real attempt ("+why+")"
		v.Kind = KindRefuted
	case regressed:
		g.markFailed(o.Strategy, "lost earlier good result: "+lost)
		g.State, g.Reason = StateDiagnose, "the last attempt destroyed earlier good work ("+lost+")"
		v.Kind = KindRegression
	case !o.OK && sameFailures(g, att.Finger)+1 >= e.Cfg.SameFailureLimit:
		g.markFailed(o.Strategy, "same failure repeated: "+clip(o.Error, 100))
		g.State, g.Reason = StateDiagnose, fmt.Sprintf("the same failure happened %d times with the same method and strategy", sameFailures(g, att.Finger)+1)
		v.Kind = KindRepeated
	case !improves(g.Best, o.Facts) && noProgress(g, e.Cfg.NoProgressLimit-1):
		g.markFailed(o.Strategy, "no measurable progress")
		g.State, g.Reason = StateDiagnose, fmt.Sprintf("%d attempts in a row did not improve the measured result", e.Cfg.NoProgressLimit)
		v.Kind = KindNoProgress
	case !o.OK:
		v.Kind = KindFailure
	default:
		v.Kind = KindProgress
	}
	if o.Recipe != "" && !o.Complete && v.Kind != KindProgress {
		e.Store.FailRecipe(scope, o.Recipe, v.Kind+": "+clip(o.Error, 120), now)
	}
	if improves(g.Best, o.Facts) && !o.Trial && !regressed {
		g.Best = copyFacts(o.Facts)
	}
	att.Verdict = v.Kind
	g.Attempts = append(g.Attempts, att)
	if len(g.Attempts) > maxAttemptsKept {
		g.Attempts = g.Attempts[len(g.Attempts)-maxAttemptsKept:]
	}
	if g.State == StateDiagnose && len(g.Hyps) >= e.Cfg.MaxDiagnoses {
		g.State, g.Reason = StateExhausted, fmt.Sprintf("all %d hypotheses were used and the goal is still not met", e.Cfg.MaxDiagnoses)
		v.Kind = KindExhausted
	}
	if g.State != StateDone && g.State != StateStopped {
		if err := e.budgetLeftNoSave(g); err != "" {
			g.State, g.Reason = StateExhausted, err
			v.Kind = KindExhausted
		}
	}
	g.LastKind = v.Kind
	v.State = g.State
	v.Evidence = e.evidence(g, o, v.Kind, lost)
	g.Evidence = v.Evidence
	e.Store.Put(g)
	return v
}

func (e *Engine) budgetLeftNoSave(g *Goal) string {
	switch {
	case g.Count >= e.Cfg.MaxAttempts:
		return fmt.Sprintf("%d of %d attempts used", g.Count, e.Cfg.MaxAttempts)
	case time.Duration(g.SpentMS)*time.Millisecond >= e.Cfg.MaxWall:
		return fmt.Sprintf("%s of %s attempt time used", (time.Duration(g.SpentMS) * time.Millisecond).Round(time.Second), e.Cfg.MaxWall)
	}
	return ""
}

func (g *Goal) markFailed(sig, why string) {
	if g.Failed == nil {
		g.Failed = map[string]string{}
	}
	g.Failed[sig] = clip(why, 200)
}

func supported(g *Goal, sig string) bool {
	for _, h := range g.Hyps {
		if h.Sig == sig && h.Outcome == "supported" {
			return true
		}
	}
	return false
}

func lastComparable(g *Goal, trial bool) *Attempt {
	for i := len(g.Attempts) - 1; i >= 0; i-- {
		if g.Attempts[i].Trial == trial {
			return &g.Attempts[i]
		}
	}
	return nil
}

// Target facts (what the goal needs) are not measurements.
func isTarget(k string) bool {
	return strings.HasSuffix(k, "_wanted") || strings.HasSuffix(k, "_total") || k == "regression" || strings.HasSuffix(k, "_ms")
}

// regression: the tool said it lost earlier good work, or a measured fact
// fell below the previous comparable attempt's value.
func regression(prev *Attempt, o Observation) (bool, string) {
	if o.Facts["regression"] > 0 {
		lost := "earlier good result"
		if v, ok := o.Facts["lost_item"]; ok {
			lost = fmt.Sprintf("item %g", v)
		}
		return true, lost + " reported lost by the tool"
	}
	if prev == nil || o.Complete {
		return false, ""
	}
	var lost []string
	for k, pv := range prev.Facts {
		if isTarget(k) || k == "lost_item" {
			continue
		}
		if v, ok := o.Facts[k]; ok && v < pv {
			lost = append(lost, fmt.Sprintf("%s %g→%g", k, pv, v))
		}
	}
	sort.Strings(lost)
	if len(lost) == 0 {
		return false, ""
	}
	return true, strings.Join(lost, ", ")
}

func improves(best, f map[string]float64) bool {
	for k, v := range f {
		if isTarget(k) || k == "lost_item" {
			continue
		}
		if b, ok := best[k]; !ok || v > b {
			return true
		}
	}
	return false
}

func sameFailures(g *Goal, finger string) int {
	n := 0
	for i := len(g.Attempts) - 1; i >= 0; i-- {
		a := g.Attempts[i]
		if a.OK || a.Finger != finger {
			break
		}
		n++
	}
	return n
}

// noProgress: the last n attempts (all failed, none improved) — with this one, n+1.
func noProgress(g *Goal, n int) bool {
	if len(g.Attempts) < n || n <= 0 {
		return false
	}
	for _, a := range g.Attempts[len(g.Attempts)-n:] {
		if a.Complete || a.Verdict == KindProgress || a.Verdict == KindSupported {
			return false
		}
	}
	return true
}

// Propose registers the model's diagnosis for a goal that requires one.
// validate checks the declarative params (unknown keys or unsafe values are
// an error); params may be empty when the strategy is a different method
// whose signature binds on its first test attempt.
func (e *Engine) Propose(scope, goal, run string, p Proposal, validate func(map[string]string) error) (string, error) {
	g := e.Store.Goal(scope, goal, e.Cfg.TTL, e.now())
	if g == nil {
		return "", fmt.Errorf("recover: goal %q has no failed attempt on record; just carry on", clip(goal, 80))
	}
	switch g.State {
	case StateDiagnose:
	case StateTrial:
		return "", fmt.Errorf("recover: hypothesis %d is still waiting for its test (%s); run it first", g.Hyps[len(g.Hyps)-1].N, g.Hyps[len(g.Hyps)-1].Test)
	case StateExhausted, StateStopped:
		return "", fmt.Errorf("recover: refused: %s", g.Reason)
	default:
		return "", fmt.Errorf("recover: nothing to diagnose for this goal (state %s); carry on", g.State)
	}
	if len(g.Hyps) >= e.Cfg.MaxDiagnoses {
		g.State, g.Reason = StateExhausted, "no hypotheses left"
		e.Store.Put(g)
		return "", fmt.Errorf("recover: refused: all %d hypotheses were used", e.Cfg.MaxDiagnoses)
	}
	if len([]rune(strings.TrimSpace(p.Hypothesis))) < 12 || strings.TrimSpace(p.Strategy) == "" || strings.TrimSpace(p.Test) == "" {
		return "", fmt.Errorf("recover: hypothesis (a cause you can test), strategy and test are all required")
	}
	if len(p.Expect) == 0 {
		return "", fmt.Errorf("recover: expect needs at least one fact the test must show, e.g. pictures_kept>=2")
	}
	var exp []Expectation
	for _, s := range p.Expect {
		x, err := ParseExpectation(s)
		if err != nil {
			return "", fmt.Errorf("recover: %v", err)
		}
		exp = append(exp, x)
	}
	if validate != nil {
		if err := validate(p.Params); err != nil {
			return "", fmt.Errorf("recover: params: %v", err)
		}
	}
	sig := Sig(p.Params)
	if sig != "" {
		if why, failed := g.Failed[sig]; failed {
			return "", fmt.Errorf("recover: refused: that strategy already failed for this goal (%s); propose a meaningfully different one", why)
		}
		for _, h := range g.Hyps {
			if h.Sig == sig {
				return "", fmt.Errorf("recover: refused: hypothesis %d already tested that exact strategy (%s)", h.N, h.Outcome)
			}
		}
	}
	norm := normText(p.Strategy)
	for _, h := range g.Hyps {
		if normText(h.Strategy) == norm {
			return "", fmt.Errorf("recover: refused: hypothesis %d proposed the same strategy; a new attempt must change what is done", h.N)
		}
	}
	h := Hypothesis{N: len(g.Hyps) + 1, Text: clip(p.Hypothesis, 400), Uncertainty: clip(p.Uncertainty, 200), Strategy: clip(p.Strategy, 300), Sig: sig, Params: p.Params, Test: clip(p.Test, 300), Expect: exp, Outcome: "pending", Run: run}
	g.Hyps = append(g.Hyps, h)
	g.State, g.Reason = StateTrial, ""
	g.Updated = e.now()
	e.Store.Put(g)
	return fmt.Sprintf("Hypothesis %d registered (not a fact until its test shows %s). Now run the test: %s. The facts of that attempt decide supported or refuted. %s", h.N, expectText(exp), h.Test, e.budgetText(g)), nil
}

func normText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// Pin fixes a goal's success criteria the first time they are given; later
// calls must give the same criteria (or none), so a run cannot lower the bar
// after a failure.
func (e *Engine) Pin(scope, goal, spec string) (*Goal, error) {
	now := e.now()
	g := e.Store.Goal(scope, goal, e.Cfg.TTL, now)
	if g == nil {
		g = &Goal{Key: goal, Scope: scope, Created: now, Updated: now, State: StateActive, Spec: spec}
		e.Store.Put(g)
		return g, nil
	}
	if g.Spec == "" && spec != "" {
		g.Spec = spec
		e.Store.Put(g)
	}
	if spec != "" && g.Spec != spec {
		return g, fmt.Errorf("goal_check: the success criteria of %q are pinned (%s); they cannot be changed or relaxed after attempts began", clip(goal, 60), clip(g.Spec, 200))
	}
	return g, nil
}

// Status is the goal's ledger (nil when none).
func (e *Engine) Status(scope, goal string) *Goal {
	return e.Store.Goal(scope, goal, e.Cfg.TTL, e.now())
}

// evidence is the compact structured text the model sees after a verdict.
func (e *Engine) evidence(g *Goal, o Observation, kind, lost string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[recovery] goal=%s verdict=%s state=%s attempt=%d/%d spent=%s/%s", clip(g.Key, 60), kind, g.State, g.Count, e.Cfg.MaxAttempts, (time.Duration(g.SpentMS) * time.Millisecond).Round(time.Second), e.Cfg.MaxWall)
	if len(o.Facts) > 0 {
		b.WriteString("\nobserved: " + factText(o.Facts))
	}
	if len(g.Best) > 0 && kind != KindVerified {
		b.WriteString("\nbest so far: " + factText(g.Best))
	}
	if lost != "" {
		b.WriteString("\nlost: " + lost)
	}
	if o.Error != "" && kind != KindVerified {
		b.WriteString("\nerror: " + clip(strings.ReplaceAll(o.Error, "\n", " "), 220))
	}
	for _, h := range g.Hyps {
		if h.Outcome == "pending" {
			continue
		}
		fmt.Fprintf(&b, "\nhypothesis %d %s: %s — %s", h.N, h.Outcome, clip(h.Text, 120), clip(h.Evidence, 160))
	}
	if len(g.Failed) > 0 && kind != KindVerified {
		b.WriteString("\nfailed strategies: " + e.failedList(g))
	}
	switch g.State {
	case StateDiagnose:
		b.WriteString("\nRECOVERY_REQUIRED: " + g.Reason + ". Do not repeat what failed and do not blame the site without evidence. Diagnose from the facts above, then call action=recover with hypothesis (the cause you suspect), uncertainty (what you are not sure about), strategy (meaningfully different), params (declarative only), test (the smallest safe attempt that would show it) and expect (facts the test must show, e.g. pictures_kept>=2). Never lower the goal, drop required content, or add placeholder content. " + e.budgetText(g))
	case StateExhausted:
		b.WriteString("\nRECOVERY_EXHAUSTED: " + g.Reason + ". Stop working on this goal. Tell the person plainly that it is NOT done, what is done, what is missing and this evidence; do not claim the site forbids it unless the evidence shows that.")
	case StateStopped:
		b.WriteString("\nSTOPPED: " + g.Reason + ". Do not try again; read the real state and report it as unconfirmed.")
	case StateTrial:
		h := g.Hyps[len(g.Hyps)-1]
		b.WriteString(fmt.Sprintf("\nTesting hypothesis %d: %s. %s", h.N, h.Test, e.budgetText(g)))
	}
	return b.String()
}

func expectText(x []Expectation) string {
	var p []string
	for _, e := range x {
		p = append(p, e.String())
	}
	return strings.Join(p, ", ")
}

func factText(f map[string]float64) string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var p []string
	for _, k := range keys {
		p = append(p, k+"="+strconv.FormatFloat(f[k], 'g', -1, 64))
	}
	return strings.Join(p, " ")
}

func copyFacts(f map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// FactsLine formats facts for a tool result.
func FactsLine(f map[string]float64) string {
	if len(f) == 0 {
		return ""
	}
	return "[facts] " + factText(f)
}

// EnvGetenv is os.Getenv (kept here so callers need not import os).
var EnvGetenv = os.Getenv
