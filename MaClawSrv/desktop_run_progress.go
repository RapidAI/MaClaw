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
)

// A live run that keeps checking a long job itself (papers 2026-11-11: Bot 1
// trained on the Mac mini for an hour, tailing the log every 90 s) told the
// person nothing until it ended. desktopRunProgressSay posts the measured
// progress lines a run's own step read (epoch, step, loss, metrics) to the
// person through Hub's say, at most every desktopRunProgressEvery, once the
// run has been working for that long. Nothing is said for a short run.
var (
	desktopRunProgressEvery = 10 * time.Minute
	desktopRunProgressMu    sync.Mutex
	desktopRunProgressSeen  = map[string]*desktopRunProgressState{}
)

type desktopRunProgressState struct {
	first, lastSay time.Time
	lastText       string
}

func desktopRunProgressSay(scope agentruntime.Scope, action, out string, err error) {
	if err != nil || strings.TrimSpace(scope.RunID) == "" || strings.TrimSpace(scope.InstanceID) == "" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "bash", "job_status":
	default:
		return
	}
	lines := desktopRunProgressLines(out)
	now := desktopLongJobNow()
	desktopRunProgressMu.Lock()
	if len(desktopRunProgressSeen) > 512 {
		desktopRunProgressSeen = map[string]*desktopRunProgressState{}
	}
	st := desktopRunProgressSeen[scope.RunID]
	if st == nil {
		st = &desktopRunProgressState{first: now}
		desktopRunProgressSeen[scope.RunID] = st
	}
	due := lines != "" && lines != st.lastText && now.Sub(st.first) >= desktopRunProgressEvery &&
		(st.lastSay.IsZero() || now.Sub(st.lastSay) >= desktopRunProgressEvery)
	if due {
		st.lastSay, st.lastText = now, lines
	}
	first := st.first
	desktopRunProgressMu.Unlock()
	if !due {
		return
	}
	sum := sha1.Sum([]byte(lines))
	j := &desktopLongJob{
		ID:          scope.RunID,
		TenantID:    scope.TenantID,
		UserID:      scope.UserID,
		InstanceID:  scope.InstanceID,
		Status:      "running",
		Fingerprint: hex.EncodeToString(sum[:8]),
	}
	text := "进度（这件还在做，已 " + strconv.Itoa(int(now.Sub(first).Minutes())) + " 分钟，下面是刚读到的输出）：\n" + lines
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := desktopLongJobSay(ctx, j, text); err != nil {
			log.Printf("[desktop-progress] run=%s progress not said: %v", scope.RunID, err)
			desktopRunProgressMu.Lock()
			if st := desktopRunProgressSeen[scope.RunID]; st != nil {
				st.lastSay, st.lastText = time.Time{}, ""
			}
			desktopRunProgressMu.Unlock()
		}
	}()
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
