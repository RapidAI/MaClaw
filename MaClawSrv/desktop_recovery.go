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

// In-process state: facts the publish pipeline measured for the call in
// flight, the goals each run touched (for the honest receipt) and the time
// spent in mutating calls since a check goal's last measurement.
var (
	desktopRecStateMu sync.Mutex
	desktopRecFacts   = map[string]desktopRecPublishFacts{}
	desktopRecRuns    = map[string][]string{} // runID -> goal keys
	desktopRecPending = map[string]time.Duration{}
	desktopRecCheck   = map[string]string{} // runID -> active check goal key
)

type desktopRecPublishFacts struct {
	facts map[string]float64
	state string
	trial bool
}

func desktopRecKey(scope agentruntime.Scope) string {
	o := desktopOwner(scope)
	return o.TenantID + "\x00" + o.UserID + "\x00" + scope.RunID
}

// desktopRecoveryNotePublish is called by the publish pipeline with its
// structured report (trusted measurements).
func desktopRecoveryNotePublish(scope agentruntime.Scope, rep *browser.PublishReport) {
	if rep == nil {
		return
	}
	f := map[string]float64{}
	for k, v := range rep.Facts {
		f[k] = v
	}
	desktopRecStateMu.Lock()
	desktopRecFacts[desktopRecKey(scope)] = desktopRecPublishFacts{facts: f, state: string(rep.State), trial: rep.Trial}
	desktopRecStateMu.Unlock()
}

func desktopRecTakeFacts(scope agentruntime.Scope) (desktopRecPublishFacts, bool) {
	desktopRecStateMu.Lock()
	defer desktopRecStateMu.Unlock()
	k := desktopRecKey(scope)
	f, ok := desktopRecFacts[k]
	delete(desktopRecFacts, k)
	return f, ok
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

type desktopRecAttempt struct {
	sk, goal, surface, method, strategy string
	params                              map[string]string
	trial                               bool
	recipe                              string
	check                               bool // a mutating step under a check goal
	note                                string
}

var desktopRecMutating = map[string]bool{"bash": true, "file_write": true, "file_edit": true, "script": true, "click": true, "type": true, "press": true, "computer": true, "app_run": true, "task_run": true, "upload": true, "rich_fill": true, "skill_run": true}

func desktopRecPublishGoal(scope agentruntime.Scope, args map[string]any, task string) (goal, surface string) {
	site := siteExpKey(desktopArg(args, "editor_url"))
	if site == "" {
		site = desktopRunSite(desktopOwner(scope))
	}
	if site == "" {
		site = "unknown"
	}
	what := desktopArg(args, "title")
	if strings.TrimSpace(what) == "" {
		what = task
	}
	return "publish|" + site + "|" + desktopRecHash(what), "site:" + site + "|publish"
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

// desktopRecoveryBefore gates one tool call. It may fill args from a
// verified recipe (returned note says so) or refuse the call.
func desktopRecoveryBefore(scope agentruntime.Scope, action string, args map[string]any, task string) (*desktopRecAttempt, error) {
	eng := desktopRecovery()
	sk := desktopRecoveryScope(scope)
	switch {
	case action == "publish" && !desktopArgBool(args, "explore"):
		params, err := desktopRecPublishArgsParams(args)
		if err != nil {
			return nil, fmt.Errorf("publish refused: %v (nothing was filled or posted)", err)
		}
		goal, surface := desktopRecPublishGoal(scope, args, task)
		att := &desktopRecAttempt{sk: sk, goal: goal, surface: surface, method: "publish", trial: desktopRecInt(args, "trial_pictures") > 0}
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
			for _, r := range eng.Store.Recipes(sk, surface, "publish", eng.Cfg.RecipeMaxAge, eng.Now()) {
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
		if err := eng.Gate(sk, goal, "publish", att.strategy); err != nil {
			return nil, err
		}
		return att, nil
	case desktopRecMutating[action]:
		desktopRecStateMu.Lock()
		goal := desktopRecCheck[desktopRecKey(scope)]
		desktopRecStateMu.Unlock()
		if goal == "" {
			return nil, nil
		}
		g := eng.Status(sk, goal)
		if g == nil || g.State == recovery.StateDone {
			return nil, nil
		}
		if err := eng.Gate(sk, goal, "check", desktopRecCheckStrategy(g)); err != nil {
			return nil, fmt.Errorf("%v (goal_check %q)", err, strings.TrimPrefix(goal, "check:"))
		}
		return &desktopRecAttempt{sk: sk, goal: goal, method: "check", check: true}, nil
	}
	return nil, nil
}

// desktopRecoveryAfter records the attempt and returns the evidence text.
func desktopRecoveryAfter(scope agentruntime.Scope, att *desktopRecAttempt, out string, err error, took time.Duration) string {
	if att == nil {
		return ""
	}
	if att.check {
		desktopRecStateMu.Lock()
		desktopRecPending[att.sk+"\x00"+att.goal] += took
		desktopRecStateMu.Unlock()
		return ""
	}
	pf, ran := desktopRecTakeFacts(scope)
	if !ran {
		// The pipeline did not run (a guard or record answered): no attempt.
		return att.note
	}
	o := recovery.Observation{Goal: att.goal, Surface: att.surface, Method: att.method, Strategy: att.strategy, Params: att.params, Recipe: att.recipe, Run: scope.RunID, OK: err == nil, Trial: pf.trial, Facts: pf.facts, Took: took}
	if err != nil {
		o.Error = desktopRecFirstLine(err.Error())
	}
	if pf.state == string(browser.PublishSubmitted) {
		o.Side = "unconfirmed"
	}
	want, hasWant := pf.facts["pictures_wanted"]
	o.Complete = err == nil && !pf.trial && pf.facts["published"] == 1 && (!hasWant || pf.facts["pictures_kept"] >= want)
	v := desktopRecovery().Observe(att.sk, o)
	log.Printf("[recovery] goal=%s verdict=%s state=%s strategy=%q", att.goal, v.Kind, v.State, att.strategy)
	text := att.note
	if v.Kind == recovery.KindVerified {
		text += "\n[recovery] goal verified by the site's own result"
		if v.Recipe != nil {
			text += fmt.Sprintf("; recipe %s recorded for this site (params only, re-verified on every use)", v.Recipe.ID)
		}
		return text
	}
	return text + "\n" + v.Evidence
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
	goal := strings.TrimSpace(desktopArg(args, "goal"))
	if goal != "" && !strings.HasPrefix(goal, "publish|") && !strings.HasPrefix(goal, "check:") {
		goal = "check:" + goal
	}
	if goal == "" {
		var waiting []string
		for _, k := range desktopRecRunGoals(scope.RunID) {
			s, g, _ := strings.Cut(k, "\x01")
			if s != sk {
				continue
			}
			if st := eng.Status(sk, g); st != nil && st.State == recovery.StateDiagnose {
				waiting = append(waiting, g)
			}
		}
		if len(waiting) != 1 {
			return "", fmt.Errorf("recover: pass goal= (goals of this run waiting for a diagnosis: %s)", strings.Join(waiting, ", "))
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
	key := "check:" + strings.TrimPrefix(name, "check:")
	eng := desktopRecovery()
	sk := desktopRecoveryScope(scope)
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
	desktopRecStateMu.Lock()
	desktopRecCheck[desktopRecKey(scope)] = key
	pk := sk + "\x00" + key
	took := desktopRecPending[pk]
	delete(desktopRecPending, pk)
	desktopRecStateMu.Unlock()
	desktopRecNoteRunGoal(scope.RunID, sk+"\x01"+key)
	facts, missed, good := desktopRecMeasure(ctx, scope, cs)
	if g.Count == 0 && took == 0 && len(missed) > 0 {
		// The baseline before any work: not an attempt.
		return fmt.Sprintf("[goal_check] %s pinned; baseline: %s\nnot met yet: %s", name, desktopRecFacts2(facts), strings.Join(missed, "; ")), nil
	}
	complete := len(missed) == 0
	o := recovery.Observation{Goal: key, Surface: "check", Method: "check", Strategy: desktopRecCheckStrategy(g), Run: scope.RunID, OK: complete, Complete: complete, Facts: facts, Took: took}
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

// desktopRecoveryOutcome is the task outcome the run's goals prove:
// "" (nothing tracked or all verified), "partial", "blocked",
// "not_submitted" or "unconfirmed", with one line of evidence.
func desktopRecoveryOutcome(runID string) (string, string) {
	eng := desktopRecovery()
	outcome, why := "", ""
	rank := map[string]int{"": 0, "not_submitted": 1, "partial": 2, "blocked": 3, "unconfirmed": 4}
	for _, k := range desktopRecRunGoals(runID) {
		sk, goal, _ := strings.Cut(k, "\x01")
		g := eng.Status(sk, goal)
		if g == nil || g.State == recovery.StateDone || g.Count == 0 {
			continue
		}
		o := ""
		switch {
		case g.State == recovery.StateStopped:
			o = "unconfirmed"
		case len(g.Best) > 0 && desktopRecSomeDone(g.Best):
			o = "partial"
		case g.State == recovery.StateExhausted:
			o = "blocked"
		default:
			o = "not_submitted"
		}
		if strings.HasPrefix(goal, "check:") && o == "not_submitted" {
			o = "blocked"
		}
		if rank[o] > rank[outcome] {
			outcome = o
			why = g.Reason
			if why == "" {
				why = g.LastKind
			}
			if len(g.Best) > 0 {
				why += "；已测得 " + desktopRecFacts2(g.Best)
			}
		}
	}
	return outcome, why
}

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
