package taskengine

import (
	"context"
	"time"
)

// StepExecutor executes a single automation step. Implementations are
// platform-specific (browser CDP, desktop input simulation, etc.).
// The engine calls Execute for each step and interprets errors via
// the RetryClassifier.
type StepExecutor interface {
	// Execute performs the step action. Returns nil on success.
	// The context carries the step-level timeout.
	Execute(ctx context.Context, step StepSpec) error
}

// StateObserver provides structured state observation and verification.
// Implementations are platform-specific.
type StateObserver interface {
	// Snapshot captures the current state for retry decisions and logging.
	Snapshot() (*StateSnapshot, error)

	// Verify checks a set of criteria against the current state.
	Verify(criteria []CriterionSpec) (*VerifyResult, error)

	// WaitForStable blocks until the target is in a stable state
	// (browser: no pending network; desktop: no visual changes).
	// Returns nil if stable within timeout, error otherwise.
	WaitForStable(timeout time.Duration) error

	// TakeCheckpoint captures a checkpoint after a step completes.
	// The returned Checkpoint is appended to TaskState.Checkpoints.
	TakeCheckpoint(stepIndex int) Checkpoint
}

// RetryClassifier classifies step errors and decides retry strategy.
// Implementations can be platform-specific (browser has more failure types
// than desktop) or shared.
type RetryClassifier interface {
	// Classify infers a FailureType from a step execution error.
	Classify(err error, step StepSpec) FailureType

	// Decide returns a retry decision based on failure type, step context,
	// retry count, and current state snapshot.
	Decide(failure FailureType, step StepSpec, retryCount int, snapshot *StateSnapshot) *RetryDecision
}

// ProgressCallback is called after each step completes (or fails).
// Implementations can update UI, activity stores, etc.
type ProgressCallback func(taskID, message string, currentStep, totalSteps int)

// ScreenParser converts a screenshot into structured UI elements.
// Implementations: YOLO-based detector, accessibility bridge adapter, OCR adapter.
// CompositeScreenParser merges results from multiple parsers.
type ScreenParser interface {
	// Parse takes a base64-encoded PNG screenshot and returns detected UI elements.
	Parse(pngBase64 string) ([]UIElement, error)
	// IsAvailable returns true if the parser is ready to use.
	IsAvailable() bool
}
