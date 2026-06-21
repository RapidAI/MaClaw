// state.go defines the V2 native workflow state model (WorkflowState, Phase,
// ToolPolicy). This is the serializable runtime state used by StateMachine
// and Store. See doc.go for package overview.
package v2

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// WorkflowStatus represents the overall workflow lifecycle.
type WorkflowStatus string

const (
	StatusActive    WorkflowStatus = "active"
	StatusCompleted WorkflowStatus = "completed"
	StatusCancelled WorkflowStatus = "cancelled"
)

// PhaseStatus represents the status of a single phase.
type PhaseStatus string

const (
	PhasePending        PhaseStatus = "pending"
	PhaseRunning        PhaseStatus = "running"
	PhaseExecuting      PhaseStatus = "executing" // background execution in progress (TaskRunner)
	PhaseWaitingConfirm PhaseStatus = "waiting_confirm"
	PhaseCompleted      PhaseStatus = "completed"
	PhaseSkipped        PhaseStatus = "skipped"
)

// ToolPolicy controls which tools are available during a phase.
type ToolPolicy string

const (
	ToolPolicyNone          ToolPolicy = "none"           // no tool restrictions
	ToolPolicyDocOnly       ToolPolicy = "doc_only"       // read/search/memory only
	ToolPolicyPlanning      ToolPolicy = "planning"       // repository inspection for reviewable planning phases
	ToolPolicyFull          ToolPolicy = "full"           // all tools (execution phase)
	ToolPolicyOpsControlled ToolPolicy = "ops_controlled" // controlled operational execution tools
)

// Phase represents one stage of a workflow.
type Phase struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	NeedsConfirm bool          `json:"needs_confirm"`
	ToolPolicy   ToolPolicy    `json:"tool_policy"`
	ExecMode     PhaseExecMode `json:"exec_mode,omitempty"`
	Status       PhaseStatus   `json:"status"`
	Output       string        `json:"output,omitempty"`

	// InputSchema is copied from the template at workflow creation time.
	// When non-nil, the phase requires form input before the agent loop runs.
	InputSchema *PhaseInputSchema `json:"input_schema,omitempty"`

	// FormData stores the user's form submission. Populated when the user
	// submits the InputSchema form via SubmitForm. Nil until submission.
	FormData map[string]interface{} `json:"form_data,omitempty"`
}

// WorkflowState is the complete, serializable state of a running workflow.
type WorkflowState struct {
	ID           string         `json:"id"`
	UserID       string         `json:"user_id"`
	Type         string         `json:"type"`
	ProjectPath  string         `json:"project_path"`
	Summary      string         `json:"summary"`
	Phases       []Phase        `json:"phases"`
	CurrentPhase int            `json:"current_phase"`
	Status       WorkflowStatus `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`

	// SupplementaryDocs stores optional supplementary documents uploaded by the user.
	// These are reference materials (research plans, publication lists, etc.) that
	// provide context for LLM generation in subsequent phases.
	// Key: original file name, Value: extracted text content (truncated to budget).
	SupplementaryDocs map[string]string `json:"supplementary_docs,omitempty"`
}

// ActivePhase returns the current phase, or nil if workflow is complete.
func (s *WorkflowState) ActivePhase() *Phase {
	if s == nil || s.CurrentPhase < 0 || s.CurrentPhase >= len(s.Phases) {
		return nil
	}
	return &s.Phases[s.CurrentPhase]
}

// IsWaitingConfirm returns true if the current phase is waiting for user confirmation.
func (s *WorkflowState) IsWaitingConfirm() bool {
	p := s.ActivePhase()
	return p != nil && p.Status == PhaseWaitingConfirm
}

// IsExecutionPhase returns true if the current phase has full tool access.
func (s *WorkflowState) IsExecutionPhase() bool {
	p := s.ActivePhase()
	return p != nil && p.ToolPolicy == ToolPolicyFull
}

// PreviousOutputs returns completed phase outputs keyed by phase ID.
// Each output is truncated to maxRunes for prompt injection.
// Base64 data URLs (inlined images) are stripped before truncation because
// they are binary noise that wastes LLM context tokens.
func (s *WorkflowState) PreviousOutputs(maxRunes int) map[string]string {
	result := make(map[string]string)
	for i := 0; i < s.CurrentPhase && i < len(s.Phases); i++ {
		p := s.Phases[i]
		if p.Output == "" {
			continue
		}
		output := stripBase64DataURLs(p.Output)
		if maxRunes > 0 {
			runes := []rune(output)
			if len(runes) > maxRunes {
				output = string(runes[:maxRunes]) + "\n...(截断)"
			}
		}
		result[p.ID] = output
	}
	return result
}

// base64DataURLRe matches Markdown image references with data: URLs.
// Used to strip inlined binary data that is useless in LLM prompt context.
var base64DataURLRe = regexp.MustCompile(`!\[[^\]]*\]\(data:[^)]+\)`)

// stripBase64DataURLs removes Markdown image references containing base64 data URLs
// from text, replacing them with a short placeholder. This prevents binary image data
// from polluting LLM prompt context (PreviousOutputs, confirm classifier, etc.).
func stripBase64DataURLs(s string) string {
	if !strings.Contains(s, "data:") {
		return s
	}
	return base64DataURLRe.ReplaceAllString(s, "[图片]")
}

// GenerateID creates a workflow ID from userID and timestamp.
func GenerateID(userID string) string {
	return fmt.Sprintf("wf2-%s-%d", userID, time.Now().UnixMilli())
}
