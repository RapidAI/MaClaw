package agent

import (
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// WorkingStateEnvKey disables task-turn working state when set to off/0/false/no.
const WorkingStateEnvKey = "MACLAW_WORKING_STATE"

// WorkingStateMarker is the line-start heading of the spliced system tail.
const WorkingStateMarker = "[任务状态]"

const (
	workingStateMaxRunes      = 400
	workingStateGoalMaxRunes  = 80
	workingStateLabelMaxRunes = 40
	workingStateMaxLive       = 2
	workingStateMaxSettled    = 2
	workingStateMaxOpen       = 2
	workingStateFactMaxRunes  = 80
	openSettleByUserReply     = "用户回答"
)

// ControlAction is the loop-owned next move after a tool or empty round.
type ControlAction string

const (
	ActionTrust         ControlAction = "trust"
	ActionRetryDiagnose ControlAction = "retry_diagnose"
	ActionReroute       ControlAction = "reroute"
	ActionEmpiric       ControlAction = "empiric"
	ActionSeekUser      ControlAction = "seek_user"
)

const (
	RoundToolOK      = "tool_ok"
	RoundToolTimeout = "tool_timeout"
	RoundToolError   = "tool_error"
	RoundLLMEmpty    = "llm_empty"
)

// RoundSignal is the input to SelectAction. Do not stuff LLM-empty into ToolExecutionOutcome.
type RoundSignal struct {
	Kind         string
	ToolName     string
	SameSigCount int
	EmptyCount   int
	Prev         ControlAction
	OpenCount    int
	// FocusLabel is set only when this signal's tool admitted a live path.
	FocusLabel string
}

// FocusItem is one live workspace slot.
type FocusItem struct {
	Label string
	Fact  string
}

// Settled is a verified claim that used a live premise.
type Settled struct {
	ID       string
	Label    string
	Claim    string
	Verifier string
	Coverage string
}

// OpenItem is an unresolved question created by a non-trust action.
type OpenItem struct {
	Tool     string
	Question string
	SettleBy string
	ClosedBy string
}

// WorkingState is the loop-owned task-turn workspace.
type WorkingState struct {
	Goal         string
	Live         []FocusItem
	Settled      []Settled
	Open         []OpenItem
	Next         string
	LastAction   ControlAction
	LastSig      string
	SigCount     int
	FinishNudges int
	Updated      time.Time
}

// WorkingStateHolder is an optional host callback, same shape as PromptProfileProvider.
type WorkingStateHolder interface {
	LoadWorkingState() *WorkingState
	SaveWorkingState(*WorkingState)
}

// WorkingStateGoalSource is an optional host callback for an active-this-turn goal projection.
type WorkingStateGoalSource interface {
	ActiveWorkingStateGoal() string
}

// WorkingStateDisabled reports MACLAW_WORKING_STATE=off.
func WorkingStateDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(WorkingStateEnvKey))) {
	case "0", "off", "false", "no":
		return true
	default:
		return false
	}
}

// NewWorkingState starts a task-turn workspace with a clipped goal.
func NewWorkingState(goal string) *WorkingState {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = "当前任务"
	}
	return &WorkingState{Goal: clipRunes(goal, workingStateGoalMaxRunes), Updated: time.Now()}
}

// GoalFromUserText takes the first sentence of userText, at most 80 runes.
func GoalFromUserText(userText string) string {
	text := strings.TrimSpace(userText)
	if text == "" {
		return "当前任务"
	}
	cut := -1
	for i, r := range text {
		if r == '。' || r == '！' || r == '？' || r == '!' || r == '?' || r == '\n' {
			cut = i
			break
		}
		if r != '.' {
			continue
		}
		// Keep "fix main.go"; only treat "." as an English sentence end.
		rest := text[i+1:]
		if rest == "" {
			cut = i
			break
		}
		next, _ := utf8.DecodeRuneInString(rest)
		if next == ' ' || next == '\t' || next == '\n' || next == '\r' {
			cut = i
			break
		}
	}
	if cut >= 0 {
		text = strings.TrimSpace(text[:cut])
	}
	if text == "" {
		return "当前任务"
	}
	return clipRunes(text, workingStateGoalMaxRunes)
}

// CloneWorkingState returns a deep copy, or nil.
func CloneWorkingState(s *WorkingState) *WorkingState {
	if s == nil {
		return nil
	}
	cp := *s
	if s.Live != nil {
		cp.Live = append([]FocusItem(nil), s.Live...)
	}
	if s.Settled != nil {
		cp.Settled = append([]Settled(nil), s.Settled...)
	}
	if s.Open != nil {
		cp.Open = append([]OpenItem(nil), s.Open...)
	}
	return &cp
}

// EnsureWorkingState creates a workspace when a carrier already exists, tools
// have executed, or this turn has an active projected goal.
func EnsureWorkingState(state *WorkingState, userText string, executedTools int, projectedGoal string) *WorkingState {
	if state != nil {
		return state
	}
	if g := strings.TrimSpace(projectedGoal); g != "" {
		return NewWorkingState(g)
	}
	if executedTools > 0 {
		return NewWorkingState(GoalFromUserText(userText))
	}
	return nil
}

// ShouldAttachWorkingState is the splice gate: not off, not light, and state exists.
func ShouldAttachWorkingState(profile PromptProfile, envOff bool, state *WorkingState) bool {
	if envOff || profile.IsLight() || state == nil {
		return false
	}
	return true
}

// AdvanceWorkingStateAfterUserReply clears a seek_user pause after the user
// already answered. Open-mic and still-pending choice peeks must not call this.
func AdvanceWorkingStateAfterUserReply(state *WorkingState) {
	if state == nil || state.LastAction != ActionSeekUser {
		return
	}
	// The user already answered. Close only seek-opens; fail/reroute/empiric
	// questions stay so the next turn can still done-check those.
	for i := range state.Open {
		if strings.TrimSpace(state.Open[i].ClosedBy) == "" && state.Open[i].SettleBy == openSettleByUserReply {
			state.Open[i].ClosedBy = "user-reply"
		}
	}
	// A done-check consumed before the pause belongs to that loop, not
	// the resumed turn — same as steer resetting FinishNudges.
	state.FinishNudges = 0
	state.LastAction = ""
	state.Next = nextContinue(state, "", "")
	state.touch()
}

func (s *WorkingState) touch() {
	if s != nil {
		s.Updated = time.Now()
	}
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func clipRunesSuffix(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[len(runes)-max:])
}
