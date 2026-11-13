package main

// Runtime self-correction for desktop runs (corelib/recovery wired into
// invokeDesktopTool). Nothing here edits a program, a site profile or a
// prompt file: the run's own model diagnoses a failure from measured facts
// through action=recover, tests a different strategy within the same user
// task, and only a verified outcome becomes a recipe (declarative params).
//
//   - publish goals: every publish call is an attempt at "publish|site|title";
//     the facts are the pipeline's own measurements (PublishReport.Facts),
//     never text read from a page.
//   - check goals: action=goal_check pins success criteria (files the task
//     must produce) and measures them; the mutating calls between two checks
//     are the attempt, and a check goal that needs a diagnosis refuses them.
//
// Budgets (attempts and wall time) are per goal and persisted per
// tenant+user, so method switches, renamed files and resumed runs spend the
// same budget. A submit whose result is unproven stops the goal for good.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

var (
	desktopRecoveryMu  sync.Mutex
	desktopRecoveryEng *recovery.Engine
	// desktopRecoveryRoot is set by tests.
	desktopRecoveryRoot string
)

func desktopRecovery() *recovery.Engine {
	desktopRecoveryMu.Lock()
	defer desktopRecoveryMu.Unlock()
	if desktopRecoveryEng == nil {
		root := desktopRecoveryRoot
		if root == "" {
			root = filepath.Join(corelib.MaclawDataDir(), "desktop-recovery")
		}
		desktopRecoveryEng = recovery.New(recovery.ConfigFromEnv(os.Getenv), recovery.NewStore(root))
	}
	return desktopRecoveryEng
}

func desktopRecoveryScope(scope agentruntime.Scope) string {
	o := desktopOwner(scope)
	return recovery.ScopeKey(o.TenantID, o.UserID)
}

// In-process state (the ledger itself is the engine's store): the attempt
// a publish call is about to make and the one whose pipeline runs, the
// goals and lineage each run touched (for the honest outcome).
// spent in mutating steps since a check goal's last measurement.
var (
	desktopRecStateMu    sync.Mutex
	desktopRecPendingAtt = map[string]*desktopRecAttempt{} // recKey -> publish attempt gated, not started
	desktopRecLive       = map[string]*desktopRecAttempt{} // recKey -> publish attempt holding its goal
	desktopRecRuns       = map[string][]string{}           // runID -> "sk\x01goal"
	desktopRecLineages   = map[string]string{}             // recKey -> lineage
	desktopRecRunLineage = map[string]string{}             // runID -> "sk\x01lineage"
	desktopRecRunSteps   = map[string]bool{}               // runID -> made a desktop step
	// desktopRecCheckScopes: runID -> the scope its goal_check measured with,
	// so the run's end can measure a reopened goal again the same way.
	desktopRecCheckScopes = map[string]agentruntime.Scope{}
)

// desktopRecReopenWhy marks a check goal a later step reopened.
const desktopRecReopenWhy = "changed after its verification; run goal_check again"

func desktopRecKey(scope agentruntime.Scope) string {
	o := desktopOwner(scope)
	return o.TenantID + "\x00" + o.UserID + "\x00" + scope.RunID
}

func desktopRecNoteRunGoal(runID, goal string) {
	if runID == "" {
		return
	}
	desktopRecStateMu.Lock()
	defer desktopRecStateMu.Unlock()
	for _, g := range desktopRecRuns[runID] {
		if g == goal {
			return
		}
	}
	if len(desktopRecRuns) > 4096 {
		desktopRecRuns = map[string][]string{}
	}
	desktopRecRuns[runID] = append(desktopRecRuns[runID], goal)
}

func desktopRecRunGoals(runID string) []string {
	desktopRecStateMu.Lock()
	defer desktopRecStateMu.Unlock()
	return append([]string(nil), desktopRecRuns[runID]...)
}

// ---- publish strategy parameters (the only things a recipe may hold) ----

var desktopRecSelectorRe = regexp.MustCompile(`^[A-Za-z0-9_\-#.\[\]="' :>+~*^$|(),]{1,120}$`)

var desktopRecPublishParamKeys = []string{"insert_prep_keys", "insert_settle_ms", "body_selector", "title_selector", "upload_selector", "upload_trigger"}

// desktopRecPublishParams canonicalizes and validates declarative publish
// strategy parameters; unknown keys and unsafe values are errors.
func desktopRecPublishParams(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	known := map[string]bool{}
	for _, k := range desktopRecPublishParamKeys {
		known[k] = true
	}
	for k, v := range in {
		v = strings.TrimSpace(v)
		if !known[k] {
			return nil, fmt.Errorf("%q is not a publish strategy parameter (allowed: %s)", k, strings.Join(desktopRecPublishParamKeys, ", "))
		}
		if v == "" {
			continue
		}
		switch k {
		case "insert_prep_keys":
			var keys []string
			for _, c := range strings.Split(v, ",") {
				c = strings.TrimSpace(c)
				if c == "" {
					continue
				}
				if _, _, err := browser.ParseKeyChord(c); err != nil {
					return nil, err
				}
				keys = append(keys, c)
			}
			if len(keys) == 0 || len(keys) > 6 {
				return nil, fmt.Errorf("insert_prep_keys needs 1-6 key chords")
			}
			v = strings.Join(keys, ",")
		case "insert_settle_ms":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 20000 {
				return nil, fmt.Errorf("insert_settle_ms must be 0-20000")
			}
			v = strconv.Itoa(n)
		case "upload_trigger":
			if len([]rune(v)) > 40 || strings.ContainsAny(v, "\n<>{}`") {
				return nil, fmt.Errorf("upload_trigger must be the button's visible text (≤40 chars)")
			}
		default:
			if !desktopRecSelectorRe.MatchString(v) || strings.Contains(strings.ToLower(v), "javascript") {
				return nil, fmt.Errorf("%s must be a plain CSS selector (≤120 chars)", k)
			}
		}
		out[k] = v
	}
	return out, nil
}

func desktopRecArgString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var p []string
		for _, e := range x {
			p = append(p, fmt.Sprint(e))
		}
		return strings.Join(p, ",")
	case []string:
		return strings.Join(x, ",")
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func desktopRecPublishArgsParams(args map[string]any) (map[string]string, error) {
	in := map[string]string{}
	for _, k := range desktopRecPublishParamKeys {
		if v, ok := args[k]; ok {
			if s := strings.TrimSpace(desktopRecArgString(v)); s != "" {
				in[k] = s
			}
		}
	}
	return desktopRecPublishParams(in)
}

// desktopPublishPrepKeys reads insert_prep_keys ("ctrl+End,Enter" or a list).
func desktopPublishPrepKeys(args map[string]any) []string {
	var out []string
	for _, c := range strings.Split(desktopRecArgString(args["insert_prep_keys"]), ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func desktopRecInt(args map[string]any, key string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(desktopRecArgString(args[key])))
	return n
}

// desktopPublishApplyStrategy maps the strategy arguments onto the spec.
func desktopPublishApplyStrategy(args map[string]any, spec *browser.PublishSpec) error {
	if _, err := desktopRecPublishArgsParams(args); err != nil {
		return fmt.Errorf("publish refused: %v (nothing was filled or posted)", err)
	}
	spec.InsertPrepKeys = desktopPublishPrepKeys(args)
	if ms := desktopRecInt(args, "insert_settle_ms"); ms > 0 {
		spec.InsertSettle = time.Duration(ms) * time.Millisecond
	}
	if n := desktopRecInt(args, "trial_pictures"); n > 0 {
		if n > 3 {
			n = 3
		}
		spec.TrialPictures = n
	}
	return nil
}

// ---- goals ----

func desktopRecHash(s string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(strings.ToLower(s)), " ")))
	return hex.EncodeToString(sum[:5])
}

// desktopRecConversation is the authoritative conversation of a turn (the
// session the person writes in); a run without one is its own conversation.
func desktopRecConversation(request agentruntime.TurnRequest, scope agentruntime.Scope) string {
	switch in := request.Input.(type) {
	case agentservice.ExecuteRequest:
		if in.Session.ID != "" {
			return "s:" + in.Session.ID
		}
	case *agentservice.ExecuteRequest:
		if in != nil && in.Session.ID != "" {
			return "s:" + in.Session.ID
		}
	}
	return "r:" + scope.RunID
}

// desktopRecLineage is the logical task this run works on: one per
// conversation and person's instruction (continuations, resumes and
// renames stay in it), computed by the engine from the person's message —
// never from anything the model passes.
func desktopRecLineage(request agentruntime.TurnRequest, scope agentruntime.Scope) (string, error) {
	k := desktopRecKey(scope)
	desktopRecStateMu.Lock()
	if l, ok := desktopRecLineages[k]; ok {
		desktopRecStateMu.Unlock()
		return l, nil
	}
	desktopRecStateMu.Unlock()
	sk := desktopRecoveryScope(scope)
	l, err := desktopRecovery().Lineage(sk, desktopRecConversation(request, scope), desktopMessageText(request))
	if err != nil {
		return "", fmt.Errorf("recovery: refused: the task ledger is unavailable (%v); nothing was done", err)
	}
	desktopRecStateMu.Lock()
	if len(desktopRecLineages) > 4096 {
		desktopRecLineages, desktopRecRunLineage = map[string]string{}, map[string]string{}
	}
	desktopRecLineages[k] = l
	desktopRecRunLineage[scope.RunID] = sk + "\x01" + l
	desktopRecStateMu.Unlock()
	return l, nil
}

type desktopRecAttempt struct {
	sk, goal, surface, method, strategy string
	params                              map[string]string
	trial                               bool
	recipe                              string
	checks                              []string // mutating step: the lineage's unfinished check goals
	steps                               []desktopRecStep
	settled                             sync.Once
	deadline                            time.Duration
	note                                string
	// recheck: the goal was stopped by an earlier run's unconfirmed write;
	// this new run of the same instruction may try again only after the
	// ledger guard reads the post list and does not find that post
	// (recheckTitle is the stopped attempt's title hash, recheckOK the
	// guard's answer).
	recheck      bool
	recheckTitle string
	recheckOK    bool

	mu       sync.Mutex
	lease    *recovery.Lease
	started  bool
	finished bool
	verdict  recovery.Verdict
	stop     chan struct{}
}

var desktopRecMutating = map[string]bool{"bash": true, "file_write": true, "file_edit": true, "script": true, "click": true, "type": true, "press": true, "computer": true, "app_run": true, "task_run": true, "upload": true, "rich_fill": true, "skill_run": true}

// desktopRecReopens: steps that can change a checked result file.
var desktopRecReopens = map[string]bool{"bash": true, "file_write": true, "file_edit": true, "app_run": true, "task_run": true, "skill_run": true, "script": true}

func desktopRecSite(scope agentruntime.Scope, args map[string]any) string {
	return desktopPublishSiteFor(scope, args)
}

// desktopPublishSiteFor is the canonical site (profile id, else registrable
// domain) a publish call is for, resolved like desktopPublish resolves it:
// editor_url, else the requested site the open page belongs to, else the
// next requested site not done. The page the tab happens to be on is only
// the last resort (run_854d0758 charged a CSDN attempt to "127.0.0.1" and
// kept one platform under three goal keys).
func desktopPublishSiteFor(scope agentruntime.Scope, args map[string]any) string {
	editor := desktopArg(args, "editor_url")
	if site := siteExpKey(editor); site != "" {
		return site
	}
	page := ""
	if host := desktopRunSite(desktopOwner(scope)); host != "" {
		page = "https://" + host + "/"
	}
	ts := desktopTaskSpecFor(scope.RunID)
	if base := desktopTaskSpecBase(scope.RunID); len(base.Sites) > 1 {
		ts = desktopPublishTarget(scope.RunID, base, ts, editor, page)
	}
	if site := siteExpKey(ts.EditorURL); site != "" {
		return site
	}
	if ts.Profile != nil && ts.Profile.ID != "" {
		return ts.Profile.ID
	}
	if site := siteExpKey(page); site != "" {
		return site
	}
	return "unknown"
}

type desktopRecPublishSpec struct {
	Pictures int    `json:"pictures"`
	Body     int    `json:"body"`
	Title    string `json:"title,omitempty"`
}

// desktopRecPublishGoal is the goal one publish call works on: the lineage's
// publish goal for the site. A renamed title stays the same goal (same
// budget, same lockout); only after that goal was verified does a different
// title start the next article of the same instruction.
func desktopRecPublishGoal(sk, lineage, site, title string) string {
	base := "publish|" + lineage + "|" + site
	th := desktopRecHash(title)
	eng := desktopRecovery()
	for i := 1; i <= 12; i++ {
		key := base
		if i > 1 {
			key = base + "|" + strconv.Itoa(i)
		}
		g := eng.Status(sk, key)
		if g == nil || g.State != recovery.StateDone {
			return key
		}
		var ps desktopRecPublishSpec
		_ = json.Unmarshal([]byte(g.Spec), &ps)
		if ps.Title == "" || ps.Title == th {
			return key // the same article again: the engine refuses a repeat
		}
	}
	return base + "|12"
}

func desktopRecRanGoal(g *recovery.Goal, run string) bool {
	for _, r := range g.Runs {
		if r == run {
			return true
		}
	}
	return false
}

// desktopRecPendingRecheck: the pending publish attempt of this run waits
// for a post-list check of a stopped goal's unconfirmed post (its title
// hash); ok records the check's answer.
func desktopRecPendingRecheck(scope agentruntime.Scope) (*desktopRecAttempt, string, bool) {
	desktopRecStateMu.Lock()
	defer desktopRecStateMu.Unlock()
	att := desktopRecPendingAtt[desktopRecKey(scope)]
	if att == nil || !att.recheck {
		return nil, "", false
	}
	return att, att.recheckTitle, true
}

// desktopRecPublishCheck refuses a requirement lower than the pinned one.
func desktopRecPublishCheck(pinned, now string) error {
	var p, n desktopRecPublishSpec
	if json.Unmarshal([]byte(pinned), &p) != nil || json.Unmarshal([]byte(now), &n) != nil {
		return fmt.Errorf("unreadable requirement")
	}
	if n.Pictures < p.Pictures {
		return fmt.Errorf("%d picture(s) instead of the %d the task started with", n.Pictures, p.Pictures)
	}
	if n.Body*10 < p.Body*6 {
		return fmt.Errorf("a body of %d characters instead of the %d the task started with", n.Body, p.Body)
	}
	return nil
}

// desktopRecCheckStrategy: a check goal's strategy is its hypothesis.
func desktopRecCheckStrategy(g *recovery.Goal) string {
	if g == nil || len(g.Hyps) == 0 {
		return ""
	}
	last := g.Hyps[len(g.Hyps)-1]
	if last.Outcome == "refuted" || last.Outcome == "abandoned" {
		return ""
	}
	return "h" + strconv.Itoa(last.N)
}

func desktopRecCheckPrefix(lineage string) string { return "check|" + lineage + "|" }

// desktopRecoveryBefore gates one tool call. It may fill args from a
// verified recipe (returned note says so) or refuse the call.
func desktopRecoveryBefore(request agentruntime.TurnRequest, scope agentruntime.Scope, action string, args map[string]any) (*desktopRecAttempt, error) {
	if action != "publish" && !desktopRecMutating[action] {
		return nil, nil
	}
	if action == "publish" && desktopArgBool(args, "explore") {
		return nil, nil
	}
	eng := desktopRecovery()
	sk := desktopRecoveryScope(scope)
	lineage, err := desktopRecLineage(request, scope)
	if err != nil {
		return nil, err
	}
	desktopRecStateMu.Lock()
	if len(desktopRecRunSteps) > 4096 {
		desktopRecRunSteps = map[string]bool{}
	}
	desktopRecRunSteps[scope.RunID] = true
	desktopRecStateMu.Unlock()
	if action == "publish" {
		params, err := desktopRecPublishArgsParams(args)
		if err != nil {
			return nil, fmt.Errorf("publish refused: %v (nothing was filled or posted)", err)
		}
		site := desktopRecSite(scope, args)
		goal := desktopRecPublishGoal(sk, lineage, site, desktopArg(args, "title"))
		att := &desktopRecAttempt{sk: sk, goal: goal, surface: "site:" + site + "|publish", method: "publish", trial: desktopRecInt(args, "trial_pictures") > 0}
		g := eng.Status(sk, goal)
		if g != nil && g.State == recovery.StateTrial && len(g.Hyps) > 0 {
			h := g.Hyps[len(g.Hyps)-1]
			if !att.trial {
				return nil, fmt.Errorf("recovery: refused: hypothesis %d is being tested; a test must not submit — call publish with trial_pictures=1..3 (fills and inserts, never submits) and the hypothesis' params", h.N)
			}
			if len(params) == 0 && len(h.Params) > 0 {
				for k, v := range h.Params {
					args[k] = v
				}
				params = h.Params
				att.note = "\n[recovery] applied hypothesis " + strconv.Itoa(h.N) + "'s params for its test: " + recovery.Sig(h.Params)
			}
		}
		if len(params) == 0 && !att.trial && g != nil && g.State == recovery.StateActive {
			// The real attempt after a passing test uses the tested params.
			for i := len(g.Hyps) - 1; i >= 0; i-- {
				if h := g.Hyps[i]; h.Outcome == "supported" && len(h.Params) > 0 {
					for k, v := range h.Params {
						args[k] = v
					}
					params = h.Params
					att.note = "\n[recovery] applied supported hypothesis " + strconv.Itoa(h.N) + "'s params: " + recovery.Sig(h.Params)
					break
				}
			}
		}
		if len(params) == 0 && !att.trial && (g == nil || g.State == recovery.StateActive) {
			for _, r := range eng.Recipes(sk, att.surface, "publish") {
				if g != nil && g.Failed[recovery.Sig(r.Params)] != "" {
					continue
				}
				for k, v := range r.Params {
					args[k] = v
				}
				params, att.recipe = r.Params, r.ID
				att.note = fmt.Sprintf("\n[recovery] applied verified recipe %s (%s; verified %d time(s), last %s) — this attempt re-verifies it; one failure retires it", r.ID, recovery.Sig(r.Params), r.Verified, r.Last.In(time.Local).Format("2006-01-02"))
				break
			}
		}
		att.params = params
		att.strategy = recovery.Sig(params)
		desktopRecNoteRunGoal(scope.RunID, sk+"\x01"+goal)
		// A cheap precheck; the attempt itself is admitted atomically when
		// the pipeline acquires the goal (desktopRecoveryStartPublish).
		if g != nil && g.State == recovery.StateStopped && !att.trial && !desktopRecRanGoal(g, scope.RunID) {
			// The person asked again (a new run of the same instruction):
			// the earlier unconfirmed post is looked up in the account's
			// post list before anything is filled (desktopPublishLedgerGuard).
			var ps desktopRecPublishSpec
			_ = json.Unmarshal([]byte(g.Spec), &ps)
			att.recheck, att.recheckTitle = true, ps.Title
		} else if err := eng.Gate(sk, goal, "publish", att.strategy); err != nil {
			return nil, err
		}
		desktopRecStateMu.Lock()
		desktopRecPendingAtt[desktopRecKey(scope)] = att
		desktopRecStateMu.Unlock()
		return att, nil
	}
	// A mutating step is part of the attempt of every unfinished check goal
	// of this task: it is gated by all of them (renaming a goal in
	// goal_check does not escape one that waits for a diagnosis), and its
	// time is bounded by the attempt time they have left.
	gs := eng.Goals(sk, desktopRecCheckPrefix(lineage))
	if len(gs) == 0 {
		return nil, nil
	}
	// Each goal reserves the step's time in its ledger before the step
	// runs (durable across a crash or restart); the step's deadline is the
	// smallest reservation.
	att := &desktopRecAttempt{sk: sk, method: "check"}
	want := desktopRecStepWant(action)
	for _, g := range gs {
		if g.State == recovery.StateDone {
			if desktopRecReopens[action] {
				eng.Reopen(sk, g.Key, desktopRecReopenWhy)
			} else {
				continue
			}
		}
		id, budget, err := eng.BeginStep(sk, g.Key, scope.RunID, desktopRecCheckStrategy(g), want)
		if err != nil {
			att.settle(0) // nothing ran: give the other reservations back
			return nil, fmt.Errorf("%v (goal_check %q)", err, desktopRecCheckName(g.Key))
		}
		if id == "" {
			continue // expired meanwhile
		}
		att.steps = append(att.steps, desktopRecStep{goal: g.Key, id: id, reserved: budget})
		if att.deadline == 0 || budget < att.deadline {
			att.deadline = budget
		}
		att.checks = append(att.checks, g.Key)
		desktopRecNoteRunGoal(scope.RunID, sk+"\x01"+g.Key)
	}
	if len(att.checks) == 0 {
		return nil, nil
	}
	return att, nil
}

// desktopRecStep is one durable step reservation (recovery.BeginStep).
type desktopRecStep struct {
	goal, id string
	reserved time.Duration
}

// desktopRecStepWant is the reservation a step asks for: its own hard time
// limit (the step cannot run longer), cut to the goal's time left.
func desktopRecStepWant(action string) time.Duration {
	if action == "rich_fill" {
		return desktopStepTimeout("publish")
	}
	return desktopStepTimeout(action)
}

// settle ends the step's reservations with its real duration, once, at the
// step's real end (a step that outlived its tool call settles when it
// returns; one whose process died stays charged in full).
func (a *desktopRecAttempt) settle(took time.Duration) {
	if a == nil || len(a.steps) == 0 {
		return
	}
	a.settled.Do(func() {
		eng := desktopRecovery()
		for _, st := range a.steps {
			eng.EndStep(a.sk, st.goal, st.id, st.reserved, took)
		}
	})
}

type desktopStepEndKey struct{}

// withDesktopStepEnd lets the code that really runs a step report its end,
// also after the tool call stopped waiting for it.
func withDesktopStepEnd(ctx context.Context, fn func()) context.Context {
	return context.WithValue(ctx, desktopStepEndKey{}, fn)
}

func desktopStepEnded(ctx context.Context) {
	if fn, ok := ctx.Value(desktopStepEndKey{}).(func()); ok && fn != nil {
		fn()
	}
}

// desktopRecBoundCommand makes a container command under a check goal stop
// at the step's deadline inside the container too (not only the wait for
// it), where coreutils timeout exists.
func desktopRecBoundCommand(command string, limit time.Duration) string {
	secs := int((limit + time.Second - 1) / time.Second)
	if secs <= 0 || strings.TrimSpace(command) == "" {
		return command
	}
	q := "'" + strings.ReplaceAll(command, "'", `'\''`) + "'"
	return fmt.Sprintf("if command -v timeout >/dev/null 2>&1; then exec timeout -k 5 %d bash -c %s; else exec bash -c %s; fi", secs, q, q)
}

func desktopRecCheckName(key string) string {
	if i := strings.LastIndex(key, "|"); i >= 0 {
		return key[i+1:]
	}
	return key
}

// desktopRecoveryStartPublish acquires the publish goal for the attempt
// about to run, from inside the pipeline: from here until the pipeline's own
// end (even past the tool call's time limit) no other attempt — in this or
// any process sharing the ledger — starts on the goal. The submit click
// waits for the SideEffect handshake; the pipeline's deadline is cut to the
// goal's attempt time left.
func desktopRecoveryStartPublish(scope agentruntime.Scope, spec *browser.PublishSpec) (*desktopRecAttempt, error) {
	desktopRecStateMu.Lock()
	att := desktopRecPendingAtt[desktopRecKey(scope)]
	delete(desktopRecPendingAtt, desktopRecKey(scope))
	desktopRecStateMu.Unlock()
	if att == nil {
		return nil, nil
	}
	eng := desktopRecovery()
	// The pipeline's resolved editor is the authority on which site this
	// attempt is for; the call-time guess (editor_url or the open tab) may
	// name another one. Re-key and re-gate before anything is filled.
	if site := siteExpKey(spec.EditorURL); site != "" {
		if parts := strings.SplitN(att.goal, "|", 4); len(parts) >= 3 && parts[0] == "publish" && parts[2] != site {
			goal := desktopRecPublishGoal(att.sk, parts[1], site, spec.Title)
			if err := eng.Gate(att.sk, goal, "publish", att.strategy); err != nil {
				return nil, fmt.Errorf("%v (nothing was filled or posted)", err)
			}
			log.Printf("[recovery] publish goal re-keyed %s -> %s (pipeline target)", att.goal, goal)
			att.goal, att.surface = goal, "site:"+site+"|publish"
			desktopRecNoteRunGoal(scope.RunID, att.sk+"\x01"+goal)
		}
	}
	if att.recheck && att.recheckOK {
		if eng.ReopenStopped(att.sk, att.goal, scope.RunID, "reopened: the earlier unconfirmed post is not in the account's post list") {
			log.Printf("[recovery] publish goal %s reopened for run %s after a post-list check", att.goal, scope.RunID)
		}
	}
	req, _ := json.Marshal(desktopRecPublishSpec{Pictures: len(spec.Files), Body: len([]rune(strings.TrimSpace(spec.Body))), Title: desktopRecHash(spec.Title)})
	lease, g, err := eng.Acquire(att.sk, recovery.AttemptRequest{Goal: att.goal, Surface: att.surface, Method: "publish", Strategy: att.strategy, Run: scope.RunID, Consequential: !att.trial, Spec: string(req), Check: desktopRecPublishCheck})
	if err != nil {
		return nil, fmt.Errorf("%v (nothing was filled or posted)", err)
	}
	if left := eng.WallLeft(g); time.Now().Add(left).Before(spec.Deadline) || spec.Deadline.IsZero() {
		spec.Deadline = time.Now().Add(left)
	}
	att.mu.Lock()
	att.lease, att.started, att.stop = lease, true, make(chan struct{})
	att.mu.Unlock()
	if !att.trial {
		spec.BeforeSubmit = func() error { return eng.SideEffect(att.sk, att.goal, lease.ID) }
	}
	go func(stop chan struct{}) {
		t := time.NewTicker(eng.Cfg.LeaseTTL / 3)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := eng.Beat(att.sk, att.goal, lease.ID); err == recovery.ErrLeaseLost {
					return
				}
			}
		}
	}(att.stop)
	desktopRecStateMu.Lock()
	desktopRecLive[desktopRecKey(scope)] = att
	desktopRecStateMu.Unlock()
	return att, nil
}

// desktopRecoveryFinishPublish records the pipeline's measured result at
// its real end and releases the goal.
func desktopRecoveryFinishPublish(scope agentruntime.Scope, rep *browser.PublishReport, err error) {
	desktopRecStateMu.Lock()
	att := desktopRecLive[desktopRecKey(scope)]
	delete(desktopRecLive, desktopRecKey(scope))
	desktopRecStateMu.Unlock()
	if att == nil {
		return
	}
	att.mu.Lock()
	defer att.mu.Unlock()
	if att.finished || att.lease == nil {
		return
	}
	close(att.stop)
	facts := map[string]float64{}
	state, trial := "", false
	if rep != nil {
		for k, v := range rep.Facts {
			facts[k] = v
		}
		state, trial = string(rep.State), rep.Trial
	}
	o := recovery.Observation{Goal: att.goal, Surface: att.surface, Method: att.method, Strategy: att.strategy, Params: att.params, Recipe: att.recipe, Run: scope.RunID, OK: err == nil, Trial: trial || att.trial, Facts: facts}
	if err != nil {
		o.Error = desktopRecFirstLine(err.Error())
		// The browser or the site did not answer before the strategy could
		// matter: not evidence about it (no refutation, no failed mark).
		o.Infra = desktopRecInfraFailure(rep, err)
	}
	if state == string(browser.PublishSubmitted) {
		o.Side = "unconfirmed"
	}
	want, hasWant := facts["pictures_wanted"]
	o.Complete = err == nil && !o.Trial && facts["published"] == 1 && (!hasWant || facts["pictures_kept"] >= want)
	att.verdict = desktopRecovery().Finish(att.sk, att.lease.ID, o)
	att.finished = true
	log.Printf("[recovery] goal=%s verdict=%s state=%s strategy=%q", att.goal, att.verdict.Kind, att.verdict.State, att.strategy)
}

// desktopRecoveryAbortPublish ends an acquired attempt whose pipeline never
// ran (a panic or refusal before RunPublish): nothing was done.
func desktopRecoveryAbortPublish(scope agentruntime.Scope, att *desktopRecAttempt) {
	if att == nil {
		return
	}
	desktopRecStateMu.Lock()
	if desktopRecLive[desktopRecKey(scope)] == att {
		delete(desktopRecLive, desktopRecKey(scope))
	}
	desktopRecStateMu.Unlock()
	att.mu.Lock()
	defer att.mu.Unlock()
	if att.finished || att.lease == nil {
		return
	}
	close(att.stop)
	desktopRecovery().Abandon(att.sk, att.goal, att.lease.ID)
	att.finished, att.started = true, false
}

// desktopRecoveryAfter returns the evidence text for the step.
func desktopRecoveryAfter(scope agentruntime.Scope, att *desktopRecAttempt, out string, err error, took time.Duration) string {
	if att == nil {
		return ""
	}
	if att.method == "check" {
		// Settled where the step really ended (desktopStepEnded); a step
		// still running past its deadline keeps its reservation until then.
		return ""
	}
	desktopRecStateMu.Lock()
	if desktopRecPendingAtt[desktopRecKey(scope)] == att {
		delete(desktopRecPendingAtt, desktopRecKey(scope))
	}
	desktopRecStateMu.Unlock()
	att.mu.Lock()
	started, finished, v := att.started, att.finished, att.verdict
	att.mu.Unlock()
	switch {
	case !started:
		// The pipeline did not run (a guard or record answered): no attempt.
		return att.note
	case !finished:
		return att.note + "\n[recovery] the attempt is still running past this step's time limit and keeps holding the goal: no other publish of it starts until it ends; its measured result goes to the task ledger. Do not publish again — observe the page and report"
	case v.Kind == recovery.KindVerified:
		text := att.note + "\n[recovery] goal verified by the site's own result"
		if v.Recipe != nil {
			text += fmt.Sprintf("; recipe %s recorded for this site (params only, re-verified on every use)", v.Recipe.ID)
		}
		return text
	}
	return att.note + "\n" + v.Evidence
}

// desktopRecInfraFailure: the attempt failed on the infrastructure (CDP or
// navigate timeout, the editor never opened), before or outside anything the
// strategy controls. A submit that may have gone out is never infra.
func desktopRecInfraFailure(rep *browser.PublishReport, err error) bool {
	if err == nil {
		return false
	}
	if rep != nil && (rep.State == browser.PublishSubmitted || rep.State == browser.PublishLive || rep.State == browser.PublishLiveIncomplete) {
		return false
	}
	msg := strings.ToLower(desktopRecFirstLine(err.Error()))
	if !desktopInfraErrRe.MatchString(msg) {
		return false
	}
	// A timeout inside the steps a strategy changes is only infra when the
	// browser itself stopped answering (a CDP timeout), not a slow editor.
	if rep != nil && rep.Stuck != "" && rep.Stuck != "observe" && !strings.Contains(msg, "cdp timeout") {
		return false
	}
	return true
}

func desktopRecFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i > 0 {
		s = s[:i]
	}
	return s
}

// ---- action=recover ----

func desktopRecover(request agentruntime.TurnRequest, scope agentruntime.Scope, args map[string]any) (string, error) {
	eng := desktopRecovery()
	sk := desktopRecoveryScope(scope)
	lineage, err := desktopRecLineage(request, scope)
	if err != nil {
		return "", err
	}
	// Only a goal of this task can be diagnosed (the model names it; the
	// task's identity is not the model's to choose).
	var mine, waiting []string
	for _, g := range append(eng.Goals(sk, "publish|"+lineage+"|"), eng.Goals(sk, desktopRecCheckPrefix(lineage))...) {
		mine = append(mine, g.Key)
		if g.State == recovery.StateDiagnose {
			waiting = append(waiting, g.Key)
		}
	}
	goal := strings.TrimSpace(desktopArg(args, "goal"))
	if goal != "" {
		found := ""
		for _, k := range mine {
			if k == goal || desktopRecCheckName(k) == goal || (strings.HasPrefix(k, "check|") && "check:"+desktopRecCheckName(k) == goal) {
				found = k
				break
			}
		}
		if found == "" && len(waiting) == 1 && strings.HasPrefix(goal, "publish") && strings.HasPrefix(waiting[0], "publish|") {
			found = waiting[0]
		}
		if found == "" && strings.HasPrefix(strings.ToLower(goal), "publish") {
			// "publish", "publish zhihu", "publish|csdn": the waiting publish
			// goal of the named site, else of the site being worked on.
			found = desktopRecWaitingFor(waiting, strings.ToLower(goal), desktopPublishSiteFor(scope, args))
		}
		if found == "" && len(waiting) == 0 && strings.HasPrefix(strings.ToLower(goal), "publish") {
			// Nothing of this task waits for a diagnosis: the publish
			// attempts never got as far as the site (run_5ecb9d6d: two CDP
			// dial failures, then "is not a goal of this task" for a goal the
			// person did ask for). Say what is known instead of refusing.
			return desktopRecoverNothingWaiting(scope, args), nil
		}
		if found == "" {
			return "", fmt.Errorf("recover: %q is not a goal of this task (goals waiting for a diagnosis: %s)", goal, strings.Join(waiting, ", "))
		}
		goal = found
	} else {
		if len(waiting) != 1 {
			return "", fmt.Errorf("recover: pass goal= (goals of this task waiting for a diagnosis: %s)", strings.Join(waiting, ", "))
		}
		goal = waiting[0]
	}
	raw := map[string]string{}
	if m, ok := args["params"].(map[string]any); ok {
		for k, v := range m {
			raw[k] = desktopRecArgString(v)
		}
	}
	validate := func(p map[string]string) error {
		if strings.HasPrefix(goal, "publish|") {
			_, err := desktopRecPublishParams(p)
			return err
		}
		if len(p) > 0 {
			return fmt.Errorf("a check goal's strategy is described in strategy=; it has no params")
		}
		return nil
	}
	params := raw
	if strings.HasPrefix(goal, "publish|") {
		var err error
		if params, err = desktopRecPublishParams(raw); err != nil {
			return "", fmt.Errorf("recover: params: %v", err)
		}
	}
	p := recovery.Proposal{
		Hypothesis:  desktopArg(args, "hypothesis"),
		Uncertainty: desktopArg(args, "uncertainty"),
		Strategy:    desktopArg(args, "strategy"),
		Params:      params,
		Test:        desktopArg(args, "test"),
		Expect:      desktopStrList(args, "expect"),
	}
	text, err := eng.Propose(sk, goal, scope.RunID, p, validate)
	if err != nil {
		return "", err
	}
	desktopRecNoteRunGoal(scope.RunID, sk+"\x01"+goal)
	log.Printf("[recovery] goal=%s hypothesis registered strategy=%q", goal, recovery.Sig(params))
	return text, nil
}

// ---- action=goal_check ----

type desktopRecCriterion struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}

var desktopRecFactName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func desktopRecCriteria(args map[string]any) ([]desktopRecCriterion, error) {
	list, _ := args["criteria"].([]any)
	var out []desktopRecCriterion
	for _, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("goal_check: each criterion is an object {kind, path, value}")
		}
		c := desktopRecCriterion{Kind: desktopArg(m, "kind"), Path: desktopArg(m, "path"), Value: desktopRecArgString(m["value"])}
		switch c.Kind {
		case "file_exists":
		case "file_min_lines", "file_min_bytes", "json_min_items":
			if n, err := strconv.Atoi(strings.TrimSpace(c.Value)); err != nil || n < 1 {
				return nil, fmt.Errorf("goal_check: %s needs value ≥1", c.Kind)
			}
		case "file_contains":
			if strings.TrimSpace(c.Value) == "" || len(c.Value) > 200 {
				return nil, fmt.Errorf("goal_check: file_contains needs value (≤200 chars)")
			}
		default:
			return nil, fmt.Errorf("goal_check: kind %q is not one of file_exists, file_min_lines, file_min_bytes, file_contains, json_min_items", c.Kind)
		}
		if !strings.HasPrefix(c.Path, "/") {
			return nil, fmt.Errorf("goal_check: path must be absolute on the desktop")
		}
		out = append(out, c)
	}
	if len(out) > 8 {
		return nil, fmt.Errorf("goal_check: at most 8 criteria")
	}
	return out, nil
}

// desktopRecMeasure reads each criterion's file and returns the facts.
func desktopRecMeasure(ctx context.Context, scope agentruntime.Scope, cs []desktopRecCriterion) (map[string]float64, []string, map[string]string) {
	facts := map[string]float64{"criteria_total": float64(len(cs))}
	var missed []string
	good := map[string]string{}
	met := 0
	for _, c := range cs {
		base := desktopRecFactName.ReplaceAllString(path.Base(c.Path), "_")
		content, err := desktopContainerFile(ctx, scope, "file_read", map[string]any{"path": c.Path})
		exists := err == nil
		ok := false
		switch c.Kind {
		case "file_exists":
			facts["exists:"+base] = b2f(exists)
			ok = exists
		case "file_min_lines":
			n := 0
			if exists {
				n = strings.Count(strings.TrimRight(content, "\n"), "\n") + 1
				if strings.TrimSpace(content) == "" {
					n = 0
				}
			}
			facts["lines:"+base] = float64(n)
			want, _ := strconv.Atoi(c.Value)
			ok = n >= want
		case "file_min_bytes":
			facts["bytes:"+base] = float64(len(content))
			want, _ := strconv.Atoi(c.Value)
			ok = exists && len(content) >= want
		case "file_contains":
			has := exists && strings.Contains(content, c.Value)
			facts["contains:"+base] = b2f(has)
			ok = has
		case "json_min_items":
			n := -1
			if exists {
				var arr []json.RawMessage
				if json.Unmarshal([]byte(content), &arr) == nil {
					n = len(arr)
				}
			}
			if n < 0 {
				facts["items:"+base] = 0
				facts["invalid_json:"+base] = 1
			} else {
				facts["items:"+base] = float64(n)
			}
			want, _ := strconv.Atoi(c.Value)
			ok = n >= want
		}
		if exists && facts["invalid_json:"+base] == 0 {
			good[c.Path] = content
		}
		if ok {
			met++
		} else {
			why := c.Kind + " " + c.Path
			if c.Value != "" {
				why += " " + c.Value
			}
			if !exists {
				why += " (file missing)"
			}
			missed = append(missed, why)
		}
	}
	facts["criteria_met"] = float64(met)
	return facts, missed, good
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

const desktopRecLastGood = ".maclaw-lastgood"

func desktopGoalCheck(ctx context.Context, request agentruntime.TurnRequest, scope agentruntime.Scope, args map[string]any) (string, error) {
	name := strings.TrimSpace(desktopArg(args, "goal"))
	if name == "" || len([]rune(name)) > 60 {
		return "", fmt.Errorf("goal_check: goal= names the result being checked (≤60 chars), e.g. \"orders.json\"")
	}
	name = strings.TrimPrefix(name, "check:")
	if strings.ContainsAny(name, "|\x00\x01") {
		return "", fmt.Errorf("goal_check: goal= may not contain |")
	}
	eng := desktopRecovery()
	sk := desktopRecoveryScope(scope)
	lineage, err := desktopRecLineage(request, scope)
	if err != nil {
		return "", err
	}
	key := desktopRecCheckPrefix(lineage) + name
	cs, err := desktopRecCriteria(args)
	if err != nil {
		return "", err
	}
	spec := ""
	if len(cs) > 0 {
		b, _ := json.Marshal(cs)
		spec = string(b)
	}
	g, err := eng.Pin(sk, key, spec)
	if err != nil {
		return "", err
	}
	if len(cs) == 0 {
		if g.Spec == "" {
			return "", fmt.Errorf("goal_check: the first check of a goal gives criteria: [{kind, path, value}] from the person's request")
		}
		_ = json.Unmarshal([]byte(g.Spec), &cs)
	}
	desktopRecNoteRunGoal(scope.RunID, sk+"\x01"+key)
	if scope.RunID != "" {
		desktopRecStateMu.Lock()
		if len(desktopRecCheckScopes) > 4096 {
			desktopRecCheckScopes = map[string]agentruntime.Scope{}
		}
		desktopRecCheckScopes[scope.RunID] = scope
		desktopRecStateMu.Unlock()
	}
	facts, missed, good := desktopRecMeasure(ctx, scope, cs)
	if g.Count == 0 && len(missed) > 0 {
		// The baseline before any work: not an attempt.
		return fmt.Sprintf("[goal_check] %s pinned; baseline: %s\nnot met yet: %s", name, desktopRecFacts2(facts), strings.Join(missed, "; ")), nil
	}
	complete := len(missed) == 0
	o := recovery.Observation{Goal: key, Surface: "check", Method: "check", Strategy: desktopRecCheckStrategy(g), Run: scope.RunID, OK: complete, Complete: complete, Facts: facts}
	if !complete {
		o.Error = "not met: " + strings.Join(missed, "; ")
	}
	prevBest := g.Best
	v := eng.Observe(sk, o)
	log.Printf("[recovery] goal=%s verdict=%s state=%s", key, v.Kind, v.State)
	text := "[goal_check] " + name + ": " + map[bool]string{true: "all criteria met", false: "NOT met"}[complete] + "\n" + v.Evidence
	// Keep the best version so far, so a later attempt cannot destroy it.
	if v.Kind != recovery.KindRegression && desktopRecImproved(prevBest, facts) && botPhaseFromTurn(request) != "plan" {
		var saved []string
		for p, content := range good {
			if _, werr := desktopContainerFile(ctx, scope, "file_write", map[string]any{"path": p + desktopRecLastGood, "content": content}); werr == nil {
				saved = append(saved, p+desktopRecLastGood)
			}
		}
		sort.Strings(saved)
		if len(saved) > 0 {
			text += "\nlast good copy kept: " + strings.Join(saved, ", ")
		}
	}
	if v.Kind == recovery.KindRegression {
		var have []string
		for _, c := range cs {
			if _, rerr := desktopContainerFile(ctx, scope, "file_read", map[string]any{"path": c.Path + desktopRecLastGood}); rerr == nil {
				have = append(have, c.Path+desktopRecLastGood)
			}
		}
		if len(have) > 0 {
			text += "\nthe last good version is at " + strings.Join(have, ", ") + "; restore it before the next attempt"
		}
	}
	return text, nil
}

func desktopRecImproved(best, f map[string]float64) bool {
	for k, v := range f {
		if strings.HasPrefix(k, "criteria_total") || strings.HasPrefix(k, "invalid_json:") {
			continue
		}
		if b, ok := best[k]; !ok || v > b {
			return true
		}
	}
	return false
}

func desktopRecFacts2(f map[string]float64) string {
	return strings.TrimPrefix(recovery.FactsLine(f), "[facts] ")
}

// ---- honest outcome for the receipt ----

// desktopRecoveryOutcome is the task outcome the run's goals prove, with
// one line of evidence: "" (nothing tracked), "done" (every goal verified by
// measurement), "partial", "blocked", "not_submitted" or "unconfirmed".
// The goals are the ones this run touched and, when it made desktop steps,
// every unfinished goal of its task — a check goal counts whether or not the
// model called goal_check again after its last change.
func desktopRecoveryOutcome(runID string) (string, string) {
	eng := desktopRecovery()
	desktopRecStateMu.Lock()
	keys := append([]string(nil), desktopRecRuns[runID]...)
	lin, steps := desktopRecRunLineage[runID], desktopRecRunSteps[runID]
	desktopRecStateMu.Unlock()
	goals := map[string]*recovery.Goal{}
	for _, k := range keys {
		sk, goal, _ := strings.Cut(k, "\x01")
		if g := eng.Status(sk, goal); g != nil {
			goals[k] = g
		}
	}
	if sk, l, ok := strings.Cut(lin, "\x01"); ok && steps {
		for _, g := range append(eng.Goals(sk, "publish|"+l+"|"), eng.Goals(sk, desktopRecCheckPrefix(l))...) {
			if g.State != recovery.StateDone {
				goals[sk+"\x01"+g.Key] = g
			}
		}
	}
	outcome, why := "", ""
	rank := map[string]int{"": 0, "done": 1, "not_submitted": 2, "partial": 3, "blocked": 4, "unconfirmed": 5}
	var done, notDone []string
	for _, g := range goals {
		o := ""
		switch {
		case g.State == recovery.StateDone:
			o = "done"
		case g.Lease != nil && g.Lease.Phase == recovery.PhaseSideEffect, g.State == recovery.StateStopped:
			o = "unconfirmed"
		case g.Count == 0 && g.Lease == nil && len(g.Attempts) == 0 && strings.HasPrefix(g.Key, "publish|"):
			continue // gated, never attempted
		case len(g.Best) > 0 && desktopRecSomeDone(g.Best):
			o = "partial"
		case g.State == recovery.StateExhausted || g.State == recovery.StateBlocked || strings.HasPrefix(g.Key, "check|"):
			o = "blocked"
		default:
			o = "not_submitted"
		}
		if o == "done" {
			done = append(done, desktopRecGoalLabel(g.Key))
		} else {
			r := g.Reason
			if r == "" {
				r = g.LastKind
			}
			notDone = append(notDone, desktopRecGoalLabel(g.Key)+" "+o+desktopClipWhy(r))
		}
		if rank[o] > rank[outcome] {
			outcome = o
			why = g.Reason
			if why == "" {
				why = g.LastKind
			}
			if o == "done" {
				why = "verified by measurement"
			}
			if len(g.Best) > 0 {
				why += "；已测得 " + desktopRecFacts2(g.Best)
			}
		}
	}
	if len(done) > 0 {
		// A requested site nobody reached is not done either.
		doneSite := map[string]bool{}
		for _, d := range done {
			doneSite[strings.TrimPrefix(d, "publish ")] = true
		}
		for _, p := range desktopTaskSitesPending(runID) {
			if !doneSite[p.ID] {
				notDone = append(notDone, "publish "+p.ID+" not_submitted: not reached")
				if rank[outcome] < rank["not_submitted"] {
					outcome = "not_submitted"
				}
			}
		}
	}
	if len(done) > 0 && len(notDone) > 0 && (outcome == "not_submitted" || outcome == "blocked") {
		// Some parts verified, others not: partial, and the evidence names
		// every part (run_854d0758 reported not_submitted with the evidence
		// of one stray goal while 小红书 was verified published).
		sort.Strings(done)
		sort.Strings(notDone)
		outcome = "partial"
		why = "done: " + strings.Join(done, ", ") + "；not done: " + strings.Join(notDone, "；")
	}
	return outcome, why
}

// desktopRecoveryEndRun drops a finished run's in-process bookkeeping.
func desktopRecoveryEndRun(runID string) {
	desktopClearFinish(runID)
	desktopRecStateMu.Lock()
	defer desktopRecStateMu.Unlock()
	delete(desktopRecRuns, runID)
	delete(desktopRecRunLineage, runID)
	delete(desktopRecRunSteps, runID)
	delete(desktopRecCheckScopes, runID)
}

// desktopRecRecheckAtEnd measures again, at the run's completion, every
// check goal of this run that a later step reopened. Bash steps after a
// passing goal_check (taking screenshots, reading files) reopened the snake
// game's goal and the run ended "partial" although all 6 criteria still
// held (Nov 11 10:57). The result is a real measurement either way: met
// closes the goal as done, not met leaves it open with what is missing.
func desktopRecRecheckAtEnd(ctx context.Context, runID string) {
	if runID == "" {
		return
	}
	desktopRecStateMu.Lock()
	scope, ok := desktopRecCheckScopes[runID]
	keys := append([]string(nil), desktopRecRuns[runID]...)
	desktopRecStateMu.Unlock()
	if !ok {
		return
	}
	eng := desktopRecovery()
	for _, k := range keys {
		sk, key, _ := strings.Cut(k, "\x01")
		g := eng.Status(sk, key)
		if g == nil || g.State == recovery.StateDone || g.Reason != desktopRecReopenWhy || g.Spec == "" || !strings.HasPrefix(key, "check|") {
			continue
		}
		var cs []desktopRecCriterion
		if json.Unmarshal([]byte(g.Spec), &cs) != nil || len(cs) == 0 {
			continue
		}
		facts, missed, _ := desktopRecMeasureAtEnd(ctx, scope, cs)
		complete := len(missed) == 0
		o := recovery.Observation{Goal: key, Surface: "check", Method: "check", Strategy: desktopRecCheckStrategy(g), Run: runID, OK: complete, Complete: complete, Facts: facts}
		if !complete {
			o.Error = "not met: " + strings.Join(missed, "; ")
		}
		v := eng.Observe(sk, o)
		log.Printf("[recovery] run-end recheck run=%s goal=%s complete=%v verdict=%s state=%s", runID, key, complete, v.Kind, v.State)
	}
}

// desktopRecMeasureAtEnd is a test seam over desktopRecMeasure.
var desktopRecMeasureAtEnd = desktopRecMeasure

func desktopRecSomeDone(best map[string]float64) bool {
	for k, v := range best {
		if v > 0 && !strings.HasSuffix(k, "_wanted") && !strings.HasSuffix(k, "_total") && k != "trial" {
			return true
		}
	}
	return false
}

// desktopRecoveryPrompt tells the model how to use the mechanism, and the
// state of any goal of this person still waiting for a diagnosis.
func desktopRecoveryPrompt() string {
	return "Self-correction: when a task's result can be checked from files, call action=goal_check first with goal= and criteria=[{kind:file_exists|file_min_lines|file_min_bytes|file_contains|json_min_items, path, value}] taken from the person's request, and again after each change. " +
		"When a tool result says RECOVERY_REQUIRED, do not repeat what failed, do not blame the site without evidence, and do not lower the goal or use placeholder content: diagnose from the facts and call action=recover with hypothesis, uncertainty, strategy (meaningfully different), params (publish only: insert_prep_keys, insert_settle_ms, body_selector, title_selector, upload_selector, upload_trigger), test (publish tests use trial_pictures=1..3, which never submits) and expect (facts like pictures_kept>=2). " +
		"A test's facts decide; a hypothesis is not a fact. RECOVERY_EXHAUSTED or STOPPED means stop and tell the person plainly what is done, what is missing and the evidence. "
}

// desktopRecWaitingFor picks the waiting publish goal a loosely named goal
// means: the one whose site the name contains, else the current site's.
func desktopRecWaitingFor(waiting []string, name, current string) string {
	pick := func(match func(site string) bool) string {
		found := ""
		for _, k := range waiting {
			parts := strings.Split(k, "|")
			if len(parts) < 3 || parts[0] != "publish" || !match(parts[2]) {
				continue
			}
			if found != "" {
				return ""
			}
			found = k
		}
		return found
	}
	if k := pick(func(site string) bool { return strings.Contains(name, site) }); k != "" {
		return k
	}
	return pick(func(site string) bool { return site == current })
}

// desktopRecoverNothingWaiting: the answer to recover when this run's
// publish has no goal waiting for a diagnosis.
func desktopRecoverNothingWaiting(scope agentruntime.Scope, args map[string]any) string {
	site := desktopPublishSiteFor(scope, args)
	var blocked []string
	if st := desktopRunFor(desktopOwner(scope)); st != nil {
		st.mu.Lock()
		for intent, class := range st.blocked {
			blocked = append(blocked, intent+" ("+class+")")
		}
		st.mu.Unlock()
	}
	out := "recover: no publish goal of this run is waiting for a diagnosis"
	if site != "" {
		out += " (site " + site + ")"
	}
	out += ": the publish attempts did not reach the site's editor, so there is no measured result to diagnose and no strategy was refuted."
	if len(blocked) > 0 {
		sort.Strings(blocked)
		out += " Blocked in this run: " + strings.Join(blocked, ", ") + "."
	}
	if desktopRouteDown(scope.RunID) {
		out += " The browser connection is failing; the next browser step reconnects first (screenshot shows the screen meanwhile)."
	}
	return out + " Next: call action=publish again (it reconnects and opens a fresh editor), or request_takeover, or report this part as not done with the error."
}
