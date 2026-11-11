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
// one model round each (papers 2026-11-12 06:45-06:50, Bot 3 training run).
// The watcher checks at the estimated time, so after a second such wait the
// bash result says so: set the next check by the estimate and end the turn.

var desktopWaitPollSleepRe = regexp.MustCompile(`(?:^|[;&|\s(])sleep\s+([0-9]+)`)

var (
	desktopWaitPollMu    sync.Mutex
	desktopWaitPollCount = map[string]int{}
)

// desktopWaitPollMin is the shortest sleep that counts as waiting for a job.
const desktopWaitPollMin = 30

func desktopWaitPollSeconds(cmd string) int {
	best := 0
	for _, m := range desktopWaitPollSleepRe.FindAllStringSubmatch(cmd, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > best {
			best = n
		}
	}
	return best
}

// desktopWaitPollNote returns the note for this bash, or "".
func desktopWaitPollNote(scope agentruntime.Scope, cmd string) string {
	runID := strings.TrimSpace(scope.RunID)
	if runID == "" || desktopWaitPollSeconds(cmd) < desktopWaitPollMin {
		return ""
	}
	j := desktopLongJobForRun(runID)
	if j == nil || (j.Status != "running" && j.Status != "stalled") {
		return ""
	}
	desktopWaitPollMu.Lock()
	if len(desktopWaitPollCount) > 4096 {
		desktopWaitPollCount = map[string]int{}
	}
	desktopWaitPollCount[runID]++
	n := desktopWaitPollCount[runID]
	desktopWaitPollMu.Unlock()
	if n < 2 {
		return ""
	}
	return "\n(note: background job " + j.ID + " is already watched and will be checked at the time you estimate. Do not wait for it here with sleep: if nothing else is left to do now, call action=job_status with job_id=" + j.ID + ", next_check_minutes from the ETA in the log and a check_reason, then end this turn. The watcher reports results, and you continue (evaluation, next round) when it wakes you.)"
}
