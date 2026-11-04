package hopjit

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

type AssembledContext struct {
	TaskContext      string                 `json:"task_context"`
	PositionContext  string                 `json:"position_context"`
	ProgressSummary  string                 `json:"progress_summary"`
	IterationHistory string                 `json:"iteration_history,omitempty"`
	RetryFeedback    string                 `json:"retry_feedback,omitempty"`
	PriorOutputs     map[string]any         `json:"prior_outputs,omitempty"`
	Inputs           map[string]any         `json:"inputs"`
	ToolManifest     []string               `json:"tool_manifest,omitempty"`
	Instruction      string                 `json:"instruction"`
	OutputSchema     []hopspec.VariableDecl `json:"output_schema,omitempty"`
}

type PromptBudget struct {
	MaxEstimatedTokens int
}

func AssemblePromptContext(snapshot EngineSnapshot, step StepReady) AssembledContext {
	context, _ := AssemblePromptContextWithBudget(snapshot, step, PromptBudget{})
	return context
}

func AssemblePromptContextWithBudget(snapshot EngineSnapshot, step StepReady, budget PromptBudget) (AssembledContext, error) {
	context := AssembledContext{
		TaskContext:      snapshot.Spec.Goal,
		PositionContext:  step.StepID,
		ProgressSummary:  renderHopProgress(snapshot),
		IterationHistory: renderHopIterations(snapshot, step.StepID),
		PriorOutputs:     map[string]any{},
		Inputs:           cloneHopInputs(step.Inputs),
		ToolManifest:     append([]string(nil), step.Tools...),
		Instruction:      step.Instruction,
		OutputSchema:     append([]hopspec.VariableDecl(nil), step.OutputSchema...),
	}
	if len(snapshot.Spec.Constraints) > 0 {
		context.TaskContext += "\nConstraints:\n- " + strings.Join(snapshot.Spec.Constraints, "\n- ")
	}
	if len(snapshot.Spec.Outputs) > 0 {
		outputs, _ := json.Marshal(snapshot.Spec.Outputs)
		context.TaskContext += "\nOutputs:\n" + string(outputs)
	}
	for _, name := range step.OutputSchema {
		if value, ok := snapshot.Variables[name.Name]; ok && value.Assigned {
			context.PriorOutputs[name.Name] = value.Value
		}
	}
	context.RetryFeedback = latestRetryFeedback(snapshot, step.StepID)
	if len(context.PriorOutputs) == 0 {
		context.PriorOutputs = nil
	}
	if budget.MaxEstimatedTokens > 0 && estimatePromptTokens(context.Render()) > budget.MaxEstimatedTokens {
		return AssembledContext{}, errors.New("PROMPT_CONTEXT_BUDGET_EXCEEDED")
	}
	return context, nil
}

func (c AssembledContext) Render() string {
	var b strings.Builder
	writeHopPromptSection(&b, "任务契约", c.TaskContext)
	writeHopPromptSection(&b, "当前位置", c.PositionContext)
	writeHopPromptSection(&b, "执行进度", c.ProgressSummary)
	writeHopPromptSection(&b, "循环历史", c.IterationHistory)
	writeHopPromptSection(&b, "重试反馈", c.RetryFeedback)
	writeHopPromptJSON(&b, "前次输出", c.PriorOutputs)
	writeHopPromptJSON(&b, "输入", c.Inputs)
	if len(c.ToolManifest) > 0 {
		writeHopPromptSection(&b, "工具白名单", strings.Join(c.ToolManifest, ", "))
	}
	writeHopPromptSection(&b, "执行说明", c.Instruction)
	writeHopPromptJSON(&b, "输出声明", c.OutputSchema)
	return b.String()
}

func renderHopProgress(snapshot EngineSnapshot) string {
	var lines []string
	for _, step := range snapshot.Spec.AllSteps() {
		lines = append(lines, fmt.Sprintf("%s [%s] %s — %s", step.ID, step.Type, step.Summary, snapshot.StepStates[step.ID]))
	}
	return strings.Join(lines, "\n")
}

func renderHopIterations(snapshot EngineSnapshot, stepID string) string {
	var lines []string
	for id, iteration := range snapshot.LoopCounters {
		if isStepAncestor(id, stepID) {
			lines = append(lines, fmt.Sprintf("%s: iteration=%d", id, iteration))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func latestRetryFeedback(snapshot EngineSnapshot, stepID string) string {
	path := strings.Split(stepID, ".")
	for index := len(path) - 1; index > 0; index-- {
		containerID := strings.Join(path[:index], ".")
		history := snapshot.RetryHistory[containerID]
		if len(history) > 0 {
			return history[len(history)-1].Failure.Reason
		}
	}
	return ""
}

func cloneHopInputs(inputs map[string]any) map[string]any {
	if inputs == nil {
		return map[string]any{}
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return inputs
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return inputs
	}
	return clone
}

func writeHopPromptSection(b *strings.Builder, title, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	fmt.Fprintf(b, "## %s\n%s\n\n", title, content)
}

func writeHopPromptJSON(b *strings.Builder, title string, value any) {
	if value == nil {
		return
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	writeHopPromptSection(b, title, string(encoded))
}

func estimatePromptTokens(text string) int {
	return (len([]rune(text)) + 3) / 4
}
