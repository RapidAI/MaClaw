package hopjit

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

type advanceKind string

const (
	advanceNone     advanceKind = ""
	advanceReady    advanceKind = "ready"
	advancePaused   advanceKind = "paused"
	advanceRestart  advanceKind = "restart"
	advanceBreak    advanceKind = "break"
	advanceContinue advanceKind = "continue"
)

type advanceResult struct {
	kind advanceKind
	step hopspec.Step
}

func (e *ExecutionEngine) advanceLocked() advanceResult {
	for round := 0; ; round++ {
		if round > 1000 {
			e.failStepLocked(hopspec.Step{}, Failure{Kind: FailureDeterministic, Reason: "EXECUTION_NO_PROGRESS"})
			return advanceResult{}
		}
		result := e.advanceStepsLocked(e.snapshot.Spec.Steps)
		if result.kind == advanceRestart && e.snapshot.TerminalState == "" {
			e.recordLocked("advance_restart", result.step.ID, "")
			continue
		}
		if result.kind != advanceNone || e.snapshot.TerminalState != "" {
			return result
		}
		if e.stepsFinished(e.snapshot.Spec.Steps) {
			e.finishLocked()
		} else if len(e.snapshot.Inflight) > 0 {
			return advanceResult{}
		} else {
			e.failStepLocked(hopspec.Step{}, Failure{Kind: FailureDeterministic, Reason: "EXECUTION_STALLED: no runnable step remains"})
		}
		return advanceResult{}
	}
}

func (e *ExecutionEngine) advanceStepsLocked(steps []*hopspec.Step) advanceResult {
	if err := e.startParallelCallsLocked(e.ctx, steps); err != nil {
		e.failStepLocked(hopspec.Step{}, Failure{Kind: FailureError, Reason: err.Error()})
		return advanceResult{}
	}
	for _, step := range steps {
		switch e.snapshot.StepStates[step.ID] {
		case StepDone, StepSkipped:
			continue
		case StepFailed:
			parentID := ""
			if index := strings.LastIndex(step.ID, "."); index >= 0 {
				parentID = step.ID[:index]
			}
			if e.snapshot.OnFailActive[parentID] {
				continue
			}
			e.propagateFailureLocked(*step)
			return advanceResult{}
		case StepRunning:
			if e.hasInflightStep(step.ID) {
				return advanceResult{}
			}
			if e.isWorkStep(step.Type) {
				return advanceResult{}
			}
			if step.Type == hopspec.StepCall {
				if e.hasInflightStep(step.ID) {
					return advanceResult{}
				}
				return e.executeCallLocked(e.ctx, step)
			}
			result := e.advanceRunningContainerLocked(step)
			if result.kind != advanceNone || e.snapshot.TerminalState != "" {
				return result
			}
			if e.snapshot.StepStates[step.ID] == StepRunning && e.stepsFinished(step.Children) && len(e.inflightForLoop(step.ID)) == 0 {
				e.markContainerDoneLocked(step)
			}
			if e.snapshot.StepStates[step.ID] == StepRunning {
				return advanceResult{}
			}
			continue
		case StepPending:
			switch step.Type {
			case hopspec.StepReason:
				e.snapshot.StepStates[step.ID] = StepRunning
				e.recordLocked("step_ready", step.ID, string(step.Type))
				return advanceResult{kind: advanceReady, step: *step}
			case hopspec.StepAct, hopspec.StepCheck, hopspec.StepCommit:
				if step.Body != "" {
					e.executeBodyStepLocked(e.ctx, step)
					if e.snapshot.TerminalState != "" {
						return advanceResult{}
					}
					return advanceResult{kind: advanceRestart, step: *step}
				}
				e.snapshot.StepStates[step.ID] = StepRunning
				e.recordLocked("step_ready", step.ID, string(step.Type))
				return advanceResult{kind: advanceReady, step: *step}
			case hopspec.StepCall:
				if step.Parallel {
					return advanceResult{}
				}
				return e.executeCallLocked(e.ctx, step)
			case hopspec.StepConfirm, hopspec.StepAsk:
				e.snapshot.StepStates[step.ID] = StepRunning
				question := strings.TrimSpace(step.Instruction)
				if question == "" {
					question = step.Summary
				}
				e.snapshot.Pause = &PauseState{
					StepID: step.ID, Kind: step.Type, Question: question,
					OutputSchema: append([]hopspec.VariableDecl(nil), step.Outputs...),
					RequireHuman: step.Attributes["require_human"] == "true",
				}
				e.recordLocked("step_paused", step.ID, string(step.Type))
				return advanceResult{kind: advancePaused, step: *step}
			case hopspec.StepExit:
				e.snapshot.StepStates[step.ID] = StepDone
				e.recordLocked("step_done", step.ID, "")
				e.finishFromExitLocked(step.ID)
				return advanceResult{}
			case hopspec.StepBreak, hopspec.StepContinue:
				e.snapshot.StepStates[step.ID] = StepDone
				e.recordLocked("step_done", step.ID, string(step.Type))
				if step.Type == hopspec.StepBreak {
					return advanceResult{kind: advanceBreak, step: *step}
				}
				return advanceResult{kind: advanceContinue, step: *step}
			case hopspec.StepOnFail:
				parentID := ""
				if index := strings.LastIndex(step.ID, "."); index >= 0 {
					parentID = step.ID[:index]
				}
				if !e.snapshot.OnFailActive[parentID] {
					e.snapshot.StepStates[step.ID] = StepSkipped
					continue
				}
				e.snapshot.StepStates[step.ID] = StepRunning
				result := e.advanceStepsLocked(step.Children)
				if result.kind != advanceNone || e.snapshot.TerminalState != "" {
					return result
				}
				if e.stepsFinished(step.Children) {
					e.markContainerDoneLocked(step)
				}
			case hopspec.StepSubtask, hopspec.StepCase, hopspec.StepBranch, hopspec.StepLoop:
				e.startContainerLocked(step)
				if e.snapshot.StepStates[step.ID] == StepDone {
					continue
				}
				result := e.advanceRunningContainerLocked(step)
				if result.kind != advanceNone || e.snapshot.TerminalState != "" {
					return result
				}
				if e.snapshot.StepStates[step.ID] == StepRunning && e.stepsFinished(step.Children) && len(e.inflightForLoop(step.ID)) == 0 {
					e.markContainerDoneLocked(step)
				}
				if e.snapshot.StepStates[step.ID] == StepRunning {
					return advanceResult{}
				}
			default:
				e.failStepLocked(*step, Failure{Kind: FailureDeterministic, Reason: "UNSUPPORTED_STEP_TYPE: " + string(step.Type)})
				return advanceResult{}
			}
		}
	}
	return advanceResult{}
}

func (e *ExecutionEngine) advanceRunningContainerLocked(step *hopspec.Step) advanceResult {
	switch step.Type {
	case hopspec.StepSubtask, hopspec.StepCase, hopspec.StepBranch, hopspec.StepOnFail:
		return e.advanceStepsLocked(step.Children)
	case hopspec.StepLoop:
		return e.advanceLoopLocked(step)
	default:
		e.failStepLocked(*step, Failure{Kind: FailureDeterministic, Reason: "INVALID_RUNNING_CONTAINER: " + string(step.Type)})
		return advanceResult{}
	}
}

func (e *ExecutionEngine) startContainerLocked(step *hopspec.Step) {
	e.snapshot.StepStates[step.ID] = StepRunning
	e.recordLocked("container_started", step.ID, string(step.Type))
	e.skipNormalOnFailLocked(step.Children)
	if step.Type == hopspec.StepBranch {
		e.selectBranchCaseLocked(step)
	}
	if step.Type == hopspec.StepLoop {
		e.startLoopLocked(step)
	}
}

func (e *ExecutionEngine) selectBranchCaseLocked(branch *hopspec.Step) {
	for _, candidate := range branch.Children {
		if candidate.Type != hopspec.StepCase || e.snapshot.StepStates[candidate.ID] != StepPending {
			continue
		}
		condition := strings.TrimSpace(candidate.Condition)
		selected := false
		if strings.EqualFold(condition, "else") {
			selected = true
		} else if condition == "" {
			e.failStepLocked(*candidate, Failure{Kind: FailureDeterministic, Reason: "CASE_CONDITION_REQUIRED: " + candidate.ID})
			return
		} else {
			value, err := evalCondition(condition, e.snapshot.Variables)
			if err != nil {
				e.failStepLocked(*candidate, Failure{Kind: FailureDeterministic, Reason: "CASE_CONDITION_INVALID: " + err.Error()})
				return
			}
			selected = value
		}
		if selected {
			e.recordLocked("branch_selected", branch.ID, candidate.ID)
			for _, other := range branch.Children {
				if other.ID != candidate.ID && e.snapshot.StepStates[other.ID] == StepPending {
					e.snapshot.StepStates[other.ID] = StepSkipped
				}
			}
			return
		}
		e.snapshot.StepStates[candidate.ID] = StepSkipped
	}
	e.snapshot.StepStates[branch.ID] = StepDone
	e.recordLocked("container_done", branch.ID, "no case matched")
}

func (e *ExecutionEngine) startLoopLocked(loop *hopspec.Step) {
	e.snapshot.LoopCounters[loop.ID] = 1
	if loop.ForEach == nil {
		e.recordLocked("loop_iteration", loop.ID, "1")
		return
	}
	value, ok := e.snapshot.Variables[loop.ForEach.List]
	items, ok := valueSlice(value.Value)
	if !ok || !value.Assigned {
		e.failStepLocked(*loop, Failure{Kind: FailureDeterministic, Reason: "FOR_EACH_LIST_MISSING: " + loop.ForEach.List})
		return
	}
	e.snapshot.LoopItems[loop.ID] = append([]any(nil), items...)
	if loop.ForEach.CollectInto != "" {
		e.snapshot.Variables[loop.ForEach.CollectInto] = VariableValue{Value: []any{}, Assigned: true}
	}
	if len(items) == 0 {
		e.snapshot.StepStates[loop.ID] = StepDone
		return
	}
	e.setLoopItemLocked(loop)
	e.recordLocked("loop_iteration", loop.ID, "1")
}

func (e *ExecutionEngine) advanceLoopLocked(loop *hopspec.Step) advanceResult {
	if loop.ForEach != nil && e.snapshot.LoopCounters[loop.ID] >= len(e.snapshot.LoopItems[loop.ID]) && len(e.inflightForLoop(loop.ID)) == 0 && e.loopDispatchComplete(loop) {
		e.flushLoopResultsLocked(loop)
		e.snapshot.StepStates[loop.ID] = StepDone
		e.recordLocked("container_done", loop.ID, "parallel barrier")
		return advanceResult{}
	}
	if loop.ForEach != nil && e.snapshot.LoopCounters[loop.ID] >= len(e.snapshot.LoopItems[loop.ID]) && len(e.inflightForLoop(loop.ID)) > 0 && e.loopDispatchComplete(loop) {
		return advanceResult{}
	}
	result := e.advanceStepsLocked(loop.Children)
	if result.kind == advanceReady || e.snapshot.TerminalState != "" {
		return result
	}
	if result.kind == advanceBreak {
		e.collectLoopItemLocked(loop, false)
		e.snapshot.StepStates[loop.ID] = StepDone
		e.recordLocked("container_done", loop.ID, "break")
		return advanceResult{}
	}
	if result.kind == advanceContinue {
		e.nextLoopIterationLocked(loop)
		if e.snapshot.StepStates[loop.ID] == StepRunning {
			return advanceResult{kind: advanceRestart}
		}
		return advanceResult{}
	}
	if result.kind != advanceNone {
		return result
	}
	if !e.stepsFinished(loop.Children) {
		if e.loopDispatchComplete(loop) {
			e.nextLoopIterationLocked(loop)
			if e.snapshot.StepStates[loop.ID] == StepRunning {
				return advanceResult{kind: advanceRestart}
			}
		}
		return result
	}
	if !e.collectLoopItemLocked(loop, true) {
		return advanceResult{}
	}
	e.nextLoopIterationLocked(loop)
	if e.snapshot.StepStates[loop.ID] == StepRunning {
		return advanceResult{kind: advanceRestart}
	}
	return advanceResult{}
}

func (e *ExecutionEngine) nextLoopIterationLocked(loop *hopspec.Step) {
	current := e.snapshot.LoopCounters[loop.ID]
	finished := false
	if loop.ForEach != nil {
		finished = current >= len(e.snapshot.LoopItems[loop.ID])
	} else {
		finished = current >= loop.MaxIterations
	}
	if finished {
		if len(e.inflightForLoop(loop.ID)) > 0 {
			e.recordLocked("loop_barrier_waiting", loop.ID, fmt.Sprintf("iterations=%d", current))
			return
		}
		e.flushLoopResultsLocked(loop)
		e.snapshot.StepStates[loop.ID] = StepDone
		e.recordLocked("container_done", loop.ID, fmt.Sprintf("iterations=%d", current))
		return
	}
	e.snapshot.LoopCounters[loop.ID] = current + 1
	for _, child := range loop.Children {
		e.resetStepForNextIteration(child)
	}
	if loop.ForEach != nil {
		e.setLoopItemLocked(loop)
	}
	if err := e.startParallelCallsLocked(e.ctx, loop.Children); err != nil {
		e.failStepLocked(*loop, Failure{Kind: FailureError, Reason: err.Error()})
		return
	}
	e.recordLocked("loop_iteration", loop.ID, fmt.Sprintf("%d", current+1))
}

func (e *ExecutionEngine) loopDispatchComplete(loop *hopspec.Step) bool {
	for _, step := range loop.Children {
		state := e.snapshot.StepStates[step.ID]
		if state == StepDone || state == StepSkipped || e.hasInflightStep(step.ID) {
			continue
		}
		return false
	}
	return true
}

func (e *ExecutionEngine) inflightForLoop(loopID string) []InflightCall {
	var out []InflightCall
	for _, inflight := range e.snapshot.Inflight {
		if isStepAncestor(loopID, inflight.StepID) {
			out = append(out, inflight)
		}
	}
	return out
}

func (e *ExecutionEngine) flushLoopResultsLocked(loop *hopspec.Step) {
	results := e.snapshot.LoopResults[loop.ID]
	if len(results) == 0 || loop.ForEach == nil || loop.ForEach.CollectInto == "" {
		return
	}
	keys := make([]int, 0, len(results))
	for key := range results {
		iteration, err := strconv.Atoi(key)
		if err == nil {
			keys = append(keys, iteration)
		}
	}
	sort.Ints(keys)
	collected := e.snapshot.Variables[loop.ForEach.CollectInto]
	items, _ := valueSlice(collected.Value)
	for _, key := range keys {
		items = append(items, results[strconv.Itoa(key)])
	}
	e.snapshot.Variables[loop.ForEach.CollectInto] = VariableValue{Value: items, Assigned: true}
	delete(e.snapshot.LoopResults, loop.ID)
}

func (e *ExecutionEngine) setLoopItemLocked(loop *hopspec.Step) {
	index := e.snapshot.LoopCounters[loop.ID] - 1
	items := e.snapshot.LoopItems[loop.ID]
	if index < 0 || index >= len(items) {
		return
	}
	e.snapshot.Variables[loop.ForEach.Item] = VariableValue{Value: items[index], Assigned: true}
}

func (e *ExecutionEngine) collectLoopItemLocked(loop *hopspec.Step, required bool) bool {
	if loop.ForEach == nil || loop.ForEach.CollectItem == "" {
		return true
	}
	for _, step := range loop.Children {
		if step.Parallel {
			return true
		}
	}
	item, ok := e.snapshot.Variables[loop.ForEach.CollectItem]
	if !ok || !item.Assigned || item.Value == nil {
		if required {
			e.failStepLocked(*loop, Failure{Kind: FailureDeterministic, Reason: "COLLECT_OUTPUT_MISSING: " + loop.ForEach.CollectItem})
			return false
		}
		return true
	}
	collected := e.snapshot.Variables[loop.ForEach.CollectInto]
	items, _ := valueSlice(collected.Value)
	items = append(items, item.Value)
	e.snapshot.Variables[loop.ForEach.CollectInto] = VariableValue{Value: items, Assigned: true}
	e.snapshot.Variables[loop.ForEach.CollectItem] = VariableValue{}
	return true
}

func (e *ExecutionEngine) resetStepForNextIteration(step *hopspec.Step) {
	e.snapshot.StepStates[step.ID] = StepPending
	delete(e.snapshot.LoopCounters, step.ID)
	delete(e.snapshot.LoopItems, step.ID)
	for _, child := range step.Children {
		e.resetStepForNextIteration(child)
	}
	if step.Type == hopspec.StepOnFail {
		e.snapshot.StepStates[step.ID] = StepSkipped
	}
}

func (e *ExecutionEngine) finishFromExitLocked(exitID string) {
	for _, step := range e.snapshot.Spec.AllSteps() {
		state := e.snapshot.StepStates[step.ID]
		if step.ID == exitID {
			continue
		}
		if state == StepPending {
			e.snapshot.StepStates[step.ID] = StepSkipped
		} else if state == StepRunning && isStepAncestor(step.ID, exitID) {
			e.snapshot.StepStates[step.ID] = StepDone
		}
	}
	e.finishLocked()
}

func (e *ExecutionEngine) markContainerDoneLocked(step *hopspec.Step) {
	e.snapshot.StepStates[step.ID] = StepDone
	delete(e.snapshot.OnFailActive, step.ID)
	e.recordLocked("container_done", step.ID, "")
}

func (e *ExecutionEngine) skipNormalOnFailLocked(steps []*hopspec.Step) {
	for _, step := range steps {
		if step.Type == hopspec.StepOnFail && e.snapshot.StepStates[step.ID] == StepPending {
			e.snapshot.StepStates[step.ID] = StepSkipped
		}
	}
}

func (e *ExecutionEngine) stepsFinished(steps []*hopspec.Step) bool {
	for _, step := range steps {
		state := e.snapshot.StepStates[step.ID]
		if state == StepDone || state == StepSkipped {
			continue
		}
		parentID := ""
		if index := strings.LastIndex(step.ID, "."); index >= 0 {
			parentID = step.ID[:index]
		}
		if state == StepFailed && e.snapshot.OnFailActive[parentID] {
			continue
		}
		return false
	}
	return true
}

func (e *ExecutionEngine) propagateFailureLocked(step hopspec.Step) {
	failure, ok := e.snapshot.Failures[step.ID]
	if !ok {
		failure = Failure{Kind: FailureError, Reason: "step " + step.ID + " failed"}
	}
	e.snapshot.TerminalFailure = &failure
	e.snapshot.TerminalState = TerminalFailed
}

func (e *ExecutionEngine) failStepLocked(step hopspec.Step, failure Failure) {
	if step.ID != "" {
		e.snapshot.StepStates[step.ID] = StepFailed
		e.snapshot.Failures[step.ID] = failure
	}
	e.snapshot.TerminalFailure = &failure
	e.snapshot.TerminalState = TerminalFailed
	e.recordLocked("step_failed", step.ID, failure.Reason)
}

func (e *ExecutionEngine) isWorkStep(stepType hopspec.StepType) bool {
	return stepType == hopspec.StepReason || stepType == hopspec.StepAct || stepType == hopspec.StepCheck
}

func (e *ExecutionEngine) firstRunningWorkStep() (hopspec.Step, bool) {
	for _, step := range e.snapshot.Spec.AllSteps() {
		if e.snapshot.StepStates[step.ID] == StepRunning && e.isWorkStep(step.Type) {
			return step, true
		}
	}
	return hopspec.Step{}, false
}

func valueSlice(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	if items, ok := value.([]any); ok {
		return items, true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Array && reflected.Kind() != reflect.Slice {
		return nil, false
	}
	items := make([]any, reflected.Len())
	for index := range items {
		items[index] = reflected.Index(index).Interface()
	}
	return items, true
}

func isStepAncestor(ancestor, id string) bool {
	return len(ancestor) < len(id) && strings.HasPrefix(id, ancestor+".")
}
