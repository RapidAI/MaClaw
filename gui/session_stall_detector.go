package main

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
	lastNudgeText string // for echo filtering
	paused        bool   // true when session is in thinking state (no output expected)
	pausedAt      time.Time // when pause started — used for MaxPauseTimeout
}

// StallDetector manages stall detection for all active sessions.
type StallDetector struct {
	mu       sync.Mutex
	config   StallDetectorConfig
	sessions map[string]*sessionStallState
	logger   func(string)

	// OnStallStateChanged is called when a session's stall state changes.
	// Parameters: sessionID, new state, current nudge count.
	// Will be wired by the integration layer (Task 2.2 / 4.1).
	OnStallStateChanged func(sessionID string, state StallState, nudgeCount int)
}

// NewStallDetector creates a StallDetector with the given config.
// Zero-value fields are replaced with sensible defaults.
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
// If the tool is "codex" (case-insensitive), monitoring is skipped because
// Codex is one-shot and does not support interactive nudge.
// If the session is already being monitored, existing monitoring is stopped first.
func (d *StallDetector) StartMonitoring(sessionID string, exec ExecutionHandle, tool string) {
	if strings.EqualFold(tool, "codex") {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Stop existing monitoring for this session if any.
	if existing, ok := d.sessions[sessionID]; ok {
		d.stopSessionLocked(existing)
		delete(d.sessions, sessionID)
	}

	ss := &sessionStallState{
		timer:      time.NewTimer(d.config.StallTimeout),
		stallState: StallStateNormal,
		nudgeCount: 0,
		lastOutput: time.Now(),
		cancelCh:   make(chan struct{}),
		exec:       exec,
		tool:       tool,
	}
	d.sessions[sessionID] = ss

	go d.monitorLoop(sessionID, ss)
}

// StopMonitoring stops stall monitoring for the given session, releasing
// all timers and goroutines.
func (d *StallDetector) StopMonitoring(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ss, ok := d.sessions[sessionID]; ok {
		d.stopSessionLocked(ss)
		delete(d.sessions, sessionID)
	}
}

// ResetTimer resets the stall timer for the given session.
// If hasNewOutput is true and the session was in StallStateSuspected,
// the nudge counter is reset to 0 and the state returns to StallStateNormal.
func (d *StallDetector) ResetTimer(sessionID string, hasNewOutput bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ss, ok := d.sessions[sessionID]
	if !ok {
		return
	}

	// Reset the timer (even if paused — output proves the session is alive).
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
// expected for an extended period (e.g., LLM thinking via slow API proxies).
// The stall timer is stopped so it won't fire during the pause.
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
// Resets the timer to the full StallTimeout duration.
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
// Returns StallStateNormal if the session is not being monitored.
func (d *StallDetector) GetState(sessionID string) StallState {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ss, ok := d.sessions[sessionID]; ok {
		return ss.stallState
	}
	return StallStateNormal
}

// GetNudgeCount returns the current nudge count for the given session.
// Returns 0 if the session is not being monitored.
func (d *StallDetector) GetNudgeCount(sessionID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ss, ok := d.sessions[sessionID]; ok {
		return ss.nudgeCount
	}
	return 0
}

// IsNudgeEcho checks whether the given line matches the last nudge text
// sent to the session. Uses substring matching because PTY output may
// wrap or fragment the nudge text across lines.
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
	// Exact match or either contains the other (handles PTY wrapping).
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

// stopSessionLocked stops monitoring for a single session.
// Caller must hold d.mu.
func (d *StallDetector) stopSessionLocked(ss *sessionStallState) {
	select {
	case <-ss.cancelCh:
		// already closed
	default:
		close(ss.cancelCh)
	}
	ss.timer.Stop()
}

// monitorLoop is the per-session goroutine that watches for stall timeouts
// and sends nudge messages.
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

			// Check if we've exceeded the max nudge count.
			if ss.nudgeCount >= d.config.MaxNudgeCount {
				ss.stallState = StallStateStuck
				cb := d.OnStallStateChanged
				nudgeCount := ss.nudgeCount
				d.mu.Unlock()

				if cb != nil {
					cb(sessionID, StallStateStuck, nudgeCount)
				}
				return
			}

			// Mark as suspected and increment nudge count.
			ss.stallState = StallStateSuspected
			ss.nudgeCount++
			cb := d.OnStallStateChanged
			nudgeCount := ss.nudgeCount
			d.mu.Unlock()

			if cb != nil {
				cb(sessionID, StallStateSuspected, nudgeCount)
			}

			// Determine nudge text.
			nudgeText := d.config.DefaultNudge
			d.mu.Lock()
			if msg, ok := d.config.NudgeMessages[strings.ToLower(ss.tool)]; ok {
				nudgeText = msg
			}
			ss.lastNudgeText = nudgeText
			exec := ss.exec
			d.mu.Unlock()

			// For SDK sessions, the first nudge sends an interrupt control
			// request instead of a user message.  The Claude Code SDK
			// protocol defines interrupt as the proper way to break out of
			// a stuck turn — it's processed even when the agent is busy
			// executing tools, whereas a user message may be queued behind
			// the current turn and never read.
			//
			// On the second nudge (if the interrupt didn't help), we fall
			// back to sending a user message as before.
			if nudgeCount == 1 {
				if _, isSDK := exec.(*SDKExecutionHandle); isSDK {
					d.logger("[stall-nudge-interrupt] session=" + sessionID + " sending SDK interrupt before nudge text")
					if err := exec.Interrupt(); err != nil {
						d.logger("[stall-nudge-interrupt-error] session=" + sessionID + " error=" + err.Error())
						// Interrupt failed — fall through to send nudge text
					} else {
						d.logger("[stall-nudge-interrupt] session=" + sessionID + " interrupt sent, skipping nudge text for this round")
						d.mu.Lock()
						ss.timer.Reset(d.config.StallTimeout)
						d.mu.Unlock()
						continue
					}
				}
			}

			// Send the nudge as a user message (non-SDK sessions, or SDK
			// sessions where interrupt didn't help on the first attempt).
			if err := exec.Write([]byte(nudgeText)); err != nil {
				d.logger("[stall-nudge-error] session=" + sessionID + " error=" + err.Error())

				d.mu.Lock()
				ss.stallState = StallStateStuck
				cb2 := d.OnStallStateChanged
				nc := ss.nudgeCount
				d.mu.Unlock()

				if cb2 != nil {
					cb2(sessionID, StallStateStuck, nc)
				}
				return
			}

			d.logger("[stall-nudge] session=" + sessionID + " nudge_count=" + strconv.Itoa(nudgeCount))

			// Reset timer for the next stall timeout period.
			d.mu.Lock()
			ss.timer.Reset(d.config.StallTimeout)
			d.mu.Unlock()
		}
	}
}
