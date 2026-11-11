package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// A live run that keeps checking a long job itself (papers 2026-11-11: Bot 1
// trained on the Mac mini for an hour, tailing the log every 90 s) told the
// person nothing until it ended. Runs now have no clock limit while they
// progress, so every run that is still working says a short status through
// Hub's say at most every desktopRunProgressEvery once it has worked that
// long: what is done (steps, files, measured lines), the last measured
// progress, what it is doing now and what is next. When nothing changed it
// stays quiet, except one heartbeat per desktopRunHeartbeatEvery that says
// honestly that nothing has moved. Nothing is said for a short run.
//
// Policy (Daniel 11-11 20:38, too frequent): the first note no sooner than
// desktopRunProgressEvery after the run started, then at most one per
// desktopRunProgressEvery, and only on a meaningful change — a new measured
// result (epoch/step/metric line) or a changed goal check. Step counts, a
// new screen or new command output are not news. A stall warning (no
// measurable progress for half the stall window) is said once per stall,
// whatever the interval; the end of the run is its final reply.
var (
	// 0 (default, Daniel 11-11 21:01): no interval status notes; a running
	// job is reported by its watcher at estimated checks and a run warns
	// once when it stalls. A positive value restores change-based notes at
	// most once per that many minutes.
	desktopRunProgressEvery = desktopEnvMinutes("MACLAW_RUN_STATUS_MINUTES", 0)
	desktopRunProgressMu    sync.Mutex
	desktopRunProgressSeen  = map[string]*desktopRunProgressState{}
)

type desktopRunProgressState struct {
	first, lastSay time.Time
	lastFP         string
	metrics        string
	saying         bool
	stallSaid      bool
}

// desktopRunProgressSay is the per-step hook: it keeps the measured lines a
// step read and gives the run its status when one is due.
func desktopRunProgressSay(scope agentruntime.Scope, action, out string, err error) {
	if strings.TrimSpace(scope.RunID) == "" || strings.TrimSpace(scope.InstanceID) == "" {
		return
	}
	lines := ""
	if err == nil {
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "bash", "job_status":
			lines = desktopRunProgressLines(out)
		}
	}
	now := desktopLongJobNow()
	desktopRunProgressMu.Lock()
	st := desktopRunProgressGetLocked(scope.RunID, now)
	if lines != "" {
		st.metrics = lines
	}
	desktopRunProgressMu.Unlock()
	desktopRunStatusTick(scope, now)
}

func desktopRunProgressGetLocked(runID string, now time.Time) *desktopRunProgressState {
	if len(desktopRunProgressSeen) > 512 {
		desktopRunProgressSeen = map[string]*desktopRunProgressState{}
	}
	st := desktopRunProgressSeen[runID]
	if st == nil {
		st = &desktopRunProgressState{first: now}
		if snap, ok := desktopWatchdogSnapshot(runID); ok && !snap.started.IsZero() && snap.started.Before(now) {
			st.first = snap.started
		}
		desktopRunProgressSeen[runID] = st
	}
	return st
}

// desktopRunStatusTick says the run's status when one is due (per step and
// from the watchdog sweep, so a run inside one long step is heard too).
func desktopRunStatusTick(scope agentruntime.Scope, now time.Time) {
	if strings.TrimSpace(scope.RunID) == "" || strings.TrimSpace(scope.InstanceID) == "" {
		return
	}
	snap, tracked := desktopWatchdogSnapshot(scope.RunID)
	if tracked && snap.stop != "" {
		return
	}
	desktopRunProgressMu.Lock()
	st := desktopRunProgressGetLocked(scope.RunID, now)
	metrics := st.metrics
	if metrics == "" && tracked {
		metrics = snap.metrics
	}
	fp := metrics
	if tracked {
		fp = strconv.Itoa(snap.stageN) + "|" + metrics
	}
	age := now.Sub(st.first)
	since := now.Sub(st.lastSay)
	if tracked && !snap.stallWarned {
		st.stallSaid = false
	}
	stall := tracked && snap.stallWarned && !st.stallSaid
	changed := fp != st.lastFP && (metrics != "" || (tracked && snap.stageN > 0))
	due := !st.saying && (stall || (desktopRunProgressEvery > 0 && age >= desktopRunProgressEvery && (st.lastSay.IsZero() || since >= desktopRunProgressEvery) && changed))
	if !due {
		desktopRunProgressMu.Unlock()
		return
	}
	prevSay, prevFP := st.lastSay, st.lastFP
	st.saying = true
	if stall {
		st.stallSaid = true
	} else {
		st.lastSay, st.lastFP = now, fp
	}
	desktopRunProgressMu.Unlock()
	heartbeat := stall
	text := desktopRunStatusText(scope, snap, tracked, metrics, age, heartbeat, now)
	key := fp
	if stall {
		key = "stall|" + snap.lastProgress.Format(time.RFC3339)
	}
	sum := sha1.Sum([]byte(key))
	j := &desktopLongJob{
		ID:          scope.RunID,
		TenantID:    scope.TenantID,
		UserID:      scope.UserID,
		InstanceID:  scope.InstanceID,
		Status:      "running",
		Fingerprint: hex.EncodeToString(sum[:8]),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := desktopLongJobSay(ctx, j, text)
		desktopRunProgressMu.Lock()
		if st := desktopRunProgressSeen[scope.RunID]; st != nil {
			st.saying = false
			if err != nil {
				// Nothing reached the person: the next step or sweep says it.
				if stall {
					st.stallSaid = false
				} else {
					st.lastSay, st.lastFP = prevSay, prevFP
				}
			}
		}
		desktopRunProgressMu.Unlock()
		if err != nil {
			log.Printf("[desktop-progress] run=%s status not said: %v", scope.RunID, err)
		}
	}()
}

// desktopRunStatusText: done, last progress, now and next, from measured
// facts only.
func desktopRunStatusText(scope agentruntime.Scope, snap desktopWatchdogSnap, tracked bool, metrics string, age time.Duration, heartbeat bool, now time.Time) string {
	var b strings.Builder
	if heartbeat {
		b.WriteString("可能卡住了（这件已做 " + strconv.Itoa(int(age.Minutes())) + " 分钟）：自 " + snap.lastProgress.Format("15:04") + " 起没有可测的进展。")
		if snap.stageWhy != "" {
			b.WriteString("最近的结果：" + snap.stageWhy + "。")
		}
		b.WriteString("再过 " + strconv.Itoa(int((watchdogStallAfter - now.Sub(snap.lastProgress)).Round(time.Minute).Minutes())) + " 分钟仍没有进展我会停下，并告诉你卡在哪、还剩什么。")
	} else {
		b.WriteString("进度（这件还在做，已 " + strconv.Itoa(int(age.Minutes())) + " 分钟）：")
		if tracked && snap.stageWhy != "" {
			b.WriteString(snap.stageWhy + "（" + snap.lastProgress.Format("15:04") + "）。")
		}
	}
	if metrics != "" {
		b.WriteString("\n刚读到的输出：\n" + metrics)
	}
	if wait := desktopScreenLeaseStatus(scope.RunID); wait != "" {
		b.WriteString("\n现在：" + wait)
	} else if act := strings.TrimSpace(loadDesktopActivity(scope.RunID)); act != "" {
		b.WriteString("\n现在：" + act)
	}
	if strings.TrimSpace(scope.SessionID) != "" {
		if pc, ok := desktopbot.RecallProgress(scope.SessionID); ok {
			if next := strings.TrimSpace(pc.Sections.NextStep); next != "" {
				b.WriteString("\n下一步：" + desktopRuneHead(desktopLongJobSecretRe.ReplaceAllString(next, "$1***"), 160))
			}
		}
	}
	return b.String()
}

// desktopRunProgressLines keeps the last few measured-progress lines.
func desktopRunProgressLines(out string) string {
	var kept []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || len(l) > 400 || !desktopRunProgressRe.MatchString(l) {
			continue
		}
		kept = append(kept, desktopLongJobSecretRe.ReplaceAllString(l, "$1***"))
	}
	if len(kept) > 3 {
		kept = kept[len(kept)-3:]
	}
	return strings.Join(kept, "\n")
}

// Training-style measurements only: a publish step's "failed" is not progress.
var desktopRunProgressRe = regexp.MustCompile(`(?i)(\bepoch\s*\d|\bstep\s+\d+\s*/\s*\d+|\bloss["']?\s*[:=]\s*[\d.]|\bacc(uracy)?["']?\s*[:=]\s*[\d.]|\bf1["']?\s*[:=]\s*[\d.]|\b\d{1,3}%\|)`)
