package hopjit

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func (e *ExecutionEngine) startParallelCallsLocked(ctx context.Context, steps []*hopspec.Step) error {
	for _, step := range steps {
		if !step.Parallel || e.snapshot.StepStates[step.ID] != StepPending {
			continue
		}
		var err error
		switch step.Type {
		case hopspec.StepCall:
			err = e.startParallelCallLocked(ctx, step)
		case hopspec.StepSubtask, hopspec.StepCase:
			err = e.startParallelSubtaskLocked(ctx, step)
		default:
			continue
		}
		if err != nil {
			return err
		}
		inflight := e.snapshot.Inflight[step.ID]
		inflight.ContainerID = parentHopStepID(step.ID)
		inflight.Iteration = e.iterationForStep(step.ID)
		key := step.ID
		if inflight.Iteration > 0 {
			delete(e.snapshot.Inflight, step.ID)
			key = fmt.Sprintf("%s#%d", step.ID, inflight.Iteration)
			if ch := e.inflightResult[step.ID]; ch != nil {
				delete(e.inflightResult, step.ID)
				e.inflightResult[key] = ch
			}
			if cancel := e.inflightCancel[step.ID]; cancel != nil {
				delete(e.inflightCancel, step.ID)
				e.inflightCancel[key] = cancel
			}
		}
		e.snapshot.Inflight[key] = inflight
	}
	return nil
}

func (e *ExecutionEngine) startParallelSubtaskLocked(ctx context.Context, step *hopspec.Step) error {
	children, err := cloneHopSteps(step.Children)
	if err != nil {
		return err
	}
	inputs := map[string]any{}
	declarations := make([]hopspec.VariableDecl, 0, len(step.Inputs))
	for _, name := range step.Inputs {
		if value, ok := e.snapshot.Variables[name]; ok && value.Assigned {
			inputs[name] = value.Value
		}
		declarations = append(declarations, hopspec.VariableDecl{Name: name, Type: "any"})
	}
	outputs := append([]hopspec.VariableDecl(nil), step.Outputs...)
	if loop := e.loopForStep(step.ID); loop != nil && loop.ForEach != nil && loop.ForEach.CollectItem != "" {
		found := false
		for _, output := range outputs {
			if output.Name == loop.ForEach.CollectItem {
				found = true
			}
		}
		if !found {
			outputs = append(outputs, hopspec.VariableDecl{Name: loop.ForEach.CollectItem, Type: "any"})
		}
	}
	spec := hopspec.SpecAST{
		Title: step.Summary, ID: step.ID, Goal: e.snapshot.Spec.Goal,
		Inputs: declarations, Outputs: outputs, Steps: children,
	}
	child := NewExecutionEngine(e.store, WithToolProvider(e.tools), WithSpecProvider(e.specs))
	child.caller = e.caller
	init, err := child.InitAST(ctx, spec, inputs)
	if err != nil {
		return err
	}
	e.startParallelDriverLocked(ctx, step, child, init.RunID)
	return nil
}

func (e *ExecutionEngine) startParallelDriverLocked(ctx context.Context, step *hopspec.Step, child *ExecutionEngine, runID string) {
	childCtx, cancel := context.WithCancel(ctx)
	resultCh := make(chan ReuseResult, 1)
	e.inflightResult[step.ID] = resultCh
	e.inflightCancel[step.ID] = cancel
	e.snapshot.Inflight[step.ID] = InflightCall{StepID: step.ID, ChildRunID: runID, Status: "inflight"}
	e.snapshot.StepStates[step.ID] = StepRunning
	e.recordLocked("parallel_dispatched", step.ID, runID)
	go func() {
		result, err := DriveReuse(childCtx, child, child.caller)
		if err != nil {
			result = ReuseResult{RunID: runID, Status: NextFailed, Failure: &Failure{Kind: FailureError, Reason: err.Error()}}
		}
		resultCh <- result
	}()
}

func cloneHopSteps(steps []*hopspec.Step) ([]*hopspec.Step, error) {
	encoded, err := json.Marshal(steps)
	if err != nil {
		return nil, err
	}
	var clone []*hopspec.Step
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, err
	}
	return clone, nil
}

func parentHopStepID(id string) string {
	if index := strings.LastIndex(id, "."); index >= 0 {
		return id[:index]
	}
	return ""
}

func (e *ExecutionEngine) iterationForStep(id string) int {
	if loop := e.loopForStep(id); loop != nil {
		return e.snapshot.LoopCounters[loop.ID]
	}
	return 0
}

func (e *ExecutionEngine) loopForStep(id string) *hopspec.Step {
	var best *hopspec.Step
	for _, step := range e.snapshot.Spec.AllSteps() {
		if step.Type == hopspec.StepLoop && isStepAncestor(step.ID, id) {
			if best == nil || len(step.ID) > len(best.ID) {
				copy := step
				best = &copy
			}
		}
	}
	return best
}

func (e *ExecutionEngine) startParallelCallLocked(ctx context.Context, step *hopspec.Step) error {
	if step.Call == nil || step.Call.Callee == "" || e.specs == nil {
		return fmt.Errorf("parallel call %s has no governed spec provider", step.ID)
	}
	markdown, err := e.specs.LoadSpec(ctx, step.Call.Callee)
	if err != nil {
		return err
	}
	inputs := map[string]any{}
	for target, source := range step.Call.Inputs {
		if value, ok := e.snapshot.Variables[source]; ok && value.Assigned {
			inputs[target] = value.Value
		}
	}
	child := NewExecutionEngine(e.store, WithToolProvider(e.tools), WithSpecProvider(e.specs))
	child.caller = e.caller
	init, err := child.Init(ctx, markdown, inputs)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	resultCh := make(chan ReuseResult, 1)
	e.inflightResult[step.ID] = resultCh
	e.inflightCancel[step.ID] = cancel
	e.snapshot.Calls[step.ID] = CallState{RunID: init.RunID, Callee: step.Call.Callee}
	e.snapshot.Inflight[step.ID] = InflightCall{StepID: step.ID, ChildRunID: init.RunID, Status: "inflight"}
	e.snapshot.StepStates[step.ID] = StepRunning
	e.recordLocked("parallel_dispatched", step.ID, init.RunID)
	go func() {
		result, err := DriveReuse(childCtx, child, child.caller)
		if err != nil {
			result = ReuseResult{RunID: init.RunID, Status: NextFailed, Failure: &Failure{Kind: FailureError, Reason: err.Error()}}
		}
		resultCh <- result
	}()
	return nil
}

func (e *ExecutionEngine) reapInflightLocked(ctx context.Context) {
	ids := make([]string, 0, len(e.snapshot.Inflight))
	for id := range e.snapshot.Inflight {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		inflight := e.snapshot.Inflight[id]
		if inflight.Status == "paused" {
			continue
		}
		result, ok := e.readInflightResult(ctx, id, inflight)
		if !ok {
			continue
		}
		step := e.stepPointerByID(inflight.StepID)
		if step == nil {
			continue
		}
		switch result.Status {
		case NextPaused:
			inflight.Status = "paused"
			e.snapshot.Inflight[id] = inflight
			if result.Pause != nil {
				pause := *result.Pause
				pause.ChildRunID = inflight.ChildRunID
				e.snapshot.Pause = &pause
			}
		case NextCompleted:
			if err := e.collectParallelOutputLocked(step, inflight, result); err != nil {
				e.cancelInflightLocked()
				e.failCallLocked(step, Failure{Kind: FailureError, Reason: err.Error()})
				return
			}
			e.markParallelSubtreeDone(step)
			e.snapshot.StepStates[step.ID] = StepDone
			delete(e.snapshot.Inflight, id)
			e.clearInflightChannel(id)
			e.recordLocked("parallel_reaped", id, inflight.ChildRunID)
		case NextFailed, NextAborted:
			delete(e.snapshot.Inflight, id)
			e.clearInflightChannel(id)
			e.cancelInflightLocked()
			reason := "parallel child failed"
			if result.Failure != nil {
				reason = result.Failure.Reason
			}
			e.failCallLocked(step, Failure{Kind: FailureError, Reason: reason})
			return
		}
	}
}

func (e *ExecutionEngine) collectParallelOutputLocked(step *hopspec.Step, inflight InflightCall, result ReuseResult) error {
	loop := e.loopForStep(step.ID)
	collectName := ""
	if loop != nil && loop.ForEach != nil {
		collectName = loop.ForEach.CollectItem
	}
	write := func(name string, value any) {
		if name == collectName {
			return
		}
		e.snapshot.Variables[name] = VariableValue{Value: value, Assigned: true}
	}
	if step.Type == hopspec.StepCall {
		for caller, callee := range step.Call.Outputs {
			value, exists := result.Outputs[callee]
			if !exists {
				return fmt.Errorf("CALL_OUTPUT_MISSING: %s", callee)
			}
			write(caller, value)
		}
	} else {
		for _, output := range step.Outputs {
			value, exists := result.Outputs[output.Name]
			if !exists {
				return fmt.Errorf("CALL_OUTPUT_MISSING: %s", output.Name)
			}
			write(output.Name, value)
		}
	}
	if collectName != "" {
		value, exists := result.Outputs[collectName]
		if !exists {
			return fmt.Errorf("COLLECT_OUTPUT_MISSING: %s", collectName)
		}
		if e.snapshot.LoopResults[loop.ID] == nil {
			e.snapshot.LoopResults[loop.ID] = map[string]any{}
		}
		e.snapshot.LoopResults[loop.ID][fmt.Sprint(inflight.Iteration)] = value
	}
	return nil
}

func (e *ExecutionEngine) readInflightResult(ctx context.Context, id string, inflight InflightCall) (ReuseResult, bool) {
	if ch := e.inflightResult[id]; ch != nil {
		select {
		case result := <-ch:
			return result, true
		default:
			return ReuseResult{}, false
		}
	}
	child := NewExecutionEngine(e.store, WithToolProvider(e.tools), WithSpecProvider(e.specs))
	snapshot, err := child.store.LoadSnapshot(ctx, inflight.ChildRunID)
	if err != nil {
		return ReuseResult{}, false
	}
	if snapshot.TerminalState != "" {
		return reuseResultFromSnapshot(snapshot), true
	}
	if snapshot.Pause != nil {
		return ReuseResult{RunID: snapshot.RunID, Status: NextPaused, Pause: snapshot.Pause, Snapshot: snapshot}, true
	}
	if err := child.Resume(ctx, inflight.ChildRunID); err != nil {
		return ReuseResult{}, false
	}
	child.caller = e.caller
	childCtx, cancel := context.WithCancel(ctx)
	ch := make(chan ReuseResult, 1)
	e.inflightResult[id] = ch
	e.inflightCancel[id] = cancel
	go func() {
		result, err := DriveReuse(childCtx, child, child.caller)
		if err != nil {
			result = ReuseResult{RunID: inflight.ChildRunID, Status: NextFailed, Failure: &Failure{Kind: FailureError, Reason: err.Error()}}
		}
		ch <- result
	}()
	return ReuseResult{}, false
}

func reuseResultFromSnapshot(snapshot EngineSnapshot) ReuseResult {
	status := NextRunning
	switch snapshot.TerminalState {
	case TerminalCompleted:
		status = NextCompleted
	case TerminalFailed:
		status = NextFailed
	case TerminalAborted:
		status = NextAborted
	}
	outputs := map[string]any{}
	for _, decl := range snapshot.Spec.Outputs {
		if value, ok := snapshot.Variables[decl.Name]; ok && value.Assigned {
			outputs[decl.Name] = value.Value
		}
	}
	return ReuseResult{RunID: snapshot.RunID, Status: status, Outputs: outputs, Failure: snapshot.TerminalFailure, Snapshot: snapshot}
}

func (e *ExecutionEngine) markParallelSubtreeDone(step *hopspec.Step) {
	for _, child := range step.Children {
		e.markParallelSubtreeDone(child)
		e.snapshot.StepStates[child.ID] = StepDone
	}
}

func (e *ExecutionEngine) clearInflightChannel(id string) {
	delete(e.inflightResult, id)
	delete(e.inflightCancel, id)
}

func (e *ExecutionEngine) hasInflightStep(stepID string) bool {
	for _, inflight := range e.snapshot.Inflight {
		if inflight.StepID == stepID {
			return true
		}
	}
	return false
}

func (e *ExecutionEngine) cancelInflightLocked() {
	for id, cancel := range e.inflightCancel {
		cancel()
		delete(e.inflightCancel, id)
		delete(e.inflightResult, id)
	}
}
