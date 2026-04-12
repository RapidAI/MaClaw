package remote

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// StallDetectorConfig holds configuration for the StallDetector.
type StallDetectorConfig struct {
	StallTimeout    time.Duration     // default 180s
	MaxNudgeCount   int               // default 2
	MaxPauseTimeout time.Duration     // default 300s — max time monitoring can stay paused before auto-resume
	NudgeMessages   map[string]string // per-tool nudge text; key = tool name (lowercase)
	DefaultNudge    string            // default: concise action-oriented nudge
}

// sessionStallState tracks stall monitoring state for a single session.
type sessionStallState struct {
	timer         *time.Timer
	stallState    StallState
	nudgeCount    int
	lastOutput    time.Time
	cancelCh      chan struct{}
	exec          ExecutionHandle
	tool          string
	lastNudgeText string
	paused        bool      // true when session is in thinking state (no output expected)
	pausedAt      time.Time // when pause started — used for MaxPauseTimeout
}

// StallDetector manages stall detection for all active sessions.
type StallDetector struct {
	mu       sync.Mutex
	config   StallDetectorConfig
	sessions map[string]*sessionStallState
	logger   func(string)

	// OnStallStateChanged is called when a session's stall state changes.
	OnStallStateChanged func(sessionID string, state StallState, nudgeCount int)
}

// NewStallDetector creates a StallDetector with the given config.
func NewStallDetector(config StallDetectorConfig, logger func(string)) *StallDetector {
	if config.StallTimeout <= 0 {
		config.StallTimeout = 180 * time.Second
	}
	if config.MaxNudgeCount <= 0 {
		config.MaxNudgeCount = 2
	}
	if config.MaxPauseTimeout <= 0 {
		config.MaxPauseTimeout = 300 * time.Second
	}
	if config.DefaultNudge == "" {
		config.DefaultNudge = "Continue working on the current task. Do not repeat or re-explain what was already stated."
	}
	if logger == nil {
		logger = func(string) {}
	}
	return &StallDetector{
		config:   config,
		sessions: make(map[string]*sessionStallState),
		logger:   logger,
	}
}

// StartMonitoring begins stall monitoring for the given session.
// Codex is skipped (one-shot, no interactive nudge).
func (d *StallDetector) StartMonitoring(sessionID string, exec ExecutionHandle, tool string) {
	if strings.EqualFold(tool, "codex") {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, ok := d.sessions[sessionID]; ok {
		d.stopSessionLocked(existing)
		delete(d.sessions, sessionID)
	}

	ss := &sessionStallState{
		timer:      time.NewTimer(d.config.StallTimeout),
		stallState: StallStateNormal,
		lastOutput: time.Now(),
		cancelCh:   make(chan struct{}),
		exec:       exec,
		tool:       tool,
	}
	d.sessions[sessionID] = ss
	go d.monitorLoop(sessionID, ss)
}

// StopMonitoring stops stall monitoring for the given session.
func (d *StallDetector) StopMonitoring(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ss, ok := d.sessions[sessionID]; ok {
		d.stopSessionLocked(ss)
		delete(d.sessions, sessionID)
	}
}

// ResetTimer resets the stall timer for the given session.
func (d *StallDetector) ResetTimer(sessionID string, hasNewOutput bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ss, ok := d.sessions[sessionID]
	if !ok {
		return
	}
	if !ss.timer.Stop() {
		select {
		case <-ss.timer.C:
		default:
		}
	}
	ss.timer.Reset(d.config.StallTimeout)
	ss.lastOutput = time.Now()

	if hasNewOutput && ss.stallState == StallStateSuspected {
		ss.nudgeCount = 0
		ss.stallState = StallStateNormal
		if d.OnStallStateChanged != nil {
			d.OnStallStateChanged(sessionID, StallStateNormal, 0)
		}
	}
}

// PauseMonitoring temporarily pauses stall detection for the given session.
// Used when the session enters a thinking state where no streaming output is
// expected for an extended period.
func (d *StallDetector) PauseMonitoring(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ss, ok := d.sessions[sessionID]
	if !ok || ss.paused {
		return
	}
	ss.paused = true
	ss.pausedAt = time.Now()
	ss.timer.Stop()
	d.logger("[stall-detector] session=" + sessionID + " monitoring paused (thinking)")
}

// ResumeMonitoring resumes stall detection after a pause.
func (d *StallDetector) ResumeMonitoring(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ss, ok := d.sessions[sessionID]
	if !ok || !ss.paused {
		return
	}
	ss.paused = false
	ss.lastOutput = time.Now()
	if !ss.timer.Stop() {
		select {
		case <-ss.timer.C:
		default:
		}
	}
	ss.timer.Reset(d.config.StallTimeout)
	d.logger("[stall-detector] session=" + sessionID + " monitoring resumed")
}

// GetState returns the current stall state for the given session.
func (d *StallDetector) GetState(sessionID string) StallState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ss, ok := d.sessions[sessionID]; ok {
		return ss.stallState
	}
	return StallStateNormal
}

// GetNudgeCount returns the current nudge count for the given session.
func (d *StallDetector) GetNudgeCount(sessionID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ss, ok := d.sessions[sessionID]; ok {
		return ss.nudgeCount
	}
	return 0
}

// IsNudgeEcho checks whether the given line matches the last nudge text.
// Uses substring matching because PTY output may wrap or fragment the
// nudge text across lines.
func (d *StallDetector) IsNudgeEcho(sessionID string, line string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ss, ok := d.sessions[sessionID]
	if !ok || ss.lastNudgeText == "" {
		return false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	nudge := strings.TrimSpace(ss.lastNudgeText)
	return trimmed == nudge || strings.Contains(nudge, trimmed) || strings.Contains(trimmed, nudge)
}

// Close stops all monitoring and releases resources.
func (d *StallDetector) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, ss := range d.sessions {
		d.stopSessionLocked(ss)
		delete(d.sessions, id)
	}
}

func (d *StallDetector) stopSessionLocked(ss *sessionStallState) {
	select {
	case <-ss.cancelCh:
	default:
		close(ss.cancelCh)
	}
	ss.timer.Stop()
}

func (d *StallDetector) monitorLoop(sessionID string, ss *sessionStallState) {
	defer func() {
		if r := recover(); r != nil {
			d.logger("[stall-detector-panic] session=" + sessionID + " recovered from panic")
		}
	}()

	for {
		select {
		case <-ss.cancelCh:
			return
		case <-ss.timer.C:
			d.mu.Lock()

			// If monitoring is paused (thinking state), check if we've
			// exceeded MaxPauseTimeout. If so, auto-resume — the LLM may
			// be stuck in thinking and will never produce streaming output
			// to trigger ResumeMonitoring.
			if ss.paused {
				if time.Since(ss.pausedAt) >= d.config.MaxPauseTimeout {
					ss.paused = false
					d.logger("[stall-detector] session=" + sessionID + " auto-resumed after MaxPauseTimeout (" + d.config.MaxPauseTimeout.String() + ")")
					// Fall through to normal stall detection below —
					// immediately trigger nudge/interrupt since the session
					// has been unresponsive for the entire pause duration.
				} else {
					ss.timer.Reset(d.config.StallTimeout)
					d.mu.Unlock()
					continue
				}
			}

			if ss.nudgeCount >= d.config.MaxNudgeCount {
				ss.stallState = StallStateStuck
				cb := d.OnStallStateChanged
				nc := ss.nudgeCount
				d.mu.Unlock()
				if cb != nil {
					cb(sessionID, StallStateStuck, nc)
				}
				return
			}

			ss.stallState = StallStateSuspected
			ss.nudgeCount++
			cb := d.OnStallStateChanged
			nc := ss.nudgeCount
			d.mu.Unlock()

			if cb != nil {
				cb(sessionID, StallStateSuspected, nc)
			}

			nudgeText := d.config.DefaultNudge
			d.mu.Lock()
			if msg, ok := d.config.NudgeMessages[strings.ToLower(ss.tool)]; ok {
				nudgeText = msg
			}
			ss.lastNudgeText = nudgeText
			exec := ss.exec
			d.mu.Unlock()

			if err := exec.Write([]byte(nudgeText)); err != nil {
				d.logger("[stall-nudge-error] session=" + sessionID + " error=" + err.Error())
				d.mu.Lock()
				ss.stallState = StallStateStuck
				cb2 := d.OnStallStateChanged
				nc2 := ss.nudgeCount
				d.mu.Unlock()
				if cb2 != nil {
					cb2(sessionID, StallStateStuck, nc2)
				}
				return
			}

			d.logger("[stall-nudge] session=" + sessionID + " nudge_count=" + strconv.Itoa(nc))

			d.mu.Lock()
			ss.timer.Reset(d.config.StallTimeout)
			d.mu.Unlock()
		}
	}
}
