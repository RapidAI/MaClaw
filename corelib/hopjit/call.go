package hopjit

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func (e *ExecutionEngine) executeCallLocked(ctx context.Context, step *hopspec.Step) advanceResult {
	if step.Call == nil || step.Call.Callee == "" {
		e.failStepLocked(*step, Failure{Kind: FailureDeterministic, Reason: "CALL_TARGET_REQUIRED: " + step.ID})
		return advanceResult{}
	}
	child := NewExecutionEngine(e.store, WithToolProvider(e.tools), WithSpecProvider(e.specs))
	child.caller = e.caller
	state, resumed := e.snapshot.Calls[step.ID]
	if resumed {
		if err := child.Resume(ctx, state.RunID); err != nil {
			e.failCallLocked(step, Failure{Kind: FailureError, Reason: err.Error()})
			return advanceResult{}
		}
	} else {
		if e.specs == nil {
			e.failCallLocked(step, Failure{Kind: FailureDeterministic, Reason: "SPEC_PROVIDER_UNAVAILABLE"})
			return advanceResult{}
		}
		markdown, err := e.specs.LoadSpec(ctx, step.Call.Callee)
		if err != nil {
			e.failCallLocked(step, Failure{Kind: FailureLackOfInfo, Reason: err.Error()})
			return advanceResult{}
		}
		inputs := map[string]any{}
		for target, source := range step.Call.Inputs {
			if value, ok := e.snapshot.Variables[source]; ok && value.Assigned {
				inputs[target] = value.Value
			}
		}
		init, err := child.Init(ctx, markdown, inputs)
		if err != nil {
			e.failCallLocked(step, Failure{Kind: FailureError, Reason: err.Error()})
			return advanceResult{}
		}
		state = CallState{RunID: init.RunID, Callee: step.Call.Callee}
		e.snapshot.Calls[step.ID] = state
		e.snapshot.StepStates[step.ID] = StepRunning
		e.recordLocked("call_started", step.ID, state.RunID)
	}
	result, err := DriveReuse(ctx, child, child.caller)
	if err != nil {
		e.failCallLocked(step, Failure{Kind: FailureError, Reason: err.Error()})
		return advanceResult{}
	}
	switch result.Status {
	case NextPaused:
		pause := *result.Pause
		pause.ChildRunID = state.RunID
		e.snapshot.Pause = &pause
		e.recordLocked("call_paused", step.ID, state.RunID)
		return advanceResult{kind: advancePaused, step: *step}
	case NextCompleted:
		for callerName, calleeName := range step.Call.Outputs {
			value, ok := result.Outputs[calleeName]
			if !ok {
				e.failCallLocked(step, Failure{Kind: FailureError, Reason: fmt.Sprintf("CALL_OUTPUT_MISSING: %s", calleeName)})
				return advanceResult{}
			}
			e.snapshot.Variables[callerName] = VariableValue{Value: value, Assigned: true}
		}
		e.snapshot.StepStates[step.ID] = StepDone
		e.recordLocked("call_completed", step.ID, state.RunID)
		return advanceResult{kind: advanceRestart, step: *step}
	case NextFailed, NextAborted:
		reason := "child run " + state.RunID + " " + string(result.Status)
		if result.Failure != nil {
			reason += ": " + result.Failure.Reason
		}
		e.failCallLocked(step, Failure{Kind: FailureError, Reason: reason})
		return advanceResult{}
	default:
		e.failCallLocked(step, Failure{Kind: FailureError, Reason: "CALL_INVALID_CHILD_STATUS: " + string(result.Status)})
		return advanceResult{}
	}
}

func (e *ExecutionEngine) failCallLocked(step *hopspec.Step, failure Failure) {
	e.snapshot.StepStates[step.ID] = StepFailed
	e.recordLocked("call_failed", step.ID, failure.Reason)
	e.handleFailureLocked(step.ID, failure)
}

func (e *ExecutionEngine) completeChildPause(ctx context.Context, outputs map[string]any) error {
	childRunID := e.snapshot.Pause.ChildRunID
	child := NewExecutionEngine(e.store, WithToolProvider(e.tools), WithSpecProvider(e.specs))
	if err := child.Resume(ctx, childRunID); err != nil {
		return err
	}
	snapshot, err := child.Snapshot()
	if err != nil {
		return err
	}
	if snapshot.Pause == nil {
		return fmt.Errorf("child run %s is not paused", childRunID)
	}
	if err := child.Complete(ctx, snapshot.Pause.StepID, outputs); err != nil {
		return err
	}
	return e.mutateLocked(ctx, func() error {
		e.snapshot.Pause = nil
		for id, inflight := range e.snapshot.Inflight {
			if inflight.ChildRunID == childRunID {
				inflight.Status = "inflight"
				e.snapshot.Inflight[id] = inflight
			}
		}
		e.recordLocked("call_resumed", snapshot.Pause.StepID, childRunID)
		return nil
	})
}
