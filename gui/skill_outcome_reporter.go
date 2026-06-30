package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// skillOutcomeReporter reports local skill execution outcomes to HubCenter,
// closing the feedback loop between local execution quality and global skill
// ranking signals. Without this, each user is an isolated island — local
// failures never affect the Hub's AvgRating, and other users have no way to
// know a skill is broken.
//
// Design principles:
// - Idempotent: same (skillName, runID) pair never reported twice (dedup by runID)
// - Throttled: same skill reported at most once per throttleInterval (24h)
// - Async: reporting runs in a background goroutine, never blocks the agent loop
// - Graceful degradation: network failures are silently logged, never surface to user
type skillOutcomeReporter struct {
	mu              sync.Mutex
	app             *App
	lastReportedAt  map[string]time.Time // skillName → last successful report time
	lastFailedAt    time.Time            // time of last failed report (any skill) for backoff
	consecutiveFail int                  // consecutive failures for exponential backoff
	reportedRunIDs  map[string]struct{}  // runID → already reported (dedup)
	maxRunIDEntries int
}

const (
	// Throttle interval: report the same skill at most once per 24 hours.
	// This prevents spamming the hub with repeated runs of the same skill
	// (e.g., a skill called 50 times in a batch session).
	outcomeReportThrottleInterval = 24 * time.Hour

	// Maximum number of runIDs to track for dedup. After this limit, oldest
	// entries are not evicted (map is cheap), but we stop growing indefinitely.
	outcomeReportMaxRunIDs = 500
)

func newSkillOutcomeReporter(app *App) *skillOutcomeReporter {
	return &skillOutcomeReporter{
		app:             app,
		lastReportedAt:  make(map[string]time.Time),
		reportedRunIDs:  make(map[string]struct{}),
		maxRunIDEntries: outcomeReportMaxRunIDs,
	}
}

// ReportOutcome asynchronously reports a skill execution outcome to HubCenter.
// Maps the local EvaluateSkillExecution score (range -2 to +2) to HubCenter's
// 1-5 rating scale:
//
//	local -2 (security alert) → hub 1
//	local -1 (error)          → hub 1
//	local  0 (no effect)      → hub 2
//	local +1 (success)        → hub 4
//	local +2 (excellent)      → hub 5
//
// Only reports skills that have a HubSkillID (i.e., came from the Hub).
// Local-only or GitHub-imported skills are never reported.
func (r *skillOutcomeReporter) ReportOutcome(skill *corelib.NLSkillEntry, runID string, localScore int) {
	if r == nil || skill == nil {
		return
	}
	// Only report skills that originated from a Hub (have a HubSkillID).
	if skill.HubSkillID == "" {
		return
	}
	if runID == "" {
		return
	}

	r.mu.Lock()
	// Backoff: if recent reports are failing, don't spam goroutines.
	if r.consecutiveFail > 0 && !r.lastFailedAt.IsZero() {
		backoff := time.Duration(r.consecutiveFail) * 5 * time.Minute
		if backoff > time.Hour {
			backoff = time.Hour
		}
		if time.Since(r.lastFailedAt) < backoff {
			r.mu.Unlock()
			return
		}
	}
	// Dedup: don't report the same run twice.
	if _, reported := r.reportedRunIDs[runID]; reported {
		r.mu.Unlock()
		return
	}
	// Throttle: don't report the same skill more than once per interval.
	if lastTime, ok := r.lastReportedAt[skill.Name]; ok {
		if time.Since(lastTime) < outcomeReportThrottleInterval {
			r.mu.Unlock()
			return
		}
	}
	// Passed throttle check — this skill hasn't been reported in 24h.
	// Evict oldest runIDs if at capacity to prevent unbounded growth.
	if len(r.reportedRunIDs) >= r.maxRunIDEntries {
		// Simple eviction: clear all entries. The 24h throttle is the primary
		// dedup mechanism; runID dedup is a secondary guard against concurrent
		// goroutines reporting the same run within the same second.
		r.reportedRunIDs = make(map[string]struct{})
	}
	r.reportedRunIDs[runID] = struct{}{}
	r.mu.Unlock()

	// Async report — never block the execution path.
	go r.doReport(skill.HubSkillID, skill.Name, runID, localScore)
}

func (r *skillOutcomeReporter) doReport(hubSkillID, skillName, runID string, localScore int) {
	hubScore := mapLocalScoreToHubRating(localScore)
	if hubScore < 1 || hubScore > 5 {
		return
	}
	// Don't report ambiguous results (localScore=0 → "no effect") — this may
	// be caused by wrong user parameters rather than a skill bug. Only report
	// clear signals: success (+1/+2) or failure (-1/-2).
	if localScore == 0 {
		return
	}

	machineID := ""
	if r.app != nil {
		cfg, err := r.app.LoadConfig()
		if err == nil {
			machineID = cfg.RemoteMachineID
		}
	}
	if machineID == "" {
		// Not registered with Hub — cannot report.
		return
	}

	r.app.ensureSkillHubClient()
	hubClient := r.app.skillHubClient
	if hubClient == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := hubClient.Rate(ctx, hubSkillID, machineID, hubScore)
	if err != nil {
		log.Printf("[skill-outcome-reporter] report failed skill=%q hub_id=%s score=%d err=%v",
			skillName, hubSkillID, hubScore, err)
		r.mu.Lock()
		delete(r.reportedRunIDs, runID)
		r.consecutiveFail++
		r.lastFailedAt = time.Now()
		r.mu.Unlock()
		return
	}

	log.Printf("[skill-outcome-reporter] reported skill=%q hub_id=%s local_score=%d hub_score=%d",
		skillName, hubSkillID, localScore, hubScore)

	r.mu.Lock()
	r.lastReportedAt[skillName] = time.Now()
	r.consecutiveFail = 0 // reset backoff on success
	r.mu.Unlock()
}

// mapLocalScoreToHubRating maps EvaluateSkillExecution score (-2 to +2) to
// HubCenter's 1-5 rating scale.
func mapLocalScoreToHubRating(localScore int) int {
	switch {
	case localScore <= -1:
		return 1 // failure/security → worst rating
	case localScore == 0:
		return 2 // no effect → below average
	case localScore == 1:
		return 4 // success → good
	case localScore >= 2:
		return 5 // excellent → best rating
	default:
		return 3
	}
}
