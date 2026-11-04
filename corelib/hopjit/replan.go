package hopjit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func (e *ExecutionEngine) Replan(ctx context.Context, subtaskID, stepsMarkdown string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshot == nil {
		return ErrRunNotInitialized
	}
	if e.snapshot.TerminalState != "" {
		return errors.New("terminal run cannot be replanned")
	}
	if e.snapshot.AdaptiveNeeded != subtaskID {
		return fmt.Errorf("INVALID_STATE: subtask %s is not awaiting replan", subtaskID)
	}
	container := e.stepPointerByID(subtaskID)
	if container == nil || container.Type != hopspec.StepSubtask && container.Type != hopspec.StepCase {
		return fmt.Errorf("INVALID_STEP_ID: replan target %s is not subtask/case", subtaskID)
	}
	if e.snapshot.ReplanCounters[subtaskID] >= 3 {
		return errors.New("REPLAN_LIMIT_EXCEEDED: subtask has exceeded 3 replan attempts")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(stepsMarkdown))))
	if e.snapshot.ReplanDigests[subtaskID] == digest {
		return errors.New("REPLAN_DUPLICATE: replacement is identical to the previous plan")
	}
	parsed, parseErrs := hopspec.Parse("# Spec: Replan\nGoal: replan\n## Steps\n" + stepsMarkdown)
	if len(parseErrs) > 0 {
		return errors.Join(parseErrs...)
	}
	if len(parsed.Steps) == 0 {
		return errors.New("REPLAN_EMPTY: at least one replacement step is required")
	}
	replacement := normalizeReplanSteps(parsed.Steps, container.ID)
	if err := validateReplanContract(container, replacement); err != nil {
		return err
	}
	if err := e.validateReplanSteps(container, replacement); err != nil {
		return err
	}
	return e.mutateLocked(ctx, func() error {
		e.cancelInflightLocked(ctx, "replan "+subtaskID)
		for _, child := range container.Children {
			e.removeStepStateLocked(child)
		}
		container.Children = replacement
		for _, child := range replacement {
			e.initializeStepStateLocked(child)
		}
		e.snapshot.StepStates[container.ID] = StepRunning
		e.snapshot.AdaptiveNeeded = ""
		e.snapshot.Pause = nil
		delete(e.snapshot.OnFailActive, container.ID)
		e.skipNormalOnFailLocked(container.Children)
		e.snapshot.ReplanCounters[container.ID]++
		e.snapshot.ReplanDigests[container.ID] = digest
		e.recordLocked("replan_submitted", container.ID, fmt.Sprintf("steps=%d", len(replacement)))
		return nil
	})
}

func normalizeReplanSteps(steps []*hopspec.Step, containerID string) []*hopspec.Step {
	for _, step := range steps {
		normalizeReplanStep(step, containerID+".")
	}
	return steps
}

func normalizeReplanStep(step *hopspec.Step, prefix string) {
	if !strings.HasPrefix(step.ID, prefix) {
		leaf := step.ID
		if index := strings.LastIndex(leaf, "."); index >= 0 {
			leaf = leaf[index+1:]
		}
		step.ID = prefix + leaf
	}
	for _, child := range step.Children {
		normalizeReplanStep(child, step.ID+".")
	}
}

func validateReplanContract(container *hopspec.Step, replacement []*hopspec.Step) error {
	produced := map[string]bool{}
	var inspect func([]*hopspec.Step) error
	inspect = func(steps []*hopspec.Step) error {
		for _, step := range steps {
			if step.Type == hopspec.StepCommit {
				return fmt.Errorf("REPLAN_COMMIT_FORBIDDEN: dynamic step %s cannot introduce commit", step.ID)
			}
			for _, output := range step.Outputs {
				produced[output.Name] = true
			}
			if err := inspect(step.Children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := inspect(replacement); err != nil {
		return err
	}
	for _, output := range container.Outputs {
		if !produced[output.Name] {
			return fmt.Errorf("REPLAN_OUTPUT_MISSING: replacement does not produce %s", output.Name)
		}
	}
	originalFinal := finalChecks(container.Children)
	replacementFinal := finalChecks(replacement)
	if len(originalFinal) != len(replacementFinal) {
		return errors.New("REPLAN_FINAL_CHECK_REQUIRED: check final must be preserved")
	}
	for index := range originalFinal {
		if finalCheckSignature(originalFinal[index]) != finalCheckSignature(replacementFinal[index]) {
			return errors.New("REPLAN_FINAL_CHECK_CHANGED: check final cannot be rewritten")
		}
	}
	return nil
}

func (e *ExecutionEngine) validateReplanSteps(container *hopspec.Step, replacement []*hopspec.Step) error {
	declarations := map[string]hopspec.VariableDecl{}
	for _, decl := range e.snapshot.Spec.Inputs {
		declarations[decl.Name] = decl
	}
	for _, decl := range e.snapshot.Spec.Outputs {
		declarations[decl.Name] = decl
	}
	for _, step := range e.snapshot.Spec.AllSteps() {
		for _, decl := range step.Outputs {
			if _, exists := declarations[decl.Name]; !exists && decl.Type != "" {
				declarations[decl.Name] = decl
			}
		}
	}
	inputs := make([]hopspec.VariableDecl, 0, len(declarations))
	for _, decl := range declarations {
		inputs = append(inputs, decl)
	}
	candidate := *container
	candidate.Children = replacement
	roots := []*hopspec.Step{&candidate}
	if candidate.Type == hopspec.StepCase {
		parentID := ""
		if index := strings.LastIndex(candidate.ID, "."); index >= 0 {
			parentID = candidate.ID[:index]
		}
		branch := &hopspec.Step{ID: parentID, Type: hopspec.StepBranch, Children: roots}
		roots = []*hopspec.Step{branch}
	}
	errs := hopspec.Validate(hopspec.SpecAST{Title: "Replan", Goal: "replan", Inputs: inputs, Steps: roots})
	return errors.Join(errs...)
}

func (e *ExecutionEngine) stepPointerByID(id string) *hopspec.Step {
	var find func([]*hopspec.Step) *hopspec.Step
	find = func(steps []*hopspec.Step) *hopspec.Step {
		for _, step := range steps {
			if step.ID == id {
				return step
			}
			if found := find(step.Children); found != nil {
				return found
			}
		}
		return nil
	}
	return find(e.snapshot.Spec.Steps)
}

func (e *ExecutionEngine) initializeStepStateLocked(step *hopspec.Step) {
	e.snapshot.StepStates[step.ID] = StepPending
	for _, child := range step.Children {
		e.initializeStepStateLocked(child)
	}
}

func (e *ExecutionEngine) removeStepStateLocked(step *hopspec.Step) {
	delete(e.snapshot.StepStates, step.ID)
	delete(e.snapshot.LoopCounters, step.ID)
	delete(e.snapshot.LoopItems, step.ID)
	delete(e.snapshot.OnFailActive, step.ID)
	for _, child := range step.Children {
		e.removeStepStateLocked(child)
	}
}

func finalChecks(steps []*hopspec.Step) []*hopspec.Step {
	var out []*hopspec.Step
	for _, step := range steps {
		if step.Type == hopspec.StepCheck && step.Final() {
			out = append(out, step)
		}
		out = append(out, finalChecks(step.Children)...)
	}
	return out
}

func finalCheckSignature(step *hopspec.Step) string {
	var outputs []string
	for _, output := range step.Outputs {
		outputs = append(outputs, output.Name+"%"+output.Type)
	}
	return step.Summary + "\n" + step.Instruction + "\n" + strings.Join(outputs, ",")
}
