package guiautomation

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// GUITaskSupervisor manages GUI task execution with retry, pause/resume/cancel.
// It delegates to taskengine.Supervisor for the execution loop, injecting
// a GUIStepExecutor (step execution) and GUIStateObserver (state observation).
//
// This ensures that all execution-loop improvements (per-step verification,
// SuccessCriteria checking, checkpoint recording, retry logic) are shared
// with the browser automation path — no parallel implementation.
type GUITaskSupervisor struct {
	locator    *ElementLocator
	input      InputSimulator
	classifier taskengine.RetryClassifier
	observer   *GUIStateObserver
	logger     func(string)

	// engine is lazily constructed on first Execute call.
	engine *taskengine.Supervisor

	// activeTaskID tracks the currently executing task for CancelAll support.
	// Only one GUI task runs at a time (Execute blocks), so a single ID suffices.
	activeTaskID string
}

// NewGUITaskSupervisor creates a supervisor. The observer is optional —
// if nil, SuccessCriteria verification and per-step Verify are skipped
// (taskengine.Supervisor handles this gracefully).
func NewGUITaskSupervisor(
	locator *ElementLocator,
	input InputSimulator,
	screenshotFn func() (string, error),
	ocr taskengine.OCRProvider,
	retrier *GUIRetryStrategy,
	logger func(string),
) *GUITaskSupervisor {
	var classifier taskengine.RetryClassifier
	if retrier != nil {
		classifier = &guiRetryClassifierAdapter{retrier: retrier}
	}

	return &GUITaskSupervisor{
		locator:    locator,
		input:      input,
		classifier: classifier,
		logger:     logger,
	}
}

// SetObserver injects a GUIStateObserver for SuccessCriteria verification
// and per-step Verify. Must be called before Execute if verification is desired.
func (s *GUITaskSupervisor) SetObserver(obs *GUIStateObserver) {
	s.observer = obs
}

// getEngine returns the lazily-constructed taskengine.Supervisor.
// Built on first call with whatever observer/classifier are set at that point.
func (s *GUITaskSupervisor) getEngine() *taskengine.Supervisor {
	if s.engine == nil {
		s.engine = taskengine.NewSupervisor(taskengine.SupervisorConfig{
			Executor: &guiStepExecutor{
				locator: s.locator,
				input:   s.input,
			},
			Observer:   s.observer,
			Classifier: s.classifier,
			Logger:     s.logger,
		})
	}
	return s.engine
}

// Execute runs a GUI task by converting GUITaskSpec to taskengine.TaskSpec
// and delegating to the unified engine.
func (s *GUITaskSupervisor) Execute(spec GUITaskSpec) (*GUITaskState, error) {
	teSpec := convertGUITaskSpec(spec)
	s.activeTaskID = teSpec.ID

	teState, err := s.getEngine().Execute(teSpec)

	s.activeTaskID = ""
	return convertTaskState(teState, spec), err
}

// CancelAll cancels the currently running task (if any).
func (s *GUITaskSupervisor) CancelAll() {
	if id := s.activeTaskID; id != "" {
		_ = s.getEngine().Cancel(id)
	}
}

// Pause requests a running task to pause after the current step.
func (s *GUITaskSupervisor) Pause(taskID string) error {
	return s.getEngine().Pause(taskID)
}

// Resume resumes a paused task.
func (s *GUITaskSupervisor) Resume(taskID string) error {
	return s.getEngine().Resume(taskID)
}

// Cancel cancels a running or paused task.
func (s *GUITaskSupervisor) Cancel(taskID string) error {
	return s.getEngine().Cancel(taskID)
}

// GetState returns the current state of a task.
func (s *GUITaskSupervisor) GetState(taskID string) (*GUITaskState, bool) {
	teState, ok := s.getEngine().GetState(taskID)
	if !ok {
		return nil, false
	}
	return &GUITaskState{
		ID:          teState.ID,
		Status:      string(teState.Status),
		TotalSteps:  teState.TotalSteps,
		CurrentStep: teState.CurrentStep,
		RetryCount:  teState.RetryCount,
		LastError:   teState.LastError,
		StartedAt:   teState.StartedAt,
	}, true
}

// ── GUIStepExecutor: adapts GUIStepSpec execution to taskengine.StepExecutor ──

// guiStepExecutor implements taskengine.StepExecutor for desktop GUI automation.
type guiStepExecutor struct {
	locator *ElementLocator
	input   InputSimulator
}

func (e *guiStepExecutor) Execute(ctx context.Context, step taskengine.StepSpec) error {
	// Recover the original GUIStepSpec from Extra if available.
	origStep := recoverOrigStep(step)

	// Resolve coordinates via locator or params.
	var x, y int
	if origStep != nil && e.locator != nil {
		result, err := e.locator.Locate(*origStep)
		if err != nil {
			return fmt.Errorf("element not found: %w", err)
		}
		x, y = result.X, result.Y
	} else {
		fmt.Sscanf(step.Params["x"], "%d", &x)
		fmt.Sscanf(step.Params["y"], "%d", &y)
	}

	switch step.Action {
	case "click":
		return e.input.Click(x, y)
	case "right_click":
		return e.input.RightClick(x, y)
	case "double_click":
		return e.input.DoubleClick(x, y)
	case "type":
		text := step.Params["text"]
		if x > 0 || y > 0 {
			if err := e.input.Click(x, y); err != nil {
				return err
			}
		}
		return e.input.Type(text)
	case "keypress":
		keys := step.Params["keys"]
		if keys == "" {
			return fmt.Errorf("keypress: missing keys param")
		}
		return e.input.KeyCombo(splitKeys(keys)...)
	case "scroll":
		dy := 0
		fmt.Sscanf(step.Params["scroll_dy"], "%d", &dy)
		return e.input.Scroll(x, y, 0, dy)
	case "drag":
		toX, toY := 0, 0
		fmt.Sscanf(step.Params["drag_to_x"], "%d", &toX)
		fmt.Sscanf(step.Params["drag_to_y"], "%d", &toY)
		return e.input.DragDrop(x, y, toX, toY)
	default:
		return fmt.Errorf("unknown GUI action: %s", step.Action)
	}
}

// ── guiRetryClassifierAdapter: adapts GUIRetryStrategy to taskengine.RetryClassifier ──

type guiRetryClassifierAdapter struct {
	retrier *GUIRetryStrategy
}

func (a *guiRetryClassifierAdapter) Classify(err error, step taskengine.StepSpec) taskengine.FailureType {
	ft := a.retrier.ClassifyFailure(err)
	switch ft {
	case GUIFailureElementNotFound:
		return taskengine.FailureElementNotFound
	case GUIFailureTimeout:
		return taskengine.FailureTimeout
	default:
		return taskengine.FailureUnknown
	}
}

func (a *guiRetryClassifierAdapter) Decide(failure taskengine.FailureType, step taskengine.StepSpec, retryCount int, snapshot *taskengine.StateSnapshot) *taskengine.RetryDecision {
	// Delegate to GUIRetryStrategy with a simplified interface.
	// Convert taskengine types back to GUI types for the legacy retrier.
	guiStep := GUIStepSpec{
		Action: step.Action,
		Params: step.Params,
	}
	guiFailType := GUIFailureUnknown
	switch failure {
	case taskengine.FailureElementNotFound:
		guiFailType = GUIFailureElementNotFound
	case taskengine.FailureTimeout:
		guiFailType = GUIFailureTimeout
	}

	screenshotB64 := ""
	ocrText := ""
	if snapshot != nil {
		screenshotB64 = snapshot.ScreenshotB64
		ocrText = snapshot.OCRText
	}

	decision := a.retrier.Decide(guiFailType, guiStep, retryCount, screenshotB64, ocrText)
	return &taskengine.RetryDecision{
		ShouldRetry:     decision.ShouldRetry,
		WaitBefore:      decision.WaitBefore,
		AdjustedTimeout: decision.AdjustedTimeout,
		Reason:          decision.Reason,
	}
}

// ── Conversion helpers ──

// convertGUITaskSpec converts GUITaskSpec to taskengine.TaskSpec.
func convertGUITaskSpec(spec GUITaskSpec) taskengine.TaskSpec {
	steps := make([]taskengine.StepSpec, len(spec.Steps))
	for i, gs := range spec.Steps {
		steps[i] = convertGUIStepSpec(gs)
	}

	criteria := make([]taskengine.CriterionSpec, len(spec.SuccessCriteria))
	for i, c := range spec.SuccessCriteria {
		criteria[i] = taskengine.CriterionSpec{
			Type:    c.Type,
			Pattern: c.Pattern,
			Window:  c.Window,
		}
	}

	return taskengine.TaskSpec{
		ID:              spec.ID,
		Description:     spec.Description,
		Steps:           steps,
		SuccessCriteria: criteria,
		MaxRetries:      spec.MaxRetries,
		StepTimeout:     spec.StepTimeout,
	}
}

// convertGUIStepSpec converts a single GUIStepSpec to taskengine.StepSpec.
func convertGUIStepSpec(gs GUIStepSpec) taskengine.StepSpec {
	ts := taskengine.StepSpec{
		Action:  gs.Action,
		Params:  gs.Params,
		Timeout: gs.Timeout,
	}
	// Stash the original GUIRecordedStep in Extra for the executor to recover.
	if gs.OrigStep != nil {
		if ts.Extra == nil {
			ts.Extra = make(map[string]interface{})
		}
		ts.Extra["gui_orig_step"] = gs.OrigStep
	}
	return ts
}

// recoverOrigStep extracts the stashed GUIRecordedStep from a taskengine.StepSpec.
func recoverOrigStep(step taskengine.StepSpec) *GUIRecordedStep {
	if step.Extra == nil {
		return nil
	}
	if orig, ok := step.Extra["gui_orig_step"].(*GUIRecordedStep); ok {
		return orig
	}
	return nil
}

// convertTaskState converts taskengine.TaskState to GUITaskState.
func convertTaskState(ts *taskengine.TaskState, spec GUITaskSpec) *GUITaskState {
	if ts == nil {
		return &GUITaskState{Status: "failed"}
	}
	gs := &GUITaskState{
		ID:          ts.ID,
		Status:      string(ts.Status),
		TotalSteps:  ts.TotalSteps,
		CurrentStep: ts.CurrentStep,
		RetryCount:  ts.RetryCount,
		LastError:   ts.LastError,
		StartedAt:   ts.StartedAt,
	}
	// Convert checkpoints.
	for _, cp := range ts.Checkpoints {
		gs.Checkpoints = append(gs.Checkpoints, GUICheckpoint{
			StepIndex:     cp.StepIndex,
			Timestamp:     cp.Timestamp,
			ScreenshotB64: cp.ScreenshotB64,
		})
	}
	return gs
}

// splitKeys splits a comma-separated key string.
func splitKeys(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
