package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// A run that already handed its long job to the watcher sometimes keeps
// waiting for it in the foreground: "sleep 85; tail log" every 1.5 min,
// one model round each (papers 2026-11-12 06:45-07:45, Bot 3 training run,
// about 60 model rounds). The watcher checks at the estimated time, so the
// bash result says so: set the next check by the estimate and end the turn.
// A wait is a sleep of 30 s or more, a sleep inside a while/until loop, or
// a shorter sleep that then reads a log or the process list. The note comes
// on the second long wait or the third wait of any kind.

var (
	desktopWaitPollSleepRe = regexp.MustCompile(`(?:^|[;&|\s(])sleep\s+([0-9]+)`)
	desktopWaitPollLoopRe  = regexp.MustCompile(`(?:^|[;&|\s(])(?:while|until)\s[^\n]*?;\s*do\b[^\n]*?\bsleep\s+[0-9]`)
	desktopWaitPollReadRe  = regexp.MustCompile(`(?:^|[;&|\s('"])(?:tail|cat|grep|head|less|wc|ps|pgrep|nvidia-smi|ls|du|stat|jq)\s`)
)

type desktopWaitPollState struct {
	long, any  int
	unwatchedN bool // the note about an unwatched process was given
}

var (
	desktopWaitPollMu  sync.Mutex
	desktopWaitPollRun = map[string]*desktopWaitPollState{}
)

// desktopWaitPollMin is the shortest sleep that counts as a wait on its own.
const desktopWaitPollMin = 30

// desktopWaitPollShort is the shortest sleep that counts when a read follows.
const desktopWaitPollShort = 3

func desktopWaitPollSeconds(cmd string) int {
	best := 0
	for _, m := range desktopWaitPollSleepRe.FindAllStringSubmatch(cmd, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > best {
			best = n
		}
	}
	return best
}

// desktopWaitPollKind: 2 a long wait, 1 a short sleep then a read, 0 none.
func desktopWaitPollKind(cmd string) int {
	secs := desktopWaitPollSeconds(cmd)
	switch {
	case secs >= desktopWaitPollMin, secs > 0 && desktopWaitPollLoopRe.MatchString(cmd):
		return 2
	case secs >= desktopWaitPollShort && desktopWaitPollReadRe.MatchString(cmd):
		return 1
	}
	return 0
}

// desktopWaitPollNote returns the note for this bash, or "".
func desktopWaitPollNote(scope agentruntime.Scope, cmd string) string {
	runID := strings.TrimSpace(scope.RunID)
	kind := desktopWaitPollKind(cmd)
	if runID == "" || kind == 0 {
		return ""
	}
	desktopWaitPollMu.Lock()
	if len(desktopWaitPollRun) > 4096 {
		desktopWaitPollRun = map[string]*desktopWaitPollState{}
	}
	r := desktopWaitPollRun[runID]
	if r == nil {
		r = &desktopWaitPollState{}
		desktopWaitPollRun[runID] = r
	}
	r.any++
	if kind == 2 {
		r.long++
	}
	due := r.long >= 2 || r.any >= 3
	desktopWaitPollMu.Unlock()
	if !due {
		return ""
	}
	j := desktopLongJobForRun(runID)
	if j != nil && (j.Status == "running" || j.Status == "stalled") {
		return "\n(note: background job " + j.ID + " is already watched and will be checked at the time you estimate. Do not wait for it here with sleep: if nothing else is left to do now, call action=job_status with job_id=" + j.ID + ", next_check_minutes from the ETA in the log and a check_reason, then end this turn. The watcher reports results, and you continue (evaluation, next round) when it wakes you.)"
	}
	desktopWaitPollMu.Lock()
	first := !r.unwatchedN
	r.unwatchedN = true
	desktopWaitPollMu.Unlock()
	if !first {
		return ""
	}
	return "\n(note: this run keeps waiting in the foreground with sleep, one model round per check. Start long work (training, evaluation, downloads) with action=bash and background=true: it is then watched and you are woken at the time you estimate (job_status next_check_minutes), instead of polling here. Do that for the next long step.)"
}
