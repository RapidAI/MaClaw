package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SessionState holds the security context for a single session, persisted
// across multiple hook invocations via a JSON file in /tmp.
type SessionState struct {
	SessionID          string      `json:"session_id"`
	ToolCallCount      int         `json:"tool_call_count"`
	HighRiskCount      int         `json:"high_risk_count"`
	LastCheckTime      time.Time   `json:"last_check_time"`
	SecurityMode       string      `json:"security_mode"`
	ModeUpgraded       bool        `json:"mode_upgraded"`
	HighRiskTimestamps []time.Time `json:"high_risk_timestamps,omitempty"`
}

// sessionStatePath returns the file path for the given session ID.
// The session ID is sanitized to prevent path traversal.
func sessionStatePath(sessionID string) string {
	safe := sanitizeSessionID(sessionID)
	return filepath.Join(os.TempDir(), fmt.Sprintf("maclaw-session-%s.json", safe))
}

// sanitizeSessionID removes path separators and dangerous characters from
// a session ID to prevent path traversal attacks.
func sanitizeSessionID(id string) string {
	var b []byte
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b = append(b, c)
		}
	}
	if len(b) == 0 {
		return "unknown"
	}
	return string(b)
}

// sessionLockPath returns the lock file path for the given session state file.
func sessionLockPath(statePath string) string {
	return statePath + ".lock"
}

// LoadSessionState loads session state from disk. If the file does not exist
// or is corrupted, a fresh initial state is returned.
func LoadSessionState(sessionID string) (*SessionState, error) {
	p := sessionStatePath(sessionID)

	data, err := os.ReadFile(p)
	if err != nil {
		// File doesn't exist or unreadable — return fresh state.
		return newSessionState(sessionID), nil
	}

	var s SessionState
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupted JSON — start fresh.
		return newSessionState(sessionID), nil
	}

	// Ensure the session ID is consistent.
	s.SessionID = sessionID
	return &s, nil
}

// Save persists the session state to disk using a lockfile to prevent
// concurrent writes. On Windows we use O_CREATE|O_EXCL on a .lock file
// as a portable advisory lock.
func (s *SessionState) Save() error {
	p := sessionStatePath(s.SessionID)
	lp := sessionLockPath(p)

	// Acquire lock — retry a few times with a short sleep.
	// If the lock file is stale (older than 30s), remove it and retry.
	var lockFile *os.File
	var err error
	for i := 0; i < 10; i++ {
		lockFile, err = os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		// Check for stale lock
		if i == 0 {
			if info, statErr := os.Stat(lp); statErr == nil {
				if time.Since(info.ModTime()) > 30*time.Second {
					_ = os.Remove(lp)
					continue
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		// Could not acquire lock — skip state update rather than block.
		return fmt.Errorf("session state lock: %w", err)
	}
	defer func() {
		lockFile.Close()
		os.Remove(lp)
	}()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("session state marshal: %w", err)
	}

	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("session state write: %w", err)
	}
	return nil
}

// IncrementToolCall bumps the tool call counter and updates LastCheckTime.
func (s *SessionState) IncrementToolCall() {
	s.ToolCallCount++
	s.LastCheckTime = time.Now()
}

// IncrementHighRisk records a high-risk event and checks whether the session
// should be upgraded to strict mode. It returns true if this call triggered
// the upgrade (i.e. the 3rd high-risk event within a 5-minute window).
func (s *SessionState) IncrementHighRisk() bool {
	now := time.Now()
	s.HighRiskTimestamps = append(s.HighRiskTimestamps, now)
	s.HighRiskCount++

	// Filter timestamps to only keep those within the 5-minute window.
	cutoff := now.Add(-5 * time.Minute)
	filtered := make([]time.Time, 0, len(s.HighRiskTimestamps))
	for _, ts := range s.HighRiskTimestamps {
		if !ts.Before(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	s.HighRiskTimestamps = filtered

	// Check upgrade threshold: ≥3 events in the window.
	if len(filtered) >= 3 && s.SecurityMode != "strict" {
		s.SecurityMode = "strict"
		s.ModeUpgraded = true
		return true
	}
	return false
}

// newSessionState creates a fresh session state with default values.
func newSessionState(sessionID string) *SessionState {
	return &SessionState{
		SessionID:    sessionID,
		SecurityMode: "standard",
		LastCheckTime: time.Now(),
	}
}
