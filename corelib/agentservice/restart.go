package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
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
	// Code is a stable log token. The person sees Sentence, not Code.
	Code string
}

func (e *RunHaltedError) Error() string {
	if e == nil {
		return "run halted"
	}
	if e.Code != "" && e.Sentence != "" {
		return e.Code + ": " + e.Sentence
	}
	if e.Sentence != "" {
		return e.Sentence
	}
	return e.Code
}

// RestartYielding is set while the process is draining so a loop stops at
// the next step boundary.
var restartYielding atomic.Bool

func RestartYielding() bool { return restartYielding.Load() }

// RunHaltReason is the host's terminal cause for a run, if it must stop
// before another model call. Nil means none.
var RunHaltReason func(runID string) error

var runInFlightTool sync.Map // run id -> RestartInFlight

// RunEnded drops process-local run state. The host sets it to clear its own
// maps. Nil is a no-op.
var RunEnded func(runID string)

// ClearRunTool drops the in-memory in-flight call for a run that has ended.
func ClearRunTool(runID string) {
	runInFlightTool.Delete(strings.TrimSpace(runID))
}

// RestartInFlight is one side-effecting call that had been dispatched and
// had not returned. The id is the model tool_call_id, so resume rewrites
// that call's result and leaves every other call alone.
type RestartInFlight struct {
	RunID string `json:"run_id,omitempty"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Args  string `json:"args"`
}

// restartOutcomeUnknown replaces the stored result of a call that was in
// flight when the process stopped. It is not a successful result and not an
// instruction to run the call again.
const restartOutcomeUnknown = "outcome unknown: interrupted by restart, verify before repeating"

type restartCheckpoint struct {
	SessionID  string           `json:"session_id"`
	InstanceID string           `json:"instance_id"`
	Resumed    bool             `json:"resumed,omitempty"`
	Attempts   int              `json:"attempts,omitempty"`
	InFlight   *RestartInFlight `json:"in_flight,omitempty"`
}

// ResumeProbe is one read of the world at the start of a resumed turn.
// The host sets it. A desktop call reads the page; a bash call reads the
// exit file and the process. Nil leaves the unknown result without a probe.
var ResumeProbe func(ctx context.Context, tenantID, userID, instanceID, sessionID, runID string, call RestartInFlight) string

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
	s.runMu.Lock()
	if s.drainCond == nil {
		s.drainCond = sync.NewCond(&s.runMu)
	}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.runMu.Lock()
			s.drainCond.Broadcast()
			s.runMu.Unlock()
		case <-stopped:
		}
	}()
	for len(s.runningRuns) > 0 && ctx.Err() == nil {
		s.drainCond.Wait()
	}
	cancels := make([]context.CancelFunc, 0, len(s.runningRuns))
	for _, cancel := range s.runningRuns {
		cancels = append(cancels, cancel)
	}
	s.runMu.Unlock()
	close(stopped)
	for _, cancel := range cancels {
		cancel()
	}
	s.activeRequests.Wait()
	s.markStillRunningInterrupted()
}

func (s *Service) mergedCheckpoint(run Run) restartCheckpoint {
	var cp restartCheckpoint
	if run.Metadata != nil {
		_ = json.Unmarshal([]byte(run.Metadata[metaRestartCheckpoint]), &cp)
	}
	if s != nil && s.store != nil && strings.TrimSpace(run.ID) != "" {
		if stored, err := s.store.GetRun(run.TenantID, run.UserID, run.InstanceID, run.ID); err == nil && stored.Metadata != nil {
			var disk restartCheckpoint
			_ = json.Unmarshal([]byte(stored.Metadata[metaRestartCheckpoint]), &disk)
			if disk.Resumed {
				cp.Resumed = true
			}
			if disk.Attempts > cp.Attempts {
				cp.Attempts = disk.Attempts
			}
			if cp.InFlight == nil {
				cp.InFlight = disk.InFlight
			}
			cp.SessionID = firstNonEmpty(cp.SessionID, disk.SessionID)
			cp.InstanceID = firstNonEmpty(cp.InstanceID, disk.InstanceID)
		}
	}
	cp.SessionID = firstNonEmpty(cp.SessionID, run.SessionID)
	cp.InstanceID = firstNonEmpty(cp.InstanceID, run.InstanceID)
	return cp
}

func (s *Service) finishInterrupted(run *Run) {
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
	cp := s.mergedCheckpoint(*run)
	raw, _ := json.Marshal(cp)
	run.Metadata[metaRestartCheckpoint] = string(raw)
	// A run that already resumed once is not offered a second trajectory.
	// A fresh interrupt replaces the trajectory, or deletes it when this
	// process has none, so the next start cannot continue from a stale one.
	if cp.Resumed || cp.Attempts >= 1 {
		delete(run.Metadata, metaRestartTrajectory)
		restartTrajectories.Delete(run.ID)
	} else if v, ok := restartTrajectories.LoadAndDelete(run.ID); ok {
		if traj, ok := v.(restartTrajectory); ok && len(traj.Entries) > 0 {
			if body, err := json.Marshal(traj); err == nil {
				run.Metadata[metaRestartTrajectory] = string(body)
			}
		} else {
			delete(run.Metadata, metaRestartTrajectory)
		}
	} else {
		delete(run.Metadata, metaRestartTrajectory)
	}
	ClearRunTool(run.ID)
	ev := runTerminalEventFor(*run, "run.interrupted", map[string]any{"error": run.Error})
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
					s.finishInterrupted(&run)
				}
			}
		}
	}
}

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
	if cp.Resumed || cp.Attempts >= 1 {
		// The flag was stored and the process died before the run was
		// executing again, or a resumed run was interrupted. Continuing
		// now would be a second start.
		if run.Status == RunStatusInterrupted {
			s.endInterrupted(p, run, restartNoticeStopped)
		}
		return false
	}
	cp.Resumed = true
	cp.Attempts = 1
	cp.SessionID = firstNonEmpty(cp.SessionID, run.SessionID)
	cp.InstanceID = firstNonEmpty(cp.InstanceID, run.InstanceID)
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	traj, trajOK := loadRestartTrajectory(run.Metadata[metaRestartTrajectory])
	if trajOK {
		calls := s.listInFlight(run.ID)
		if cp.InFlight != nil && strings.TrimSpace(cp.InFlight.ID) != "" {
			seen := false
			for _, call := range calls {
				if call.ID == cp.InFlight.ID {
					seen = true
					break
				}
			}
			if !seen {
				calls = append(calls, *cp.InFlight)
			}
		}
		for _, call := range calls {
			if strings.TrimSpace(call.ID) == "" {
				continue
			}
			probe := ""
			if ResumeProbe != nil {
				probe = ResumeProbe(ctx, p.TenantID, p.UserID, cp.InstanceID, cp.SessionID, run.ID, call)
			}
			traj.Entries = rewriteInterruptedResult(traj.Entries, call, probe)
		}
		if body, err := json.Marshal(traj); err == nil {
			run.Metadata[metaRestartTrajectory] = string(body)
		}
	}
	raw, _ := json.Marshal(cp)
	run.Metadata[metaRestartCheckpoint] = string(raw)
	// Stored before the run is admitted again, so a crash cannot start it twice.
	_ = s.store.SaveRun(run)
	instLive, err := s.store.GetInstance(p.TenantID, p.UserID, cp.InstanceID)
	desktop := err == nil && strings.TrimSpace(instLive.Metadata["hub_bot"]) == "1"
	if err != nil || !desktop || strings.TrimSpace(cp.SessionID) == "" || !trajOK {
		s.endInterrupted(p, run, restartNoticeStopped)
		return false
	}
	_ = inst
	s.sayRestart(p, cp.InstanceID, cp.SessionID, restartNoticeContinue)
	meta := map[string]string{"restart_resume": "1", "restart_from": run.ID}
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

// rewriteInterruptedResult replaces the result of the in-flight call.
// A different call, including another click, is left as it was stored.
func rewriteInterruptedResult(entries []agent.ConversationEntry, call RestartInFlight, probe string) []agent.ConversationEntry {
	id := strings.TrimSpace(call.ID)
	if id == "" {
		return entries
	}
	body := restartOutcomeUnknown
	if text := strings.TrimSpace(probe); text != "" {
		body += "\n" + text
	}
	out := append([]agent.ConversationEntry(nil), entries...)
	found := false
	for i := range out {
		if out[i].Role != "tool" || strings.TrimSpace(out[i].ToolCallID) != id {
			continue
		}
		out[i].Content = body
		out[i].ToolOutcome = "uncertain"
		found = true
	}
	if !found {
		out = append(out, agent.ConversationEntry{
			Role: "tool", ToolCallID: id, ToolName: call.Name, Content: body, ToolOutcome: "uncertain",
		})
	}
	return out
}

func inflightKey(runID, callID string) string {
	return strings.TrimSpace(runID) + "\x00" + strings.TrimSpace(callID)
}

func (s *Service) inflightPath() string {
	if s == nil || strings.TrimSpace(s.dataRoot) == "" {
		return ""
	}
	return filepath.Join(s.dataRoot, "restart-inflight.json")
}

func (s *Service) loadInFlight() {
	if s == nil || s.inflightLoaded {
		return
	}
	s.inflightLoaded = true
	if s.inflight == nil {
		s.inflight = map[string]RestartInFlight{}
	}
	path := s.inflightPath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var rows []RestartInFlight
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	for _, row := range rows {
		if strings.TrimSpace(row.RunID) == "" || strings.TrimSpace(row.ID) == "" {
			continue
		}
		s.inflight[inflightKey(row.RunID, row.ID)] = row
	}
}

func (s *Service) flushInFlight() error {
	path := s.inflightPath()
	if path == "" {
		return nil
	}
	rows := make([]RestartInFlight, 0, len(s.inflight))
	for _, row := range s.inflight {
		rows = append(rows, row)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// RecordInFlight stores one dispatched call in its own row. It does not
// rewrite the run row. Several calls in one step each have a row.
func (s *Service) RecordInFlight(p Principal, instanceID, runID string, call RestartInFlight) error {
	if s == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(call.ID) == "" {
		return nil
	}
	_ = p
	_ = instanceID
	call.RunID = runID
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	s.loadInFlight()
	s.inflight[inflightKey(runID, call.ID)] = call
	return s.flushInFlight()
}

// ClearInFlight drops one call that has returned, including during drain.
func (s *Service) ClearInFlight(p Principal, instanceID, runID, toolCallID string) error {
	if s == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(toolCallID) == "" {
		return nil
	}
	_ = p
	_ = instanceID
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	s.loadInFlight()
	delete(s.inflight, inflightKey(runID, toolCallID))
	return s.flushInFlight()
}

func (s *Service) listInFlight(runID string) []RestartInFlight {
	if s == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	s.loadInFlight()
	var out []RestartInFlight
	prefix := strings.TrimSpace(runID) + "\x00"
	for key, row := range s.inflight {
		if strings.HasPrefix(key, prefix) {
			out = append(out, row)
		}
	}
	return out
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
