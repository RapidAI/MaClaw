package codingruntime

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryStore is primarily useful for TUI embedding and deterministic tests.
// It preserves the same lease/transition rules as SQLiteStore.
type MemoryStore struct {
	mu           sync.Mutex
	tasks        map[string]*Task
	attempts     map[string]*Attempt
	events       map[string][]Event
	childResults map[string]ChildTaskResult
	// consumedContinuations maps the waiting_child parent Attempt to the fresh
	// review Attempt that consumed its delivered results.
	consumedContinuations map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tasks: map[string]*Task{}, attempts: map[string]*Attempt{}, events: map[string][]Event{}, childResults: map[string]ChildTaskResult{}, consumedContinuations: map[string]string{}}
}

func (s *MemoryStore) CreateTask(task Task) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task = normalizeTaskForLedger(task)
	if task.TaskID == "" {
		task.TaskID = uuid.NewString()
	}
	if _, exists := s.tasks[task.TaskID]; exists {
		return nil, fmt.Errorf("task %s already exists", task.TaskID)
	}
	if task.Status == "" {
		task.Status = TaskQueued
	}
	if !validStartStatus(task.Status) {
		return nil, fmt.Errorf("%w: create task in %s", ErrInvalidTransition, task.Status)
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	task.UpdatedAt = task.CreatedAt
	s.tasks[task.TaskID] = cloneTask(&task)
	return cloneTask(&task), nil
}

func (s *MemoryStore) GetTask(taskID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.tasks[taskID]; task != nil {
		return cloneTask(task), nil
	}
	return nil, ErrNotFound
}

func (s *MemoryStore) ListChildTasks(parentTaskID string) ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks[parentTaskID] == nil {
		return nil, ErrNotFound
	}
	out := make([]*Task, 0)
	for _, task := range s.tasks {
		if task.ParentTaskID == parentTaskID {
			out = append(out, cloneTask(task))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out, nil
}

func (s *MemoryStore) GetAttempt(attemptID string) (*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt := s.attempts[attemptID]; attempt != nil {
		return cloneAttempt(attempt), nil
	}
	return nil, ErrNotFound
}

func (s *MemoryStore) ListAttempts(taskID string) ([]*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks[taskID] == nil {
		return nil, ErrNotFound
	}
	out := make([]*Attempt, 0)
	for _, attempt := range s.attempts {
		if attempt.TaskID == taskID {
			out = append(out, cloneAttempt(attempt))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AttemptNo < out[j].AttemptNo })
	return out, nil
}

func (s *MemoryStore) MarkTaskWaitingApproval(taskID string, now time.Time) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return nil, ErrNotFound
	}
	if task.Status != TaskQueued && task.Status != TaskWaitingApproval {
		return nil, fmt.Errorf("%w: waiting approval", ErrInvalidTransition)
	}
	task.Status, task.UpdatedAt = TaskWaitingApproval, now
	return cloneTask(task), nil
}

// MarkTaskReadyForRecovery permits a new attempt only after the recovery
// service has completed its read-only probe and recorded human confirmation.
func (s *MemoryStore) MarkTaskReadyForRecovery(taskID string, now time.Time) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	if task == nil {
		return nil, ErrNotFound
	}
	if task.Status != TaskInterrupted {
		return nil, fmt.Errorf("%w: ready for recovery", ErrInvalidTransition)
	}
	task.Status, task.UpdatedAt = TaskQueued, now
	return cloneTask(task), nil
}

func (s *MemoryStore) StartAttempt(taskID, leaseOwner string, leaseFor time.Duration, policy PolicySnapshot, now time.Time) (*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startAttemptLocked(taskID, leaseOwner, leaseFor, policy, now)
}

func (s *MemoryStore) ConsumeParentContinuation(taskID, parentAttemptID, reviewAttemptID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks[taskID] == nil || s.attempts[parentAttemptID] == nil || s.attempts[reviewAttemptID] == nil {
		return ErrNotFound
	}
	parentAttempt, reviewAttempt := s.attempts[parentAttemptID], s.attempts[reviewAttemptID]
	if parentAttempt.TaskID != taskID || parentAttempt.Status != TaskWaitingChild || reviewAttempt.TaskID != taskID || reviewAttempt.Status != TaskRunning {
		return fmt.Errorf("%w: consume parent continuation", ErrInvalidTransition)
	}
	if existing := s.consumedContinuations[parentAttemptID]; existing != "" {
		return ErrContinuationConsumed
	}
	s.consumedContinuations[parentAttemptID] = reviewAttemptID
	eventType, payload := normalizeEventForLedger("parent_continuation_consumed", codingRuntimeErrorDigest(parentAttemptID+"|"+reviewAttemptID))
	s.events[reviewAttemptID] = append(s.events[reviewAttemptID], Event{TaskID: taskID, AttemptID: reviewAttemptID, Sequence: uint64(len(s.events[reviewAttemptID]) + 1), Type: eventType, PayloadDigest: payload, CreatedAt: now})
	return nil
}

func (s *MemoryStore) IsParentContinuationConsumed(parentAttemptID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts[parentAttemptID] == nil {
		return false, ErrNotFound
	}
	return s.consumedContinuations[parentAttemptID] != "", nil
}

func (s *MemoryStore) RecordWorkspaceBefore(attemptID, leaseOwner string, probe *WorkspaceProbe, now time.Time) (*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[attemptID]
	if attempt == nil {
		return nil, ErrNotFound
	}
	if attempt.Status != TaskRunning {
		return nil, ErrAttemptNotRunning
	}
	if attempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	attempt.WorkspaceBefore = cloneProbe(probe)
	return cloneAttempt(attempt), nil
}

func (s *MemoryStore) AdmitReadOnlyChildren(parentAttemptID, leaseOwner string, specs []ChildTaskSpec, policy PolicySnapshot, now time.Time) ([]ChildTaskHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parentAttempt := s.attempts[parentAttemptID]
	if parentAttempt == nil {
		return nil, ErrNotFound
	}
	if parentAttempt.Status != TaskRunning {
		return nil, ErrAttemptNotRunning
	}
	if parentAttempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	parent := s.tasks[parentAttempt.TaskID]
	if parent == nil {
		return nil, ErrNotFound
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: at least one child is required", ErrInvalidTransition)
	}
	for _, spec := range specs {
		if err := validateReadOnlyChildSpec(*parent, spec, policy); err != nil {
			return nil, err
		}
	}
	handles := make([]ChildTaskHandle, 0, len(specs))
	for _, spec := range specs {
		childID := uuid.NewString()
		child := normalizeTaskForLedger(Task{TaskID: childID, WorkflowID: parent.WorkflowID, PhaseID: parent.PhaseID, OwnerID: parent.OwnerID, ParentTaskID: parent.TaskID, ProjectRef: firstChildValue(spec.ProjectRef, parent.ProjectRef), Mode: firstChildValue(spec.Mode, parent.Mode), RequestedWork: spec.RequestedWork, PolicyDigest: policy.Digest, Status: TaskQueued, CreatedAt: now, UpdatedAt: now})
		s.tasks[childID] = &child
		handles = append(handles, ChildTaskHandle{TaskID: childID, ParentTaskID: parent.TaskID, ParentAttemptID: parentAttemptID, Name: spec.Name, Status: TaskQueued, ExecutionTarget: child.Mode})
	}
	parentAttempt.Status, parentAttempt.LeaseUntil, parentAttempt.FinishedAt = TaskWaitingChild, time.Time{}, now
	parent.Status, parent.UpdatedAt = TaskWaitingChild, now
	eventType, payloadDigest := normalizeEventForLedger("children_admitted", childHandlesDigest(handles))
	event := Event{TaskID: parent.TaskID, AttemptID: parentAttemptID, Sequence: uint64(len(s.events[parentAttemptID]) + 1), Type: eventType, PayloadDigest: payloadDigest, CreatedAt: now}
	s.events[parentAttemptID] = append(s.events[parentAttemptID], event)
	return handles, nil
}

func (s *MemoryStore) CompleteChildTask(childTaskID string, result ChildTaskResult, now time.Time) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	child := s.tasks[childTaskID]
	if child == nil {
		return nil, ErrNotFound
	}
	if child.ParentTaskID == "" || !child.Status.Terminal() {
		return nil, fmt.Errorf("%w: child task is not terminal", ErrInvalidTransition)
	}
	if result.Status != "" && result.Status != child.Status {
		return nil, fmt.Errorf("%w: child result status differs from task", ErrInvalidTransition)
	}
	if _, exists := s.childResults[childTaskID]; exists {
		return nil, fmt.Errorf("%w: child result already delivered", ErrInvalidTransition)
	}
	result.TaskID, result.Status, result.Summary, result.EvidenceDigest, result.CompletedAt = childTaskID, child.Status, boundedChildTaskSummary(result.Summary), boundedLedgerText(result.EvidenceDigest, maxPayloadDigestRunes), now
	s.childResults[childTaskID] = result
	parent := s.tasks[child.ParentTaskID]
	if parent == nil {
		return nil, ErrNotFound
	}
	allDelivered := true
	for _, candidate := range s.tasks {
		if candidate.ParentTaskID == parent.TaskID {
			if _, ok := s.childResults[candidate.TaskID]; !ok {
				allDelivered = false
				break
			}
		}
	}
	if allDelivered && parent.Status == TaskWaitingChild {
		parent.Status, parent.UpdatedAt = TaskQueued, now
	}
	return cloneTask(parent), nil
}

func (s *MemoryStore) ListChildResults(parentTaskID string) ([]ChildTaskResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks[parentTaskID] == nil {
		return nil, ErrNotFound
	}
	out := make([]ChildTaskResult, 0)
	for childID, result := range s.childResults {
		if child := s.tasks[childID]; child != nil && child.ParentTaskID == parentTaskID {
			out = append(out, result)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CompletedAt.Equal(out[j].CompletedAt) {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].CompletedAt.Before(out[j].CompletedAt)
	})
	return out, nil
}

func (s *MemoryStore) startAttemptLocked(taskID, leaseOwner string, leaseFor time.Duration, policy PolicySnapshot, now time.Time) (*Attempt, error) {
	task := s.tasks[taskID]
	if task == nil {
		return nil, ErrNotFound
	}
	if leaseOwner == "" || leaseFor <= 0 {
		return nil, fmt.Errorf("%w: start attempt", ErrInvalidTransition)
	}
	var err error
	policy, err = NormalizeWriterPolicy(*task, policy)
	if err != nil {
		return nil, err
	}
	maxNo := 0
	for _, attempt := range s.attempts {
		if attempt.TaskID == taskID && attempt.AttemptNo > maxNo {
			maxNo = attempt.AttemptNo
		}
		if attempt.Status == TaskRunning && attempt.LeaseUntil.After(now) {
			if attempt.TaskID == taskID {
				return nil, ErrLeaseHeld
			}
			other := s.tasks[attempt.TaskID]
			if other != nil {
				if conflict := WriterAdmissionConflict(*task, policy, *other, attempt.Policy); conflict.Conflicts {
					return nil, WriterAdmissionError{Conflict: conflict}
				}
			}
		}
	}
	if !validStartStatus(task.Status) {
		return nil, fmt.Errorf("%w: start attempt", ErrInvalidTransition)
	}
	attempt := &Attempt{AttemptID: uuid.NewString(), TaskID: taskID, AttemptNo: maxNo + 1, LeaseOwner: leaseOwner, LeaseUntil: now.Add(leaseFor), Status: TaskRunning, Policy: policy, SideEffectState: SideEffectNone, StartedAt: now}
	s.attempts[attempt.AttemptID] = attempt
	task.Status, task.UpdatedAt = TaskRunning, now
	return cloneAttempt(attempt), nil
}

func (s *MemoryStore) AppendEvent(attemptID, leaseOwner, eventType, payloadDigest string, now time.Time) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[attemptID]
	if attempt == nil {
		return nil, ErrNotFound
	}
	if attempt.Status != TaskRunning {
		return nil, ErrAttemptNotRunning
	}
	if attempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	eventType, payloadDigest = normalizeEventForLedger(eventType, payloadDigest)
	event := Event{TaskID: attempt.TaskID, AttemptID: attemptID, Sequence: uint64(len(s.events[attemptID]) + 1), Type: eventType, PayloadDigest: payloadDigest, CreatedAt: now}
	s.events[attemptID] = append(s.events[attemptID], event)
	return &event, nil
}

func (s *MemoryStore) RecordStaleCallback(attemptID, payloadDigest string, now time.Time) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[attemptID]
	if attempt == nil {
		return nil, ErrNotFound
	}
	if attempt.Status == TaskRunning {
		return nil, fmt.Errorf("%w: running attempt", ErrInvalidTransition)
	}
	event := Event{TaskID: attempt.TaskID, AttemptID: attemptID, Sequence: uint64(len(s.events[attemptID]) + 1), Type: "stale_callback_discarded", PayloadDigest: boundedLedgerText(payloadDigest, maxPayloadDigestRunes), CreatedAt: now}
	s.events[attemptID] = append(s.events[attemptID], event)
	return &event, nil
}

func (s *MemoryStore) ListEvents(attemptID string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts[attemptID] == nil {
		return nil, ErrNotFound
	}
	events := s.events[attemptID]
	out := make([]Event, len(events))
	copy(out, events)
	return out, nil
}

// AppendRecoveryEvent records a read-only recovery decision after an attempt
// has ended. It deliberately does not require a live lease: a recovered
// process cannot truthfully claim ownership of the interrupted execution.
func (s *MemoryStore) AppendRecoveryEvent(attemptID, eventType, payloadDigest string, now time.Time) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[attemptID]
	if attempt == nil {
		return nil, ErrNotFound
	}
	if attempt.Status != TaskInterrupted && attempt.SideEffectState != SideEffectUncertain {
		return nil, fmt.Errorf("%w: recovery event", ErrInvalidTransition)
	}
	eventType, payloadDigest = normalizeEventForLedger(eventType, payloadDigest)
	event := Event{TaskID: attempt.TaskID, AttemptID: attemptID, Sequence: uint64(len(s.events[attemptID]) + 1), Type: eventType, PayloadDigest: payloadDigest, CreatedAt: now}
	s.events[attemptID] = append(s.events[attemptID], event)
	return &event, nil
}

func (s *MemoryStore) FinishAttempt(attemptID, leaseOwner string, input FinishInput, now time.Time) (*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	input = normalizeFinishInputForLedger(input)
	attempt := s.attempts[attemptID]
	if attempt == nil {
		return nil, ErrNotFound
	}
	if attempt.Status != TaskRunning || !validTerminalStatus(input.Status) {
		return nil, fmt.Errorf("%w: finish attempt", ErrInvalidTransition)
	}
	if attempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	attempt.Status, attempt.SideEffectState = input.Status, input.SideEffectState
	attempt.WorkspaceAfter, attempt.ErrorCode, attempt.ErrorSummary, attempt.FinishedAt = cloneProbe(input.WorkspaceAfter), input.ErrorCode, input.ErrorSummary, now
	attempt.LeaseUntil = time.Time{}
	task := s.tasks[attempt.TaskID]
	task.Status, task.UpdatedAt = input.Status, now
	return cloneAttempt(attempt), nil
}

// CancelTask makes user cancellation durable for the whole task subtree. A
// parent may be waiting without a lease while read-only children run, so only
// cancelling the parent's current Attempt is insufficient: descendants would
// otherwise remain executable and could later re-queue their parent.
func (s *MemoryStore) CancelTask(taskID string, now time.Time) ([]*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks[taskID] == nil {
		return nil, ErrNotFound
	}
	ids := s.taskSubtreeLocked(taskID)
	cancelled := make([]*Attempt, 0, len(ids))
	for _, id := range ids {
		task := s.tasks[id]
		if task == nil || task.Status.Terminal() {
			continue
		}
		for _, attempt := range s.attempts {
			if attempt.TaskID != id || attempt.Status.Terminal() {
				continue
			}
			wasRunning := attempt.Status == TaskRunning
			attempt.Status, attempt.LeaseUntil, attempt.FinishedAt = TaskCancelled, time.Time{}, now
			if wasRunning && attempt.SideEffectState == SideEffectNone {
				attempt.SideEffectState = SideEffectUncertain
			}
			event := Event{TaskID: id, AttemptID: attempt.AttemptID, Sequence: uint64(len(s.events[attempt.AttemptID]) + 1), Type: "task_cancelled", CreatedAt: now}
			s.events[attempt.AttemptID] = append(s.events[attempt.AttemptID], event)
			cancelled = append(cancelled, cloneAttempt(attempt))
		}
		task.Status, task.UpdatedAt = TaskCancelled, now
	}
	sort.Slice(cancelled, func(i, j int) bool {
		if cancelled[i].TaskID == cancelled[j].TaskID {
			return cancelled[i].AttemptNo < cancelled[j].AttemptNo
		}
		return cancelled[i].TaskID < cancelled[j].TaskID
	})
	return cancelled, nil
}

func (s *MemoryStore) taskSubtreeLocked(rootID string) []string {
	ids := []string{rootID}
	for i := 0; i < len(ids); i++ {
		for id, task := range s.tasks {
			if task.ParentTaskID == ids[i] {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func (s *MemoryStore) ExpireLeases(now time.Time) ([]*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []*Attempt
	for _, attempt := range s.attempts {
		if attempt.Status != TaskRunning || attempt.LeaseUntil.After(now) {
			continue
		}
		attempt.Status, attempt.SideEffectState, attempt.FinishedAt = TaskInterrupted, SideEffectUncertain, now
		attempt.LeaseUntil = time.Time{}
		if task := s.tasks[attempt.TaskID]; task != nil {
			task.Status, task.UpdatedAt = TaskInterrupted, now
			// A parent released its lease intentionally while children ran. If
			// one child dies with the process, the parent cannot remain
			// waiting_child forever: neither side may be replayed automatically.
			if task.ParentTaskID != "" {
				s.interruptWaitingParentLocked(task.ParentTaskID, now)
			}
		}
		event := Event{TaskID: attempt.TaskID, AttemptID: attempt.AttemptID, Sequence: uint64(len(s.events[attempt.AttemptID]) + 1), Type: "lease_expired", CreatedAt: now}
		s.events[attempt.AttemptID] = append(s.events[attempt.AttemptID], event)
		expired = append(expired, cloneAttempt(attempt))
	}
	return expired, nil
}

// InterruptUnstartedChildren is a startup reconciliation step. A queued child
// has no live executor/lease to survive a process boundary, so leaving its
// parent in waiting_child would be a permanent stall. This does not schedule
// or replay either task: it marks the child and its waiting parent interrupted
// so the normal explicit recovery protocol can inspect and continue later.
func (s *MemoryStore) InterruptUnstartedChildren(now time.Time) ([]*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parents := map[string]bool{}
	for _, child := range s.tasks {
		if child.ParentTaskID == "" || child.Status != TaskQueued {
			continue
		}
		parent := s.tasks[child.ParentTaskID]
		if parent == nil || parent.Status != TaskWaitingChild {
			continue
		}
		child.Status, child.UpdatedAt = TaskInterrupted, now
		parents[parent.TaskID] = true
	}
	interrupted := make([]*Attempt, 0, len(parents))
	for parentID := range parents {
		before := s.latestWaitingParentAttemptLocked(parentID)
		s.interruptWaitingParentLocked(parentID, now)
		if before != nil {
			interrupted = append(interrupted, cloneAttempt(before))
		}
	}
	sort.Slice(interrupted, func(i, j int) bool { return interrupted[i].TaskID < interrupted[j].TaskID })
	return interrupted, nil
}

func (s *MemoryStore) interruptWaitingParentLocked(parentTaskID string, now time.Time) {
	parent := s.tasks[parentTaskID]
	if parent == nil || parent.Status != TaskWaitingChild {
		return
	}
	parent.Status, parent.UpdatedAt = TaskInterrupted, now
	latest := s.latestWaitingParentAttemptLocked(parentTaskID)
	if latest == nil {
		return
	}
	latest.Status, latest.SideEffectState, latest.FinishedAt = TaskInterrupted, SideEffectNone, now
	event := Event{TaskID: parentTaskID, AttemptID: latest.AttemptID, Sequence: uint64(len(s.events[latest.AttemptID]) + 1), Type: "child_lease_expired", CreatedAt: now}
	s.events[latest.AttemptID] = append(s.events[latest.AttemptID], event)
}

func (s *MemoryStore) latestWaitingParentAttemptLocked(parentTaskID string) *Attempt {
	var latest *Attempt
	for _, attempt := range s.attempts {
		if attempt.TaskID != parentTaskID || attempt.Status != TaskWaitingChild {
			continue
		}
		if latest == nil || attempt.AttemptNo > latest.AttemptNo {
			latest = attempt
		}
	}
	return latest
}

func (s *MemoryStore) ListRecoveryCandidates() ([]*Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var candidates []*Attempt
	for _, attempt := range s.attempts {
		if attempt.Status == TaskInterrupted || attempt.SideEffectState == SideEffectUncertain {
			candidates = append(candidates, cloneAttempt(attempt))
		}
	}
	return candidates, nil
}

func cloneTask(in *Task) *Task {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func cloneProbe(in *WorkspaceProbe) *WorkspaceProbe {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func cloneAttempt(in *Attempt) *Attempt {
	if in == nil {
		return nil
	}
	out := *in
	out.WorkspaceBefore = cloneProbe(in.WorkspaceBefore)
	out.WorkspaceAfter = cloneProbe(in.WorkspaceAfter)
	return &out
}

func firstChildValue(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func childHandlesDigest(handles []ChildTaskHandle) string {
	parts := make([]string, 0, len(handles))
	for _, handle := range handles {
		parts = append(parts, handle.TaskID+"|"+handle.Name+"|"+handle.ExecutionTarget)
	}
	return codingRuntimeErrorDigest(strings.Join(parts, "\n"))
}
