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
	"time"

	"github.com/google/uuid"
)

// Status represents the lifecycle state of a goal.
type Status string

const (
	StatusActive      Status = "active"
	StatusPaused      Status = "paused"
	StatusBudgetLimit Status = "budget_limited"
	StatusComplete    Status = "complete"
	StatusFailed      Status = "failed"
)

// Goal represents a persisted long-running task objective.
type Goal struct {
	GoalID    string `json:"goal_id"`    // UUID, regenerated on every replacement
	UserID    string `json:"user_id"`
	Objective string `json:"objective"`
	Status    Status `json:"status"`

	// Budget constraints (0 = unlimited)
	TokenBudget int `json:"token_budget,omitempty"`
	MaxTurns    int `json:"max_turns,omitempty"`

	// Usage accounting
	TokensUsed      int `json:"tokens_used"`
	TimeUsedSeconds int `json:"time_used_seconds"`
	TurnsUsed       int `json:"turns_used"`

	// MaClaw-specific: verifiable completion conditions
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	ProjectPath        string   `json:"project_path,omitempty"`

	// Continuation control
	ConsecutiveNoToolTurns int `json:"consecutive_no_tool_turns"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Completion/failure metadata
	Summary string `json:"summary,omitempty"` // set on complete/fail
}

// IsTerminal returns true if the goal has reached a final state.
func (g *Goal) IsTerminal() bool {
	switch g.Status {
	case StatusComplete, StatusFailed, StatusBudgetLimit:
		return true
	}
	return false
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
