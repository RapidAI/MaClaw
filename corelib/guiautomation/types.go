package guiautomation

import "time"

// GUIRecordedFlow represents a complete recorded GUI operation flow.
type GUIRecordedFlow struct {
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	RecordedAt      time.Time          `json:"recorded_at"`
	TargetApp       string             `json:"target_app"`
	Steps           []GUIRecordedStep  `json:"steps"`
	SuccessCriteria []GUICriterionSpec `json:"success_criteria,omitempty"`
}

// GUIRecordedStep represents a single recorded GUI operation step with
// three layers of locator information captured simultaneously.
type GUIRecordedStep struct {
	Action      string        `json:"action"` // click, type, scroll, drag, keypress
	WindowTitle string        `json:"window_title"`
	Timestamp   time.Duration `json:"timestamp"`

	// Three-tier locator info (all recorded simultaneously)
	AccessibilityID *AccessibilityRef `json:"accessibility,omitempty"` // tier 1: control identity
	SnapshotRef     string            `json:"snapshot_ref,omitempty"`  // tier 2: screenshot file path
	Coords          [2]int            `json:"coords"`                  // tier 3: screen coordinates

	// Operation parameters
	Text     string   `json:"text,omitempty"`
	Keys     []string `json:"keys,omitempty"`
	DragTo   [2]int   `json:"drag_to,omitempty"`
	ScrollDY int      `json:"scroll_dy,omitempty"`
}

// AccessibilityRef identifies a UI control via accessibility properties.
type AccessibilityRef struct {
	Role  string `json:"role"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// GUICriterionSpec defines a success criterion for a GUI task.
type GUICriterionSpec struct {
	Type    string `json:"type"` // ocr_contains, window_exists, element_exists
	Pattern string `json:"pattern"`
	Window  string `json:"window,omitempty"`
}

// GUITaskSpec describes a GUI task to be executed by the supervisor.
type GUITaskSpec struct {
	ID              string             `json:"id"`
	Description     string             `json:"description"`
	Steps           []GUIStepSpec      `json:"steps"`
	SuccessCriteria []GUICriterionSpec `json:"success_criteria,omitempty"`
	MaxRetries      int                `json:"max_retries"`
	StepTimeout     time.Duration      `json:"step_timeout"`
}

// GUIStepSpec describes a single step within a GUI task.
type GUIStepSpec struct {
	Action   string            `json:"action"`
	Params   map[string]string `json:"params"`
	OrigStep *GUIRecordedStep  `json:"orig_step,omitempty"`
	Timeout  time.Duration     `json:"timeout,omitempty"`
}

// GUITaskState tracks the execution state of a GUI task.
type GUITaskState struct {
	ID          string          `json:"id"`
	Status      GUITaskStatus   `json:"status"` // running, completed, failed, paused, cancelled
	TotalSteps  int             `json:"total_steps"`
	CurrentStep int             `json:"current_step"`
	RetryCount  int             `json:"retry_count"`
	LastError   string          `json:"last_error,omitempty"`
	StartedAt   time.Time       `json:"started_at"`
	Checkpoints []GUICheckpoint `json:"checkpoints,omitempty"`
}

// GUICheckpoint records the state after executing a step.
type GUICheckpoint struct {
	StepIndex     int       `json:"step_index"`
	Timestamp     time.Time `json:"timestamp"`
	WindowTitle   string    `json:"window_title"`
	ScreenshotB64 string    `json:"screenshot_b64,omitempty"`
	Strategy      string    `json:"strategy"`
}
