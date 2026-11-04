package hopjit

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func (e *ExecutionEngine) handleFailureLocked(stepID string, failure Failure) {
	e.cancelInflightLocked(e.ctx, "step failed: "+stepID)
	e.snapshot.Failures[stepID] = failure
	containers := e.failureContainers(stepID)
	for _, container := range containers {
		if e.failureInsideActiveOnFail(stepID, container.ID) {
			continue
		}
		annealed := e.containerHasCommitted(container)
		if !annealed && failure.Kind != FailureDeterministic && container.Adaptive {
			e.snapshot.TerminalState = ""
			e.snapshot.TerminalFailure = nil
			e.snapshot.Pause = nil
			e.snapshot.AdaptiveNeeded = container.ID
			e.recordLocked("adaptive_needed", container.ID, failure.Reason)
			return
		}
		if !annealed && failure.Kind != FailureDeterministic {
			limit := 3
			if container.Retry != nil {
				limit = *container.Retry
			}
			attempt := e.snapshot.RetryCounters[container.ID]
			if attempt < limit {
				attempt++
				e.snapshot.RetryCounters[container.ID] = attempt
				e.snapshot.RetryHistory[container.ID] = append(e.snapshot.RetryHistory[container.ID], RetryRecord{
					Attempt: attempt, Failure: failure, StepsTried: e.terminalDescendantIDs(container),
				})
				e.resetContainerForRetryLocked(container)
				e.recordLocked("container_retry", container.ID, failure.Reason)
				return
			}
		}
		if e.activateOnFailLocked(container, failure) {
			return
		}
	}
	e.snapshot.TerminalFailure = &failure
	e.snapshot.TerminalState = TerminalFailed
	e.recordLocked("run_failed", stepID, failure.Reason)
}

func (e *ExecutionEngine) resetContainerForRetryLocked(container *hopspec.Step) {
	e.snapshot.TerminalState = ""
	e.snapshot.TerminalFailure = nil
	e.snapshot.Pause = nil
	delete(e.snapshot.OnFailActive, container.ID)
	e.snapshot.StepStates[container.ID] = StepRunning
	for _, child := range container.Children {
		e.resetStepForNextIteration(child)
	}
	e.skipNormalOnFailLocked(container.Children)
}

func (e *ExecutionEngine) activateOnFailLocked(container *hopspec.Step, failure Failure) bool {
	var fallback *hopspec.Step
	for _, child := range container.Children {
		if child.Type == hopspec.StepOnFail {
			fallback = child
			break
		}
	}
	if fallback == nil {
		return false
	}
	for _, child := range container.Children {
		if child.ID != fallback.ID && e.snapshot.StepStates[child.ID] == StepPending {
			e.snapshot.StepStates[child.ID] = StepSkipped
		}
	}
	e.snapshot.TerminalState = ""
	e.snapshot.TerminalFailure = nil
	e.snapshot.Pause = nil
	e.snapshot.OnFailActive[container.ID] = true
	e.snapshot.StepStates[container.ID] = StepRunning
	e.snapshot.StepStates[fallback.ID] = StepPending
	e.recordLocked("on_fail_started", container.ID, failure.Reason)
	return true
}

func (e *ExecutionEngine) failureContainers(stepID string) []*hopspec.Step {
	path := e.stepPath(stepID)
	containers := make([]*hopspec.Step, 0, len(path))
	for index := len(path) - 1; index >= 0; index-- {
		if path[index].Type == hopspec.StepSubtask || path[index].Type == hopspec.StepCase {
			containers = append(containers, path[index])
		}
	}
	return containers
}

func (e *ExecutionEngine) stepPath(stepID string) []*hopspec.Step {
	var visit func([]*hopspec.Step, []*hopspec.Step) []*hopspec.Step
	visit = func(steps []*hopspec.Step, prefix []*hopspec.Step) []*hopspec.Step {
		for _, step := range steps {
			path := append(append([]*hopspec.Step(nil), prefix...), step)
			if step.ID == stepID {
				return path
			}
			if found := visit(step.Children, path); found != nil {
				return found
			}
		}
		return nil
	}
	return visit(e.snapshot.Spec.Steps, nil)
}

func (e *ExecutionEngine) terminalDescendantIDs(container *hopspec.Step) []string {
	var ids []string
	for _, step := range container.Children {
		state := e.snapshot.StepStates[step.ID]
		if state == StepDone || state == StepFailed {
			ids = append(ids, step.ID)
		}
	}
	return ids
}

func (e *ExecutionEngine) containerHasCommitted(container *hopspec.Step) bool {
	for stepID := range e.snapshot.CommittedSteps {
		if stepID == container.ID || strings.HasPrefix(stepID, container.ID+".") {
			return true
		}
	}
	return false
}

func (e *ExecutionEngine) failureInsideActiveOnFail(stepID, containerID string) bool {
	if !e.snapshot.OnFailActive[containerID] {
		return false
	}
	prefix := containerID + "."
	return strings.HasPrefix(stepID, prefix)
}
