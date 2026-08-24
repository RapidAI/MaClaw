package browser

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ── Task Spec ──

// TaskSpec defines a browser automation task.
type TaskSpec struct {
	ID              string          `json:"id"`
	Description     string          `json:"description"`
	Steps           []StepSpec      `json:"steps"`
	SuccessCriteria []CriterionSpec `json:"success_criteria"`
	MaxRetries      int             `json:"max_retries"`  // default 3
	StepTimeout     time.Duration   `json:"step_timeout"` // default 30s
}

// StepTargetSpec identifies the intended browsing context for a step.
type StepTargetSpec struct {
	TabID   string `json:"tab_id,omitempty"`
	FrameID string `json:"frame_id,omitempty"`
}

// LocatorSpec provides a structured fallback locator.
type LocatorSpec struct {
	Selector string `json:"selector,omitempty"`
	Role     string `json:"role,omitempty"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text,omitempty"`
	Ref      string `json:"ref,omitempty"`
}

// StepTrace captures a structured execution record for one step.
type StepTrace struct {
	StepIndex   int       `json:"step_index"`
	Action      string    `json:"action"`
	TabID       string    `json:"tab_id,omitempty"`
	FrameID     string    `json:"frame_id,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	RetryReason string    `json:"retry_reason,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
}

// StepSpec defines a single browser operation step.
type StepSpec struct {
	Action    string            `json:"action"`            // navigate, click, type, wait, scroll, select
	Params    map[string]string `json:"params"`            // action-specific: url, ref, selector, text, value, delta_y
	Verify    *CriterionSpec    `json:"verify,omitempty"`  // optional per-step verification
	Timeout   time.Duration     `json:"timeout,omitempty"` // overrides TaskSpec.StepTimeout
	Target    *StepTargetSpec   `json:"target,omitempty"`
	Fallbacks []LocatorSpec     `json:"fallbacks,omitempty"`
}

// CriterionSpec defines a success criterion for verification.
type CriterionSpec struct {
	Type     string `json:"type"`               // dom_exists, dom_text, url_contains, url_matches, ax_exists, ax_name, ax_role, frame_url_contains, network_request, console_no_error
	Selector string `json:"selector,omitempty"` // CSS selector (for dom_*)
	Pattern  string `json:"pattern"`            // match pattern (text/regex/URL)
	TabID    string `json:"tab_id,omitempty"`
	FrameID  string `json:"frame_id,omitempty"`
}

// ── Task State ──

// TaskStatus represents the current status of a browser task.
type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// TaskState holds the runtime state of a browser task execution.
type TaskState struct {
	ID               string                `json:"id"`
	Status           TaskStatus            `json:"status"`
	CurrentStep      int                   `json:"current_step"`
	TotalSteps       int                   `json:"total_steps"`
	RetryCount       int                   `json:"retry_count"`
	LastError        string                `json:"last_error,omitempty"`
	Checkpoints      []Checkpoint          `json:"checkpoints,omitempty"`
	StepTraces       []StepTrace           `json:"step_traces,omitempty"`
	AskUser          *agent.AskUserRequest `json:"ask_user,omitempty"`
	LastResultStatus string                `json:"last_result_status,omitempty"`
	StartedAt        time.Time             `json:"started_at"`
}

// Checkpoint is a snapshot taken after each step execution.
type Checkpoint struct {
	StepIndex     int       `json:"step_index"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	TabID         string    `json:"tab_id,omitempty"`
	FrameID       string    `json:"frame_id,omitempty"`
	ScreenshotB64 string    `json:"screenshot_b64,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// ── Verification ──

// VerifyResult holds the outcome of success criteria verification.
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

// ── Retry ──

// FailureType categorizes why a browser step failed.
type FailureType int

const (
	FailureElementNotFound    FailureType = iota // CSS selector matched nothing
	FailureTimeout                               // step exceeded timeout
	FailurePageChanged                           // URL changed unexpectedly
	FailureUnknownState                          // cannot determine page state
	FailureVerificationFailed                    // post-step verification failed
	FailureWrongTab                              // target tab not active or mismatched
	FailureWrongFrame                            // target frame not active or mismatched
	FailurePopupNotCaptured                      // expected popup/tab did not appear
	FailureConsoleError                          // JS console reported an error
	FailureNetworkBlocked                        // request blocked or failed
)

// String returns a human-readable label.
func (f FailureType) String() string {
	switch f {
	case FailureElementNotFound:
		return "element_not_found"
	case FailureTimeout:
		return "timeout"
	case FailurePageChanged:
		return "page_changed"
	case FailureUnknownState:
		return "unknown_state"
	case FailureVerificationFailed:
		return "verification_failed"
	case FailureWrongTab:
		return "wrong_tab"
	case FailureWrongFrame:
		return "wrong_frame"
	case FailurePopupNotCaptured:
		return "popup_not_captured"
	case FailureConsoleError:
		return "console_error"
	case FailureNetworkBlocked:
		return "network_blocked"
	default:
		return "unknown"
	}
}

// RetryDecision describes what to do after a step failure.
type RetryDecision struct {
	ShouldRetry  bool          `json:"should_retry"`
	AdjustedStep *StepSpec     `json:"adjusted_step,omitempty"` // nil = retry original
	WaitBefore   time.Duration `json:"wait_before"`
	Reason       string        `json:"reason"`
	NeedsLLM     bool          `json:"needs_llm"`             // LLM should decide next action
	LLMContext   string        `json:"llm_context,omitempty"` // context for LLM
}

// ── Page Snapshot (used by retry strategy) ──

// PageSnapshot captures the current browser page state for analysis.
type PageSnapshot struct {
	URL           string      `json:"url"`
	Title         string      `json:"title"`
	TabID         string      `json:"tab_id,omitempty"`
	FrameID       string      `json:"frame_id,omitempty"`
	OCRText       []OCRResult `json:"ocr_text,omitempty"`
	DOMSnippet    string      `json:"dom_snippet,omitempty"` // truncated HTML
	NetworkEvents []string    `json:"network_events,omitempty"`
	ConsoleEvents []string    `json:"console_events,omitempty"`
}

// ── OCR ──

// OCRResult represents a single text region recognized by OCR.
type OCRResult struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	BBox       [4]int  `json:"bbox"` // x, y, width, height
}

// OCRProvider abstracts OCR recognition (native PP-OCRv6 engine or LLM Vision).
type OCRProvider interface {
	Recognize(pngBase64 string) ([]OCRResult, error)
	IsAvailable() bool
	Close()
}

// FormatOCRForLLM formats OCR results into a text description for LLM consumption.
func FormatOCRForLLM(results []OCRResult) string {
	if len(results) == 0 {
		return "（未识别到文本）"
	}
	var lines []string
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("[%d,%d,%d,%d] %q (置信度: %.2f)",
			r.BBox[0], r.BBox[1], r.BBox[2], r.BBox[3], r.Text, r.Confidence))
	}
	return fmt.Sprintf("页面 OCR 识别结果（共 %d 个文本区域）:\n%s",
		len(results), strings.Join(lines, "\n"))
}
