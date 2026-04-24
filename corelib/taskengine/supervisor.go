package taskengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// taskEntry wraps TaskState with control channels for pause/resume/cancel.
type taskEntry struct {
	state   *TaskState
	cancel  context.CancelFunc
	pauseC  chan struct{}
	resumeC chan struct{}
}

// Supervisor is the unified task execution engine. It orchestrates
// step execution, per-step verification, retry logic, checkpointing,
// and final success criteria verification.
//
// Platform-specific behavior is injected via StepExecutor, StateObserver,
// and RetryClassifier. The Supervisor itself contains zero platform-specific code.
type Supervisor struct {
	mu        sync.RWMutex
	tasks     map[string]*taskEntry
	idCounter int

	executor   StepExecutor
	observer   StateObserver
	classifier RetryClassifier
	onProgress ProgressCallback
	logger     func(string)
}

// SupervisorConfig holds the dependencies for creating a Supervisor.
type SupervisorConfig struct {
	Executor   StepExecutor
	Observer   StateObserver
	Classifier RetryClassifier
	OnProgress ProgressCallback
	Logger     func(string)
}

// NewSupervisor creates a Supervisor with the given platform-specific
// dependencies. All fields in config are optional except Executor.
func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	s := &Supervisor{
		tasks:      make(map[string]*taskEntry),
		executor:   cfg.Executor,
		observer:   cfg.Observer,
		classifier: cfg.Classifier,
		onProgress: cfg.OnProgress,
		logger:     cfg.Logger,
	}
	if s.classifier == nil {
		s.classifier = &defaultClassifier{}
	}
	return s
}

// Execute runs a task to completion. It blocks until all steps finish,
// the task is cancelled, or a step fails beyond retry limits.
func (s *Supervisor) Execute(spec TaskSpec) (*TaskState, error) {
	if spec.MaxRetries <= 0 {
		spec.MaxRetries = 3
	}
	if spec.StepTimeout <= 0 {
		spec.StepTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register task
	s.mu.Lock()
	s.idCounter++
	if spec.ID == "" {
		spec.ID = fmt.Sprintf("task-%d", s.idCounter)
	}
	state := &TaskState{
		ID:         spec.ID,
		Status:     StatusRunning,
		TotalSteps: len(spec.Steps),
		StartedAt:  time.Now(),
	}
	entry := &taskEntry{
		state:   state,
		cancel:  cancel,
		pauseC:  make(chan struct{}, 1),
		resumeC: make(chan struct{}, 1),
	}
	s.tasks[spec.ID] = entry
	s.mu.Unlock()

	s.log("task %s started: %s (%d steps)", spec.ID, spec.Description, len(spec.Steps))
	s.progress(spec.ID, "started", 0, len(spec.Steps))

	// Execute steps
	for i, step := range spec.Steps {
		if err := ctx.Err(); err != nil {
			state.Status = StatusCancelled
			state.LastError = "cancelled by user"
			s.removeTask(spec.ID)
			return state, fmt.Errorf("cancelled")
		}

		state.CurrentStep = i + 1
		trace := StepTrace{StepIndex: i, Action: step.Action, StartedAt: time.Now()}
		state.StepTraces = append(state.StepTraces, trace)
		s.progress(spec.ID, fmt.Sprintf("step %d/%d: %s", i+1, len(spec.Steps), step.Action), i+1, len(spec.Steps))

		err := s.executeStepWithRetry(ctx, spec, step, i, state)
		if err != nil {
			// Distinguish cancellation from step failure
			if ctx.Err() != nil {
				state.Status = StatusCancelled
				state.LastError = "cancelled by user"
			} else {
				state.Status = StatusFailed
				state.LastError = err.Error()
			}
			s.updateLastTrace(state, err.Error())
			s.log("task %s failed at step %d: %v", spec.ID, i+1, err)
			s.progress(spec.ID, fmt.Sprintf("failed at step %d: %v", i+1, err), i+1, len(spec.Steps))
			s.removeTask(spec.ID)
			return state, err
		}
		s.updateLastTrace(state, "ok")

		// Checkpoint
		if s.observer != nil {
			cp := s.observer.TakeCheckpoint(i)
			s.appendCheckpoint(state, cp)
		}

		// Pause/resume
		if err := s.handlePause(ctx, spec.ID, state, i); err != nil {
			return state, err
		}
	}

	// Final success criteria verification
	if len(spec.SuccessCriteria) > 0 && s.observer != nil {
		_ = s.observer.WaitForStable(3 * time.Second)

		result, err := s.observer.Verify(spec.SuccessCriteria)
		if err != nil {
			state.Status = StatusFailed
			state.LastError = fmt.Sprintf("verification error: %v", err)
			s.removeTask(spec.ID)
			return state, err
		}
		if !result.Passed {
			state.Status = StatusFailed
			details, _ := json.Marshal(result.Details)
			state.LastError = fmt.Sprintf("success criteria not met: %s", string(details))
			s.log("task %s verification failed: %s", spec.ID, state.LastError)
			s.removeTask(spec.ID)
			return state, fmt.Errorf("%s", state.LastError)
		}
	}

	state.Status = StatusCompleted
	s.log("task %s completed successfully", spec.ID)
	s.progress(spec.ID, "completed", len(spec.Steps), len(spec.Steps))
	s.removeTask(spec.ID)
	return state, nil
}

// GetState returns the current state of a task.
func (s *Supervisor) GetState(taskID string) (*TaskState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return nil, false
	}
	return entry.state, true
}

// Cancel cancels a running or paused task.
func (s *Supervisor) Cancel(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	st := entry.state.Status
	if st != StatusRunning && st != StatusPaused {
		return fmt.Errorf("task %s is not running or paused (status=%s)", taskID, st)
	}
	entry.cancel()
	return nil
}

// Pause requests a running task to pause after the current step.
func (s *Supervisor) Pause(taskID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if entry.state.Status != StatusRunning {
		return fmt.Errorf("task %s is not running (status=%s)", taskID, entry.state.Status)
	}
	select {
	case entry.pauseC <- struct{}{}:
	default:
	}
	return nil
}

// Resume resumes a paused task.
func (s *Supervisor) Resume(taskID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if entry.state.Status != StatusPaused {
		return fmt.Errorf("task %s is not paused (status=%s)", taskID, entry.state.Status)
	}
	select {
	case entry.resumeC <- struct{}{}:
	default:
	}
	return nil
}

// ── internal ──

func (s *Supervisor) executeStepWithRetry(ctx context.Context, spec TaskSpec, step StepSpec, stepIdx int, state *TaskState) error {
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = spec.StepTimeout
	}

	currentTimeout := timeout
	for retry := 0; ; retry++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled")
		}

		// Execute step with timeout. The step context inherits the parent
		// cancel so that Supervisor.Cancel() propagates to the executor.
		stepCtx, stepCancel := context.WithTimeout(ctx, currentTimeout)
		execErr := s.executor.Execute(stepCtx, step)
		stepCancel()

		// Distinguish cancellation from timeout
		if execErr != nil && ctx.Err() != nil {
			return fmt.Errorf("cancelled")
		} else if execErr != nil && stepCtx.Err() != nil && execErr == context.DeadlineExceeded {
			execErr = fmt.Errorf("step timed out after %v", currentTimeout)
		}

		// Per-step verification (only if execution succeeded)
		if execErr == nil && step.Verify != nil && s.observer != nil {
			vr, verr := s.observer.Verify([]CriterionSpec{*step.Verify})
			if verr != nil {
				execErr = verr
			} else if !vr.Passed {
				detail := ""
				if len(vr.Details) > 0 {
					detail = vr.Details[0].Error
				}
				execErr = fmt.Errorf("step verification failed: %s", detail)
			}
		}

		if execErr == nil {
			return nil // success
		}

		// Classify failure and decide retry
		failType := s.classifier.Classify(execErr, step)

		var snapshot *StateSnapshot
		if s.observer != nil {
			snapshot, _ = s.observer.Snapshot()
		}

		decision := s.classifier.Decide(failType, step, retry, snapshot)
		if !decision.ShouldRetry || retry >= spec.MaxRetries {
			reason := decision.Reason
			if retry >= spec.MaxRetries && decision.ShouldRetry {
				reason = fmt.Sprintf("exceeded max retries (%d)", spec.MaxRetries)
			}
			return fmt.Errorf("step %d failed: %v (%s)", stepIdx+1, execErr, reason)
		}

		s.log("task step %d retry %d: %s", stepIdx+1, retry+1, decision.Reason)
		state.RetryCount++

		if decision.WaitBefore > 0 {
			select {
			case <-time.After(decision.WaitBefore):
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			}
		}
		if decision.AdjustedTimeout > 0 {
			currentTimeout = decision.AdjustedTimeout
		}
	}
}

func (s *Supervisor) handlePause(ctx context.Context, taskID string, state *TaskState, stepIdx int) error {
	s.mu.RLock()
	entry := s.tasks[taskID]
	s.mu.RUnlock()
	if entry == nil {
		return nil
	}
	select {
	case <-entry.pauseC:
		state.Status = StatusPaused
		s.log("task %s paused after step %d", taskID, stepIdx+1)
		select {
		case <-entry.resumeC:
			state.Status = StatusRunning
			s.log("task %s resumed", taskID)
		case <-ctx.Done():
			state.Status = StatusCancelled
			state.LastError = "cancelled while paused"
			return fmt.Errorf("cancelled while paused")
		}
	default:
	}
	return nil
}

func (s *Supervisor) removeTask(taskID string) {
	s.mu.Lock()
	delete(s.tasks, taskID)
	s.mu.Unlock()
}

func (s *Supervisor) updateLastTrace(state *TaskState, summary string) {
	if len(state.StepTraces) > 0 {
		state.StepTraces[len(state.StepTraces)-1].Summary = summary
		state.StepTraces[len(state.StepTraces)-1].EndedAt = time.Now()
	}
}

func (s *Supervisor) appendCheckpoint(state *TaskState, cp Checkpoint) {
	const maxCheckpoints = 20
	state.Checkpoints = append(state.Checkpoints, cp)
	if len(state.Checkpoints) > maxCheckpoints {
		state.Checkpoints = state.Checkpoints[len(state.Checkpoints)-maxCheckpoints:]
	}
	// Only keep screenshot on the most recent checkpoint
	for i := 0; i < len(state.Checkpoints)-1; i++ {
		state.Checkpoints[i].ScreenshotB64 = ""
	}
}

func (s *Supervisor) log(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger(fmt.Sprintf(format, args...))
	}
}

func (s *Supervisor) progress(taskID, message string, current, total int) {
	if s.onProgress != nil {
		s.onProgress(taskID, message, current, total)
	}
}

// ── default retry classifier ──

type defaultClassifier struct{}

func (d *defaultClassifier) Classify(err error, step StepSpec) FailureType {
	if err == nil {
		return FailureUnknown
	}
	msg := err.Error()
	switch {
	case contains(msg, "not found") || contains(msg, "no element"):
		return FailureElementNotFound
	case contains(msg, "timeout") || contains(msg, "timed out"):
		return FailureTimeout
	case contains(msg, "verification failed"):
		return FailureVerificationFailed
	default:
		return FailureUnknown
	}
}

func (d *defaultClassifier) Decide(failure FailureType, step StepSpec, retryCount int, snapshot *StateSnapshot) *RetryDecision {
	// Note: the default classifier uses a fixed max of 3 retries.
	// Platform-specific classifiers should use TaskSpec.MaxRetries
	// passed via their own constructor.
	const maxRetries = 3
	if retryCount >= maxRetries {
		return &RetryDecision{ShouldRetry: false, Reason: fmt.Sprintf("exceeded max retries (%d)", maxRetries)}
	}
	switch failure {
	case FailureElementNotFound:
		wait := time.Duration(3*(retryCount+1)) * time.Second
		return &RetryDecision{ShouldRetry: true, WaitBefore: wait, Reason: "element not found, waiting before retry"}
	case FailureTimeout:
		extended := step.Timeout * time.Duration(retryCount+2)
		if extended <= 0 {
			// step.Timeout is 0 (using TaskSpec default) — use a reasonable extension.
			extended = 60 * time.Second
		}
		return &RetryDecision{ShouldRetry: true, AdjustedTimeout: extended, Reason: "timeout, extending step timeout"}
	default:
		return &RetryDecision{ShouldRetry: true, WaitBefore: 2 * time.Second, Reason: "retrying after short wait"}
	}
}

func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
