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
		switch e.snapshot.StepStates[step.ID] {
		case StepDone, StepSkipped:
			continue
		case StepPending:
			if !step.Parallel {
				return nil
			}
		default:
			return nil
		}
		var err error
		switch step.Type {
		case hopspec.StepCall:
			err = e.startParallelCallLocked(ctx, step)
		case hopspec.StepSubtask, hopspec.StepCase:
			err = e.startParallelSubtaskLocked(ctx, step)
		default:
			return nil
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
	child := e.newChildEngine()
	child.caller = e.caller
	init, err := child.InitAST(ctx, spec, inputs)
	if err != nil {
		return err
	}
	e.startParallelDriverLocked(ctx, step, child, init.RunID)
	return nil
}

func (e *ExecutionEngine) startParallelDriverLocked(ctx context.Context, step *hopspec.Step, child *ExecutionEngine, runID string) {
	e.snapshot.Inflight[step.ID] = InflightCall{StepID: step.ID, ChildRunID: runID, Status: "inflight"}
	e.snapshot.StepStates[step.ID] = StepRunning
	e.recordLocked("parallel_dispatched", step.ID, runID)
	e.launchInflightDriverLocked(ctx, step.ID, child, runID)
}

func (e *ExecutionEngine) launchInflightDriverLocked(ctx context.Context, id string, child *ExecutionEngine, runID string) {
	if cancel := e.inflightCancel[id]; cancel != nil {
		cancel()
	}
	childCtx, cancel := context.WithCancel(ctx)
	resultCh := make(chan ReuseResult, 1)
	e.inflightResult[id] = resultCh
	e.inflightCancel[id] = cancel
	child.caller = e.caller
	go func() {
		result, err := DriveReuse(childCtx, child, child.caller)
		if err != nil {
			result = ReuseResult{RunID: runID, Status: NextFailed, Failure: &Failure{Kind: FailureError, Reason: err.Error()}}
		}
		resultCh <- result
		e.notifyInflight()
	}()
}

func (e *ExecutionEngine) notifyInflight() {
	select {
	case e.inflightNotify <- struct{}{}:
	default:
	}
}

func (e *ExecutionEngine) waitForInflight(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.inflightNotify:
		return nil
	}
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
	child, err := e.newCallEngine()
	if err != nil {
		return err
	}
	child.caller = e.caller
	init, err := child.Init(ctx, markdown, inputs)
	if err != nil {
		return err
	}
	e.snapshot.Calls[step.ID] = CallState{RunID: init.RunID, Callee: step.Call.Callee}
	e.snapshot.Inflight[step.ID] = InflightCall{StepID: step.ID, ChildRunID: init.RunID, Status: "inflight"}
	e.snapshot.StepStates[step.ID] = StepRunning
	e.recordLocked("parallel_dispatched", step.ID, init.RunID)
	e.launchInflightDriverLocked(ctx, step.ID, child, init.RunID)
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
			if e.snapshot.Pause != nil && e.snapshot.Pause.ChildRunID == inflight.ChildRunID {
				continue
			}
			inflight.Status = "inflight"
			e.snapshot.Inflight[id] = inflight
		}
		step := e.stepPointerByID(inflight.StepID)
		if step == nil {
			e.failStepLocked(hopspec.Step{}, Failure{Kind: FailureError, Reason: "INFLIGHT_STEP_MISSING: " + inflight.StepID})
			return
		}
		result, ok, err := e.readInflightResult(ctx, id, inflight)
		if err != nil {
			e.failCallLocked(step, Failure{Kind: FailureError, Reason: err.Error()})
			return
		}
		if !ok {
			continue
		}
		switch result.Status {
		case NextPaused:
			if result.Pause == nil {
				e.failCallLocked(step, Failure{Kind: FailureError, Reason: "PARALLEL_PAUSE_MISSING: " + id})
				return
			}
			inflight.Status = "paused"
			e.snapshot.Inflight[id] = inflight
			e.clearInflightChannel(id)
			pause := *result.Pause
			pause.ChildRunID = inflight.ChildRunID
			e.snapshot.Pause = &pause
			return
		case NextCompleted:
			if err := e.collectParallelOutputLocked(step, inflight, result); err != nil {
				e.failCallLocked(step, Failure{Kind: FailureError, Reason: err.Error()})
				return
			}
			delete(e.snapshot.Inflight, id)
			e.clearInflightChannel(id)
			if !e.hasInflightStep(step.ID) {
				e.markParallelSubtreeDone(step)
				e.snapshot.StepStates[step.ID] = StepDone
			}
			e.recordLocked("parallel_reaped", id, inflight.ChildRunID)
		case NextFailed, NextAborted:
			delete(e.snapshot.Inflight, id)
			e.clearInflightChannel(id)
			reason := "parallel child failed"
			if result.Failure != nil && strings.TrimSpace(result.Failure.Reason) != "" {
				reason = result.Failure.Reason
			}
			e.failCallLocked(step, Failure{Kind: FailureError, Reason: reason})
			return
		default:
			e.failCallLocked(step, Failure{Kind: FailureError, Reason: "INFLIGHT_INVALID_CHILD_STATUS: " + string(result.Status)})
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
	var collected any
	hasCollected := false
	write := func(name string, value any) {
		if name == collectName {
			collected = value
			hasCollected = true
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
	if collectName != "" && hasCollected {
		if e.snapshot.LoopResults[loop.ID] == nil {
			e.snapshot.LoopResults[loop.ID] = map[string]any{}
		}
		e.snapshot.LoopResults[loop.ID][fmt.Sprint(inflight.Iteration)] = collected
	}
	return nil
}

func (e *ExecutionEngine) readInflightResult(ctx context.Context, id string, inflight InflightCall) (ReuseResult, bool, error) {
	if ch := e.inflightResult[id]; ch != nil {
		select {
		case result, open := <-ch:
			if !open {
				return ReuseResult{}, false, fmt.Errorf("INFLIGHT_CHANNEL_CLOSED: %s", id)
			}
			return result, true, nil
		default:
			return ReuseResult{}, false, nil
		}
	}
	child := e.newChildEngine()
	snapshot, err := child.store.LoadSnapshot(ctx, inflight.ChildRunID)
	if err != nil {
		return ReuseResult{}, false, fmt.Errorf("INFLIGHT_LOAD_FAILED: %s: %w", inflight.ChildRunID, err)
	}
	if snapshot.TerminalState != "" {
		return reuseResultFromSnapshot(snapshot), true, nil
	}
	if snapshot.Pause != nil {
		return ReuseResult{RunID: snapshot.RunID, Status: NextPaused, Pause: snapshot.Pause, Snapshot: snapshot}, true, nil
	}
	if err := child.Resume(ctx, inflight.ChildRunID); err != nil {
		return ReuseResult{}, false, fmt.Errorf("INFLIGHT_RESUME_FAILED: %s: %w", inflight.ChildRunID, err)
	}
	e.launchInflightDriverLocked(ctx, id, child, inflight.ChildRunID)
	return ReuseResult{}, false, nil
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
	if cancel := e.inflightCancel[id]; cancel != nil {
		cancel()
	}
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

func (e *ExecutionEngine) cancelInflightLocked(ctx context.Context, reason string) {
	if ctx == nil {
		ctx = context.Background()
	}
	runIDs := map[string]bool{}
	for _, inflight := range e.snapshot.Inflight {
		if inflight.ChildRunID != "" {
			runIDs[inflight.ChildRunID] = true
		}
	}
	for stepID, call := range e.snapshot.Calls {
		if call.RunID != "" && e.snapshot.StepStates[stepID] == StepRunning {
			runIDs[call.RunID] = true
		}
	}
	if e.snapshot.Pause != nil && e.snapshot.Pause.ChildRunID != "" {
		runIDs[e.snapshot.Pause.ChildRunID] = true
	}
	for id, cancel := range e.inflightCancel {
		cancel()
		delete(e.inflightCancel, id)
		delete(e.inflightResult, id)
	}
	e.snapshot.Inflight = map[string]InflightCall{}
	ids := make([]string, 0, len(runIDs))
	for id := range runIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, runID := range ids {
		child := e.newChildEngine()
		if err := child.Resume(context.WithoutCancel(ctx), runID); err != nil {
			e.recordLocked("child_abort_failed", "", runID+": "+err.Error())
			continue
		}
		snapshot, err := child.Snapshot()
		if err != nil {
			e.recordLocked("child_abort_failed", "", runID+": "+err.Error())
			continue
		}
		if snapshot.TerminalState != "" {
			continue
		}
		if err := child.Abort(context.WithoutCancel(ctx), reason); err != nil {
			e.recordLocked("child_abort_failed", "", runID+": "+err.Error())
		}
	}
}
