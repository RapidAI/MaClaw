package hopjit

import (
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

const SnapshotFormatVersion = 1

type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

type TerminalState string

const (
	TerminalCompleted TerminalState = "completed"
	TerminalFailed    TerminalState = "failed"
	TerminalAborted   TerminalState = "aborted"
)

type NextStatus string

const (
	NextStepReady      NextStatus = "step_ready"
	NextRunning        NextStatus = "running"
	NextPaused         NextStatus = "paused"
	NextAdaptiveNeeded NextStatus = "adaptive_needed"
	NextCompleted      NextStatus = "completed"
	NextFailed         NextStatus = "failed"
	NextAborted        NextStatus = "aborted"
)

type FailureKind string

const (
	FailureError         FailureKind = "error"
	FailureDeterministic FailureKind = "deterministic"
	FailureLackOfInfo    FailureKind = "lack_of_info"
	FailureTool          FailureKind = "tool_failure"
)

type Failure struct {
	Kind   FailureKind `json:"kind"`
	Reason string      `json:"reason"`
}

type VariableValue struct {
	Value    any  `json:"value"`
	Assigned bool `json:"assigned"`
}

type ExecutionEvent struct {
	Sequence uint64    `json:"sequence"`
	At       time.Time `json:"at"`
	StepID   string    `json:"step_id,omitempty"`
	Event    string    `json:"event"`
	Detail   string    `json:"detail,omitempty"`
}

type RetryRecord struct {
	Attempt    int      `json:"attempt"`
	Failure    Failure  `json:"failure"`
	StepsTried []string `json:"steps_tried,omitempty"`
}

type PauseState struct {
	StepID       string                 `json:"step_id"`
	ChildRunID   string                 `json:"child_run_id,omitempty"`
	Kind         hopspec.StepType       `json:"kind"`
	Question     string                 `json:"question"`
	OutputSchema []hopspec.VariableDecl `json:"output_schema,omitempty"`
	RequireHuman bool                   `json:"require_human,omitempty"`
}

type CallState struct {
	RunID  string `json:"run_id"`
	Callee string `json:"callee"`
}

type InflightCall struct {
	StepID      string `json:"step_id"`
	ChildRunID  string `json:"child_run_id"`
	ContainerID string `json:"container_id,omitempty"`
	Iteration   int    `json:"iteration,omitempty"`
	Status      string `json:"status"`
}

type EngineSnapshot struct {
	FormatVersion   int                       `json:"format_version"`
	RunID           string                    `json:"run_id"`
	OwnerID         string                    `json:"owner_id,omitempty"`
	CallDepth       int                       `json:"call_depth,omitempty"`
	Spec            hopspec.SpecAST           `json:"spec"`
	StepStates      map[string]StepStatus     `json:"step_states"`
	Variables       map[string]VariableValue  `json:"variables"`
	LoopCounters    map[string]int            `json:"loop_counters,omitempty"`
	LoopItems       map[string][]any          `json:"loop_items,omitempty"`
	LoopResults     map[string]map[string]any `json:"loop_results,omitempty"`
	Calls           map[string]CallState      `json:"calls,omitempty"`
	ResolvedSpecs   map[string]string         `json:"resolved_specs,omitempty"`
	Inflight        map[string]InflightCall   `json:"inflight,omitempty"`
	RetryCounters   map[string]int            `json:"retry_counters,omitempty"`
	RetryHistory    map[string][]RetryRecord  `json:"retry_history,omitempty"`
	ReplanCounters  map[string]int            `json:"replan_counters,omitempty"`
	ReplanDigests   map[string]string         `json:"replan_digests,omitempty"`
	CommittedSteps  map[string]bool           `json:"committed_steps,omitempty"`
	OnFailActive    map[string]bool           `json:"on_fail_active,omitempty"`
	AdaptiveNeeded  string                    `json:"adaptive_needed_subtask,omitempty"`
	Pause           *PauseState               `json:"pause,omitempty"`
	Failures        map[string]Failure        `json:"failures,omitempty"`
	Events          []ExecutionEvent          `json:"events,omitempty"`
	TerminalState   TerminalState             `json:"terminal_state,omitempty"`
	TerminalFailure *Failure                  `json:"terminal_failure,omitempty"`
	AbortReason     string                    `json:"abort_reason,omitempty"`
	StateSeq        uint64                    `json:"state_seq"`
}

type InitResult struct {
	RunID string `json:"run_id"`
}

type StepReady struct {
	RunID        string                 `json:"run_id"`
	Goal         string                 `json:"goal"`
	Constraints  []string               `json:"constraints,omitempty"`
	StepID       string                 `json:"step_id"`
	StepType     hopspec.StepType       `json:"step_type"`
	Summary      string                 `json:"summary"`
	Inputs       map[string]any         `json:"inputs"`
	Instruction  string                 `json:"instruction,omitempty"`
	Tools        []string               `json:"tools,omitempty"`
	OutputSchema []hopspec.VariableDecl `json:"output_schema,omitempty"`
	Recovered    bool                   `json:"recovered,omitempty"`
	IterationID  string                 `json:"iteration_id,omitempty"`
	Context      AssembledContext       `json:"context"`
}

type NextResult struct {
	Status   NextStatus     `json:"status"`
	RunID    string         `json:"run_id"`
	Step     *StepReady     `json:"step,omitempty"`
	Pause    *PauseState    `json:"pause,omitempty"`
	Outputs  map[string]any `json:"outputs,omitempty"`
	Failure  *Failure       `json:"failure,omitempty"`
	Snapshot EngineSnapshot `json:"snapshot"`
}

var (
	ErrSnapshotNotFound  = errors.New("hopjit snapshot not found")
	ErrStaleSnapshot     = errors.New("hopjit stale snapshot")
	ErrStepNotRunning    = errors.New("hopjit step is not running")
	ErrRunNotInitialized = errors.New("hopjit run is not initialized")
	ErrRunAccessDenied   = errors.New("hopjit run access denied")
)
