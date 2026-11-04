package hopjit

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func (e *ExecutionEngine) executeBodyStepLocked(ctx context.Context, step *hopspec.Step) {
	e.snapshot.StepStates[step.ID] = StepRunning
	e.recordLocked("body_started", step.ID, string(step.Type))
	localVariables := map[string]VariableValue{}
	for _, name := range step.Inputs {
		if value, ok := e.snapshot.Variables[name]; ok {
			localVariables[name] = value
		}
	}
	err := executeHopPython(ctx, step.Body, localVariables, e.tools)
	if err == nil {
		outputs := map[string]any{}
		for _, decl := range step.Outputs {
			if value, ok := localVariables[decl.Name]; ok && value.Assigned {
				outputs[decl.Name] = value.Value
			}
		}
		err = validateStepOutputs(*step, outputs)
		if err == nil {
			for _, decl := range step.Outputs {
				e.snapshot.Variables[decl.Name] = VariableValue{Value: outputs[decl.Name], Assigned: true}
			}
		}
	}
	if err != nil {
		failure := Failure{Kind: FailureError, Reason: fmt.Sprintf("BODY_FAILED: %v", err)}
		e.snapshot.StepStates[step.ID] = StepFailed
		e.recordLocked("step_failed", step.ID, failure.Reason)
		e.handleFailureLocked(step.ID, failure)
		return
	}
	e.snapshot.StepStates[step.ID] = StepDone
	if step.Type == hopspec.StepCommit {
		e.snapshot.CommittedSteps[step.ID] = true
	}
	e.recordLocked("step_done", step.ID, "hop_python")
	if step.Type == hopspec.StepCheck {
		outputs := map[string]any{}
		for _, decl := range step.Outputs {
			outputs[decl.Name] = e.snapshot.Variables[decl.Name].Value
		}
		if verdict, ok := checkVerdict(*step, outputs); ok && !verdict {
			e.handleFailureLocked(step.ID, Failure{Kind: FailureError, Reason: checkFailureReason(*step, outputs)})
		}
	}
}
