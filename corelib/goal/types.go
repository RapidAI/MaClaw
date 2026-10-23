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
	"strconv"
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
	// StoppedAt is when the composer clock last froze. Pause, failure, and a
	// filled budget all stamp it. Continue clears it after banking the gap.
	StoppedAt time.Time `json:"stopped_at,omitempty"`
	// ClockHoldSeconds is time the clock was held. The composer clock subtracts
	// it, so continue resumes from the frozen reading instead of jumping forward.
	ClockHoldSeconds int64 `json:"clock_hold_seconds,omitempty"`

	// Completion/failure metadata
	Summary string `json:"summary,omitempty"` // set on complete/fail
}

// statusIsTerminal reports a final state. Pause is not final: the bar can
// continue it. Keep this list separate from the clock so a pause cannot
// start being treated as finished.
func statusIsTerminal(status Status) bool {
	switch status {
	case StatusComplete, StatusFailed, StatusBudgetLimit:
		return true
	default:
		return false
	}
}

// statusEndsGoalClock reports a status whose composer elapsed clock stays put.
// Pause holds the reading. Continue banks that gap and the clock resumes
// from the frozen value.
func statusEndsGoalClock(status Status) bool {
	return status == StatusPaused || statusIsTerminal(status)
}

// ComposerClockStopped reports whether the composer elapsed clock stays put.
func (g *Goal) ComposerClockStopped() bool {
	return statusEndsGoalClock(g.Status)
}

// IsTerminal returns true if the goal has reached a final state.
// Pause stops the clock, but the goal can still be continued.
func (g *Goal) IsTerminal() bool {
	return statusIsTerminal(g.Status)
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

// DefaultMaxTurns is unlimited. Create leaves MaxTurns at 0 unless the caller
// sets one. ShouldContinue treats 0 as no turn cap. The goal runs until it
// completes, fails, hits a token budget, sees two turns with no tools, or
// the user stops it.
const DefaultMaxTurns = 0

// LegacyDefaultMaxTurns is the cap create wrote when max_turns was omitted,
// before a goal could run for days.
const LegacyDefaultMaxTurns = 50

// RaisedDefaultMaxTurns is the later automatic cap (about a week at one turn
// a minute). Load clears only these two exact values. Any other explicit
// max_turns stays. A caller who chose 50 or 10000 on purpose is
// indistinguishable from the old default and is cleared too.
const RaisedDefaultMaxTurns = 10000

// MaxTurnsLabel is the turn cap shown to the model. Zero is unlimited.
func MaxTurnsLabel(turns int) string {
	if turns <= 0 {
		return "不限"
	}
	return strconv.Itoa(turns)
}

// FormatTurnCount is the used/cap line shown to the model. A zero cap is
// unlimited, so the text must not look like the budget is already spent.
func FormatTurnCount(used, max int) string {
	return strconv.Itoa(used) + " / " + MaxTurnsLabel(max)
}
