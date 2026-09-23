package browser

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// BrowserTaskSupervisor manages browser task execution with verification and retry.
type BrowserTaskSupervisor struct {
	mu       sync.RWMutex
	tasks    map[string]*taskEntry
	verifier *TaskVerifier
	retrier  *RetryStrategy
	loopMgr  *agent.BackgroundLoopManager
	statusC  chan agent.StatusEvent
	logger   func(string)

	// sessionFn returns the current browser CDP session.
	sessionFn func() (*Session, error)

	// agentSessionFn returns the stable browser agent session when task tools are
	// invoked through browser-session-*.
	agentSessionFn func() (*BrowserAgentSession, error)

	idCounter int
}

// taskEntry wraps TaskState with a cancel function for interruption.
type taskEntry struct {
	state   *TaskState
	spec    TaskSpec
	cancel  context.CancelFunc
	pauseC  chan struct{} // signal to pause after current step
	resumeC chan struct{} // signal to resume from paused state
}

type stepOutcome struct {
	result *BrowserActionResult
	err    error
}

func (o stepOutcome) isAskOrBlocked() bool {
	if o.result == nil {
		return false
	}
	return o.result.Status == "ask" || o.result.Status == "blocked"
}

// NewBrowserTaskSupervisor creates a supervisor.
func NewBrowserTaskSupervisor(
	loopMgr *agent.BackgroundLoopManager,
	statusC chan agent.StatusEvent,
	ocr OCRProvider,
	sessionFn func() (*Session, error),
	logger func(string),
) *BrowserTaskSupervisor {
	verifier := NewTaskVerifier(ocr, sessionFn)
	retrier := NewRetryStrategy(3, 3, ocr)
	return &BrowserTaskSupervisor{
		tasks:     make(map[string]*taskEntry),
		verifier:  verifier,
		retrier:   retrier,
		loopMgr:   loopMgr,
		statusC:   statusC,
		logger:    logger,
		sessionFn: sessionFn,
	}
}

// Execute runs a browser task. It blocks until the task completes, fails, or is cancelled.
func (s *BrowserTaskSupervisor) Execute(spec TaskSpec) (*TaskState, error) {
	if spec.FastBatch {
		if spec.MaxRetries < 0 {
			spec.MaxRetries = 0
		}
	} else if spec.MaxRetries <= 0 {
		spec.MaxRetries = 3
	}
	if spec.StepTimeout <= 0 {
		spec.StepTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.DiscardAll()

	// Generate task ID and register
	s.mu.Lock()
	s.idCounter++
	if spec.ID == "" {
		spec.ID = fmt.Sprintf("bt-%d", s.idCounter)
	}
	state := &TaskState{
		ID:         spec.ID,
		Status:     TaskStatusRunning,
		TotalSteps: len(spec.Steps),
		StartedAt:  time.Now(),
		StepTraces: []StepTrace{},
	}
	s.tasks[spec.ID] = &taskEntry{state: state, spec: spec, cancel: cancel, pauseC: make(chan struct{}, 1), resumeC: make(chan struct{}, 1)}
	s.mu.Unlock()

	s.log("browser task %s started: %s (%d steps)", spec.ID, spec.Description, len(spec.Steps))
	s.emitProgress(spec.ID, "started", 0, len(spec.Steps))

	var batchSession *BrowserAgentSession
	if spec.FastBatch && s.agentSessionFn != nil {
		agentSession, err := s.agentSessionFn()
		if err != nil {
			state.Status = TaskStatusFailed
			state.LastError = err.Error()
			return state, fmt.Errorf("browser session: %w", err)
		}
		if agentSession == nil {
			state.Status = TaskStatusFailed
			state.LastError = "browser session not connected"
			return state, fmt.Errorf("browser session not connected")
		}
		agentSession.bindLatestSnapshotRefs(spec.Steps)
		if err := agentSession.preflightBatchRefs(spec.Steps); err != nil {
			state.Status = TaskStatusFailed
			state.LastError = err.Error()
			return state, err
		}
		agentSession.beginFastBatch()
		defer agentSession.endFastBatch()
		batchSession = agentSession
	}

	// Execute steps
	for i, step := range spec.Steps {
		// Check cancellation
		if err := ctx.Err(); err != nil {
			state.Status = TaskStatusCancelled
			state.LastError = "cancelled by user"
			return state, fmt.Errorf("cancelled")
		}

		state.CurrentStep = i + 1
		stepTrace := StepTrace{StepIndex: i, Action: step.Action, StartedAt: time.Now()}
		if step.Target != nil {
			stepTrace.TabID = step.Target.TabID
			stepTrace.FrameID = step.Target.FrameID
		}
		state.StepTraces = append(state.StepTraces, stepTrace)
		s.emitProgress(spec.ID, fmt.Sprintf("step %d/%d: %s", i+1, len(spec.Steps), step.Action), i+1, len(spec.Steps))

		outcome := s.executeStepWithRetry(ctx, spec, step, i, state)
		if outcome.isAskOrBlocked() {
			state.Status = TaskStatusPaused
			if result := outcome.result; result != nil {
				state.AskUser = result.AskUser
				state.LastResultStatus = result.Status
				if result.Status == "blocked" {
					state.LastError = firstNonEmpty(result.Display, "blocked")
				}
				if len(state.StepTraces) > 0 {
					state.StepTraces[len(state.StepTraces)-1].Summary = result.Status
					state.StepTraces[len(state.StepTraces)-1].EndedAt = time.Now()
				}
				s.log("browser task %s paused at step %d: %s", spec.ID, i+1, result.Status)
				s.emitProgress(spec.ID, fmt.Sprintf("paused at step %d: %s", i+1, result.Status), i+1, len(spec.Steps))
			}
			return state, nil
		}
		if outcome.err != nil {
			err := outcome.err
			state.Status = TaskStatusFailed
			state.LastError = err.Error()
			rememberBatchObservation(state, outcome.result)
			if len(state.StepTraces) > 0 {
				state.StepTraces[len(state.StepTraces)-1].Summary = err.Error()
				state.StepTraces[len(state.StepTraces)-1].EndedAt = time.Now()
			}
			s.log("browser task %s failed at step %d: %v", spec.ID, i+1, err)
			s.emitProgress(spec.ID, fmt.Sprintf("failed at step %d: %v", i+1, err), i+1, len(spec.Steps))
			return state, err
		}
		if len(state.StepTraces) > 0 {
			state.StepTraces[len(state.StepTraces)-1].Summary = "ok"
			state.StepTraces[len(state.StepTraces)-1].EndedAt = time.Now()
		}
		if outcome.result != nil && outcome.result.batchStop {
			state.Status = TaskStatusStopped
			state.StoppedReason = outcome.result.batchStopReason
			if outcome.result.Status == "unchanged" {
				state.StoppedReason = "unchanged"
			}
			rememberBatchObservation(state, outcome.result)
			if outcome.result.batchObserveErr != "" {
				state.ObservationError = outcome.result.batchObserveErr
			}
			s.log("browser task %s stopped at step %d: %s", spec.ID, i+1, state.StoppedReason)
			s.emitProgress(spec.ID, fmt.Sprintf("stopped at step %d: %s", i+1, state.StoppedReason), i+1, len(spec.Steps))
			return state, nil
		}

		// Take checkpoint after each step. Fast batches observe once at the end.
		if !spec.FastBatch {
			s.takeCheckpoint(state, i)
		}

		// Check for pause signal after step completion
		s.mu.RLock()
		entry := s.tasks[spec.ID]
		s.mu.RUnlock()
		if entry != nil {
			select {
			case <-entry.pauseC:
				state.Status = TaskStatusPaused
				s.log("browser task %s paused after step %d", spec.ID, i+1)
				s.emitProgress(spec.ID, fmt.Sprintf("paused after step %d", i+1), i+1, len(spec.Steps))
				// Block until resume or cancel
				select {
				case <-entry.resumeC:
					state.Status = TaskStatusRunning
					s.log("browser task %s resumed", spec.ID)
					s.emitProgress(spec.ID, "resumed", i+1, len(spec.Steps))
				case <-ctx.Done():
					state.Status = TaskStatusCancelled
					state.LastError = "cancelled while paused"
					return state, fmt.Errorf("cancelled while paused")
				}
			default:
				// not paused, continue
			}
		}
	}

	if batchSession != nil {
		obs, obsErr := batchSession.finishFastBatch()
		if obsErr != nil {
			state.Status = TaskStatusStopped
			state.StoppedReason = "observe_failed"
			state.ObservationError = obsErr.Error()
			s.log("browser task %s stopped: final observation failed: %v", spec.ID, obsErr)
			s.emitProgress(spec.ID, "final observation failed", len(spec.Steps), len(spec.Steps))
			return state, nil
		}
		if obs != nil {
			state.Observation = obs.Display
			state.SnapshotID = obs.Snapshot.SnapshotID
			state.ObservationData = obs.Data
		}
	}

	// Final success criteria verification
	if len(spec.SuccessCriteria) > 0 {
		_ = s.verifier.WaitForStable(2 * time.Second)

		result, err := s.verifier.Verify(spec.SuccessCriteria)
		if err != nil {
			state.Status = TaskStatusFailed
			state.LastError = fmt.Sprintf("verification error: %v", err)
			return state, err
		}
		if !result.Passed {
			state.Status = TaskStatusFailed
			state.LastError = compactVerifyFailure(result)
			s.log("browser task %s verification failed: %s", spec.ID, state.LastError)
			return state, fmt.Errorf("%s", state.LastError)
		}
	}

	state.Status = TaskStatusCompleted
	s.log("browser task %s completed successfully", spec.ID)
	s.emitProgress(spec.ID, "completed", len(spec.Steps), len(spec.Steps))
	return state, nil
}

// GetState returns the current state of a task.
func (s *BrowserTaskSupervisor) GetState(taskID string) (*TaskState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok || entry == nil || entry.state == nil {
		return nil, false
	}
	return entry.state, true
}

// DiscardAll cancels and removes every tracked task for this supervisor.
func (s *BrowserTaskSupervisor) DiscardAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, entry := range s.tasks {
		if entry != nil && entry.cancel != nil {
			entry.cancel()
		}
		delete(s.tasks, id)
	}
}

// ResumeSpec returns the original spec and 0-based index of the paused step.
func (s *BrowserTaskSupervisor) ResumeSpec(taskID string) (TaskSpec, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok || entry == nil || entry.state == nil || entry.state.Status != TaskStatusPaused {
		return TaskSpec{}, 0, false
	}
	from := entry.state.CurrentStep - 1
	if from < 0 {
		from = 0
	}
	return entry.spec, from, true
}

// Cancel cancels a running or paused task.
func (s *BrowserTaskSupervisor) Cancel(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	st := entry.state.Status
	if st != TaskStatusRunning && st != TaskStatusPaused {
		return fmt.Errorf("task %s is not running or paused (status=%s)", taskID, st)
	}
	entry.cancel() // signal the context
	return nil
}

// Pause requests a running task to pause after the current step completes.
func (s *BrowserTaskSupervisor) Pause(taskID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if entry.state.Status != TaskStatusRunning {
		return fmt.Errorf("task %s is not running (status=%s)", taskID, entry.state.Status)
	}
	select {
	case entry.pauseC <- struct{}{}:
	default:
		// already signalled
	}
	return nil
}

// Resume resumes a paused task.
func (s *BrowserTaskSupervisor) Resume(taskID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if entry.state.Status != TaskStatusPaused {
		return fmt.Errorf("task %s is not paused (status=%s)", taskID, entry.state.Status)
	}
	select {
	case entry.resumeC <- struct{}{}:
	default:
	}
	return nil
}

// Verify runs success criteria verification on the current page (standalone).
func (s *BrowserTaskSupervisor) Verify(criteria []CriterionSpec) (*VerifyResult, error) {
	return s.verifier.Verify(criteria)
}

// ── internal ──

func stepOutcomeFailure(o stepOutcome) error {
	if o.err != nil {
		return o.err
	}
	if o.result != nil && o.result.GoalClass && o.result.Status == "unchanged" {
		// The click already ran. In a fast batch, failing the step clears the
		// submit guard and the model clicks again. Stop instead and keep the guard.
		if o.result.batchStop {
			return nil
		}
		return fmt.Errorf("submit click did not change the page")
	}
	return nil
}

func (s *BrowserTaskSupervisor) forgetStepSubmit(outcome stepOutcome) {
	if s == nil || outcome.result == nil || outcome.result.submitRememberKey == "" || s.agentSessionFn == nil {
		return
	}
	agentSession, err := s.agentSessionFn()
	if err != nil || agentSession == nil {
		return
	}
	agentSession.forgetSubmitClick(outcome.result.submitRememberKey)
}

func (s *BrowserTaskSupervisor) executeStepWithRetry(ctx context.Context, spec TaskSpec, step StepSpec, stepIdx int, state *TaskState) stepOutcome {
	timeout := stepBudget(step, spec.StepTimeout)

	currentStep := step
	for retry := 0; ; retry++ {
		if err := ctx.Err(); err != nil {
			return stepOutcome{err: fmt.Errorf("cancelled")}
		}

		outcome := s.executeOneStep(ctx, currentStep, timeout)
		if outcome.err != nil && outcome.result == nil && isPolicyDenied(outcome.err) {
			outcome = stepOutcome{result: blockedActionResult(currentStep.Action, outcome.err.Error())}
		}
		if outcome.isAskOrBlocked() {
			return outcome
		}

		err := stepOutcomeFailure(outcome)
		// Step-level verification
		if err == nil && currentStep.Verify != nil {
			vr, verr := s.verifier.Verify([]CriterionSpec{*currentStep.Verify})
			if verr != nil {
				err = verr
			} else if vr == nil || !vr.Passed {
				err = formatStepVerifyFailure(vr)
			}
		}
		outcome.err = err

		if err == nil {
			if len(state.StepTraces) > 0 {
				state.StepTraces[len(state.StepTraces)-1].Summary = "ok"
				state.StepTraces[len(state.StepTraces)-1].EndedAt = time.Now()
			}
			return outcome
		}

		s.forgetStepSubmit(outcome)

		if spec.FastBatch && retry >= spec.MaxRetries {
			return stepOutcome{err: fmt.Errorf("step %d failed: %v", stepIdx+1, err)}
		}
		if s.retrier == nil {
			return stepOutcome{err: err}
		}

		// Decide retry
		failType := s.retrier.ClassifyFailure(err, currentStep)
		decision := s.retrier.Decide(failType, currentStep, retry, nil)

		if !decision.ShouldRetry {
			return stepOutcome{err: fmt.Errorf("step %d failed: %v (%s)", stepIdx+1, err, decision.Reason)}
		}

		s.log("browser task %s step %d retry %d: %s", spec.ID, stepIdx+1, retry+1, decision.Reason)
		state.RetryCount++

		if decision.WaitBefore > 0 {
			timer := time.NewTimer(decision.WaitBefore)
			select {
			case <-ctx.Done():
				timer.Stop()
				return stepOutcome{err: fmt.Errorf("cancelled")}
			case <-timer.C:
			}
		}
		if decision.AdjustedStep != nil {
			currentStep = *decision.AdjustedStep
		}
	}
}

// stepBudget keeps a wait step from being cancelled at the same moment its
// sleep ends. A timeout of 10s plus a 10s wait used to fail the batch.
func stepBudget(step StepSpec, fallback time.Duration) time.Duration {
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = fallback
	}
	if !strings.EqualFold(strings.TrimSpace(step.Action), "wait") {
		return timeout
	}
	wait := waitStepDuration(step.Params)
	if timeout < wait+2*time.Second {
		return wait + 2*time.Second
	}
	return timeout
}

func waitStepDuration(params map[string]string) time.Duration {
	if v := firstStepParam(params, "duration_ms", "ms"); v != "" {
		var ms int
		if _, err := fmt.Sscanf(v, "%d", &ms); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	sec := 10
	if v := firstStepParam(params, "timeout"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &sec); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 10 * time.Second
}

func (s *BrowserTaskSupervisor) executeOneStep(ctx context.Context, step StepSpec, timeout time.Duration) stepOutcome {
	stepCtx, stepCancel := context.WithTimeout(ctx, timeout)
	defer stepCancel()

	ch := make(chan stepOutcome, 1)
	go func() {
		if s.agentSessionFn != nil {
			agentSession, err := s.agentSessionFn()
			if err != nil {
				ch <- stepOutcome{err: fmt.Errorf("browser session: %w", err)}
				return
			}
			result, err := s.doAgentStep(agentSession, step)
			ch <- stepOutcome{result: result, err: err}
			return
		}
		if s.sessionFn == nil {
			ch <- stepOutcome{err: fmt.Errorf("browser session not connected")}
			return
		}
		sess, err := s.sessionFn()
		if err != nil {
			ch <- stepOutcome{err: fmt.Errorf("browser session: %w", err)}
			return
		}
		if sess == nil {
			ch <- stepOutcome{err: fmt.Errorf("browser session not connected")}
			return
		}
		ch <- stepOutcome{err: s.doStep(sess, step)}
	}()

	select {
	case outcome := <-ch:
		return outcome
	case <-stepCtx.Done():
		if ctx.Err() != nil {
			return stepOutcome{err: fmt.Errorf("cancelled")}
		}
		return stepOutcome{err: fmt.Errorf("step timed out after %v", timeout)}
	}
}

func (s *BrowserTaskSupervisor) doAgentStep(agentSession *BrowserAgentSession, step StepSpec) (*BrowserActionResult, error) {
	if agentSession == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	step.Action = strings.ToLower(strings.TrimSpace(step.Action))
	snapshotID := step.Params["snapshot_id"]
	ref := step.Params["ref"]
	selector := step.Params["selector"]
	switch step.Action {
	case "navigate":
		url := step.Params["url"]
		if url == "" {
			return nil, fmt.Errorf("navigate: missing url param")
		}
		return agentSession.Navigate(url)

	case "click":
		text := step.Params["text"]
		var result *BrowserActionResult
		var err error
		if ref == "" && selector == "" && text != "" {
			result, err = agentSession.ClickText(snapshotID, text)
		} else if ref == "" && selector == "" {
			return nil, fmt.Errorf("click: missing selector/ref/text param")
		} else {
			result, err = agentSession.Click(snapshotID, ref, selector)
		}
		if result != nil {
			agentSession.rememberSubmitClickIfOK(result.submitRememberKey, result)
		}
		return result, err

	case "click_at":
		return nil, fmt.Errorf("click_at step is disabled in stable browser tasks; use click with ref/selector/text")

	case "type":
		text := firstStepParam(step.Params, "text", "value")
		contentFormat := step.Params["content_format"]
		return agentSession.TypeContent(snapshotID, ref, selector, text, contentFormat)

	case "wait":
		timeoutMS := 0
		if v := firstStepParam(step.Params, "duration_ms", "ms"); v != "" {
			fmt.Sscanf(v, "%d", &timeoutMS)
		}
		if timeoutMS <= 0 {
			timeoutSec := 10
			if v, ok := step.Params["timeout"]; ok {
				fmt.Sscanf(v, "%d", &timeoutSec)
			}
			timeoutMS = timeoutSec * 1000
		}
		return agentSession.Wait(snapshotID, ref, selector, timeoutMS)

	case "eval":
		return nil, fmt.Errorf("eval step is disabled in stable browser tasks; use observe/extract plus page-level actions")

	case "scroll":
		dx, dy := stepScrollDelta(step.Params)
		return agentSession.ScrollBy(snapshotID, ref, selector, dx, dy)

	case "select":
		return agentSession.SelectOption(snapshotID, ref, selector, firstStepParam(step.Params, "value", "label", "text"))

	case "hover":
		return agentSession.Hover(snapshotID, ref, selector)

	case "press":
		return agentSession.Press(firstStepParam(step.Params, "key", "text"))

	case "dialog":
		accept := true
		if v := strings.ToLower(strings.TrimSpace(step.Params["accept"])); v == "false" || v == "0" {
			accept = false
		}
		return agentSession.HandleDialog(accept, step.Params["text"])

	case "set_files":
		return agentSession.SetFilesOn(snapshotID, ref, selector, splitStepList(firstStepParam(step.Params, "files", "file", "path", "paths")))

	default:
		return nil, fmt.Errorf("unknown action: %s", step.Action)
	}
}

// firstStepParam returns the first non-empty step field. Models often send
// type text as value, press keys as text, and scroll distance as dy.
func firstStepParam(params map[string]string, keys ...string) string {
	if params == nil {
		return ""
	}
	for _, key := range keys {
		if v := strings.TrimSpace(params[key]); v != "" {
			return v
		}
	}
	return ""
}

func stepScrollDelta(params map[string]string) (dx, dy int) {
	dx, dy = 0, 500
	if v := firstStepParam(params, "delta_x", "dx"); v != "" {
		fmt.Sscanf(v, "%d", &dx)
	}
	if v := firstStepParam(params, "delta_y", "dy"); v != "" {
		fmt.Sscanf(v, "%d", &dy)
	}
	return dx, dy
}

func (s *BrowserTaskSupervisor) doStep(sess *Session, step StepSpec) error {
	step.Action = strings.ToLower(strings.TrimSpace(step.Action))
	switch step.Action {
	case "navigate":
		url := step.Params["url"]
		if url == "" {
			return fmt.Errorf("navigate: missing url param")
		}
		_, err := sess.Navigate(url)
		return err

	case "click":
		sel := step.Params["selector"]
		if sel == "" {
			return fmt.Errorf("click: missing selector param")
		}
		return sess.Click(sel)

	case "hover":
		sel := step.Params["selector"]
		if sel == "" {
			return fmt.Errorf("hover: missing selector param")
		}
		return sess.Hover(sel)

	case "click_at":
		return fmt.Errorf("click_at step is disabled in stable browser tasks; use click with selector")

	case "type":
		sel := step.Params["selector"]
		text := firstStepParam(step.Params, "text", "value")
		contentFormat := step.Params["content_format"]
		if sel == "" {
			return fmt.Errorf("type: missing selector param")
		}
		return sess.TypeContent(sel, text, contentFormat)

	case "wait":
		sel := step.Params["selector"]
		if sel == "" {
			return fmt.Errorf("wait: missing selector param")
		}
		timeoutSec := 10
		if v, ok := step.Params["timeout"]; ok {
			fmt.Sscanf(v, "%d", &timeoutSec)
		}
		return sess.WaitForSelector(sel, timeoutSec)

	case "eval":
		return fmt.Errorf("eval step is disabled in stable browser tasks; use observe/extract plus page-level actions")

	case "scroll":
		dx, dy := stepScrollDelta(step.Params)
		return sess.Scroll(dx, dy)

	case "select":
		sel := step.Params["selector"]
		val := firstStepParam(step.Params, "value", "label", "text")
		if sel == "" {
			return fmt.Errorf("select: missing selector param")
		}
		return sess.Select(sel, val)

	case "press":
		key := firstStepParam(step.Params, "key", "text")
		if key == "" {
			return fmt.Errorf("press: missing key param")
		}
		return sess.Press(key)

	case "dialog":
		accept := true
		if v := strings.ToLower(strings.TrimSpace(step.Params["accept"])); v == "false" || v == "0" {
			accept = false
		}
		return sess.HandleDialog(accept, step.Params["text"])

	default:
		return fmt.Errorf("unknown action: %s", step.Action)
	}
}

func (s *BrowserTaskSupervisor) takeCheckpoint(state *TaskState, stepIdx int) {
	if s == nil || s.sessionFn == nil || state == nil {
		return
	}
	sess, err := s.sessionFn()
	if err != nil || sess == nil {
		return
	}
	info, _ := sess.Info()
	cp := Checkpoint{
		StepIndex: stepIdx,
		Timestamp: time.Now(),
		TabID:     sess.activeTabID,
		FrameID:   sess.activeFrameID,
	}
	if info != nil {
		cp.URL = info.URL
		cp.Title = info.Title
	}
	if len(state.StepTraces) > 0 {
		state.StepTraces[len(state.StepTraces)-1].TabID = cp.TabID
		state.StepTraces[len(state.StepTraces)-1].FrameID = cp.FrameID
	}
	// Checkpoints are metadata-only on the hot path. Pixel capture is excluded
	// from the stable browser mechanism; observe/extract use DOM state instead.
	// Cap checkpoints at 10.
	const maxCheckpoints = 10
	state.Checkpoints = append(state.Checkpoints, cp)
	if len(state.Checkpoints) > maxCheckpoints {
		state.Checkpoints = state.Checkpoints[len(state.Checkpoints)-maxCheckpoints:]
	}
}

func (s *BrowserTaskSupervisor) capturePageSnapshot() *PageSnapshot {
	if s == nil || s.sessionFn == nil {
		return nil
	}
	sess, err := s.sessionFn()
	if err != nil || sess == nil {
		return nil
	}
	info, _ := sess.Info()
	ps := &PageSnapshot{}
	if info != nil {
		ps.URL = info.URL
		ps.Title = info.Title
	}
	ps.DOMSnippet = strings.Join(sess.lastNetworkLines(), "\n")
	ps.TabID = sess.activeTabID
	ps.FrameID = sess.activeFrameID
	ps.NetworkEvents = sess.lastNetworkLines()
	ps.ConsoleEvents = sess.lastErrorLines()
	// Try to get a DOM snippet
	html, err := sess.GetHTML("")
	if err == nil && len(html) > 500 {
		ps.DOMSnippet = html[:500] + "..."
	} else if err == nil {
		ps.DOMSnippet = html
	}
	return ps
}

func (s *BrowserTaskSupervisor) emitProgress(taskID, message string, current, total int) {
	if s.statusC == nil {
		return
	}
	select {
	case s.statusC <- agent.StatusEvent{
		Type:    agent.StatusEventProgress,
		LoopID:  taskID,
		Message: message,
	}:
	default:
	}
}

func rememberBatchObservation(state *TaskState, result *BrowserActionResult) {
	if state == nil || result == nil {
		return
	}
	if result.Display != "" {
		state.Observation = result.Display
	}
	if result.SnapshotID != "" {
		state.SnapshotID = result.SnapshotID
	}
	if result.Data != nil {
		state.ObservationData = result.Data
	}
}

func (s *BrowserTaskSupervisor) log(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger(fmt.Sprintf(format, args...))
	}
}
