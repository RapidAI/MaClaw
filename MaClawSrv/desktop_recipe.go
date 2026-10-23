package main

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// Data-gathering recipes (generic, every task and site).
//
// A run that gathers material with bash/script and then reaches a verified
// outcome (published page seen, or a file delivered) leaves its working
// gathering command in the user's experience store, keyed by the task
// intent. The next run whose task matches gets that command injected and is
// told to run it first (adapting dates/params), exploring only if it fails.
// This replaces re-discovering the same data source every run.

const (
	desktopRecipeMinOutRunes = 400  // output big enough to be material
	desktopRecipeMaxCmdRunes = 4000 // stored command cap
	desktopRecipeMaxStored   = 8
	desktopRecipeMatchMin    = 0.35
)

type desktopRecipe struct {
	Intent string `json:"intent"`
	Action string `json:"action"` // bash or script
	Code   string `json:"code"`
	Count  int    `json:"count"`
	Last   int64  `json:"last"`
	// V is the capture rule version. Recipes from older rules (v<3 could store
	// post-write scripts) are never offered and get replaced on validate.
	V int `json:"v,omitempty"`
}

const desktopRecipeVersion = 3

type desktopRecipeRun struct {
	frozen bool // a write started: later scripts are the write flow, not gathering
	intent string
	action string
	code   string
}

var desktopRecipeRuns sync.Map // run key -> *desktopRecipeRun

// desktopRecipeIntent normalizes a task text: digits become #, punctuation
// and spaces collapse, so "做10.18的…" and "做10.20的…" share an intent.
func desktopRecipeIntent(task string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(task) {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune('#')
			space = false
		case unicode.IsLetter(r) || unicode.Is(unicode.Han, r):
			b.WriteRune(r)
			space = false
		default:
			if !space {
				b.WriteRune(' ')
				space = true
			}
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(s) > 300 {
		s = string([]rune(s)[:300])
	}
	return s
}

func desktopRecipeBigrams(s string) map[string]bool {
	rs := []rune(strings.ReplaceAll(s, " ", ""))
	m := map[string]bool{}
	for i := 0; i+1 < len(rs); i++ {
		m[string(rs[i:i+2])] = true
	}
	return m
}

// desktopRecipeSimilarity is the bigram overlap of two intents relative to
// the shorter one (a task card may wrap the original task in more text).
func desktopRecipeSimilarity(a, b string) float64 {
	A, B := desktopRecipeBigrams(a), desktopRecipeBigrams(b)
	if len(A) == 0 || len(B) == 0 {
		return 0
	}
	n := 0
	for k := range A {
		if B[k] {
			n++
		}
	}
	min := len(A)
	if len(B) < min {
		min = len(B)
	}
	return float64(n) / float64(min)
}

// desktopRecipeBegin remembers the task intent of this run (prompt time).
func desktopRecipeBegin(scope agentruntime.Scope, task string) {
	key := desktopTrajRunKey(scope)
	intent := desktopRecipeIntent(task)
	if key == "" || intent == "" {
		return
	}
	v, _ := desktopRecipeRuns.LoadOrStore(key, &desktopRecipeRun{})
	r := v.(*desktopRecipeRun)
	if r.intent == "" {
		r.intent = intent
	}
}

// desktopRecipeObserve keeps the latest successful gathering step of the run.
func desktopRecipeObserve(scope agentruntime.Scope, action string, args map[string]any, out string, err error) {
	v, ok := desktopRecipeRuns.Load(desktopTrajRunKey(scope))
	if !ok {
		return
	}
	if desktopRecipeWriteActions[action] || (action == "script" && desktopRecipeScriptWritesRe.MatchString(desktopArg(args, "script"))) {
		// Gathering is what happens before the first write on any site.
		v.(*desktopRecipeRun).frozen = true
		return
	}
	if err != nil || (action != "bash" && action != "script") || v.(*desktopRecipeRun).frozen {
		return
	}
	code := desktopArg(args, "command")
	if action == "script" {
		code = desktopArg(args, "script")
	}
	code = strings.TrimSpace(code)
	if code == "" || utf8.RuneCountInString(code) > desktopRecipeMaxCmdRunes || utf8.RuneCountInString(out) < desktopRecipeMinOutRunes {
		return
	}
	if strings.Contains(out, "exit -1") || strings.Contains(strings.ToLower(out), "traceback") {
		return
	}
	// A page script on the target site is a write step, not gathering.
	if action == "script" && desktopPublishAttemptRe.MatchString(out) {
		return
	}
	r := v.(*desktopRecipeRun)
	r.action, r.code = action, desktopbot.ScrubSecrets(code)
}

// A page script that changes the page (types, sets values, clicks) is a
// write step on any site: it freezes gathering like rich_fill does.
var desktopRecipeScriptWritesRe = regexp.MustCompile(`\btype\s*\(|\.value\s*=[^=]|\.click\s*\(|dispatchEvent\s*\(|execCommand\s*\(|innerHTML\s*=[^=]|textContent\s*=[^=]|\.submit\s*\(`)

var desktopRecipeWriteActions = map[string]bool{
	"rich_fill": true, "type": true, "click": true, "press": true, "batch": true,
	"computer": true, "app_run": true, "file_write": true, "deliver": true,
}

// desktopRecipeValidate stores the run's gathering step once the run reached
// a verified outcome. Idempotent per run.
func desktopRecipeValidate(scope agentruntime.Scope) {
	key := desktopTrajRunKey(scope)
	v, ok := desktopRecipeRuns.Load(key)
	if !ok {
		return
	}
	r := v.(*desktopRecipeRun)
	if r.intent == "" || r.code == "" || !desktopExperienceEnabled() {
		return
	}
	desktopRecipeRuns.Delete(key)
	desktopExperienceMu.Lock()
	defer desktopExperienceMu.Unlock()
	path := desktopExperiencePath(scope)
	f := desktopExperienceLoad(path)
	now := time.Now().Unix()
	found := false
	for i := range f.Recipes {
		if f.Recipes[i].Intent == r.intent {
			if f.Recipes[i].Code == r.code && f.Recipes[i].V >= desktopRecipeVersion {
				f.Recipes[i].Count++
			} else {
				f.Recipes[i].Code, f.Recipes[i].Action, f.Recipes[i].Count = r.code, r.action, 1
				f.Recipes[i].V = desktopRecipeVersion
			}
			f.Recipes[i].Last = now
			found = true
		}
	}
	if !found {
		f.Recipes = append(f.Recipes, desktopRecipe{Intent: r.intent, Action: r.action, Code: r.code, Count: 1, Last: now, V: desktopRecipeVersion})
	}
	sort.SliceStable(f.Recipes, func(i, j int) bool { return f.Recipes[i].Last > f.Recipes[j].Last })
	if len(f.Recipes) > desktopRecipeMaxStored {
		f.Recipes = f.Recipes[:desktopRecipeMaxStored]
	}
	desktopExperienceSave(path, f)
	log.Printf("[desktop-recipe] stored intent=%q action=%s len=%d", desktopStepClip(r.intent, 80), r.action, len(r.code))
	desktopEmit(scope, desktopEvent{Event: "recipe_write", Action: r.action, Reason: desktopStepClip(r.intent, 160)})
}

// desktopRecipeText is the injected guidance for a matching stored recipe.
func desktopRecipeText(scope agentruntime.Scope, task string) string {
	intent := desktopRecipeIntent(task)
	if intent == "" || !desktopExperienceEnabled() {
		return ""
	}
	desktopExperienceMu.Lock()
	f := desktopExperienceLoad(desktopExperiencePath(scope))
	desktopExperienceMu.Unlock()
	best, score := -1, 0.0
	for i, r := range f.Recipes {
		if r.V < desktopRecipeVersion {
			continue
		}
		if s := desktopRecipeSimilarity(intent, r.Intent); s > score {
			best, score = i, s
		}
	}
	if best < 0 || score < desktopRecipeMatchMin {
		return ""
	}
	r := f.Recipes[best]
	desktopEmit(scope, desktopEvent{Event: "recipe_hit", Action: r.Action, Reason: fmt.Sprintf("score=%.2f count=%d", score, r.Count)})
	return fmt.Sprintf("Data recipe from an earlier verified run of this kind of task (worked x%d): run it FIRST as ONE action=%s, changing only dates/parameters for this task; explore other sources only if it fails or returns nothing. ```%s``` ", r.Count, r.Action, r.Code)
}
