// Package goal provides a persistent goal store for long-running autonomous
// task objectives. A goal is a single persisted objective per user that drives
// the agent's auto-continuation loop until the goal is reached or budget is
// exhausted.
//
// Design principles (adapted from OpenAI Codex /goal):
//   - One goal per user at a time
//   - goal_id versioning prevents stale updates from clobbering newer goals
//   - Model can create and complete goals; pause/resume are system-controlled
//   - Synchronous atomic persistence (no debounce) — crash-safe
package goal

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Phase is the auto-continuation stage of an active goal.
type Phase string

const (
	PhasePlan    Phase = "plan"
	PhaseExecute Phase = "execute"
)

// StepStatus is the lifecycle of one decomposed step.
type StepStatus string

const (
	StepPending  StepStatus = "pending"
	StepProgress StepStatus = "progress"
	StepDone     StepStatus = "done"
	StepFailed   StepStatus = "failed"
)

// Step is one ordered unit of work produced by the planning turn.
type Step struct {
	Index  int        `json:"index"`
	Title  string     `json:"title"`
	Detail string     `json:"detail,omitempty"`
	Status StepStatus `json:"status"`
	Result string     `json:"result,omitempty"`
}

// Status represents the lifecycle state of a goal.
type Status string

const (
	StatusActive      Status = "active"
	StatusPaused      Status = "paused"
	StatusBudgetLimit Status = "budget_limited"
	StatusComplete    Status = "complete"
	StatusFailed      Status = "failed"
)

// StatusOptions is the closed set of statuses a managed turn may set on a
// goal, and the enum both hosts render into the invocation schema. The rest
// of the lifecycle is system-controlled (pause, resume, budget) and never a
// model-supplied argument. It is documentation for the model, not a boundary:
// StatusCanonical is the boundary, and a set rendered apart from it is the
// drift this package already avoids for IsTerminal by keeping both answers
// next to the type itself.
func StatusOptions() []string {
	return []string{"complete", "failed"}
}

// StatusCanonical maps model-supplied status text to the lifecycle value it
// records. Aliases ("completed", "fail") are accepted but never advertised: a
// model that sends one means the same thing as the canonical name it is
// aliased from. An empty or unknown string is refused -- unlike a schedule
// record's optional status, an update turn only parses a status it intends to
// set, so there is no unset reading to preserve.
func StatusCanonical(raw string) (Status, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "complete", "completed":
		return StatusComplete, true
	case "failed", "fail":
		return StatusFailed, true
	default:
		return "", false
	}
}

// StatusHint renders the canonical vocabulary for a rejection message, so a
// model that ignored the enum still learns the valid names from its first
// failure and can retry inside the turn.
func StatusHint() string {
	return "allowed: " + strings.Join(StatusOptions(), ", ")
}

// Goal represents a persisted long-running task objective.
type Goal struct {
	GoalID    string `json:"goal_id"` // UUID, regenerated on every replacement
	UserID    string `json:"user_id"`
	Objective string `json:"objective"`
	Status    Status `json:"status"`

	// Budget constraints (0 = unlimited)
	TokenBudget int `json:"token_budget,omitempty"`
	// TokenWindow is the fixed number of tokens added each time the user
	// continues past a filled token cap. Zero means it has not been chosen
	// yet, and the configured budget is that window.
	TokenWindow int `json:"token_window,omitempty"`
	MaxTurns    int `json:"max_turns,omitempty"`

	// Usage accounting
	TokensUsed      int `json:"tokens_used"`
	TimeUsedSeconds int `json:"time_used_seconds"`
	TurnsUsed       int `json:"turns_used"`

	// MaClaw-specific: verifiable completion conditions
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	ProjectPath        string   `json:"project_path,omitempty"`

	// Plan is filled by the first continuation turn. PhasePlan forbids
	// implementation; PhaseExecute runs one pending step at a time.
	Understanding string `json:"understanding,omitempty"`
	Steps         []Step `json:"steps,omitempty"`
	Phase         Phase  `json:"phase,omitempty"`

	// Continuation control
	ConsecutiveNoToolTurns int `json:"consecutive_no_tool_turns"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// StoppedAt is when the goal last became terminal. Pause does not set it.
	StoppedAt time.Time `json:"stopped_at,omitempty"`
	// ClockHoldSeconds is time spent terminal. The composer clock subtracts it,
	// so continue resumes from the frozen reading instead of jumping forward.
	ClockHoldSeconds int64 `json:"clock_hold_seconds,omitempty"`

	// Completion/failure metadata
	Summary string `json:"summary,omitempty"` // set on complete/fail
}

// statusEndsGoalClock reports a status whose composer elapsed clock stays put.
// Pause is a hold, so the age clock keeps moving until the goal actually ends.
func statusEndsGoalClock(status Status) bool {
	switch status {
	case StatusComplete, StatusFailed, StatusBudgetLimit:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the goal has reached a final state.
func (g *Goal) IsTerminal() bool {
	return statusEndsGoalClock(g.Status)
}

// ShouldContinue returns true if the goal is in a state that allows
// auto-continuation.
func (g *Goal) ShouldContinue() bool {
	if g.Status != StatusActive {
		return false
	}
	// Budget checks
	if g.TokenBudget > 0 && g.TokensUsed >= g.TokenBudget {
		return false
	}
	if g.MaxTurns > 0 && g.TurnsUsed >= g.MaxTurns {
		return false
	}
	// No-tool suppression: 2 consecutive turns with zero tool calls → pause
	if g.ConsecutiveNoToolTurns >= 2 {
		return false
	}
	return true
}

// NewGoalID generates a fresh goal identifier.
func NewGoalID() string {
	return uuid.New().String()
}

// DefaultMaxTurns is the default iteration cap when no explicit max is set.
const DefaultMaxTurns = 50
