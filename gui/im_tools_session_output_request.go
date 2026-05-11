package main

import "time"

const (
	defaultSessionOutputLineLimit = 30
	maxSessionOutputLineLimit     = 100
	sessionStartupPollAttempts    = 10
	sessionStartupPollInterval    = 500 * time.Millisecond
)

func sessionOutputLineLimit(args map[string]interface{}) int {
	maxLines := defaultSessionOutputLineLimit
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
		if maxLines > maxSessionOutputLineLimit {
			maxLines = maxSessionOutputLineLimit
		}
	}
	return maxLines
}

func waitForSessionStartupOutput(session *RemoteSession) {
	waitForSessionStartupOutputWithInterval(session, sessionStartupPollAttempts, sessionStartupPollInterval)
}

func waitForSessionStartupOutputWithInterval(session *RemoteSession, attempts int, interval time.Duration) {
	if session == nil || attempts <= 0 {
		return
	}
	session.mu.RLock()
	isStarting := session.Status == SessionStarting
	hasOutput := len(session.RawOutputLines) > 0
	session.mu.RUnlock()
	if !isStarting || hasOutput {
		return
	}

	for i := 0; i < attempts; i++ {
		if interval > 0 {
			time.Sleep(interval)
		}
		session.mu.RLock()
		changed := session.Status != SessionStarting || len(session.RawOutputLines) > 0
		session.mu.RUnlock()
		if changed {
			return
		}
	}
}
