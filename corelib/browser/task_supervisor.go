package browser

import (
	"context"
	"encoding/json"
	"fmt"
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

	idCounter int
}

// taskEntry wraps TaskState with a cancel function for interruption.
type taskEntry struct {
	state  *TaskState
	cancel context.CancelFunc
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
	if spec.MaxRetries <= 0 {
		spec.MaxRetries = 3
	}
	if spec.StepTimeout <= 0 {
		spec.StepTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	}
	s.tasks[spec.ID] = &taskEntry{state: state, cancel: cancel}
	s.mu.Unlock()

	s.log("browser task %s started: %s (%d steps)", spec.ID, spec.Description, len(spec.Steps))
	s.emitProgress(spec.ID, "started", 0, len(spec.Steps))

	// Execute steps
	for i, step := range spec.Steps {
		// Check cancellation
		if err := ctx.Err(); err != nil {
			state.Status = TaskStatusFailed
			state.LastError = "cancelled by user"
			return state, fmt.Errorf("cancelled")
		}

		state.CurrentStep = i + 1
		s.emitProgress(spec.ID, fmt.Sprintf("step %d/%d: %s", i+1, len(spec.Steps), step.Action), i+1, len(spec.Steps))

		err := s.executeStepWithRetry(ctx, spec, step, i, state)
		if err != nil {
			state.Status = TaskStatusFailed
			state.LastError = err.Error()
			s.log("browser task %s failed at step %d: %v", spec.ID, i+1, err)
			s.emitProgress(spec.ID, fmt.Sprintf("failed at step %d: %v", i+1, err), i+1, len(spec.Steps))
			return state, err
		}

		// Take checkpoint after each step
		s.takeCheckpoint(state, i)
	}

	// Final success criteria verification
	if len(spec.SuccessCriteria) > 0 {
		_ = s.verifier.WaitForStable(3 * time.Second)

		result, err := s.verifier.Verify(spec.SuccessCriteria)
		if err != nil {
			state.Status = TaskStatusFailed
			state.LastError = fmt.Sprintf("verification error: %v", err)
			return state, err
		}
		if !result.Passed {
			state.Status = TaskStatusFailed
			details, _ := json.Marshal(result.Details)
			state.LastError = fmt.Sprintf("success criteria not met: %s", string(details))
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
	if !ok {
		return nil, false
	}
	return entry.state, true
}

// Cancel cancels a running task.
func (s *BrowserTaskSupervisor) Cancel(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if entry.state.Status != TaskStatusRunning {
		return fmt.Errorf("task %s is not running (status=%s)", taskID, entry.state.Status)
	}
	entry.cancel() // signal the context
	return nil
}

// Verify runs success criteria verification on the current page (standalone).
func (s *BrowserTaskSupervisor) Verify(criteria []CriterionSpec) (*VerifyResult, error) {
	return s.verifier.Verify(criteria)
}

// ── internal ──

func (s *BrowserTaskSupervisor) executeStepWithRetry(ctx context.Context, spec TaskSpec, step StepSpec, stepIdx int, state *TaskState) error {
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = spec.StepTimeout
	}

	currentStep := step
	for retry := 0; ; retry++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled")
		}

		err := s.executeOneStep(ctx, currentStep, timeout)

		// Step-level verification
		if err == nil && currentStep.Verify != nil {
			vr, verr := s.verifier.Verify([]CriterionSpec{*currentStep.Verify})
			if verr != nil {
				err = verr
			} else if !vr.Passed {
				err = fmt.Errorf("step verification failed: %s", vr.Details[0].Error)
			}
		}

		if err == nil {
			return nil // success
		}

		// Decide retry
		failType := s.retrier.ClassifyFailure(err, currentStep)
		snapshot := s.capturePageSnapshot()
		decision := s.retrier.Decide(failType, currentStep, retry, snapshot)

		if !decision.ShouldRetry {
			return fmt.Errorf("step %d failed: %v (%s)", stepIdx+1, err, decision.Reason)
		}

		s.log("browser task %s step %d retry %d: %s", spec.ID, stepIdx+1, retry+1, decision.Reason)
		state.RetryCount++

		if decision.WaitBefore > 0 {
			time.Sleep(decision.WaitBefore)
		}
		if decision.AdjustedStep != nil {
			currentStep = *decision.AdjustedStep
		}
		// If NeedsLLM, we still retry — the LLMContext is available for the caller
		// to inspect via task state. In a future iteration, this will trigger
		// an actual LLM call.
	}
}

func (s *BrowserTaskSupervisor) executeOneStep(ctx context.Context, step StepSpec, timeout time.Duration) error {
	sess, err := s.sessionFn()
	if err != nil {
		return fmt.Errorf("browser session: %w", err)
	}

	stepCtx, stepCancel := context.WithTimeout(ctx, timeout)
	defer stepCancel()

	ch := make(chan error, 1)
	go func() {
		ch <- s.doStep(sess, step)
	}()

	select {
	case err := <-ch:
		return err
	case <-stepCtx.Done():
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("step timed out after %v", timeout)
	}
}

func (s *BrowserTaskSupervisor) doStep(sess *Session, step StepSpec) error {
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

	case "click_at":
		sel := step.Params["selector"]
		if sel == "" {
			return fmt.Errorf("click_at: missing selector param")
		}
		return sess.ClickAt(sel)

	case "type":
		sel := step.Params["selector"]
		text := step.Params["text"]
		if sel == "" {
			return fmt.Errorf("type: missing selector param")
		}
		return sess.Type(sel, text)

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
		expr := step.Params["expression"]
		if expr == "" {
			return fmt.Errorf("eval: missing expression param")
		}
		_, err := sess.Eval(expr)
		return err

	case "scroll":
		dx, dy := 0, 500
		if v, ok := step.Params["delta_x"]; ok {
			fmt.Sscanf(v, "%d", &dx)
		}
		if v, ok := step.Params["delta_y"]; ok {
			fmt.Sscanf(v, "%d", &dy)
		}
		return sess.Scroll(dx, dy)

	case "select":
		sel := step.Params["selector"]
		val := step.Params["value"]
		if sel == "" {
			return fmt.Errorf("select: missing selector param")
		}
		return sess.Select(sel, val)

	default:
		return fmt.Errorf("unknown action: %s", step.Action)
	}
}

func (s *BrowserTaskSupervisor) takeCheckpoint(state *TaskState, stepIdx int) {
	sess, err := s.sessionFn()
	if err != nil {
		return
	}
	info, _ := sess.Info()
	cp := Checkpoint{
		StepIndex: stepIdx,
		Timestamp: time.Now(),
	}
	if info != nil {
		cp.URL = info.URL
		cp.Title = info.Title
	}
	// Only keep the last screenshot to save memory
	imgB64, err := sess.Screenshot(false)
	if err == nil {
		cp.ScreenshotB64 = imgB64
	}
	// Cap checkpoints at 10, clear old screenshots to save memory
	const maxCheckpoints = 10
	state.Checkpoints = append(state.Checkpoints, cp)
	if len(state.Checkpoints) > maxCheckpoints {
		// Remove oldest, but first clear its screenshot
		state.Checkpoints = state.Checkpoints[len(state.Checkpoints)-maxCheckpoints:]
	}
	// Only keep screenshot on the most recent checkpoint
	for i := 0; i < len(state.Checkpoints)-1; i++ {
		state.Checkpoints[i].ScreenshotB64 = ""
	}
}

func (s *BrowserTaskSupervisor) capturePageSnapshot() *PageSnapshot {
	sess, err := s.sessionFn()
	if err != nil {
		return nil
	}
	info, _ := sess.Info()
	ps := &PageSnapshot{}
	if info != nil {
		ps.URL = info.URL
		ps.Title = info.Title
	}
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

func (s *BrowserTaskSupervisor) log(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger(fmt.Sprintf(format, args...))
	}
}
