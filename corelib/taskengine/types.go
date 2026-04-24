// Package taskengine provides a unified execution contract for GUI automation
// tasks. Both browser (CDP) and desktop (accessibility + input simulation)
// automation share this contract, eliminating parallel implementations of
// the same execution-retry-verify loop.
//
// Architecture:
//
//	taskengine.Supervisor  (generic execution loop)
//	    ├── StepExecutor   (injected: browser or desktop step execution)
//	    └── StateObserver  (injected: browser or desktop state observation)
//
// Adding a new automation target (e.g. mobile) requires only implementing
// StepExecutor + StateObserver. The execution loop, retry logic, verification,
// checkpoint recording, and pause/resume/cancel are provided by Supervisor.
package taskengine

import "time"

// ── Task Specification ──

// TaskSpec defines an automation task as an ordered sequence of steps
// with optional success criteria verified after all steps complete.
type TaskSpec struct {
	ID              string        `json:"id"`
	Description     string        `json:"description"`
	Steps           []StepSpec    `json:"steps"`
	SuccessCriteria []CriterionSpec `json:"success_criteria,omitempty"`
	MaxRetries      int           `json:"max_retries"`  // per-step max retries; default 3
	StepTimeout     time.Duration `json:"step_timeout"` // default per-step timeout; default 30s
}

// StepSpec defines a single automation step. The Action and Params are
// interpreted by the injected StepExecutor — the engine itself is agnostic
// to what actions exist.
type StepSpec struct {
	Action  string            `json:"action"`
	Params  map[string]string `json:"params"`
	Verify  *CriterionSpec    `json:"verify,omitempty"`  // optional per-step verification
	Timeout time.Duration     `json:"timeout,omitempty"` // overrides TaskSpec.StepTimeout

	// Platform-specific extensions (opaque to the engine).
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// ── Verification ──

// CriterionSpec defines a success criterion. The Type determines which
// StateObserver method handles verification. Platform-specific fields
// (Selector, Window, TabID, etc.) are only used by the corresponding observer.
type CriterionSpec struct {
	Type    string `json:"type"`    // e.g. text_contains, element_exists, url_contains, window_exists, screenshot_match
	Pattern string `json:"pattern"` // match pattern (text substring, regex, URL fragment, etc.)

	// Targeting — observer uses whichever fields are relevant.
	Selector string `json:"selector,omitempty"` // CSS selector (browser) or "role::name" (desktop)
	Window   string `json:"window,omitempty"`   // window title substring (desktop)
	TabID    string `json:"tab_id,omitempty"`   // browser tab
	FrameID  string `json:"frame_id,omitempty"` // browser frame
	Timeout  int    `json:"timeout,omitempty"`  // wait timeout in seconds (default 5)
}

// VerifyResult holds the outcome of verifying a set of criteria.
type VerifyResult struct {
	Passed  bool              `json:"passed"`
	Details []CriterionResult `json:"details"`
}

// CriterionResult holds the outcome of a single criterion check.
type CriterionResult struct {
	Criterion CriterionSpec `json:"criterion"`
	Passed    bool          `json:"passed"`
	Actual    string        `json:"actual,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// ── Task State ──

// TaskStatus represents the lifecycle state of a task execution.
type TaskStatus string

const (
	StatusRunning   TaskStatus = "running"
	StatusPaused    TaskStatus = "paused"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
	StatusCancelled TaskStatus = "cancelled"
)

// TaskState holds the runtime state of a task execution.
type TaskState struct {
	ID          string       `json:"id"`
	Status      TaskStatus   `json:"status"`
	CurrentStep int          `json:"current_step"`
	TotalSteps  int          `json:"total_steps"`
	RetryCount  int          `json:"retry_count"`
	LastError   string       `json:"last_error,omitempty"`
	Checkpoints []Checkpoint `json:"checkpoints,omitempty"`
	StepTraces  []StepTrace  `json:"step_traces,omitempty"`
	StartedAt   time.Time    `json:"started_at"`
}

// StepTrace captures a structured execution record for one step.
type StepTrace struct {
	StepIndex   int       `json:"step_index"`
	Action      string    `json:"action"`
	Summary     string    `json:"summary,omitempty"`
	RetryReason string    `json:"retry_reason,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`

	// Platform-specific trace data (opaque to the engine).
	Extra map[string]string `json:"extra,omitempty"`
}

// Checkpoint is a snapshot taken after each step execution.
type Checkpoint struct {
	StepIndex     int       `json:"step_index"`
	ScreenshotB64 string    `json:"screenshot_b64,omitempty"`
	Timestamp     time.Time `json:"timestamp"`

	// Platform-specific checkpoint data (opaque to the engine).
	Extra map[string]string `json:"extra,omitempty"`
}

// ── Retry ──

// FailureType categorizes why a step failed. The engine defines common
// types; platform-specific executors may return any of these.
type FailureType int

const (
	FailureElementNotFound    FailureType = iota // target element not found
	FailureTimeout                               // step exceeded timeout
	FailureStateChanged                          // unexpected state change (page navigated, window closed, etc.)
	FailureUnknownState                          // cannot determine current state
	FailureVerificationFailed                    // per-step verification failed
	FailureUnknown                               // unclassified
)

// String returns a human-readable label.
func (f FailureType) String() string {
	switch f {
	case FailureElementNotFound:
		return "element_not_found"
	case FailureTimeout:
		return "timeout"
	case FailureStateChanged:
		return "state_changed"
	case FailureUnknownState:
		return "unknown_state"
	case FailureVerificationFailed:
		return "verification_failed"
	default:
		return "unknown"
	}
}

// RetryDecision describes how to handle a failed step.
type RetryDecision struct {
	ShouldRetry     bool          `json:"should_retry"`
	WaitBefore      time.Duration `json:"wait_before"`
	AdjustedTimeout time.Duration `json:"adjusted_timeout,omitempty"` // 0 = keep original
	Reason          string        `json:"reason"`
	NeedsLLM        bool          `json:"needs_llm"`
	LLMContext      string        `json:"llm_context,omitempty"`
}

// ── OCR ──

// OCRProvider abstracts OCR text recognition. This is a minimal interface
// that both browser.OCRProvider and any future OCR implementation satisfy.
// Defined here so that StateObserver implementations don't need to import
// platform-specific packages for OCR.
type OCRProvider interface {
	Recognize(pngBase64 string) ([]OCRResult, error)
	IsAvailable() bool
}

// OCRResult represents a single text region recognized by OCR.
type OCRResult struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	BBox       [4]int  `json:"bbox"` // x, y, width, height
}

// ── State Snapshot ──

// StateSnapshot captures the current state for retry decisions and logging.
// Platform-specific observers populate whichever fields are relevant.
type StateSnapshot struct {
	ScreenshotB64  string            `json:"screenshot_b64,omitempty"`
	OCRText        string            `json:"ocr_text,omitempty"`
	UIElements     []UIElement       `json:"ui_elements,omitempty"`
	WindowTitle    string            `json:"window_title,omitempty"`
	URL            string            `json:"url,omitempty"`
	Title          string            `json:"title,omitempty"`
	FocusedElement map[string]string `json:"focused_element,omitempty"` // role, name, value
	Extra          map[string]string `json:"extra,omitempty"`
}

// UIElement is a platform-agnostic representation of a UI element detected
// on screen. Both accessibility.Bridge and vision models (OmniParser YOLO)
// produce UIElements through the ScreenParser interface.
type UIElement struct {
	Type         string  `json:"type"`          // button, edit, text, icon, menu, checkbox, etc.
	Name         string  `json:"name"`          // human-readable label or functional description
	Value        string  `json:"value"`         // current value (accessibility only; empty for vision)
	BBox         [4]int  `json:"bbox"`          // x, y, width, height in screen coordinates
	Interactable bool    `json:"interactable"`  // whether the element can be clicked/typed into
	Confidence   float64 `json:"confidence"`    // 1.0 for accessibility, model confidence for vision
	Source       string  `json:"source"`        // "accessibility", "yolo", "ocr"
}
