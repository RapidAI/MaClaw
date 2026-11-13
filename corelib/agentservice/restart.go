package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const metaRestartCheckpoint = "restart_checkpoint"
const metaRestartTrajectory = "restart_trajectory"

// restartTrajectory is the conversation the loop had committed at the last
// step boundary, plus the task workspace. It is the checkpoint a restart
// continues from. Tool calls stay structured so the next turn does not
// flatten them into prose and run them again.
type restartTrajectory struct {
	Entries []agent.ConversationEntry `json:"entries"`
	Working *agent.WorkingState       `json:"working,omitempty"`
}

var restartTrajectories sync.Map // run id -> restartTrajectory

// stashRestartTrajectory keeps the loop's committed history until the run
// is stored as interrupted. An empty history is not a checkpoint.
func stashRestartTrajectory(runID string, entries []agent.ConversationEntry, working *agent.WorkingState) {
	runID = strings.TrimSpace(runID)
	if runID == "" || len(entries) == 0 {
		return
	}
	copied := append([]agent.ConversationEntry(nil), entries...)
	restartTrajectories.Store(runID, restartTrajectory{Entries: copied, Working: agent.CloneWorkingState(working)})
}

func loadRestartTrajectory(raw string) (restartTrajectory, bool) {
	var traj restartTrajectory
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &traj) != nil || len(traj.Entries) == 0 {
		return restartTrajectory{}, false
	}
	return traj, true
}

// restartNoticeContinue is the sentence a person sees when a task goes on
// after the service process comes back.
const restartNoticeContinue = "服务刚重启，这项任务接着做。"

// restartNoticeStopped is the sentence when that task cannot be continued.
const restartNoticeStopped = "这项任务被服务重启打断了，没法接着做。"

// RunHaltedError ends a run with a sentence the person can see. It is not a
// cancel and not a model retry.
type RunHaltedError struct {
	Sentence string
}

func (e *RunHaltedError) Error() string {
	if e == nil {
		return "run halted"
	}
	return e.Sentence
}

// RestartYielding is set while the process is draining so a loop stops at
// the next step boundary.
var restartYielding atomic.Bool

func RestartYielding() bool { return restartYielding.Load() }

// RunHaltReason is the host's terminal cause for a run, if it must stop
// before another model call. Nil means none.
var RunHaltReason func(runID string) error

var runInFlightTool sync.Map // run id -> desktop action name

// NoteRunTool records the desktop action a run is inside. An empty action
// clears it. Shutdown reads it so a click that may have landed is not repeated.
func NoteRunTool(runID, action string) {
	runID = strings.TrimSpace(runID)
	action = strings.TrimSpace(action)
	if runID == "" {
		return
	}
	if action == "" {
		runInFlightTool.Delete(runID)
		return
	}
	runInFlightTool.Store(runID, action)
}

func runTool(runID string) string {
	v, ok := runInFlightTool.Load(strings.TrimSpace(runID))
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func restartUncertain(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "publish", "click", "press":
		return strings.ToLower(strings.TrimSpace(action))
	default:
		return ""
	}
}

type restartCheckpoint struct {
	SessionID       string `json:"session_id"`
	InstanceID      string `json:"instance_id"`
	UncertainAction string `json:"uncertain_action,omitempty"`
	Resumed         bool   `json:"resumed,omitempty"`
}

func (s *Service) drainingNow() bool {
	return s != nil && s.draining.Load()
}

// refuseNewRun rejects a run admitted after shutdown has started.
func (s *Service) refuseNewRun() error {
	if s.drainingNow() {
		return ErrServiceDraining
	}
	return nil
}

// Drain stops new runs, lets in-flight runs reach a step boundary until ctx
// ends, then stores whatever is still running as interrupted.
func (s *Service) Drain(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.draining.Store(true)
	restartYielding.Store(true)
	defer restartYielding.Store(false)
	for {
		s.runMu.Lock()
		n := len(s.runningRuns)
		s.runMu.Unlock()
		if n == 0 || ctx.Err() != nil {
			break
		}
		timer := time.NewTimer(15 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	s.runMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.runningRuns))
	for _, cancel := range s.runningRuns {
		cancels = append(cancels, cancel)
	}
	s.runMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	s.activeRequests.Wait()
	s.markStillRunningInterrupted()
}

func (s *Service) finishInterrupted(run *Run, uncertain string) {
	if run == nil {
		return
	}
	completed := s.now()
	run.Status = RunStatusInterrupted
	run.Error = runInterruptedError
	run.CompletedAt = &completed
	if !run.StartedAt.IsZero() {
		run.DurationMs = completed.Sub(run.StartedAt).Milliseconds()
	}
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	raw, _ := json.Marshal(restartCheckpoint{SessionID: run.SessionID, InstanceID: run.InstanceID, UncertainAction: uncertain})
	run.Metadata[metaRestartCheckpoint] = string(raw)
	if v, ok := restartTrajectories.LoadAndDelete(run.ID); ok {
		if traj, ok := v.(restartTrajectory); ok && len(traj.Entries) > 0 {
			if body, err := json.Marshal(traj); err == nil {
				run.Metadata[metaRestartTrajectory] = string(body)
			}
		}
	}
	ev := runTerminalEventFor(*run, "run.interrupted", map[string]any{"error": run.Error, "uncertain_action": uncertain})
	committed, err := s.saveRunTerminalWithEvents(*run, []RunEvent{ev})
	if err != nil {
		_ = s.store.SaveRun(*run)
		committed = false
	}
	if !committed {
		s.emitRunEvent(*run, ev.Type, ev.Payload)
	}
}

func (s *Service) markStillRunningInterrupted() {
	if s == nil || s.store == nil {
		return
	}
	store, err := snapshotStore(s.store)
	if err != nil {
		return
	}
	tenants, err := store.ListTenants()
	if err != nil {
		return
	}
	for _, t := range tenants {
		users, _ := store.ListUsers(t.ID)
		for _, u := range users {
			instances, _ := store.ListInstances(t.ID, u.ID)
			for _, inst := range instances {
				runs, _ := store.ListRuns(t.ID, u.ID, inst.ID)
				for _, run := range runs {
					if run.Status != RunStatusRunning {
						continue
					}
					if _, live := s.peekRunCancel(run.ID); live {
						continue
					}
					uncertain := restartUncertain(runTool(run.ID))
					s.finishInterrupted(&run, uncertain)
				}
			}
		}
	}
}

// OnRunResumed tells the host a session is continuing and which action must
// be re-checked instead of repeated. Nil is a no-op.
var OnRunResumed func(sessionID, uncertainAction string)

// ResumeInterruptedRuns continues each interrupted desktop run once. A run
// that cannot continue is ended with restartNoticeStopped.
func (s *Service) ResumeInterruptedRuns(ctx context.Context) int {
	if s == nil || s.store == nil || ctx == nil {
		return 0
	}
	store, err := snapshotStore(s.store)
	if err != nil {
		log.Printf("[agentservice] resume interrupted: snapshot: %v", err)
		return 0
	}
	tenants, err := store.ListTenants()
	if err != nil {
		log.Printf("[agentservice] resume interrupted: list tenants: %v", err)
		return 0
	}
	n := 0
	for _, t := range tenants {
		users, err := store.ListUsers(t.ID)
		if err != nil {
			continue
		}
		for _, u := range users {
			instances, err := store.ListInstances(t.ID, u.ID)
			if err != nil {
				continue
			}
			byID := map[string]Instance{}
			for _, inst := range instances {
				byID[inst.ID] = inst
			}
			for _, inst := range instances {
				runs, err := store.ListRuns(t.ID, u.ID, inst.ID)
				if err != nil {
					continue
				}
				for _, run := range runs {
					if run.Status != RunStatusInterrupted {
						continue
					}
					if s.resumeOne(ctx, Principal{TenantID: t.ID, UserID: u.ID}, byID[run.InstanceID], run) {
						n++
					}
				}
			}
		}
	}
	return n
}

func (s *Service) resumeOne(ctx context.Context, p Principal, inst Instance, run Run) bool {
	var cp restartCheckpoint
	if run.Metadata != nil {
		_ = json.Unmarshal([]byte(run.Metadata[metaRestartCheckpoint]), &cp)
	}
	if cp.Resumed {
		// The flag was stored and the process died before the run was
		// executing again. Continuing now would be a second start.
		if run.Status == RunStatusInterrupted {
			s.endInterrupted(p, run, restartNoticeStopped)
		}
		return false
	}
	cp.Resumed = true
	cp.SessionID = firstNonEmpty(cp.SessionID, run.SessionID)
	cp.InstanceID = firstNonEmpty(cp.InstanceID, run.InstanceID)
	raw, _ := json.Marshal(cp)
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	run.Metadata[metaRestartCheckpoint] = string(raw)
	// Stored before the run is admitted again, so a crash cannot start it twice.
	_ = s.store.SaveRun(run)
	_, trajOK := loadRestartTrajectory(run.Metadata[metaRestartTrajectory])
	instLive, err := s.store.GetInstance(p.TenantID, p.UserID, cp.InstanceID)
	desktop := err == nil && strings.TrimSpace(instLive.Metadata["hub_bot"]) == "1"
	if err != nil || !desktop || strings.TrimSpace(cp.SessionID) == "" || !trajOK {
		s.endInterrupted(p, run, restartNoticeStopped)
		return false
	}
	_ = inst
	s.sayRestart(p, cp.InstanceID, cp.SessionID, restartNoticeContinue)
	if OnRunResumed != nil && cp.UncertainAction != "" {
		OnRunResumed(cp.SessionID, cp.UncertainAction)
	}
	meta := map[string]string{"restart_resume": "1", "restart_from": run.ID}
	if cp.UncertainAction != "" {
		meta["restart_uncertain"] = cp.UncertainAction
	}
	continuing := run
	_, err = s.PostMessageAsync(ctx, p, cp.InstanceID, cp.SessionID, PostMessageInput{
		Content:   restartNoticeContinue,
		Metadata:  meta,
		resumeRun: &continuing,
	})
	if err != nil {
		log.Printf("[agentservice] resume run %s: %v", run.ID, err)
		s.endInterrupted(p, run, restartNoticeStopped)
		return false
	}
	return true
}

// adoptRestartTrajectory replaces the flattened session tail with the
// committed conversation. Messages before the interrupted turn stay.
// The continuation sentence is the current user message and is not part
// of this history.
func (c *coreAgentCallbacks) adoptRestartTrajectory(req ExecuteRequest) {
	if c == nil || req.Message.Metadata == nil || c.executor == nil {
		return
	}
	from := strings.TrimSpace(req.Message.Metadata["restart_from"])
	if from == "" {
		return
	}
	svc := c.executor.getService()
	if svc == nil || svc.store == nil {
		return
	}
	run, err := svc.store.GetRun(req.Principal.TenantID, req.Principal.UserID, req.Instance.ID, from)
	if err != nil || run.Metadata == nil {
		return
	}
	traj, ok := loadRestartTrajectory(run.Metadata[metaRestartTrajectory])
	if !ok {
		return
	}
	prior := []agent.ConversationEntry{}
	if id := strings.TrimSpace(run.UserMessageID); id != "" {
		if msgs, err := svc.store.ListMessages(req.Session.ID); err == nil {
			var kept []Message
			for _, m := range msgs {
				if m.ID == id || m.ID == req.Message.ID {
					break
				}
				kept = append(kept, m)
			}
			prior = convertHistoryToEntries(kept, "")
		}
	}
	c.history = append(prior, traj.Entries...)
	c.restartWorking = traj.Working
}

func (c *coreAgentCallbacks) LoadWorkingState() *agent.WorkingState {
	if c == nil {
		return nil
	}
	return c.restartWorking
}

func (c *coreAgentCallbacks) SaveWorkingState(state *agent.WorkingState) {
	if c == nil {
		return
	}
	c.restartWorking = state
}

func (s *Service) sayRestart(p Principal, instanceID, sessionID, text string) {
	msg := Message{
		ID: NewID("msg"), SessionID: sessionID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID,
		Role: MessageRoleAssistant, OutputType: "text/plain", Content: text,
		Metadata:  map[string]string{"send_to_user": text, "restart_notice": "1"},
		CreatedAt: s.now(),
	}
	_ = s.store.SaveMessage(msg)
}

func (s *Service) endInterrupted(p Principal, run Run, text string) {
	msg := Message{
		ID: NewID("msg"), SessionID: run.SessionID, TenantID: p.TenantID, UserID: p.UserID, InstanceID: run.InstanceID,
		Role: MessageRoleAssistant, OutputType: "text/plain", Content: text,
		Metadata:  map[string]string{"send_to_user": text, "restart_notice": "1", "task_outcome": "blocked", "task_outcome_evidence": "interrupted by restart"},
		CreatedAt: s.now(),
	}
	_ = s.store.SaveMessage(msg)
	completed := s.now()
	run.Status = RunStatusFailed
	run.Error = "interrupted by restart"
	run.CompletedAt = &completed
	run.AssistantMessageID = msg.ID
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	run.Metadata["task_outcome"] = "blocked"
	run.Metadata["task_outcome_evidence"] = "interrupted by restart"
	run.Metadata["send_to_user"] = text
	_ = s.store.SaveRun(run)
}

// shutdownGrace is how long a stop waits for a step boundary. The unit's
// TimeoutStopSec must be longer than this plus the HTTP shutdown.
func ShutdownGrace() time.Duration {
	return 20 * time.Second
}

// errors.Is helper so a wrapped halt is recognized.
func IsRunHalted(err error) (*RunHaltedError, bool) {
	var halt *RunHaltedError
	if errors.As(err, &halt) {
		return halt, true
	}
	return nil, false
}
