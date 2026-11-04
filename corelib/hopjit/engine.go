package hopjit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

type ExecutionEngine struct {
	mu             sync.Mutex
	store          PersistenceProvider
	snapshot       *EngineSnapshot
	now            func() time.Time
	tools          ToolProvider
	specs          SpecProvider
	caller         ReuseCaller
	ctx            context.Context
	inflightResult map[string]chan ReuseResult
	inflightCancel map[string]context.CancelFunc
}

func NewExecutionEngine(store PersistenceProvider, options ...EngineOption) *ExecutionEngine {
	if store == nil {
		store = NewMemoryPersistence()
	}
	engine := &ExecutionEngine{
		store: store, now: time.Now,
		inflightResult: map[string]chan ReuseResult{}, inflightCancel: map[string]context.CancelFunc{},
	}
	for _, option := range options {
		if option != nil {
			option(engine)
		}
	}
	return engine
}

func (e *ExecutionEngine) Init(ctx context.Context, markdown string, inputs map[string]any) (InitResult, error) {
	spec, parseErrs := hopspec.Parse(markdown)
	if len(parseErrs) > 0 {
		return InitResult{}, errors.Join(parseErrs...)
	}
	return e.InitAST(ctx, spec, inputs)
}

func (e *ExecutionEngine) InitAST(ctx context.Context, spec hopspec.SpecAST, inputs map[string]any) (InitResult, error) {
	if validationErrs := hopspec.Validate(spec); len(validationErrs) > 0 {
		return InitResult{}, errors.Join(validationErrs...)
	}
	for _, step := range spec.AllSteps() {
		if step.Body != "" {
			if err := validateHopPythonBody(step.Body); err != nil {
				return InitResult{}, fmt.Errorf("step %s: %w", step.ID, err)
			}
		}
	}
	for _, input := range spec.Inputs {
		if _, ok := inputs[input.Name]; !ok {
			return InitResult{}, fmt.Errorf("MISSING_INPUT: required input %s was not provided", input.Name)
		}
	}
	runID, err := newRunID()
	if err != nil {
		return InitResult{}, err
	}
	snapshot := &EngineSnapshot{
		FormatVersion:  SnapshotFormatVersion,
		RunID:          runID,
		Spec:           spec,
		StepStates:     map[string]StepStatus{},
		Variables:      map[string]VariableValue{},
		LoopCounters:   map[string]int{},
		LoopItems:      map[string][]any{},
		LoopResults:    map[string]map[string]any{},
		Calls:          map[string]CallState{},
		Inflight:       map[string]InflightCall{},
		RetryCounters:  map[string]int{},
		RetryHistory:   map[string][]RetryRecord{},
		ReplanCounters: map[string]int{},
		ReplanDigests:  map[string]string{},
		CommittedSteps: map[string]bool{},
		OnFailActive:   map[string]bool{},
		Failures:       map[string]Failure{},
	}
	for _, step := range spec.AllSteps() {
		snapshot.StepStates[step.ID] = StepPending
	}
	for _, input := range spec.Inputs {
		snapshot.Variables[input.Name] = VariableValue{Value: inputs[input.Name], Assigned: true}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snapshot = snapshot
	e.recordLocked("run_initialized", "", spec.ID)
	if err := e.persistLocked(ctx); err != nil {
		e.snapshot = nil
		return InitResult{}, err
	}
	return InitResult{RunID: runID}, nil
}

func (e *ExecutionEngine) Next(ctx context.Context) (NextResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	e.ctx = ctx
	if e.snapshot == nil {
		return NextResult{}, ErrRunNotInitialized
	}
	switch e.snapshot.TerminalState {
	case TerminalCompleted:
		return e.resultLocked(NextCompleted), nil
	case TerminalFailed:
		return e.resultLocked(NextFailed), nil
	case TerminalAborted:
		return e.resultLocked(NextAborted), nil
	}
	if e.snapshot.AdaptiveNeeded != "" {
		return e.resultLocked(NextAdaptiveNeeded), nil
	}
	if e.snapshot.Pause != nil {
		step, ok := e.stepByID(e.snapshot.Pause.StepID)
		if !ok {
			return NextResult{}, fmt.Errorf("paused step %s is missing", e.snapshot.Pause.StepID)
		}
		result := e.resultLocked(NextPaused)
		result.Step = e.stepReadyLocked(step)
		return result, nil
	}
	if step, ok := e.firstRunningWorkStep(); ok {
		result := e.resultLocked(NextRunning)
		result.Step = e.stepReadyLocked(step)
		return result, nil
	}
	if len(e.snapshot.Spec.Steps) == 0 {
		return e.resultLocked(NextAdaptiveNeeded), nil
	}
	var advance advanceResult
	if err := e.mutateLocked(ctx, func() error {
		e.reapInflightLocked(ctx)
		advance = e.advanceLocked()
		return nil
	}); err != nil {
		return NextResult{}, err
	}
	if advance.kind == advanceReady || advance.kind == advancePaused {
		status := NextStepReady
		if advance.kind == advancePaused {
			status = NextPaused
		}
		result := e.resultLocked(status)
		result.Step = e.stepReadyLocked(advance.step)
		return result, nil
	}
	if e.snapshot.Pause != nil {
		return e.resultLocked(NextPaused), nil
	}
	if e.snapshot.TerminalState == "" && len(e.snapshot.Inflight) > 0 {
		return e.resultLocked(NextRunning), nil
	}
	return e.terminalResultLocked(), nil
}

func (e *ExecutionEngine) Complete(ctx context.Context, stepID string, outputs map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshot == nil {
		return ErrRunNotInitialized
	}
	if e.snapshot.Pause != nil && e.snapshot.Pause.ChildRunID != "" {
		return e.completeChildPause(ctx, outputs)
	}
	step, ok := e.stepByID(stepID)
	if !ok {
		return fmt.Errorf("INVALID_STEP_ID: %s", stepID)
	}
	state := e.snapshot.StepStates[stepID]
	if state == StepDone {
		return e.sameCompletedOutputs(step, outputs)
	}
	if state != StepRunning {
		return fmt.Errorf("%w: step %s is %s", ErrStepNotRunning, stepID, state)
	}
	if err := validateStepOutputs(step, outputs); err != nil {
		return err
	}
	return e.mutateLocked(ctx, func() error {
		for _, decl := range step.Outputs {
			e.snapshot.Variables[decl.Name] = VariableValue{Value: outputs[decl.Name], Assigned: true}
		}
		e.snapshot.StepStates[stepID] = StepDone
		e.snapshot.Pause = nil
		e.recordLocked("step_done", stepID, "")
		if step.Type == hopspec.StepCheck {
			if verdict, ok := checkVerdict(step, outputs); ok && !verdict {
				failure := Failure{Kind: FailureError, Reason: checkFailureReason(step, outputs)}
				e.handleFailureLocked(stepID, failure)
			}
		}
		if step.Type == hopspec.StepConfirm {
			if approved, ok := checkVerdict(step, outputs); ok && !approved {
				failure := Failure{Kind: FailureError, Reason: "CONFIRM_REJECTED: " + stepID}
				e.snapshot.TerminalFailure = &failure
				e.snapshot.TerminalState = TerminalFailed
				for _, remaining := range e.snapshot.Spec.AllSteps() {
					if e.snapshot.StepStates[remaining.ID] == StepPending {
						e.snapshot.StepStates[remaining.ID] = StepSkipped
					}
				}
				e.recordLocked("run_failed", stepID, failure.Reason)
			}
		}
		return nil
	})
}

func (e *ExecutionEngine) Fail(ctx context.Context, stepID string, failure Failure) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshot == nil {
		return ErrRunNotInitialized
	}
	if _, ok := e.stepByID(stepID); !ok {
		return fmt.Errorf("INVALID_STEP_ID: %s", stepID)
	}
	if e.snapshot.StepStates[stepID] != StepRunning {
		return fmt.Errorf("%w: step %s is %s", ErrStepNotRunning, stepID, e.snapshot.StepStates[stepID])
	}
	if strings.TrimSpace(failure.Reason) == "" {
		return errors.New("failure reason is required")
	}
	if failure.Kind == "" {
		failure.Kind = FailureError
	}
	return e.mutateLocked(ctx, func() error {
		e.snapshot.StepStates[stepID] = StepFailed
		e.snapshot.Pause = nil
		e.recordLocked("step_failed", stepID, failure.Reason)
		e.handleFailureLocked(stepID, failure)
		return nil
	})
}

func (e *ExecutionEngine) Resume(ctx context.Context, runID string) error {
	snapshot, err := e.store.LoadSnapshot(ctx, runID)
	if err != nil {
		return err
	}
	if snapshot.FormatVersion != SnapshotFormatVersion {
		return fmt.Errorf("unsupported snapshot format %d", snapshot.FormatVersion)
	}
	normalizeSnapshotMaps(&snapshot)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snapshot = &snapshot
	if snapshot.TerminalState != "" {
		return nil
	}
	return e.mutateLocked(ctx, func() error {
		reset := 0
		for id, state := range e.snapshot.StepStates {
			step, ok := e.stepByID(id)
			if state == StepRunning && ok && step.Type == hopspec.StepCommit {
				failure := Failure{Kind: FailureError, Reason: "COMMIT_OUTCOME_UNKNOWN: " + id}
				e.snapshot.StepStates[id] = StepFailed
				e.snapshot.Failures[id] = failure
				e.snapshot.TerminalFailure = &failure
				e.snapshot.TerminalState = TerminalFailed
				continue
			}
			if state == StepRunning && ok && e.isWorkStep(step.Type) {
				e.snapshot.StepStates[id] = StepPending
				reset++
			}
		}
		e.recordLocked("run_resumed", "", fmt.Sprintf("reset_running=%d", reset))
		return nil
	})
}

func (e *ExecutionEngine) Abort(ctx context.Context, reason string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshot == nil {
		return ErrRunNotInitialized
	}
	if e.snapshot.TerminalState == TerminalAborted {
		return nil
	}
	if e.snapshot.TerminalState != "" {
		return errors.New("terminal run cannot be aborted")
	}
	return e.mutateLocked(ctx, func() error {
		for id, state := range e.snapshot.StepStates {
			if state == StepPending || state == StepRunning {
				e.snapshot.StepStates[id] = StepSkipped
			}
		}
		e.cancelInflightLocked()
		e.snapshot.Inflight = map[string]InflightCall{}
		e.snapshot.Pause = nil
		e.snapshot.AbortReason = strings.TrimSpace(reason)
		e.snapshot.TerminalState = TerminalAborted
		e.recordLocked("run_aborted", "", reason)
		return nil
	})
}

func (e *ExecutionEngine) Snapshot() (EngineSnapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshot == nil {
		return EngineSnapshot{}, ErrRunNotInitialized
	}
	return CloneSnapshot(*e.snapshot)
}

func (e *ExecutionEngine) mutateLocked(ctx context.Context, mutate func() error) error {
	before, err := CloneSnapshot(*e.snapshot)
	if err != nil {
		return err
	}
	if err := mutate(); err != nil {
		return err
	}
	if err := e.persistLocked(ctx); err != nil {
		e.snapshot = &before
		return err
	}
	return nil
}

func (e *ExecutionEngine) persistLocked(ctx context.Context) error {
	e.snapshot.StateSeq++
	return e.store.SaveSnapshot(ctx, *e.snapshot)
}

func (e *ExecutionEngine) finishLocked() {
	missing := e.unassignedOutputs()
	if len(missing) > 0 {
		failure := Failure{Kind: FailureDeterministic, Reason: "OUTPUT_INCOMPLETE: " + strings.Join(missing, ", ")}
		e.snapshot.TerminalFailure = &failure
		e.snapshot.TerminalState = TerminalFailed
		e.recordLocked("run_failed", "", failure.Reason)
		return
	}
	e.snapshot.TerminalState = TerminalCompleted
	e.recordLocked("run_completed", "", "")
}

func (e *ExecutionEngine) unassignedOutputs() []string {
	var missing []string
	for _, output := range e.snapshot.Spec.Outputs {
		value, ok := e.snapshot.Variables[output.Name]
		if !ok || !value.Assigned || value.Value == nil {
			missing = append(missing, output.Name)
		}
	}
	return missing
}

func (e *ExecutionEngine) firstStepWithState(state StepStatus) (hopspec.Step, bool) {
	for _, step := range e.snapshot.Spec.AllSteps() {
		if e.snapshot.StepStates[step.ID] == state {
			return step, true
		}
	}
	return hopspec.Step{}, false
}

func (e *ExecutionEngine) stepByID(id string) (hopspec.Step, bool) {
	for _, step := range e.snapshot.Spec.AllSteps() {
		if step.ID == id {
			return step, true
		}
	}
	return hopspec.Step{}, false
}

func (e *ExecutionEngine) stepReadyLocked(step hopspec.Step) *StepReady {
	inputs := map[string]any{}
	for _, name := range step.Inputs {
		if value, ok := e.snapshot.Variables[name]; ok && value.Assigned {
			inputs[name] = value.Value
		}
	}
	ready := &StepReady{
		RunID: e.snapshot.RunID, Goal: e.snapshot.Spec.Goal, Constraints: append([]string(nil), e.snapshot.Spec.Constraints...),
		StepID: step.ID, StepType: step.Type, Summary: step.Summary,
		Inputs: inputs, Instruction: step.Instruction, Tools: append([]string(nil), step.Tools...), OutputSchema: append([]hopspec.VariableDecl(nil), step.Outputs...),
	}
	ready.Context = AssemblePromptContext(*e.snapshot, *ready)
	return ready
}

func (e *ExecutionEngine) resultLocked(status NextStatus) NextResult {
	clone, _ := CloneSnapshot(*e.snapshot)
	result := NextResult{Status: status, RunID: clone.RunID, Pause: clone.Pause, Failure: clone.TerminalFailure, Snapshot: clone}
	if status == NextCompleted {
		result.Outputs = outputsFromSnapshot(clone)
	}
	return result
}

func (e *ExecutionEngine) terminalResultLocked() NextResult {
	switch e.snapshot.TerminalState {
	case TerminalCompleted:
		return e.resultLocked(NextCompleted)
	case TerminalAborted:
		return e.resultLocked(NextAborted)
	default:
		return e.resultLocked(NextFailed)
	}
}

func (e *ExecutionEngine) recordLocked(event, stepID, detail string) {
	e.snapshot.Events = append(e.snapshot.Events, ExecutionEvent{
		Sequence: uint64(len(e.snapshot.Events) + 1), At: e.now().UTC(), StepID: stepID, Event: event, Detail: detail,
	})
}

func (e *ExecutionEngine) supports(stepType hopspec.StepType) bool {
	return stepType == hopspec.StepReason || stepType == hopspec.StepAct || stepType == hopspec.StepCheck
}

func (e *ExecutionEngine) sameCompletedOutputs(step hopspec.Step, outputs map[string]any) error {
	if err := validateStepOutputs(step, outputs); err != nil {
		return err
	}
	for _, decl := range step.Outputs {
		current := e.snapshot.Variables[decl.Name]
		if !current.Assigned || !reflect.DeepEqual(current.Value, outputs[decl.Name]) {
			return errors.New("STALE_RESUBMIT: completed step outputs differ")
		}
	}
	return nil
}

func validateStepOutputs(step hopspec.Step, outputs map[string]any) error {
	declared := map[string]bool{}
	for _, decl := range step.Outputs {
		declared[decl.Name] = true
		value, ok := outputs[decl.Name]
		if !ok {
			return fmt.Errorf("OUTPUT_MISSING: step %s did not return %s", step.ID, decl.Name)
		}
		if err := validateValueType(decl.Type, value); err != nil {
			return fmt.Errorf("OUTPUT_TYPE_MISMATCH: %s: %w", decl.Name, err)
		}
	}
	for name := range outputs {
		if !declared[name] {
			return fmt.Errorf("UNDECLARED_OUTPUT: step %s returned %s", step.ID, name)
		}
	}
	return nil
}

func validateValueType(declared string, value any) error {
	if value == nil {
		return nil
	}
	if strings.HasPrefix(declared, "[") && strings.HasSuffix(declared, "]") {
		if reflect.TypeOf(value).Kind() != reflect.Slice {
			return fmt.Errorf("expected %s", declared)
		}
		return nil
	}
	switch declared {
	case "bool":
		if _, ok := value.(bool); !ok {
			return errors.New("expected bool")
		}
	case "int":
		switch number := value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		case float64:
			if number != float64(int64(number)) {
				return errors.New("expected int")
			}
		default:
			return errors.New("expected int")
		}
	case "float":
		if reflect.TypeOf(value).Kind() < reflect.Int || reflect.TypeOf(value).Kind() > reflect.Float64 {
			return errors.New("expected float")
		}
	case "line", "text", "markdown", "prompt":
		if _, ok := value.(string); !ok {
			return errors.New("expected string")
		}
	}
	return nil
}

func checkVerdict(step hopspec.Step, outputs map[string]any) (bool, bool) {
	for _, decl := range step.Outputs {
		if decl.Type == "bool" {
			verdict, ok := outputs[decl.Name].(bool)
			return verdict, ok
		}
	}
	return false, false
}

func checkFailureReason(step hopspec.Step, outputs map[string]any) string {
	for _, decl := range step.Outputs {
		if decl.Type == "text" || decl.Type == "yaml" {
			if reason := strings.TrimSpace(fmt.Sprint(outputs[decl.Name])); reason != "" && reason != "<nil>" {
				return reason
			}
		}
	}
	return "check failed"
}

func outputsFromSnapshot(snapshot EngineSnapshot) map[string]any {
	outputs := map[string]any{}
	for _, decl := range snapshot.Spec.Outputs {
		if value, ok := snapshot.Variables[decl.Name]; ok && value.Assigned {
			outputs[decl.Name] = value.Value
		}
	}
	return outputs
}

func newRunID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(bytes), nil
}
