package agentservice

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botlog"
)

// Pausing holds a running run at its next step boundary: the step under way
// (a model call or a tool) finishes, then the loop waits before starting the
// next one. Resume releases the same run with its conversation intact; it is
// not a new run. A pause that is never resumed ends by policy instead of
// holding a desktop and a session forever: after the timeout the run is
// cancelled with a clear reason.
//
// Like supplements this is in-process only. A run lives only as long as this
// process holds it, so a durable pause would outlive the thing it pauses.

// DefaultRunPauseTimeout is how long a pause may last before the run is
// cancelled. MACLAW_RUN_PAUSE_TIMEOUT_SEC overrides it.
const DefaultRunPauseTimeout = 30 * time.Minute

// MaxRunPauseTimeout caps any caller-chosen timeout.
const MaxRunPauseTimeout = 6 * time.Hour

// RunPauseState is what pause, resume and status calls report.
type RunPauseState struct {
	RunID string `json:"run_id"`
	// Paused is true from the pause request until resume, cancel or timeout.
	Paused bool `json:"paused"`
	// Held is true once the loop actually reached a step boundary and is
	// waiting; a paused run that is still finishing its current step is not
	// held yet.
	Held       bool       `json:"held"`
	PausedAt   *time.Time `json:"paused_at,omitempty"`
	HeldAt     *time.Time `json:"held_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	TimeoutSec int64      `json:"timeout_sec,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Outcome    string     `json:"outcome,omitempty"` // resumed, cancelled, timed_out
	OnTimeout  string     `json:"on_timeout,omitempty"`
}

type runPauseGate struct {
	principal  Principal
	instanceID string
	reason     string
	pausedAt   time.Time
	heldAt     time.Time
	expires    time.Time
	timeout    time.Duration
	wake       chan struct{}
	woken      bool
}

type runPauses struct {
	mu    sync.Mutex
	gates map[string]*runPauseGate
	// last keeps the final outcome of recently ended pauses, bounded.
	last  map[string]string
	order []string
}

func runPauseTimeoutFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("MACLAW_RUN_PAUSE_TIMEOUT_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			d := time.Duration(n) * time.Second
			if d > MaxRunPauseTimeout {
				d = MaxRunPauseTimeout
			}
			return d
		}
	}
	return DefaultRunPauseTimeout
}

func (g *runPauseGate) state(runID string) RunPauseState {
	st := RunPauseState{RunID: runID, Paused: true, Reason: g.reason, TimeoutSec: int64(g.timeout / time.Second), OnTimeout: "cancel"}
	p, e := g.pausedAt, g.expires
	st.PausedAt, st.ExpiresAt = &p, &e
	if !g.heldAt.IsZero() {
		h := g.heldAt
		st.Held, st.HeldAt = true, &h
	}
	return st
}

func (r *runPauses) finish(runID, outcome string) {
	if r.last == nil {
		r.last = map[string]string{}
	}
	if _, ok := r.last[runID]; !ok {
		r.order = append(r.order, runID)
	}
	r.last[runID] = outcome
	for len(r.order) > 256 {
		delete(r.last, r.order[0])
		r.order = r.order[1:]
	}
}

// release wakes a held run without resuming it; used by cancel. It reports
// whether a pause was open.
func (r *runPauses) release(runID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.gates[runID]
	if !ok {
		return false
	}
	delete(r.gates, runID)
	if !g.woken {
		g.woken = true
		close(g.wake)
	}
	r.finish(runID, "cancelled")
	return true
}

// PauseRun asks a running run to hold at its next step boundary. timeout <= 0
// uses the configured default. Pausing an already paused run returns its
// current state unchanged.
func (s *Service) PauseRun(ctx context.Context, p Principal, instanceID, runID, reason string, timeout time.Duration) (*RunPauseState, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	run, err := s.pausableRun(p, instanceID, runID)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = runPauseTimeoutFromEnv()
	}
	if timeout > MaxRunPauseTimeout {
		timeout = MaxRunPauseTimeout
	}
	now := s.now()
	s.pauses.mu.Lock()
	if s.pauses.gates == nil {
		s.pauses.gates = map[string]*runPauseGate{}
	}
	g, ok := s.pauses.gates[run.ID]
	if !ok {
		g = &runPauseGate{principal: p, instanceID: instanceID, reason: strings.TrimSpace(reason), pausedAt: now, expires: now.Add(timeout), timeout: timeout, wake: make(chan struct{})}
		s.pauses.gates[run.ID] = g
	}
	st := g.state(run.ID)
	s.pauses.mu.Unlock()
	if !ok {
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.pause_requested", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": run.SessionID, "timeout_sec": strconv.FormatInt(st.TimeoutSec, 10)}})
		s.emitRunEvent(*run, "run.pause_requested", map[string]any{"timeout_sec": st.TimeoutSec})
		botlog.Write(run.ID, "srv.run_pause", nil, "run", run.ID, "timeout_sec", strconv.FormatInt(st.TimeoutSec, 10))
	}
	return &st, nil
}

// ResumeRun releases a paused run so the same run continues.
func (s *Service) ResumeRun(ctx context.Context, p Principal, instanceID, runID string) (*RunPauseState, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, strings.TrimSpace(runID))
	if err != nil {
		return nil, ErrRunNotFound
	}
	s.pauses.mu.Lock()
	g, ok := s.pauses.gates[run.ID]
	if ok {
		delete(s.pauses.gates, run.ID)
		if !g.woken {
			g.woken = true
			close(g.wake)
		}
		s.pauses.finish(run.ID, "resumed")
	}
	s.pauses.mu.Unlock()
	st := RunPauseState{RunID: run.ID, Outcome: "resumed"}
	if !ok {
		if run.Status != RunStatusRunning {
			return nil, ErrRunNotRunning
		}
		// Resuming a run that is not paused is a no-op, not an error: the
		// caller's intent (the run keeps going) already holds.
		st.Outcome = "not_paused"
		return &st, nil
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.resumed", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": run.SessionID}})
	s.emitRunEvent(run, "run.resumed", map[string]any{})
	botlog.Write(run.ID, "srv.run_resume", nil, "run", run.ID)
	return &st, nil
}

// RunPause reports a run's pause state.
func (s *Service) RunPause(ctx context.Context, p Principal, instanceID, runID string) (*RunPauseState, error) {
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, strings.TrimSpace(runID))
	if err != nil {
		return nil, ErrRunNotFound
	}
	s.pauses.mu.Lock()
	defer s.pauses.mu.Unlock()
	if g, ok := s.pauses.gates[run.ID]; ok {
		st := g.state(run.ID)
		return &st, nil
	}
	return &RunPauseState{RunID: run.ID, Outcome: s.pauses.last[run.ID]}, nil
}

func (s *Service) pausableRun(p Principal, instanceID, runID string) (*Run, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, ErrRunNotFound
	}
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)
	if err != nil {
		return nil, ErrRunNotFound
	}
	if run.Status != RunStatusRunning {
		return nil, ErrRunNotRunning
	}
	// Only a run this process executes can be held at a boundary.
	if _, ok := s.peekRunCancel(run.ID); !ok {
		return nil, ErrRunNotRunning
	}
	return &run, nil
}

// holdAtStepBoundary is called by the loop before each step. It returns at
// once unless the run is paused; then it waits for resume, cancel, or the
// pause timeout, which cancels the run.
func (s *Service) holdAtStepBoundary(runID string) {
	if s == nil {
		return
	}
	s.pauses.mu.Lock()
	g, ok := s.pauses.gates[runID]
	if !ok {
		s.pauses.mu.Unlock()
		return
	}
	if g.heldAt.IsZero() {
		g.heldAt = s.now()
	}
	wake, wait := g.wake, time.Until(g.expires)
	s.pauses.mu.Unlock()
	botlog.Write(runID, "srv.run_held", nil, "run", runID)
	if wait < 0 {
		wait = 0
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-wake:
		return
	case <-t.C:
	}
	// Timed out: end the pause and cancel the run under the pauser's own
	// principal, so it is recorded exactly like a person pressing stop.
	s.pauses.mu.Lock()
	cur, still := s.pauses.gates[runID]
	if still && cur == g {
		delete(s.pauses.gates, runID)
		g.woken = true
		close(g.wake)
		s.pauses.finish(runID, "timed_out")
	}
	s.pauses.mu.Unlock()
	if !still || cur != g {
		return
	}
	botlog.Write(runID, "srv.run_pause_timeout", nil, "run", runID)
	_, _ = s.CancelRun(context.Background(), g.principal, g.instanceID, runID)
	s.pauses.mu.Lock()
	s.pauses.finish(runID, "timed_out")
	s.pauses.mu.Unlock()
}
