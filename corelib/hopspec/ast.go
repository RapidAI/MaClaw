package hopspec

import "strings"

type StepType string

const (
	StepReason   StepType = "reason"
	StepAct      StepType = "act"
	StepCheck    StepType = "check"
	StepConfirm  StepType = "confirm"
	StepAsk      StepType = "ask"
	StepCommit   StepType = "commit"
	StepCall     StepType = "call"
	StepSubtask  StepType = "subtask"
	StepLoop     StepType = "loop"
	StepBranch   StepType = "branch"
	StepCase     StepType = "case"
	StepBreak    StepType = "break"
	StepContinue StepType = "continue"
	StepExit     StepType = "exit"
	StepOnFail   StepType = "on_fail"
)

type SourceLocation struct {
	LineStart int `json:"line_start"`
	LineEnd   int `json:"line_end"`
}

type VariableDecl struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type ForEachSpec struct {
	Item        string `json:"item"`
	List        string `json:"list"`
	CollectItem string `json:"collect_item,omitempty"`
	CollectInto string `json:"collect_into,omitempty"`
}

type CallSpec struct {
	Callee  string            `json:"callee"`
	Inputs  map[string]string `json:"inputs,omitempty"`
	Outputs map[string]string `json:"outputs,omitempty"`
}

type Step struct {
	ID            string            `json:"step_id"`
	Type          StepType          `json:"step_type"`
	Summary       string            `json:"summary"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	Inputs        []string          `json:"inputs,omitempty"`
	Tools         []string          `json:"tools,omitempty"`
	Outputs       []VariableDecl    `json:"outputs,omitempty"`
	Instruction   string            `json:"instruction,omitempty"`
	Body          string            `json:"body,omitempty"`
	Children      []*Step           `json:"children,omitempty"`
	Source        SourceLocation    `json:"source_location"`
	Retry         *int              `json:"retry,omitempty"`
	Adaptive      bool              `json:"adaptive,omitempty"`
	Parallel      bool              `json:"parallel,omitempty"`
	MaxIterations int               `json:"max_iterations,omitempty"`
	ForEach       *ForEachSpec      `json:"for_each,omitempty"`
	Call          *CallSpec         `json:"call,omitempty"`
	Condition     string            `json:"condition,omitempty"`
}

func (s Step) Free() bool {
	_, ok := s.Attributes["free"]
	return ok
}

func (s Step) Final() bool {
	_, ok := s.Attributes["final"]
	return ok
}

type SpecAST struct {
	Title       string         `json:"title"`
	ID          string         `json:"id,omitempty"`
	Goal        string         `json:"goal"`
	Constraints []string       `json:"constraints,omitempty"`
	Inputs      []VariableDecl `json:"inputs,omitempty"`
	Outputs     []VariableDecl `json:"outputs,omitempty"`
	Steps       []*Step        `json:"steps,omitempty"`
}

func (s SpecAST) AllSteps() []Step {
	var out []Step
	var walk func([]*Step)
	walk = func(steps []*Step) {
		for _, step := range steps {
			out = append(out, *step)
			walk(step.Children)
		}
	}
	walk(s.Steps)
	return out
}

func (s SpecAST) DeclaredOutput(name string) (VariableDecl, bool) {
	for _, output := range s.Outputs {
		if output.Name == strings.TrimSpace(name) {
			return output, true
		}
	}
	return VariableDecl{}, false
}
