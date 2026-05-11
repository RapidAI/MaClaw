package main

import (
	"context"
	"log"
	"strings"
	"time"
)

// startSessionMonitor launches a background goroutine that periodically polls
// the session bound to a skill run and updates SessionProgress in the run
// status. This gives callers of get_skill_run visibility into what the
// session's internal AI agent is doing, without requiring them to call
// query_session separately.
//
// The monitor stops when:
//   - The parent context is cancelled (skill run finished or cancelled)
//   - The session reaches a terminal state (completed/failed/exited)
//   - The session is not found (deleted or expired)
//   - The maximum monitor duration is exceeded (10 minutes)
func (r *SkillRunner) startSessionMonitor(ctx context.Context, run *skillRun, sessionID string) {
	if r.executor == nil || r.executor.manager == nil {
		log.Printf("[skill-session-monitor] cannot start: manager not initialized")
		return
	}
	if strings.TrimSpace(sessionID) == "" {
		return
	}

	manager := r.executor.manager
	maxDuration := 10 * time.Minute
	pollInterval := 5 * time.Second

	go func() {
		monitorCtx, cancel := context.WithTimeout(ctx, maxDuration)
		defer cancel()

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		pollCount := 0
		log.Printf("[skill-session-monitor] started for session %s (interval=%v, max=%v)",
			sessionID, pollInterval, maxDuration)

		for {
			select {
			case <-monitorCtx.Done():
				log.Printf("[skill-session-monitor] stopped for session %s (ctx done, polls=%d)", sessionID, pollCount)
				return
			case <-ticker.C:
				pollCount++
				progress := r.pollSessionProgress(manager, sessionID, pollCount)
				if progress == nil {
					log.Printf("[skill-session-monitor] session %s not found, stopping", sessionID)
					return
				}

				r.mu.Lock()
				run.status.SessionProgress = progress
				r.mu.Unlock()

				// Stop monitoring if session reached terminal state.
				if progress.SessionStatus.IsTerminal() {
					log.Printf("[skill-session-monitor] session %s reached terminal state %q (polls=%d)",
						sessionID, progress.SessionStatus, pollCount)
					return
				}
			}
		}
	}()
}

// pollSessionProgress reads the current state of a session and returns a
// SessionProgressInfo snapshot. Returns nil if the session doesn't exist.
func (r *SkillRunner) pollSessionProgress(manager *RemoteSessionManager, sessionID string, pollCount int) *SessionProgressInfo {
	session, ok := manager.Get(sessionID)
	if !ok {
		return nil
	}

	session.mu.RLock()
	status := session.Status
	summary := session.Summary
	// Copy last 10 raw output lines while holding the lock to avoid
	// reading a slice that's being appended to concurrently.
	var lastLines []string
	if n := len(session.RawOutputLines); n > 0 {
		start := 0
		if n > 10 {
			start = n - 10
		}
		lastLines = make([]string, n-start)
		copy(lastLines, session.RawOutputLines[start:])
	}
	session.mu.RUnlock()

	return &SessionProgressInfo{
		SessionStatus:   status,
		CurrentTask:     summary.CurrentTask,
		ProgressSummary: summary.ProgressSummary,
		LastResult:      truncateSessionField(summary.LastResult, 200),
		LastCommand:     summary.LastCommand,
		WaitingForUser:  summary.WaitingForUser,
		LastOutputLines: lastLines,
		UpdatedAt:       time.Now().Format(time.RFC3339),
		PollCount:       pollCount,
	}
}

// truncateSessionField truncates a string to maxLen, appending "..." if truncated.
func truncateSessionField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
