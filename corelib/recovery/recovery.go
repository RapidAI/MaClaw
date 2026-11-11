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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	MaxSpan          time.Duration // elapsed time from a goal's first attempt (model thinking included)
	LeaseTTL         time.Duration // an attempt whose holder stopped beating this long is dead
	MinLeft          time.Duration // an attempt needs at least this much wall budget left
	// InfraLimit infrastructure failures in a row (the browser or the site
	// did not answer: CDP/navigate timeouts, editor never opened) block the
	// goal for InfraCooldown. They say nothing about the strategy: no
	// hypothesis is refuted and no strategy is marked failed by them.
	InfraLimit    int
	InfraCooldown time.Duration
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
		MaxSpan:          90 * time.Minute,
		LeaseTTL:         2 * time.Minute,
		MinLeft:          30 * time.Second,
		InfraLimit:       2,
		InfraCooldown:    10 * time.Minute,
	}
}

// ConfigFromEnv overrides DefaultConfig with MACLAW_RECOVERY_* values
// (SAME_FAILURES, NO_PROGRESS, DIAGNOSES, ATTEMPTS, WALL_MIN, TTL_HOURS,
// SPAN_MIN, LEASE_SEC, INFRA_FAILURES, INFRA_COOLDOWN_MIN);
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
	c.MaxSpan = time.Duration(num("SPAN_MIN", int(c.MaxSpan/time.Minute), 5, 24*60)) * time.Minute
	c.LeaseTTL = time.Duration(num("LEASE_SEC", int(c.LeaseTTL/time.Second), 20, 900)) * time.Second
	c.InfraLimit = num("INFRA_FAILURES", c.InfraLimit, 1, 10)
	c.InfraCooldown = time.Duration(num("INFRA_COOLDOWN_MIN", int(c.InfraCooldown/time.Minute), 1, 240)) * time.Minute
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
	// StateBlocked: the infrastructure (browser, network, site) failed
	// InfraLimit times in a row; attempts wait for InfraCooldown, then the
	// goal resumes in the state it had. Observation and reporting go on.
	StateBlocked = "blocked"
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
	KindStale      = "stale"
	// KindInfra: the attempt failed before its strategy could matter.
	KindInfra = "infrastructure_failure"
	// KindInconclusive: the test ran but did not report a fact its
	// expectation names; the hypothesis is neither supported nor refuted.
	KindInconclusive = "hypothesis_inconclusive"
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
	// Infra: the browser, the network or the site did not answer (CDP or
	// navigate timeout, the editor never opened). Not evidence about the
	// strategy; repeated, it blocks the goal (StateBlocked).
	Infra bool
	Error string // error text (fingerprinted, clipped)
	Facts map[string]float64
	Took  time.Duration
	At    time.Time
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
	Infra    bool               `json:"infra,omitempty"`
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
	Lease    *Lease             `json:"lease,omitempty"`    // the attempt in flight, if any
	// Open: work was done on the goal since its last measurement; that
	// work is one attempt, counted when its first step began.
	Open bool `json:"open,omitempty"`
	// Steps are the work steps in flight, each holding a reservation that
	// is already in SpentMS (see BeginStep).
	Steps []Step `json:"steps,omitempty"`
	// InfraFails counts infrastructure failures in a row; BlockedUntil and
	// Resume belong to StateBlocked.
	InfraFails   int       `json:"infra_fails,omitempty"`
	BlockedUntil time.Time `json:"blocked_until,omitempty"`
	Resume       string    `json:"resume,omitempty"`
}

// Step is one work step in flight under a goal that is measured, not leased
// (goal_check goals). Its reservation is charged to SpentMS when it begins
// and settled to the real duration when it ends; a step whose end is never
// reported (its process died) keeps the full reservation charged.
type Step struct {
	ID         string    `json:"id"`
	Holder     string    `json:"holder"`
	Run        string    `json:"run,omitempty"`
	Started    time.Time `json:"started"`
	ReservedMS int64     `json:"reserved_ms"`
	Until      time.Time `json:"until"`
}

const maxStepsInFlight = 32

// Attempt phases of a lease.
const (
	PhaseRunning    = "running"     // no consequential effect yet: a dead holder's attempt is reclaimed
	PhaseSideEffect = "side_effect" // a consequential write may have gone out: a dead holder stops the goal
)

// Lease is the one attempt in flight at a goal. Only its holder may finish
// it; a holder that stops beating for LeaseTTL is dead.
type Lease struct {
	ID            string    `json:"id"`
	Holder        string    `json:"holder"`
	Run           string    `json:"run,omitempty"`
	Method        string    `json:"method"`
	Strategy      string    `json:"strategy,omitempty"`
	Consequential bool      `json:"consequential,omitempty"`
	Phase         string    `json:"phase"`
	Started       time.Time `json:"started"`
	Beat          time.Time `json:"beat"`
}

// ErrLeaseLost: this attempt no longer holds its goal (taken over or ended).
var ErrLeaseLost = errors.New("recovery: this attempt no longer holds its goal")

var processHolder = func() string {
	h, _ := os.Hostname()
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s:%d:%x", h, os.Getpid(), b)
}()

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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

// ---- transactions ----

// tx runs fn on goal key (nil when none) inside the scope's transaction.
func (e *Engine) tx(scope, key string, fn func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error)) error {
	now := e.now()
	return e.Store.update(scope, func(f *scopeFile) (bool, error) {
		g := liveGoal(f, key, e.Cfg.TTL, now)
		if g != nil {
			g.Scope = scope
		}
		ng, write, err := fn(f, g, now)
		if ng != nil {
			ng.Scope = scope
			f.Goals[key] = ng
		}
		return write || ng != nil, err
	})
}

// reap ends a lease whose holder is dead: before any side effect the
// attempt counts as interrupted; after one the goal stops (never retried).
func (e *Engine) reap(g *Goal, now time.Time) bool {
	l := g.Lease
	if l == nil || now.Sub(l.Beat) <= e.Cfg.LeaseTTL {
		return false
	}
	took := l.Beat.Sub(l.Started)
	g.SpentMS += took.Milliseconds()
	att := Attempt{At: now, Run: l.Run, Method: l.Method, Strategy: l.Strategy, TookMS: took.Milliseconds()}
	if l.Phase == PhaseSideEffect {
		g.State, g.Reason = StateStopped, "an attempt stopped answering after its consequential write began; its result is unknown"
		att.Error, att.Verdict = "holder gone after the side effect began", KindStopped
	} else {
		att.Error, att.Verdict = "interrupted before any side effect (holder gone)", KindFailure
		att.Finger = Fingerprint(l.Method, l.Strategy, att.Error)
	}
	g.Attempts = appendAttempt(g.Attempts, att)
	g.Lease = nil
	g.Updated = now
	return true
}

func appendAttempt(a []Attempt, x Attempt) []Attempt {
	a = append(a, x)
	if len(a) > maxAttemptsKept {
		a = a[len(a)-maxAttemptsKept:]
	}
	return a
}

// admit applies state, budget and strategy rules to one more attempt.
func (e *Engine) admit(g *Goal, strategy string, now time.Time) (bool, error) {
	unblocked := false
	if g.State == StateBlocked {
		if now.Before(g.BlockedUntil) {
			return false, fmt.Errorf("recovery: refused: BLOCKED — %s; no attempt before %s. Do not retry it now: observe (screenshot, probe) or request_takeover, work on the other parts of the task, and report this part as blocked with this evidence", g.Reason, g.BlockedUntil.In(time.Local).Format("15:04"))
		}
		g.State, g.Reason, g.InfraFails, g.BlockedUntil = g.Resume, "", 0, time.Time{}
		if g.State == "" || g.State == StateBlocked {
			g.State = StateActive
		}
		g.Resume = ""
		unblocked = true
	}
	changed, err := e.admitState(g, strategy, now)
	return changed || unblocked, err
}

func (e *Engine) admitState(g *Goal, strategy string, now time.Time) (bool, error) {
	if why := e.outOfBudget(g, now); why != "" {
		changed := g.State != StateExhausted
		g.State, g.Reason = StateExhausted, why
		return changed, fmt.Errorf("recovery: refused: this goal is out of recovery budget (%s); stop and report what is done, what is missing and the evidence", why)
	}
	switch g.State {
	case StateStopped:
		return false, fmt.Errorf("recovery: refused: %s; the earlier attempt may already have taken effect, so it is never repeated — observe the real state and report it", g.Reason)
	case StateExhausted:
		return false, fmt.Errorf("recovery: refused: this goal is out of recovery budget (%s); stop and report what is done, what is missing and the evidence", g.Reason)
	case StateDiagnose:
		return false, fmt.Errorf("recovery: refused: %s. Before another attempt call action=recover with hypothesis, uncertainty, strategy (meaningfully different from: %s), test and expect. %s", g.Reason, e.failedList(g), e.budgetText(g))
	case StateTrial:
		h := &g.Hyps[len(g.Hyps)-1]
		if h.Sig != "" && h.Sig != strategy {
			return false, fmt.Errorf("recovery: refused: hypothesis %d is being tested with strategy %q; run its test (%s) with exactly that strategy first", h.N, h.Sig, h.Test)
		}
		if h.Sig == "" {
			if why, failed := g.Failed[strategy]; failed {
				return false, fmt.Errorf("recovery: refused: that is the strategy that already failed (%s); hypothesis %d needs a different one", why, h.N)
			}
		}
		return false, nil
	}
	if why, failed := g.Failed[strategy]; failed && len(g.Hyps) > 0 {
		return false, fmt.Errorf("recovery: refused: strategy %q already failed for this goal (%s)", strategyLabel(strategy), why)
	}
	return false, nil
}

func (e *Engine) outOfBudget(g *Goal, now time.Time) string {
	if g.State == StateDone || g.State == StateStopped {
		return ""
	}
	spent := time.Duration(g.SpentMS) * time.Millisecond
	switch {
	case g.Count >= e.Cfg.MaxAttempts && !g.Open:
		return fmt.Sprintf("%d of %d attempts used", g.Count, e.Cfg.MaxAttempts)
	case spent >= e.Cfg.MaxWall || (g.Lease == nil && e.Cfg.MaxWall-spent < e.Cfg.MinLeft):
		return fmt.Sprintf("%s of %s attempt time used", spent.Round(time.Second), e.Cfg.MaxWall)
	case e.Cfg.MaxSpan > 0 && g.Count > 0 && now.Sub(g.Created) >= e.Cfg.MaxSpan:
		return fmt.Sprintf("%s since the first attempt (limit %s)", now.Sub(g.Created).Round(time.Second), e.Cfg.MaxSpan)
	}
	return ""
}

// WallLeft is the attempt time a goal has left (for the tool's own deadline).
func (e *Engine) WallLeft(g *Goal) time.Duration {
	return e.wallLeftAt(g, e.now())
}

func (e *Engine) wallLeftAt(g *Goal, now time.Time) time.Duration {
	if g == nil {
		return e.Cfg.MaxWall
	}
	left := e.Cfg.MaxWall - time.Duration(g.SpentMS)*time.Millisecond
	if e.Cfg.MaxSpan > 0 && g.Count > 0 {
		if s := e.Cfg.MaxSpan - now.Sub(g.Created); s < left {
			left = s
		}
	}
	if left < 0 {
		left = 0
	}
	return left
}

// BeginStep admits one work step under a measured goal and charges its
// reservation to the goal's attempt time before the step runs, so a crash
// or restart before the next measurement never loses (or resets) it. The
// first step after a measurement opens the next attempt and counts it;
// later steps of the same attempt are not counted again. The returned
// budget (≤ want) is the step's hard deadline; the caller must end the
// step with EndStep at its real end. An empty id with a nil error means the
// goal is gone (expired) and nothing was reserved.
func (e *Engine) BeginStep(scope, goal, run, strategy string, want time.Duration) (string, time.Duration, error) {
	var id string
	var budget time.Duration
	err := e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil {
			return nil, false, nil
		}
		changed := e.reap(g, now)
		changed = e.pruneSteps(g, now) || changed
		if g.Lease != nil {
			return nil, changed, fmt.Errorf("recovery: refused: another attempt at this goal is still running (%s, started %s ago); wait for its result", g.Lease.Method, now.Sub(g.Lease.Started).Round(time.Second))
		}
		if len(g.Steps) > 0 && e.wallLeftAt(g, now) < e.Cfg.MinLeft {
			// The time is held by steps still running, not used up: wait
			// for them instead of ending the goal on a reservation.
			return nil, changed, fmt.Errorf("recovery: refused: the attempt time this goal has left is held by %d step(s) still running; wait for them, then measure", len(g.Steps))
		}
		ch, err := e.admit(g, strategy, now)
		if err != nil {
			return g, changed || ch, err
		}
		if len(g.Steps) >= maxStepsInFlight {
			return g, changed, fmt.Errorf("recovery: refused: %d steps of this goal are still running; wait for them", len(g.Steps))
		}
		left := e.wallLeftAt(g, now)
		if left <= 0 {
			return g, changed, fmt.Errorf("recovery: refused: this goal has no attempt time left; measure what was reached and report it")
		}
		budget = want
		if budget <= 0 || budget > left {
			budget = left
		}
		if !g.Open {
			g.Count++
			g.Open = true
			if g.Count == 1 {
				g.Created = now
			}
		}
		if run != "" && (len(g.Runs) == 0 || g.Runs[len(g.Runs)-1] != run) {
			g.Runs = append(g.Runs, run)
		}
		id = newID()
		g.SpentMS += budget.Milliseconds()
		g.Steps = append(g.Steps, Step{ID: id, Holder: processHolder, Run: run, Started: now, ReservedMS: budget.Milliseconds(), Until: now.Add(budget)})
		g.Updated = now
		return g, true, nil
	})
	if err != nil {
		return "", 0, err
	}
	return id, budget, nil
}

// EndStep settles a step at its real end: the goal is charged what the
// step really took instead of its reservation (an overrun past the
// reservation is charged too). A step already pruned as dead stays charged
// in full; only an overrun beyond its reservation is added. Calling it more
// than once for one step is the caller's error (it would charge an overrun
// twice); took is the step's real duration.
func (e *Engine) EndStep(scope, goal, id string, reserved, took time.Duration) {
	if id == "" {
		return
	}
	if took < 0 {
		took = 0
	}
	_ = e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil {
			return nil, false, nil
		}
		for i, st := range g.Steps {
			if st.ID == id {
				g.SpentMS += took.Milliseconds() - st.ReservedMS
				if g.SpentMS < 0 {
					g.SpentMS = 0
				}
				g.Steps = append(g.Steps[:i:i], g.Steps[i+1:]...)
				g.Updated = now
				return g, true, nil
			}
		}
		if over := took - reserved; over > 0 {
			g.SpentMS += over.Milliseconds()
			g.Updated = now
			return g, true, nil
		}
		return nil, false, nil
	})
}

// pruneSteps forgets steps whose deadline passed more than LeaseTTL ago:
// their holder stopped answering. Their reservation stays charged.
func (e *Engine) pruneSteps(g *Goal, now time.Time) bool {
	kept := g.Steps[:0]
	for _, st := range g.Steps {
		if now.Sub(st.Until) <= e.Cfg.LeaseTTL {
			kept = append(kept, st)
		}
	}
	pruned := len(kept) != len(g.Steps)
	if len(kept) == 0 {
		kept = nil
	}
	g.Steps = kept
	return pruned
}

// Gate is asked before a non-leased attempt at goal (nil allows it).
func (e *Engine) Gate(scope, goal, method, strategy string) error {
	return e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil {
			return nil, false, nil
		}
		reaped := e.reap(g, now)
		if g.Lease != nil {
			return nil, reaped, fmt.Errorf("recovery: refused: another attempt at this goal is still running (%s, started %s ago); wait for its result", g.Lease.Method, now.Sub(g.Lease.Started).Round(time.Second))
		}
		changed, err := e.admit(g, strategy, now)
		return nil, changed || reaped, err
	})
}

// AttemptRequest asks for the lease of one attempt.
type AttemptRequest struct {
	Goal, Surface, Method, Strategy, Run string
	Consequential                        bool
	// Spec, when set, is pinned on first use; Check refuses a later spec
	// that lowers it (a nil Check requires equality).
	Spec  string
	Check func(pinned, spec string) error
}

// Acquire reserves one attempt at a goal: the attempt is counted now, and no
// other attempt (any process sharing the store) may start until this one
// ends with Finish or Abandon, or its holder is dead.
func (e *Engine) Acquire(scope string, r AttemptRequest) (*Lease, *Goal, error) {
	var out *Lease
	var snap *Goal
	err := e.tx(scope, r.Goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		created := false
		if g == nil {
			g = &Goal{Key: r.Goal, Scope: scope, Surface: r.Surface, Created: now, Updated: now, State: StateActive}
			created = true
		}
		reaped := e.reap(g, now)
		if g.Lease != nil {
			return nil, reaped, fmt.Errorf("recovery: refused: another attempt at this goal is still running (%s, started %s ago); never start a second one — wait for its result", g.Lease.Method, now.Sub(g.Lease.Started).Round(time.Second))
		}
		if g.State == StateDone {
			// A verified goal is not attempted again within its lineage.
			return nil, reaped, fmt.Errorf("recovery: refused: this goal was already verified as done; report the result instead of doing it again")
		}
		changed, err := e.admit(g, r.Strategy, now)
		if err != nil {
			if created {
				return nil, false, err
			}
			if changed || reaped {
				return g, true, err
			}
			return nil, false, err
		}
		if r.Spec != "" {
			switch {
			case g.Spec == "":
				g.Spec = r.Spec
			case r.Check != nil:
				if err := r.Check(g.Spec, r.Spec); err != nil {
					return g, reaped, fmt.Errorf("recovery: refused: %v (the goal's requirement is pinned: %s)", err, clip(g.Spec, 160))
				}
			case g.Spec != r.Spec:
				return g, reaped, fmt.Errorf("recovery: refused: the goal's requirement is pinned (%s) and cannot change", clip(g.Spec, 160))
			}
		}
		if r.Surface != "" {
			g.Surface = r.Surface
		}
		g.Count++
		if g.Count == 1 {
			g.Created = now
		}
		g.Updated = now
		if r.Run != "" && (len(g.Runs) == 0 || g.Runs[len(g.Runs)-1] != r.Run) {
			g.Runs = append(g.Runs, r.Run)
		}
		g.Lease = &Lease{ID: newID(), Holder: processHolder, Run: r.Run, Method: r.Method, Strategy: r.Strategy, Consequential: r.Consequential, Phase: PhaseRunning, Started: now, Beat: now}
		l := *g.Lease
		out = &l
		snap = g.clone()
		return g, true, nil
	})
	return out, snap, err
}

func (e *Engine) holding(g *Goal, id string, now time.Time) bool {
	return g != nil && g.Lease != nil && g.Lease.ID == id && now.Sub(g.Lease.Beat) <= e.Cfg.LeaseTTL
}

// Beat keeps a lease alive while its attempt really runs.
func (e *Engine) Beat(scope, goal, id string) error {
	return e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil || g.Lease == nil || g.Lease.ID != id {
			return nil, false, ErrLeaseLost
		}
		g.Lease.Beat = now
		return g, true, nil
	})
}

// SideEffect is the handshake right before a consequential write: it
// succeeds only while the lease is held and alive, and from then on a dead
// holder stops the goal instead of freeing it. A caller that gets an error
// must not perform the write.
func (e *Engine) SideEffect(scope, goal, id string) error {
	return e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if !e.holding(g, id, now) {
			return nil, false, ErrLeaseLost
		}
		g.Lease.Phase, g.Lease.Beat = PhaseSideEffect, now
		return g, true, nil
	})
}

// Abandon ends a lease whose attempt never ran (refused before anything was
// done): the reserved attempt is given back.
func (e *Engine) Abandon(scope, goal, id string) {
	_ = e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil || g.Lease == nil || g.Lease.ID != id || g.Lease.Phase != PhaseRunning {
			return nil, false, nil
		}
		g.Lease = nil
		if g.Count > 0 {
			g.Count--
		}
		return g, true, nil
	})
}

// Finish records the leased attempt's observation and ends the lease. A
// result from an attempt that no longer holds the goal is not recorded.
func (e *Engine) Finish(scope, id string, o Observation) Verdict {
	var v Verdict
	err := e.tx(scope, o.Goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil || g.Lease == nil || g.Lease.ID != id {
			v = Verdict{Kind: KindStale, Evidence: "[recovery] this attempt's result arrived after the goal was taken over or closed; nothing was recorded from it"}
			if g != nil {
				v.State = g.State
			}
			return nil, false, nil
		}
		if o.Took <= 0 {
			o.Took = now.Sub(g.Lease.Started)
		}
		g.Lease = nil
		v = e.observe(f, g, o, now, false)
		return g, true, nil
	})
	if err != nil {
		v = Verdict{Kind: KindStale, Evidence: "[recovery] the result could not be recorded: " + err.Error()}
	}
	return v
}

// Observe records one non-leased attempt (goal_check measurements).
func (e *Engine) Observe(scope string, o Observation) Verdict {
	var v Verdict
	err := e.tx(scope, o.Goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil {
			g = &Goal{Key: o.Goal, Scope: scope, Created: now, State: StateActive}
		}
		e.reap(g, now)
		e.pruneSteps(g, now)
		if !o.At.IsZero() {
			now = o.At
		}
		// Work since the last measurement was counted when its first step
		// began (BeginStep), and its time is charged by the steps: the
		// measurement closes that attempt instead of counting another.
		count := !g.Open
		g.Open = false
		v = e.observe(f, g, o, now, count)
		return g, true, nil
	})
	if err != nil {
		v = Verdict{Kind: KindStale, Evidence: "[recovery] the result could not be recorded: " + err.Error()}
	}
	return v
}

// Reopen puts a verified goal back to active (its result was changed after
// the verification, so it must be measured again).
func (e *Engine) Reopen(scope, goal, why string) {
	_ = e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil || g.State != StateDone {
			return nil, false, nil
		}
		g.State, g.Reason, g.Updated = StateActive, why, now
		return g, true, nil
	})
}

// ReopenStopped lets a new run of the same instruction try a goal that was
// stopped because its consequential write went out unconfirmed — only after
// the caller has read the real state and found the write did not take
// effect (e.g. the post is not in the account's post list). A run that
// already worked on the goal, or a goal held by a live attempt, is not
// reopened.
func (e *Engine) ReopenStopped(scope, goal, run, why string) bool {
	reopened := false
	_ = e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil || g.State != StateStopped || g.Lease != nil || run == "" {
			return nil, false, nil
		}
		for _, r := range g.Runs {
			if r == run {
				return nil, false, nil
			}
		}
		g.State, g.Reason, g.Updated = StateActive, why, now
		reopened = true
		return g, true, nil
	})
	return reopened
}

// observe decides what one attempt means (inside the transaction).
func (e *Engine) observe(f *scopeFile, g *Goal, o Observation, now time.Time, count bool) Verdict {
	if o.Surface != "" {
		g.Surface = o.Surface
	}
	g.Updated = now
	if o.Run != "" && (len(g.Runs) == 0 || g.Runs[len(g.Runs)-1] != o.Run) {
		g.Runs = append(g.Runs, o.Run)
	}
	if count {
		g.Count++
		if g.Count == 1 {
			g.Created = now
		}
	}
	g.SpentMS += o.Took.Milliseconds()
	att := Attempt{At: now, Run: o.Run, Method: o.Method, Strategy: o.Strategy, OK: o.OK, Complete: o.Complete, Trial: o.Trial, Infra: o.Infra && !o.Complete && o.Side == "", Facts: o.Facts, TookMS: o.Took.Milliseconds(), Error: clip(o.Error, 300)}
	if !att.Infra {
		g.InfraFails = 0
	}
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
	if att.Infra {
		regressed, lost = false, ""
	}
	switch {
	case o.Side == "unconfirmed":
		g.State, g.Reason = StateStopped, "a consequential write went out without proof of its result"
		v.Kind = KindStopped
	case o.Complete:
		v.Kind = KindVerified
		g.State, g.Reason = StateDone, ""
		for i := range g.Hyps {
			if g.Hyps[i].Sig == o.Strategy && g.Hyps[i].Outcome != "refuted" && g.Hyps[i].Outcome != "pending" || (hyp != nil && &g.Hyps[i] == hyp) {
				g.Hyps[i].Outcome = "verified"
				g.Hyps[i].Evidence = "goal verified: " + factText(o.Facts)
			}
		}
		if !o.Trial && len(o.Params) > 0 && o.Surface != "" {
			v.Recipe = verifyRecipe(f, o, now)
		}
	case att.Infra:
		// The browser or the site did not answer: the strategy was never
		// put to the test. Nothing is refuted or marked failed; the attempt
		// is not counted against the goal's attempts (its time is).
		v.Kind = KindInfra
		if g.Count > 0 {
			g.Count--
		}
		g.InfraFails++
		if g.InfraFails >= e.Cfg.InfraLimit && g.State != StateBlocked {
			g.Resume, g.State = g.State, StateBlocked
			g.BlockedUntil = now.Add(e.Cfg.InfraCooldown)
			g.Reason = fmt.Sprintf("the browser or the site did not respond %d times in a row (%s)", g.InfraFails, clip(o.Error, 120))
		}
	case hyp != nil:
		// The hypothesis' test: its expected facts decide, nothing else.
		var missed []string
		absent := 0
		for _, ex := range hyp.Expect {
			if !ex.Met(o.Facts) {
				got := "missing"
				if val, ok := o.Facts[ex.Fact]; ok {
					got = strconv.FormatFloat(val, 'g', -1, 64)
				} else {
					absent++
				}
				missed = append(missed, fmt.Sprintf("%s (observed %s)", ex, got))
			}
		}
		if o.OK && !regressed && len(missed) > 0 && absent == len(missed) {
			// The test ran and reported other facts, not the expected
			// one: that is no evidence either way. The strategy stays
			// open; the next diagnosis must expect facts the test reports.
			why := strings.Join(missed, ", ")
			hyp.Outcome, hyp.Evidence = "inconclusive", "the test did not report "+why+"; it reported: "+factText(o.Facts)
			g.State, g.Reason = StateDiagnose, "hypothesis "+strconv.Itoa(hyp.N)+"'s test did not measure what it expected ("+clip(why, 120)+"; the test reported "+clip(factText(o.Facts), 80)+"); register a test whose expect names facts it reports"
			v.Kind = KindInconclusive
		} else if len(missed) == 0 && !regressed {
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
		// A test can pass at small scale and the real attempt still fail.
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
		failRecipe(f, o.Recipe, o.Run, v.Kind+": "+clip(o.Error, 120), now)
	}
	if improves(g.Best, o.Facts) && !o.Trial && !regressed {
		g.Best = copyFacts(o.Facts)
	}
	att.Verdict = v.Kind
	g.Attempts = appendAttempt(g.Attempts, att)
	if g.State == StateDiagnose && len(g.Hyps) >= e.Cfg.MaxDiagnoses {
		g.State, g.Reason = StateExhausted, fmt.Sprintf("all %d hypotheses were used and the goal is still not met", e.Cfg.MaxDiagnoses)
		v.Kind = KindExhausted
	}
	if g.State != StateDone && g.State != StateStopped {
		if why := e.outOfBudget(g, now); why != "" {
			g.State, g.Reason = StateExhausted, why
			v.Kind = KindExhausted
		}
	}
	g.LastKind = v.Kind
	v.State = g.State
	v.Evidence = e.evidence(g, o, v.Kind, lost)
	g.Evidence = v.Evidence
	return v
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
		if g.Attempts[i].Trial == trial && !g.Attempts[i].Infra {
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
		if a.Infra {
			continue // the infrastructure, not this method, failed
		}
		if a.OK || a.Finger != finger {
			break
		}
		n++
	}
	return n
}

// noProgress: the last n attempts (all failed, none improved) — with this one, n+1.
func noProgress(g *Goal, n int) bool {
	if n <= 0 {
		return false
	}
	seen := 0
	for i := len(g.Attempts) - 1; i >= 0 && seen < n; i-- {
		a := g.Attempts[i]
		if a.Infra {
			continue // says nothing about progress
		}
		if a.Complete || a.Verdict == KindProgress || a.Verdict == KindSupported {
			return false
		}
		seen++
	}
	return seen == n
}

// Propose registers the model's diagnosis for a goal that requires one.
// validate checks the declarative params (unknown keys or unsafe values are
// an error); params may be empty when the strategy is a different method
// whose signature binds on its first test attempt.
func (e *Engine) Propose(scope, goal, run string, p Proposal, validate func(map[string]string) error) (string, error) {
	var text string
	err := e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil {
			return nil, false, fmt.Errorf("recover: goal %q has no failed attempt on record; just carry on", clip(goal, 80))
		}
		switch g.State {
		case StateDiagnose:
		case StateTrial:
			return nil, false, fmt.Errorf("recover: hypothesis %d is still waiting for its test (%s); run it first", g.Hyps[len(g.Hyps)-1].N, g.Hyps[len(g.Hyps)-1].Test)
		case StateExhausted, StateStopped:
			return nil, false, fmt.Errorf("recover: refused: %s", g.Reason)
		default:
			return nil, false, fmt.Errorf("recover: nothing to diagnose for this goal (state %s); carry on", g.State)
		}
		if len(g.Hyps) >= e.Cfg.MaxDiagnoses {
			g.State, g.Reason = StateExhausted, "no hypotheses left"
			return g, true, fmt.Errorf("recover: refused: all %d hypotheses were used", e.Cfg.MaxDiagnoses)
		}
		if len([]rune(strings.TrimSpace(p.Hypothesis))) < 12 || strings.TrimSpace(p.Strategy) == "" || strings.TrimSpace(p.Test) == "" {
			return nil, false, fmt.Errorf("recover: hypothesis (a cause you can test), strategy and test are all required")
		}
		if len(p.Expect) == 0 {
			return nil, false, fmt.Errorf("recover: expect needs at least one fact the test must show, e.g. pictures_kept>=2")
		}
		var exp []Expectation
		for _, s := range p.Expect {
			x, err := ParseExpectation(s)
			if err != nil {
				return nil, false, fmt.Errorf("recover: %v", err)
			}
			exp = append(exp, x)
		}
		if validate != nil {
			if err := validate(p.Params); err != nil {
				return nil, false, fmt.Errorf("recover: params: %v", err)
			}
		}
		sig := Sig(p.Params)
		if sig != "" {
			if why, failed := g.Failed[sig]; failed {
				return nil, false, fmt.Errorf("recover: refused: that strategy already failed for this goal (%s); propose a meaningfully different one", why)
			}
			for _, h := range g.Hyps {
				if h.Sig == sig {
					return nil, false, fmt.Errorf("recover: refused: hypothesis %d already tested that exact strategy (%s)", h.N, h.Outcome)
				}
			}
		}
		norm := normText(p.Strategy)
		for _, h := range g.Hyps {
			if normText(h.Strategy) == norm {
				return nil, false, fmt.Errorf("recover: refused: hypothesis %d proposed the same strategy; a new attempt must change what is done", h.N)
			}
		}
		h := Hypothesis{N: len(g.Hyps) + 1, Text: clip(p.Hypothesis, 400), Uncertainty: clip(p.Uncertainty, 200), Strategy: clip(p.Strategy, 300), Sig: sig, Params: p.Params, Test: clip(p.Test, 300), Expect: exp, Outcome: "pending", Run: run}
		g.Hyps = append(g.Hyps, h)
		g.State, g.Reason = StateTrial, ""
		g.Updated = now
		text = fmt.Sprintf("Hypothesis %d registered (not a fact until its test shows %s). Now run the test: %s. The facts of that attempt decide supported or refuted. %s", h.N, expectText(exp), h.Test, e.budgetText(g))
		return g, true, nil
	})
	return text, err
}

func normText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// Pin fixes a goal's success criteria the first time they are given; later
// calls must give the same criteria (or none), so a run cannot lower the bar
// after a failure.
func (e *Engine) Pin(scope, goal, spec string) (*Goal, error) {
	var out *Goal
	err := e.tx(scope, goal, func(f *scopeFile, g *Goal, now time.Time) (*Goal, bool, error) {
		if g == nil {
			g = &Goal{Key: goal, Scope: scope, Created: now, Updated: now, State: StateActive, Spec: spec}
			out = g.clone()
			return g, true, nil
		}
		write := false
		if g.Spec == "" && spec != "" {
			g.Spec, write = spec, true
		}
		out = g.clone()
		if spec != "" && g.Spec != spec {
			return nil, write, fmt.Errorf("goal_check: the success criteria of %q are pinned (%s); they cannot be changed or relaxed after attempts began", clip(goal, 60), clip(g.Spec, 200))
		}
		if write {
			return g, true, nil
		}
		return nil, false, nil
	})
	return out, err
}

// Status is the goal's ledger (nil when none or unreadable).
func (e *Engine) Status(scope, goal string) *Goal {
	g, _ := e.Store.Goal(scope, goal, e.Cfg.TTL, e.now())
	return g
}

// Goals lists the live goals under a key prefix (a lineage).
func (e *Engine) Goals(scope, prefix string) []*Goal {
	gs, _ := e.Store.Goals(scope, prefix, e.Cfg.TTL, e.now())
	return gs
}

var continueRe = regexp.MustCompile(`(?i)^\s*(继续|接着|再试|重试|再来|再发|还是|好的|好|嗯|行|可以|ok|okay|go on|continue|retry|resume|try again)`)

// NewTask reports whether a person's message is a new instruction rather
// than a nudge to continue (only the person's own words start a new task).
func NewTask(msg string) bool {
	r := []rune(strings.TrimSpace(msg))
	if len(r) < 12 {
		return false
	}
	if continueRe.MatchString(string(r)) && len(r) < 40 {
		return false
	}
	return true
}

// Lineage returns the current logical-task id of one conversation for the
// person's message: the same id for continuations, resumes and repeats of
// the same instruction; a new generation only for a new instruction.
func (e *Engine) Lineage(scope, conversation, message string) (string, error) {
	sum := sha256.Sum256([]byte(conversation))
	conv := hex.EncodeToString(sum[:6])
	ah := sha256.Sum256([]byte(normText(message)))
	anchor := hex.EncodeToString(ah[:6])
	gen := 0
	err := e.Store.update(scope, func(f *scopeFile) (bool, error) {
		now := e.now()
		l := f.Lineages[conv]
		switch {
		case l == nil:
			l = &Lineage{Gen: 1, Anchor: anchor}
			f.Lineages[conv] = l
		case NewTask(message) && anchor != l.Anchor:
			l.Gen++
			l.Anchor = anchor
		}
		l.Updated = now
		gen = l.Gen
		return true, nil
	})
	return fmt.Sprintf("%s#%d", conv, gen), err
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
	case StateBlocked:
		b.WriteString(fmt.Sprintf("\nRECOVERY_BLOCKED: %s. This is the infrastructure, not the strategy: nothing was refuted. Do not retry before %s; observe (screenshot, probe) or request_takeover, do the other parts of the task, and report this part as blocked with this evidence.", g.Reason, g.BlockedUntil.In(time.Local).Format("15:04")))
	}
	if kind == KindInfra && g.State != StateBlocked {
		b.WriteString(fmt.Sprintf("\nINFRASTRUCTURE: the browser or the site did not respond; the strategy was not tested and nothing was refuted (%d of %d in a row before the goal is blocked). Observe the page (screenshot, probe) before trying again.", g.InfraFails, e.Cfg.InfraLimit))
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

// Recipes are the verified, non-stale recipes for a surface and method.
func (e *Engine) Recipes(scope, surface, method string) []Recipe {
	return e.Store.Recipes(scope, surface, method, e.Cfg.RecipeMaxAge, e.now())
}
