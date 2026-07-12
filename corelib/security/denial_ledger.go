package security

// DenialLedger tracks consecutive tool policy denials and can auto-pause
// autonomous agent work (OpenSquilla-inspired denial ledger).
//
// Env:
//   MACLAW_DENIAL_PAUSE=off|0|false  — disable auto-pause
//   MACLAW_DENIAL_PAUSE_THRESHOLD=N  — consecutive denials before pause (default 5)

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DenialPauseEnvKey disables the ledger pause when off.
	DenialPauseEnvKey = "MACLAW_DENIAL_PAUSE"
	// DenialPauseThresholdEnvKey sets consecutive deny count (default 5).
	DenialPauseThresholdEnvKey = "MACLAW_DENIAL_PAUSE_THRESHOLD"
	defaultDenialPauseThreshold = 5
)

// DenialLedgerSnapshot is a JSON-friendly view for doctor / CLI.
type DenialLedgerSnapshot struct {
	Enabled           bool   `json:"enabled"`
	Threshold         int    `json:"threshold"`
	ConsecutiveDenies int    `json:"consecutive_denies"`
	TotalDenies       int64  `json:"total_denies"`
	Paused            bool   `json:"paused"`
	LastTool          string `json:"last_tool,omitempty"`
	LastReason        string `json:"last_reason,omitempty"`
	LastAt            string `json:"last_at,omitempty"`
	PauseMessage      string `json:"pause_message,omitempty"`
}

// DenialLedger is a process-local consecutive-deny tracker.
type DenialLedger struct {
	mu          sync.Mutex
	threshold   int
	enabled     bool
	consecutive int
	total       int64
	paused      bool
	lastTool    string
	lastReason  string
	lastAt      time.Time
}

var processDenialLedger = newDenialLedgerFromEnv()

func newDenialLedgerFromEnv() *DenialLedger {
	l := &DenialLedger{
		threshold: defaultDenialPauseThreshold,
		enabled:   true,
	}
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(DenialPauseEnvKey)))
	switch raw {
	case "0", "off", "false", "no", "disabled":
		l.enabled = false
	}
	if t := strings.TrimSpace(os.Getenv(DenialPauseThresholdEnvKey)); t != "" {
		if n, err := strconv.Atoi(t); err == nil {
			if n > 0 {
				l.threshold = n
			} else if n == 0 {
				l.enabled = false
			}
		}
	}
	return l
}

// ProcessDenialLedger returns the process-wide ledger.
func ProcessDenialLedger() *DenialLedger {
	return processDenialLedger
}

// ResetProcessDenialLedgerForTest replaces the process ledger (tests only).
func ResetProcessDenialLedgerForTest() {
	processDenialLedger = newDenialLedgerFromEnv()
}

// RecordDeny increments consecutive denials. Returns true if this call triggered pause.
func (l *DenialLedger) RecordDeny(toolName, reason string) (becamePaused bool) {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total++
	l.consecutive++
	l.lastTool = strings.TrimSpace(toolName)
	l.lastReason = strings.TrimSpace(reason)
	l.lastAt = time.Now().UTC()
	if !l.enabled || l.threshold <= 0 {
		return false
	}
	if l.paused {
		return false
	}
	if l.consecutive >= l.threshold {
		l.paused = true
		return true
	}
	return false
}

// RecordAllow resets the consecutive deny streak (successful tool path).
func (l *DenialLedger) RecordAllow() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consecutive = 0
}

// IsPaused reports whether autonomous work should stop.
func (l *DenialLedger) IsPaused() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled && l.paused
}

// ClearPause clears the pause flag and consecutive counter (operator recovery).
func (l *DenialLedger) ClearPause() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paused = false
	l.consecutive = 0
}

// Snapshot returns a copy of current state.
func (l *DenialLedger) Snapshot() DenialLedgerSnapshot {
	if l == nil {
		return DenialLedgerSnapshot{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s := DenialLedgerSnapshot{
		Enabled:           l.enabled,
		Threshold:         l.threshold,
		ConsecutiveDenies: l.consecutive,
		TotalDenies:       l.total,
		Paused:            l.paused,
		LastTool:          l.lastTool,
		LastReason:        l.lastReason,
	}
	if !l.lastAt.IsZero() {
		s.LastAt = l.lastAt.Format(time.RFC3339)
	}
	if l.paused {
		s.PauseMessage = fmt.Sprintf(
			"autonomous tools paused after %d consecutive security denials (last=%s). Clear with doctor/CLI resume or MACLAW_DENIAL_PAUSE=off",
			l.threshold, l.lastTool,
		)
	}
	return s
}

// PauseBlockMessage is returned to agent loops when paused.
func (l *DenialLedger) PauseBlockMessage() string {
	s := l.Snapshot()
	if !s.Paused {
		return ""
	}
	return s.PauseMessage
}
