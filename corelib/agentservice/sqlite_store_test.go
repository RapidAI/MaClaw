package agentservice

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSQLiteStoreLifecycleIsAtomicAndDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	message := Message{ID: "msg-sqlite", SessionID: "sess-sqlite", TenantID: "tenant", UserID: "user", InstanceID: "instance", Role: MessageRoleUser, Content: "hello", CreatedAt: now}
	run := Run{ID: "run-sqlite", TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: message.ID, Status: RunStatusRunning, StartedAt: now}
	if err := store.SaveRunAdmissionWithEvents(message, run, []RunEvent{{ID: "run:run-sqlite:run.started", TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, SessionID: run.SessionID, RunID: run.ID, Type: "run.started"}}); err != nil {
		t.Fatal(err)
	}
	assistant := Message{ID: "assistant-sqlite", SessionID: message.SessionID, TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, Role: MessageRoleAssistant, Content: "world", CreatedAt: now}
	run.Status = RunStatusSucceeded
	run.AssistantMessageID = assistant.ID
	if err := store.SaveRunCompletionWithEvents(assistant, run, []RunEvent{
		{ID: "run:run-sqlite:assistant.message", TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, SessionID: run.SessionID, RunID: run.ID, Type: "assistant.message", Payload: map[string]any{"message_id": assistant.ID}},
		{ID: "run:run-sqlite:run.completed", TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, SessionID: run.SessionID, RunID: run.ID, Type: "run.completed", Payload: map[string]any{"message_id": assistant.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	messages, err := reopened.ListMessages(message.SessionID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
	gotRun, err := reopened.GetRun(run.TenantID, run.UserID, run.InstanceID, run.ID)
	if err != nil || gotRun.Status != RunStatusSucceeded || gotRun.AssistantMessageID != assistant.ID {
		t.Fatalf("run=%#v err=%v", gotRun, err)
	}
	events, err := reopened.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10)
	if err != nil || len(events) != 3 || events[0].Sequence != 1 || events[2].Type != "run.completed" {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}

func TestSQLiteStoreGenericLifecycleTransactionUsesSharedOutbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generic-service.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	message := Message{ID: "msg-generic-sqlite", SessionID: "sess-generic-sqlite", TenantID: "tenant", UserID: "user", InstanceID: "instance", Role: MessageRoleUser, Content: "hello", CreatedAt: now}
	run := Run{ID: "run-generic-sqlite", TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: message.ID, Status: RunStatusRunning, StartedAt: now}
	if err := store.CommitRunLifecycle(RunLifecycleMutation{
		Phase: RunLifecyclePhaseAdmission, Message: &message, Run: run,
		Events: []RunEvent{{TenantID: run.TenantID, UserID: run.UserID, RunID: run.ID, Type: "run.started"}},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10)
	if err != nil || len(events) != 1 || events[0].Type != "run.started" {
		t.Fatalf("generic lifecycle events=%#v err=%v", events, err)
	}
	if _, err := store.GetRun(run.TenantID, run.UserID, run.InstanceID, run.ID); err != nil {
		t.Fatalf("generic lifecycle run not committed: %v", err)
	}
}

func TestSQLiteStoreLifecycleRollbackLeavesNoPartialState(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	message := Message{ID: "msg-rollback", SessionID: "sess", TenantID: "tenant", UserID: "user", InstanceID: "instance", Role: MessageRoleUser, Content: "hello"}
	run := Run{ID: "run-rollback", TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: message.ID, Status: RunStatusRunning}
	// A malformed payload cannot be encoded by the JSON state document. The
	// event itself is valid, so force a repository error with a duplicate client
	// message key after the first transaction and verify no second run is left.
	message.Metadata = map[string]string{"client_message_id": "same"}
	if err := store.SaveRunAdmissionWithEvents(message, run, nil); err != nil {
		t.Fatal(err)
	}
	badMessage := Message{ID: "msg-bad", SessionID: "sess", TenantID: "tenant", UserID: "user", InstanceID: "instance", Role: MessageRoleUser, Content: "bad"}
	badRun := Run{ID: "run-bad", TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: badMessage.ID, Status: RunStatusRunning}
	if err := store.SaveRunAdmissionWithEvents(badMessage, badRun, []RunEvent{{TenantID: badRun.TenantID, UserID: badRun.UserID, InstanceID: badRun.InstanceID, SessionID: badRun.SessionID, RunID: badRun.ID, Type: "run.started", Payload: map[string]any{"unsupported": func() {}}}}); err == nil {
		t.Fatal("unsupported event payload should fail state serialization")
	}
	if _, err := store.GetRun(badRun.TenantID, badRun.UserID, badRun.InstanceID, badRun.ID); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("bad run=%v; want absent after rollback", err)
	}
	duplicate := message
	duplicate.ID = "msg-rollback-2"
	duplicateRun := run
	duplicateRun.ID = "run-rollback-2"
	if err := store.SaveRunAdmissionWithEvents(duplicate, duplicateRun, nil); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate err=%v; want ErrAlreadyExists", err)
	}
	if _, err := store.GetRun(message.TenantID, message.UserID, message.InstanceID, duplicateRun.ID); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("duplicate run=%v; want absent", err)
	}
	messages, err := store.ListMessages(message.SessionID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
}

func TestSQLiteStoreConcurrentLifecycleWritesRemainSerialized(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const writers = 20
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			run := Run{ID: "run-concurrent-" + string(rune('a'+i)), TenantID: "tenant", UserID: "user", InstanceID: "instance", SessionID: "session", UserMessageID: "msg-concurrent-" + string(rune('a'+i)), Status: RunStatusRunning}
			message := Message{ID: run.UserMessageID, SessionID: run.SessionID, TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, Role: MessageRoleUser, Content: "hello"}
			if err := store.SaveRunAdmissionWithEvents(message, run, []RunEvent{{TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, SessionID: run.SessionID, RunID: run.ID, Type: "run.started"}}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent lifecycle write: %v", err)
	}
	runs, err := store.ListRuns("tenant", "user", "instance")
	if err != nil || len(runs) != writers {
		t.Fatalf("runs=%d err=%v; want %d", len(runs), err, writers)
	}
}

func TestNewServiceCanSelectSQLiteControlPlaneBackend(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), StoreBackend: "sqlite", TokenSecret: "test-token-secret-0123456789"}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.store.(*SQLiteStore); !ok {
		t.Fatalf("service store=%T; want *SQLiteStore", svc.store)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewServiceRejectsUnknownStoreBackend(t *testing.T) {
	if _, err := NewService(Config{DataRoot: t.TempDir(), StoreBackend: "postgres", TokenSecret: "test-token-secret-0123456789"}, nil, EchoExecutor{}); err == nil {
		t.Fatal("unknown store backend should fail during composition")
	}
}

func TestSQLiteStoreSeparateHandlesKeepLifecycleWritesAndIdempotencyAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()
	const writers = 2
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i, store := range []*SQLiteStore{first, second} {
		wg.Add(1)
		go func(i int, store *SQLiteStore) {
			defer wg.Done()
			message := Message{ID: "cross-msg-" + string(rune('a'+i)), SessionID: "cross-session", TenantID: "tenant", UserID: "user", InstanceID: "instance", Role: MessageRoleUser, Content: "hello", Metadata: map[string]string{"client_message_id": "key-" + string(rune('a'+i))}}
			run := Run{ID: "cross-run-" + string(rune('a'+i)), TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: message.ID, Status: RunStatusRunning}
			if err := store.SaveRunAdmissionWithEvents(message, run, []RunEvent{{TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID, SessionID: run.SessionID, RunID: run.ID, Type: "run.started"}}); err != nil {
				errs <- err
			}
		}(i, store)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("cross-handle lifecycle write: %v", err)
		}
	}
	runs, err := first.ListRuns("tenant", "user", "instance")
	if err != nil || len(runs) != writers {
		t.Fatalf("runs=%d err=%v; want %d", len(runs), err, writers)
	}
}

func TestSQLiteStoreImportsLegacyJSONStateWithoutSecrets(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "store.json")
	dbPath := filepath.Join(root, "service.db")
	legacy := `{"tenants":{"tenant":{"id":"tenant","name":"legacy"}},"credentials":{"legacy":{"id":"cred","tenant_id":"tenant","user_id":"user","api_key":"legacy-secret","status":"active"}}}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.GetTenant("tenant"); err != nil {
		t.Fatalf("legacy tenant was not imported: %v", err)
	}
	cred, err := store.GetCredentialByAPIKey("legacy-secret")
	if err != nil || cred.APIKeyHash == "" || cred.APIKey != "" {
		t.Fatalf("legacy credential normalization failed: %#v err=%v", cred, err)
	}
	var payload []byte
	if err := store.db.QueryRow(`SELECT state_json FROM agentservice_state WHERE id=1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) == "" || string(payload) == legacy || string(payload) == "legacy-secret" {
		t.Fatalf("sqlite database did not persist normalized imported state: %s", payload)
	}
	if string(payload) == "" || strings.Contains(string(payload), "legacy-secret") {
		t.Fatalf("sqlite state leaked plaintext credential: %s", payload)
	}
}

func TestSQLiteStoreSeparateHandlesAssignMonotonicEventSequences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, store := range []*SQLiteStore{first, second} {
		wg.Add(1)
		go func(store *SQLiteStore) {
			defer wg.Done()
			_, err := store.Append(RunEvent{TenantID: "tenant", UserID: "user", RunID: "run", Type: "assistant.delta"})
			if err != nil {
				errs <- err
			}
		}(store)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("cross-handle append: %v", err)
	}
	events, err := first.ListAfter("tenant", "user", "run", 0, 10)
	if err != nil || len(events) != 2 || events[0].Sequence != 1 || events[1].Sequence != 2 {
		t.Fatalf("events=%#v err=%v; want sequences 1,2", events, err)
	}
}

func TestSQLiteStoreCrossHandleDuplicateClientMessageIsRejectedAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()
	message := Message{SessionID: "session", TenantID: "tenant", UserID: "user", InstanceID: "instance", Role: MessageRoleUser, Content: "hello", Metadata: map[string]string{"client_message_id": "same"}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, store := range []*SQLiteStore{first, second} {
		wg.Add(1)
		go func(i int, store *SQLiteStore) {
			defer wg.Done()
			candidate := message
			candidate.ID = "msg-duplicate-" + string(rune('a'+i))
			err := store.SaveMessage(candidate)
			errs <- err
		}(i, store)
	}
	wg.Wait()
	close(errs)
	successes := 0
	duplicates := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAlreadyExists):
			duplicates++
		default:
			t.Fatalf("cross-handle duplicate write: %v", err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("successes=%d duplicates=%d; want one of each", successes, duplicates)
	}
}
