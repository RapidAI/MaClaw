package agentservice

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type closableServiceStore struct {
	Store
	closeCalls atomic.Int32
}

type failingRunStore struct {
	Store
	err error
}

func (s failingRunStore) SaveRun(Run) error { return s.err }

// capabilityRunEventOnlyStore models a custom repository that has adopted the
// durable outbox API but has not yet moved lifecycle writes into one
// transaction. Embedding interfaces keeps this test focused on capability
// discovery without implementing the full Store surface.
type capabilityRunEventOnlyStore struct {
	Store
	RunEventStore
}

// capabilityAtomicMethodsStore models an adopter that exposes lifecycle
// transaction methods but does not let Service read those events from the
// same RunEventStore. It must not be advertised as atomic.
type capabilityAtomicMethodsStore struct {
	Store
	RunAdmissionEventStore
	RunCompletionEventStore
	RunTerminalEventStore
}

// transactionalLifecycleStore models a custom SQL repository that adopts the
// generic transaction seam. It delegates the actual state semantics to the
// in-memory implementation while recording which path Service selected.
type transactionalLifecycleStore struct {
	Store
	RunEventStore
	commits atomic.Int32
}

func (s *transactionalLifecycleStore) CommitRunLifecycle(mutation RunLifecycleMutation) error {
	s.commits.Add(1)
	base, ok := s.Store.(*MemoryStore)
	if !ok {
		return errors.New("transactional test store base unavailable")
	}
	switch mutation.Phase {
	case RunLifecyclePhaseAdmission:
		return base.SaveRunAdmissionWithEvents(*mutation.Message, mutation.Run, mutation.Events)
	case RunLifecyclePhaseCompletion:
		return base.SaveRunCompletionWithEvents(*mutation.Message, mutation.Run, mutation.Events)
	case RunLifecyclePhaseTerminal:
		return base.SaveRunTerminalWithEvents(mutation.Run, mutation.Events)
	default:
		return errors.New("unexpected lifecycle phase")
	}
}

func TestDescribeLifecyclePersistenceClassifiesRepositoryGuarantees(t *testing.T) {
	cases := []struct {
		name  string
		store Store
		want  LifecyclePersistenceCapabilities
	}{
		{name: "nil", store: nil, want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeBestEffort}},
		{name: "memory", store: NewMemoryStore(), want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeAtomic, DurableOutbox: true, AdmissionAtomic: true, AdmissionOutboxAtomic: true, CompletionAtomic: true, CompletionOutboxAtomic: true, TerminalOutboxAtomic: true}},
		{name: "file", store: func() Store { v, _ := NewFileStore(filepath.Join(t.TempDir(), "state.json")); return v }(), want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeAtomic, DurableOutbox: true, AdmissionAtomic: true, AdmissionOutboxAtomic: true, CompletionAtomic: true, CompletionOutboxAtomic: true, TerminalOutboxAtomic: true}},
		{name: "sqlite", store: func() Store { v, _ := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db")); return v }(), want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeAtomic, DurableOutbox: true, AdmissionAtomic: true, AdmissionOutboxAtomic: true, CompletionAtomic: true, CompletionOutboxAtomic: true, TerminalOutboxAtomic: true}},
		{name: "run_event_only", store: &capabilityRunEventOnlyStore{RunEventStore: NewMemoryRunEventStore()}, want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeMixed, DurableOutbox: true}},
		{name: "lifecycle_methods_without_outbox", store: &capabilityAtomicMethodsStore{}, want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeBestEffort, AdmissionOutboxAtomic: true, CompletionOutboxAtomic: true, TerminalOutboxAtomic: true}},
		{name: "custom", store: &closableServiceStore{}, want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeBestEffort}},
		{name: "generic_transaction", store: func() Store {
			base := NewMemoryStore()
			return &transactionalLifecycleStore{Store: base, RunEventStore: base}
		}(), want: LifecyclePersistenceCapabilities{Mode: LifecyclePersistenceModeAtomic, DurableOutbox: true, AdmissionAtomic: true, AdmissionOutboxAtomic: true, CompletionAtomic: true, CompletionOutboxAtomic: true, TerminalOutboxAtomic: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DescribeLifecyclePersistence(tc.store)
			if got != tc.want {
				t.Fatalf("DescribeLifecyclePersistence()=%#v, want %#v", got, tc.want)
			}
			if closer, ok := tc.store.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
		})
	}
}

func TestServicePrefersGenericRunLifecycleTransactionStore(t *testing.T) {
	base := NewMemoryStore()
	store := &transactionalLifecycleStore{Store: base, RunEventStore: base}
	svc, _, _ := setupServiceWithStore(t, store)
	defer svc.Close()
	now := time.Now().UTC()
	message := Message{ID: "msg_generic_tx", SessionID: "sess_generic_tx", TenantID: "tenant", UserID: "user", InstanceID: "inst", Role: MessageRoleUser, Content: "hello", CreatedAt: now}
	run := Run{ID: "run_generic_tx", TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: message.ID, Status: RunStatusRunning, StartedAt: now}
	committed, err := svc.saveRunAdmissionWithEvents(message, run, []RunEvent{{TenantID: run.TenantID, UserID: run.UserID, RunID: run.ID, Type: "run.started"}})
	if err != nil || !committed {
		t.Fatalf("generic admission committed=%v err=%v", committed, err)
	}
	if got := store.commits.Load(); got != 1 {
		t.Fatalf("generic transaction calls=%d, want 1", got)
	}
	if events, err := base.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10); err != nil || len(events) != 1 {
		t.Fatalf("transactional events=%#v err=%v", events, err)
	}
}

func TestRunLifecycleTransactionMutationValidation(t *testing.T) {
	base := NewMemoryStore()
	cases := []RunLifecycleMutation{
		{Phase: RunLifecyclePhaseAdmission, Run: Run{ID: "run"}},
		{Phase: RunLifecyclePhaseTerminal, Message: &Message{}, Run: Run{ID: "run"}},
		{Phase: RunLifecyclePhase("unknown"), Run: Run{ID: "run"}},
		{Phase: RunLifecyclePhaseAdmission, Message: &Message{}, Run: Run{ID: "run"}, Events: []RunEvent{{RunID: "other", Type: "run.started"}}},
	}
	for i, mutation := range cases {
		if err := base.CommitRunLifecycle(mutation); err == nil {
			t.Fatalf("case %d unexpectedly accepted invalid mutation", i)
		}
	}
}

func (s *closableServiceStore) Close() error {
	s.closeCalls.Add(1)
	return nil
}

func TestServiceClosesCustomControlPlaneStore(t *testing.T) {
	store := &closableServiceStore{Store: NewMemoryStore()}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := store.closeCalls.Load(); got != 1 {
		t.Fatalf("custom store close calls=%d; want 1", got)
	}
	// Close remains idempotent even when the custom store owns resources.
	if err := svc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := store.closeCalls.Load(); got != 1 {
		t.Fatalf("custom store close calls after second Close=%d; want 1", got)
	}
}

func TestNewServiceCanRequireAtomicLifecycleStore(t *testing.T) {
	store := &closableServiceStore{Store: NewMemoryStore()}
	_, err := NewService(Config{
		DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345",
		RequireAtomicLifecycle: true,
	}, store, EchoExecutor{})
	if err == nil || !strings.Contains(err.Error(), "RunLifecycleTransactionStore") {
		t.Fatalf("NewService error = %v, want atomic lifecycle requirement", err)
	}
}

func TestFileStoreSaveRunAdmissionPersistsMessageAndRunTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	message := Message{ID: "msg_admission", SessionID: "sess_admission", TenantID: "tenant", UserID: "user", InstanceID: "inst", Role: MessageRoleUser, Content: "hello", CreatedAt: now}
	run := Run{ID: "run_admission", TenantID: "tenant", UserID: "user", InstanceID: "inst", SessionID: "sess_admission", UserMessageID: message.ID, Status: RunStatusRunning, StartedAt: now}
	if err := store.SaveRunAdmission(message, run); err != nil {
		t.Fatalf("SaveRunAdmission: %v", err)
	}
	assistant := Message{ID: "msg_assistant", SessionID: message.SessionID, TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, Role: MessageRoleAssistant, Content: "world", CreatedAt: now}
	run.Status = RunStatusSucceeded
	run.AssistantMessageID = assistant.ID
	if err := store.SaveRunCompletion(assistant, run); err != nil {
		t.Fatalf("SaveRunCompletion: %v", err)
	}
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := reopened.ListMessages(message.SessionID)
	if err != nil || len(messages) != 2 || messages[0].ID != message.ID || messages[1].ID != assistant.ID {
		t.Fatalf("persisted messages = %#v, err=%v", messages, err)
	}
	got, err := reopened.GetRun(run.TenantID, run.UserID, run.InstanceID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserMessageID != message.ID || got.Status != RunStatusSucceeded || got.AssistantMessageID != assistant.ID {
		t.Fatalf("persisted run = %#v", got)
	}
}

func TestServiceLegacyLifecycleFallbackRollsBackOrphanMessage(t *testing.T) {
	base := NewMemoryStore()
	store := &failingRunStore{Store: base, err: errors.New("run write failed")}
	svc, principal, instance := setupServiceWithStore(t, store)
	defer svc.Close()
	_, _, err := svc.PostMessage(context.Background(), principal, instance.ID, "session-missing", PostMessageInput{Content: "hello"})
	if err == nil {
		t.Fatal("expected missing session error")
	}

	// Create a valid session through the service, then force the second write
	// in the compatibility (non-transactional) admission path to fail.
	session, err := svc.CreateSession(context.Background(), principal, instance.ID, CreateSessionInput{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.PostMessage(context.Background(), principal, instance.ID, session.ID, PostMessageInput{Content: "hello"})
	if err == nil || err.Error() != "run write failed" {
		t.Fatalf("PostMessage error=%v, want run write failed", err)
	}
	messages, err := base.ListMessages(session.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("legacy fallback left orphan messages: %#v", messages)
	}
}

func setupServiceWithStore(t *testing.T, store Store) (*Service, Principal, Instance) {
	t.Helper()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "key", MaclawLLMModel: "model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	instance, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	return svc, principal, *instance
}

func TestRunLifecycleTransactionsCommitOutboxWithState(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC().Truncate(time.Microsecond)
	message := Message{ID: "msg_tx", SessionID: "sess_tx", TenantID: "tenant", UserID: "user", InstanceID: "inst", Role: MessageRoleUser, Content: "hello", Metadata: map[string]string{"client_message_id": "tx-key"}, CreatedAt: now}
	run := Run{ID: "run_tx", TenantID: "tenant", UserID: "user", InstanceID: "inst", SessionID: "sess_tx", UserMessageID: message.ID, Status: RunStatusRunning, StartedAt: now}
	started := RunEvent{TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, SessionID: run.SessionID, RunID: run.ID, Type: "run.started"}
	if err := store.SaveRunAdmissionWithEvents(message, run, []RunEvent{started}); err != nil {
		t.Fatalf("SaveRunAdmissionWithEvents: %v", err)
	}
	duplicateRun := run
	duplicateRun.ID = "run_tx_duplicate"
	if err := store.SaveRunAdmissionWithEvents(message, duplicateRun, []RunEvent{started}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate admission error=%v; want ErrAlreadyExists", err)
	}
	assistant := Message{ID: "msg_tx_assistant", SessionID: message.SessionID, TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, Role: MessageRoleAssistant, Content: "world", CreatedAt: now}
	run.Status = RunStatusSucceeded
	run.AssistantMessageID = assistant.ID
	completed := RunEvent{TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, SessionID: run.SessionID, RunID: run.ID, Type: "run.completed", Payload: map[string]any{"message_id": assistant.ID}}
	if err := store.SaveRunCompletionWithEvents(assistant, run, []RunEvent{completed}); err != nil {
		t.Fatalf("SaveRunCompletionWithEvents: %v", err)
	}
	events, err := store.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	if events[0].Type != "run.started" || events[1].Type != "run.completed" || events[1].Payload["message_id"] != assistant.ID {
		t.Fatalf("unexpected transaction events: %#v", events)
	}
	messages, err := store.ListMessages(message.SessionID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
}

func TestMemoryStoreRunLifecycleTransactionRollsBackOnEventFailure(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	message := Message{ID: "msg_atomic_failure", SessionID: "sess_atomic_failure", TenantID: "tenant", UserID: "user", InstanceID: "inst", Role: MessageRoleUser, Content: "hello", CreatedAt: now}
	run := Run{ID: "run_atomic_failure", TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: message.ID, Status: RunStatusRunning, StartedAt: now}
	// The first event is valid; the second one fails envelope validation. The
	// repository must expose neither event nor the message/run after the
	// failed transaction.
	err := store.SaveRunAdmissionWithEvents(message, run, []RunEvent{
		{TenantID: run.TenantID, UserID: run.UserID, RunID: run.ID, Type: "run.started"},
		{TenantID: run.TenantID, UserID: run.UserID, RunID: run.ID, Type: "run.invalid\nnewline"},
	})
	if err == nil {
		t.Fatal("expected invalid event error")
	}
	if _, err := store.GetRun(run.TenantID, run.UserID, run.InstanceID, run.ID); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("run remained visible after rollback: %v", err)
	}
	messages, err := store.ListMessages(message.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("message remained visible after rollback: %#v", messages)
	}
	events, err := store.ListAfter(run.TenantID, run.UserID, run.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("partial events remained visible after rollback: %#v", events)
	}
}

func TestMemoryStoreRunCompletionAndTerminalRollbackOnEventFailure(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	base := Run{ID: "run_completion_failure", TenantID: "tenant", UserID: "user", InstanceID: "inst", SessionID: "sess_completion_failure", UserMessageID: "msg", Status: RunStatusRunning, StartedAt: now}
	if err := store.SaveRun(base); err != nil {
		t.Fatal(err)
	}
	assistant := Message{ID: "msg_assistant_failure", SessionID: base.SessionID, TenantID: base.TenantID, UserID: base.UserID, InstanceID: base.InstanceID, Role: MessageRoleAssistant, Content: "done", CreatedAt: now}
	completed := base
	completed.Status = RunStatusSucceeded
	completed.AssistantMessageID = assistant.ID
	if err := store.SaveRunCompletionWithEvents(assistant, completed, []RunEvent{{TenantID: base.TenantID, UserID: base.UserID, RunID: base.ID, Type: "run.completed"}, {TenantID: base.TenantID, UserID: base.UserID, RunID: base.ID, Type: "   "}}); err == nil {
		t.Fatal("expected completion event error")
	}
	got, err := store.GetRun(base.TenantID, base.UserID, base.InstanceID, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusRunning || got.AssistantMessageID != "" {
		t.Fatalf("run changed after completion rollback: %#v", got)
	}
	if messages, _ := store.ListMessages(base.SessionID); len(messages) != 0 {
		t.Fatalf("assistant message remained after completion rollback: %#v", messages)
	}
	terminal := base
	terminal.Status = RunStatusFailed
	if err := store.SaveRunTerminalWithEvents(terminal, []RunEvent{{TenantID: base.TenantID, UserID: base.UserID, RunID: base.ID, Type: "run.failed"}, {TenantID: base.TenantID, UserID: base.UserID, RunID: base.ID, Type: "   "}}); err == nil {
		t.Fatal("expected terminal event error")
	}
	got, err = store.GetRun(base.TenantID, base.UserID, base.InstanceID, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusRunning {
		t.Fatalf("run changed after terminal rollback: %#v", got)
	}
	if events, _ := store.ListAfter(base.TenantID, base.UserID, base.ID, 0, 20); len(events) != 0 {
		t.Fatalf("terminal partial events remained after rollback: %#v", events)
	}
}

func TestFileStoreRunLifecycleTransactionPersistsOutboxAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	run := Run{ID: "run_file_tx", TenantID: "tenant", UserID: "user", InstanceID: "inst", SessionID: "sess", UserMessageID: "msg_file_tx", Status: RunStatusRunning, StartedAt: now}
	message := Message{ID: run.UserMessageID, SessionID: run.SessionID, TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, Role: MessageRoleUser, Content: "hello", CreatedAt: now}
	if err := store.SaveRunAdmissionWithEvents(message, run, []RunEvent{{TenantID: run.TenantID, UserID: run.UserID, RunID: run.ID, Type: "run.started"}}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	events, err := reopened.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10)
	if err != nil || len(events) != 1 || events[0].Type != "run.started" {
		t.Fatalf("reopened events=%#v err=%v", events, err)
	}
}

func TestFileStoreConcurrentMessageAndEventWritesDoNotDeadlock(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "service.json"))
	if err != nil {
		t.Fatal(err)
	}
	const writes = 8
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writes; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = store.SaveMessage(Message{ID: fmt.Sprintf("msg-%d", i), SessionID: "sess", Role: MessageRoleUser, Content: "hello"})
		}(i)
		go func(i int) {
			defer wg.Done()
			_, _ = store.Append(RunEvent{TenantID: "tenant", UserID: "user", RunID: "run", Type: "assistant.delta", Payload: map[string]any{"i": i}})
		}(i)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent FileStore writes deadlocked")
	}
}

func TestFileStoreMutationRollsBackMemoryOnFlushFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	baseline := Message{ID: "msg_flush_baseline", SessionID: "sess_flush", Role: MessageRoleUser, Content: "baseline"}
	if err := store.SaveMessage(baseline); err != nil {
		t.Fatal(err)
	}
	// Point the durable target at a missing parent without touching the
	// in-memory repository. AtomicWriteFile must fail, and the failed mutation
	// must be rolled back from the caller's perspective as well.
	store.path = filepath.Join(root, "missing-parent", "state.json")
	deferred := Message{ID: "msg_flush_failed", SessionID: baseline.SessionID, Role: MessageRoleUser, Content: "must rollback"}
	if err := store.SaveMessage(deferred); err == nil {
		t.Fatal("expected flush failure")
	}
	messages, err := store.ListMessages(baseline.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != baseline.ID {
		t.Fatalf("failed flush leaked in-memory mutation: %#v", messages)
	}
	if _, err := store.Append(RunEvent{TenantID: "tenant", UserID: "user", RunID: "run_flush_failed", Type: "assistant.delta", Payload: map[string]any{"delta": "must rollback"}}); err == nil {
		t.Fatal("expected event flush failure")
	}
	if events, err := store.ListAfter("tenant", "user", "run_flush_failed", 0, 20); err != nil {
		t.Fatal(err)
	} else if len(events) != 0 {
		t.Fatalf("failed event flush leaked in-memory mutation: %#v", events)
	}
}

func TestMemoryStoreRejectsDuplicateClientSessionAndMessageKeys(t *testing.T) {
	store := NewMemoryStore()
	firstSession := Session{ID: "sess-1", TenantID: "tenant", UserID: "user", InstanceID: "inst", Metadata: map[string]string{"client_session_key": "same"}}
	if err := store.SaveSession(firstSession); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(Session{ID: "sess-2", TenantID: firstSession.TenantID, UserID: firstSession.UserID, InstanceID: firstSession.InstanceID, Metadata: map[string]string{"client_session_key": "same"}}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate session key error=%v; want ErrAlreadyExists", err)
	}
	firstMessage := Message{ID: "msg-1", SessionID: firstSession.ID, TenantID: firstSession.TenantID, UserID: firstSession.UserID, InstanceID: firstSession.InstanceID, Role: MessageRoleUser, Metadata: map[string]string{"client_message_id": "same"}}
	if err := store.SaveMessage(firstMessage); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(Message{ID: "msg-2", SessionID: firstMessage.SessionID, TenantID: firstMessage.TenantID, UserID: firstMessage.UserID, InstanceID: firstMessage.InstanceID, Role: MessageRoleUser, Metadata: map[string]string{"client_message_id": "same"}}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate message key error=%v; want ErrAlreadyExists", err)
	}
}

func TestDefaultServicePersistsRunOutboxAlongsideFileStore(t *testing.T) {
	root := t.TempDir()
	svc, err := NewService(Config{DataRoot: root, TokenSecret: "test-token-secret-0123456789012345"}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tenant, err := svc.CreateTenant(ctx, CreateTenantInput{Name: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "user"})
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(ctx, principal, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "key", MaclawLLMModel: "model"}); err != nil {
		t.Fatal(err)
	}
	instance, err := svc.CreateInstance(ctx, principal, CreateInstanceInput{Name: "instance"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CreateSession(ctx, principal, instance.ID, CreateSessionInput{Title: "session"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, _, err := svc.SendMessage(ctx, principal, instance.ID, SendMessageInput{SessionID: session.ID, Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewService(Config{DataRoot: root, TokenSecret: "test-token-secret-0123456789012345"}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	events, err := reopened.ListRunEvents(ctx, principal, run.ID, 0, 20)
	if err != nil || len(events) < 3 {
		t.Fatalf("reopened events=%#v err=%v", events, err)
	}
	if events[0].Type != "run.started" || events[len(events)-1].Type != "run.completed" {
		t.Fatalf("unexpected reopened lifecycle events: %#v", events)
	}
}
