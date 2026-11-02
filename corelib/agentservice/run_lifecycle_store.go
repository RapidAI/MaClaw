package agentservice

import (
	"errors"
	"fmt"
	"strings"
)

// RunLifecyclePhase identifies the one of the three run lifecycle mutations
// that may be committed with its durable event outbox.  The phase is part of
// the mutation rather than inferred from the Run status so a repository can
// reject ambiguous or forged transitions before touching its database.
type RunLifecyclePhase string

const (
	RunLifecyclePhaseAdmission  RunLifecyclePhase = "admission"
	RunLifecyclePhaseCompletion RunLifecyclePhase = "completion"
	RunLifecyclePhaseTerminal   RunLifecyclePhase = "terminal"
)

// RunLifecycleMutation is the transport-neutral transaction payload shared by
// GUI, MaClawSrv and custom Store implementations.  A transaction-capable
// repository must commit the run/message (when present) and every event in
// Events atomically.  Repositories must not mutate the caller-owned slice or
// message pointer.
//
// Admission and completion require Message; terminal transitions deliberately
// omit it because failed/cancelled runs do not necessarily have an assistant
// message.
type RunLifecycleMutation struct {
	Phase   RunLifecyclePhase
	Message *Message
	Run     Run
	Events  []RunEvent
}

// RunLifecycleTransactionStore is the preferred extension for custom Store
// implementations.  Embedding RunEventStore is intentional: it proves that
// the transaction's outbox is the same durable event surface consumed by SSE
// and replay.  A repository that only implements CommitRunLifecycle but keeps
// events elsewhere must remain on the compatibility path and must not claim
// atomic lifecycle guarantees.
type RunLifecycleTransactionStore interface {
	RunEventStore
	CommitRunLifecycle(RunLifecycleMutation) error
}

func validateRunLifecycleMutation(m RunLifecycleMutation) error {
	switch m.Phase {
	case RunLifecyclePhaseAdmission, RunLifecyclePhaseCompletion:
		if m.Message == nil {
			return fmt.Errorf("run lifecycle %s requires message", m.Phase)
		}
	case RunLifecyclePhaseTerminal:
		if m.Message != nil {
			return errors.New("run lifecycle terminal must not include message")
		}
	default:
		return fmt.Errorf("unsupported run lifecycle phase %q", m.Phase)
	}
	if strings.TrimSpace(m.Run.ID) == "" {
		return errors.New("run lifecycle run id is required")
	}
	if err := validateRunEventPayloads(m.Events); err != nil {
		return err
	}
	for _, event := range m.Events {
		if id := strings.TrimSpace(event.RunID); id != "" && id != m.Run.ID {
			return fmt.Errorf("run lifecycle event %q belongs to run %q, want %q", event.Type, id, m.Run.ID)
		}
		if tenant := strings.TrimSpace(event.TenantID); tenant != "" && tenant != m.Run.TenantID {
			return fmt.Errorf("run lifecycle event tenant %q does not match run tenant %q", tenant, m.Run.TenantID)
		}
		if user := strings.TrimSpace(event.UserID); user != "" && user != m.Run.UserID {
			return fmt.Errorf("run lifecycle event user %q does not match run user %q", user, m.Run.UserID)
		}
	}
	return nil
}

// RunAdmissionStore is an optional Store extension for atomically admitting
// the user message and its initial running Run. Keeping this capability
// additive preserves compatibility with existing/custom Store implementations
// while giving SQLite-backed repositories a single transaction seam for the
// run/message/outbox commit.
type RunAdmissionStore interface {
	SaveRunAdmission(Message, Run) error
}

// RunCompletionStore is the terminal counterpart to RunAdmissionStore. It is
// optional for backwards compatibility and provides one repository seam for
// committing the assistant message and terminal run state together.
type RunCompletionStore interface {
	SaveRunCompletion(Message, Run) error
}

// RunAdmissionEventStore extends RunAdmissionStore with the durable outbox
// boundary. Implementations must commit the user message, running run and all
// supplied admission events as one logical transaction. The event list is
// intentionally supplied by the service so transports cannot invent a second
// run-started event vocabulary.
type RunAdmissionEventStore interface {
	SaveRunAdmissionWithEvents(Message, Run, []RunEvent) error
}

// RunCompletionEventStore is the terminal counterpart of
// RunAdmissionEventStore. It commits the assistant message, terminal run and
// terminal events together. A store may implement this without implementing
// the admission extension while migrating.
type RunCompletionEventStore interface {
	SaveRunCompletionWithEvents(Message, Run, []RunEvent) error
}

// RunTerminalEventStore covers failed/cancelled runs that do not have an
// assistant message. It keeps the terminal run state and its terminal event
// in the same repository transaction where the backend supports it.
type RunTerminalEventStore interface {
	SaveRunTerminalWithEvents(Run, []RunEvent) error
}

// LifecyclePersistenceCapabilities describes how strongly a repository can
// commit run state and its durable outbox. It is intentionally derived from
// optional Store extensions so custom embedders can migrate incrementally
// without changing the base Store interface or silently claiming ACID
// semantics they do not provide.
type LifecyclePersistenceCapabilities struct {
	Mode                   string `json:"mode"`
	DurableOutbox          bool   `json:"durable_outbox"`
	AdmissionAtomic        bool   `json:"admission_atomic"`
	AdmissionOutboxAtomic  bool   `json:"admission_outbox_atomic"`
	CompletionAtomic       bool   `json:"completion_atomic"`
	CompletionOutboxAtomic bool   `json:"completion_outbox_atomic"`
	TerminalOutboxAtomic   bool   `json:"terminal_outbox_atomic"`
}

const (
	LifecyclePersistenceModeAtomic     = "atomic"
	LifecyclePersistenceModeMixed      = "mixed"
	LifecyclePersistenceModeBestEffort = "best_effort"
)

// DescribeLifecyclePersistence reports repository guarantees without
// inspecting concrete backend names. "atomic" means all lifecycle phases and
// their outbox events share repository transaction seams; "mixed" means the
// outbox is durable but at least one lifecycle transition still uses the
// compatibility fallback; "best_effort" means a custom Store has no durable
// event repository and Service must use the legacy split writes.
func DescribeLifecyclePersistence(store Store) LifecyclePersistenceCapabilities {
	if store == nil {
		return LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeBestEffort}
	}
	_, durableOutbox := store.(RunEventStore)
	_, admissionAtomic := store.(RunAdmissionStore)
	_, admissionOutboxAtomic := store.(RunAdmissionEventStore)
	_, completionAtomic := store.(RunCompletionStore)
	_, completionOutboxAtomic := store.(RunCompletionEventStore)
	_, terminalOutboxAtomic := store.(RunTerminalEventStore)
	// A generic transaction store is authoritative for all three phases. It
	// embeds RunEventStore, so the event booleans are safe to promote too.
	if _, ok := store.(RunLifecycleTransactionStore); ok {
		admissionAtomic = true
		admissionOutboxAtomic = true
		completionAtomic = true
		completionOutboxAtomic = true
		terminalOutboxAtomic = true
	}
	mode := LifecyclePersistenceModeBestEffort
	if durableOutbox {
		mode = LifecyclePersistenceModeMixed
	}
	// The service only wires a standalone event repository when the Store does
	// not implement RunEventStore. In that compatibility composition the
	// lifecycle methods may exist but their events are not readable from the
	// service outbox, so do not claim an atomic contract unless the repository
	// also owns the durable event surface.
	if durableOutbox && admissionOutboxAtomic && completionOutboxAtomic && terminalOutboxAtomic {
		mode = LifecyclePersistenceModeAtomic
	}
	return LifecyclePersistenceCapabilities{
		Mode:                   mode,
		DurableOutbox:          durableOutbox,
		AdmissionAtomic:        admissionAtomic,
		AdmissionOutboxAtomic:  admissionOutboxAtomic,
		CompletionAtomic:       completionAtomic,
		CompletionOutboxAtomic: completionOutboxAtomic,
		TerminalOutboxAtomic:   terminalOutboxAtomic,
	}
}

func (s *MemoryStore) SaveRunAdmission(message Message, run Run) error {
	return s.SaveRunAdmissionWithEvents(message, run, nil)
}

func (s *MemoryStore) SaveRunAdmissionWithEvents(message Message, run Run, events []RunEvent) error {
	if s == nil {
		return ErrServiceClosed
	}
	if err := validateRunEventPayloads(events); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// MemoryStore is also used as the default repository in tests and embedded
	// hosts. Treat the whole operation as one transaction, including event
	// normalization/idempotency: an error in a later event must not leave the
	// message, run, or an earlier event visible.
	before := s.snapshotLocked()
	if err := s.ensureUniqueClientMessageLocked(message); err != nil {
		return err
	}
	s.messages[message.SessionID] = append(s.messages[message.SessionID], message)
	s.touchMessagesAppend(message.SessionID)
	s.runs[run.ID] = run
	s.touch(collRuns, run.ID)
	for _, event := range events {
		if _, err := s.appendRunEventLocked(event); err != nil {
			s.restoreLocked(before)
			return err
		}
	}
	return nil
}

func (s *MemoryStore) SaveRunCompletion(message Message, run Run) error {
	return s.SaveRunCompletionWithEvents(message, run, nil)
}

func (s *MemoryStore) SaveRunCompletionWithEvents(message Message, run Run, events []RunEvent) error {
	if s == nil {
		return ErrServiceClosed
	}
	if err := validateRunEventPayloads(events); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.snapshotLocked()
	if err := s.ensureUniqueClientMessageLocked(message); err != nil {
		return err
	}
	s.messages[message.SessionID] = append(s.messages[message.SessionID], message)
	s.touchMessagesAppend(message.SessionID)
	s.runs[run.ID] = run
	s.touch(collRuns, run.ID)
	for _, event := range events {
		if _, err := s.appendRunEventLocked(event); err != nil {
			s.restoreLocked(before)
			return err
		}
	}
	return nil
}

func (s *MemoryStore) SaveRunTerminalWithEvents(run Run, events []RunEvent) error {
	if s == nil {
		return ErrServiceClosed
	}
	if err := validateRunEventPayloads(events); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.snapshotLocked()
	s.runs[run.ID] = run
	s.touch(collRuns, run.ID)
	for _, event := range events {
		if _, err := s.appendRunEventLocked(event); err != nil {
			s.restoreLocked(before)
			return err
		}
	}
	return nil
}

// CommitRunLifecycle implements the generic transaction seam for the built-in
// in-memory repository. Keeping the dispatch here (instead of in Service)
// means custom repositories can adopt one stable contract without inheriting
// transport-specific admission/completion call sites.
func (s *MemoryStore) CommitRunLifecycle(mutation RunLifecycleMutation) error {
	if err := validateRunLifecycleMutation(mutation); err != nil {
		return err
	}
	switch mutation.Phase {
	case RunLifecyclePhaseAdmission:
		return s.SaveRunAdmissionWithEvents(*mutation.Message, mutation.Run, mutation.Events)
	case RunLifecyclePhaseCompletion:
		return s.SaveRunCompletionWithEvents(*mutation.Message, mutation.Run, mutation.Events)
	case RunLifecyclePhaseTerminal:
		return s.SaveRunTerminalWithEvents(mutation.Run, mutation.Events)
	default:
		return fmt.Errorf("unsupported run lifecycle phase %q", mutation.Phase)
	}
}

var (
	_ RunAdmissionEventStore       = (*MemoryStore)(nil)
	_ RunCompletionEventStore      = (*MemoryStore)(nil)
	_ RunTerminalEventStore        = (*MemoryStore)(nil)
	_ RunEventStore                = (*MemoryStore)(nil)
	_ RunLifecycleTransactionStore = (*MemoryStore)(nil)
)
