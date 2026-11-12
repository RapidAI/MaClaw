package main

import (
	"fmt"
	"hash/fnv"
	"log"
	"regexp"
	"strings"
)

// Progress-aware action budget (Daniel 11-12: no fixed limit while a task
// makes real progress; stop only on a genuine no-progress stall). Bot 3's
// training run reached 143 of 200 actions at 07:44 while each poll showed a
// new training step. The fixed 200-action cap would have ended it in the
// middle of its third round.
//
// MACLAW_DESKTOP_RUN_MAX_ACTIONS is now the first budget, not a hard stop.
// When a run reaches its budget and recent actions keep producing new
// outcomes, the budget grows by MACLAW_DESKTOP_RUN_EXTEND_ACTIONS (default
// 100; 0 keeps the old fixed cap). New outcomes are new job metrics, a
// training step that moved, a file written, or any tool result not seen
// before in this run. Only a run whose recent actions repeat what it already
// saw is stopped, and it is told where it got stuck.

const desktopRunExtendActionsEnv = "MACLAW_DESKTOP_RUN_EXTEND_ACTIONS"

const (
	// The last new outcome must be at most this many actions ago.
	desktopBudgetRecentActions = 30
	// A window must hold at least this many new outcomes (and 10%).
	desktopBudgetMinNovel = 5
	desktopBudgetSeenMax  = 4096
	desktopBudgetNovelMax = 512
)

func desktopRunExtendActions() int {
	return desktopEnvInt(desktopRunExtendActionsEnv, 100, 0, 10000)
}

type desktopBudgetTrack struct {
	extra      int                 // actions granted on top of the first budget
	extensions int                 // how many times it grew
	seen       map[uint64]struct{} // outcomes already seen in this run
	novelAt    []int               // action numbers that brought a new outcome
	lastNovel  int                 // action number of the latest new outcome
	lastNote   string              // that outcome, clipped
	lastStep   string              // the latest action and its outcome, clipped
	stopLogged bool                // the stop was logged once
}

// Looking at things is not progress on its own.
var desktopBudgetLookOnly = map[string]bool{
	"screenshot": true, "probe": true, "wait": true, "job_list": true, "window_list": true,
	"screen_info": true, "clipboard_get": true, "app_list": true, "skill_list": true,
	"file_list": true, "app_probe": true, "invalid": true,
}

// Screen actions report coordinates and counters; only their wording counts.
var desktopBudgetScreenActions = map[string]bool{
	"click": true, "type": true, "press": true, "computer": true, "app_run": true,
	"task_run": true, "batch": true, "script": true, "navigate": true, "window": true,
}

var desktopBudgetDigitsRe = regexp.MustCompile(`[0-9]+`)
var desktopBudgetSpaceRe = regexp.MustCompile(`\s+`)

func desktopBudgetLimitLocked(st *desktopRunState) int {
	max := desktopRunMaxActions()
	if max <= 0 {
		return 0
	}
	return max + st.budget.extra
}

// desktopBudgetNoteOutcomeLocked records whether this step brought anything
// new. st.actions already counts it.
func desktopBudgetNoteOutcomeLocked(st *desktopRunState, action, result string, failed bool) {
	b := &st.budget
	text := strings.TrimSpace(desktopBudgetSpaceRe.ReplaceAllString(result, " "))
	b.lastStep = action + ": " + desktopRuneHead(text, 120)
	if failed {
		b.lastStep = action + " failed"
	}
	if failed || text == "" || desktopBudgetLookOnly[action] {
		return
	}
	key := text
	if desktopBudgetScreenActions[action] {
		key = desktopBudgetDigitsRe.ReplaceAllString(text, "#")
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(action + "\x00" + key))
	sum := h.Sum64()
	if b.seen == nil || len(b.seen) > desktopBudgetSeenMax {
		b.seen = map[uint64]struct{}{}
	}
	if _, ok := b.seen[sum]; ok {
		return
	}
	b.seen[sum] = struct{}{}
	b.novelAt = append(b.novelAt, st.actions)
	if len(b.novelAt) > desktopBudgetNovelMax {
		b.novelAt = b.novelAt[len(b.novelAt)-desktopBudgetNovelMax:]
	}
	b.lastNovel = st.actions
	b.lastNote = action + ": " + desktopRuneHead(text, 120)
}

// desktopBudgetProgressingLocked: recent actions keep bringing new outcomes.
// It returns how many new outcomes were in the window, for the log.
func desktopBudgetProgressingLocked(st *desktopRunState) (bool, int, int) {
	window := desktopRunExtendActions()
	if window <= 0 {
		window = 100
	}
	if max := desktopRunMaxActions(); max > 0 && max < window {
		window = max
	}
	n := 0
	for _, at := range st.budget.novelAt {
		if at > st.actions-window {
			n++
		}
	}
	need := desktopBudgetMinNovel
	if window/10 > need {
		need = window / 10
	}
	recent := st.budget.lastNovel > 0 && st.actions-st.budget.lastNovel <= desktopBudgetRecentActions
	return n >= need && recent, n, window
}

// desktopBudgetActionsLocked decides at the budget: grow it (nil) or stop.
func desktopBudgetActionsLocked(st *desktopRunState) error {
	limit := desktopBudgetLimitLocked(st)
	if limit <= 0 || st.actions < limit {
		return nil
	}
	chunk := desktopRunExtendActions()
	ok, n, window := desktopBudgetProgressingLocked(st)
	if chunk > 0 && ok {
		st.budget.extra += chunk
		st.budget.extensions++
		log.Printf("[desktop-budget] run=%s extended to %d actions (+%d, extension %d): %d new outcomes in the last %d actions, latest at action %d (%s)",
			st.scope.RunID, desktopBudgetLimitLocked(st), chunk, st.budget.extensions, n, window, st.budget.lastNovel, st.budget.lastNote)
		return nil
	}
	where := "no action in this run brought a new result"
	if st.budget.lastNovel > 0 {
		where = fmt.Sprintf("the last new result was at action %d (%s); the %d actions since then repeated what was already seen (latest: %s)",
			st.budget.lastNovel, st.budget.lastNote, st.actions-st.budget.lastNovel, st.budget.lastStep)
	}
	if !st.budget.stopLogged {
		st.budget.stopLogged = true
		log.Printf("[desktop-budget] run=%s stopped at %d actions (limit %d, %d extensions): no progress; %s", st.scope.RunID, st.actions, limit, st.budget.extensions, where)
	}
	if chunk <= 0 {
		return fmt.Errorf("desktop run budget spent: %d desktop actions in this run (limit %d). Stop operating the desktop now and tell the person what was done and what is left", st.actions, limit)
	}
	return fmt.Errorf("desktop run stopped for lack of progress: %d desktop actions in this run, and %s. Stop operating the desktop now and tell the person what was done, where it is stuck (the step above) and what is left", st.actions, where)
}
