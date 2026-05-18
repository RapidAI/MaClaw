package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mockConfirmationStore is a minimal in-memory ConfirmationStore for testing.
type mockConfirmationStore struct {
	confs         map[string]*Confirmation
	updatedStatus map[string]ConfirmationStatus
	updatedNotes  map[string]string
}

func newMockConfirmationStore() *mockConfirmationStore {
	return &mockConfirmationStore{
		confs:         make(map[string]*Confirmation),
		updatedStatus: make(map[string]ConfirmationStatus),
		updatedNotes:  make(map[string]string),
	}
}

func (m *mockConfirmationStore) Create(_ context.Context, conf *Confirmation) error {
	if conf.ID == "" {
		conf.ID = generateConfirmationID()
	}
	m.confs[conf.ID] = conf
	return nil
}

func (m *mockConfirmationStore) Get(_ context.Context, id string) (*Confirmation, error) {
	conf, ok := m.confs[id]
	if !ok {
		return nil, nil
	}
	return conf, nil
}

func (m *mockConfirmationStore) UpdateStatus(_ context.Context, id string, status ConfirmationStatus, notes string) error {
	conf, ok := m.confs[id]
	if !ok {
		return errors.New("not found")
	}
	conf.Status = status
	conf.Notes = notes
	m.updatedStatus[id] = status
	m.updatedNotes[id] = notes
	return nil
}

func (m *mockConfirmationStore) IncrementReminders(_ context.Context, id string) error {
	conf, ok := m.confs[id]
	if !ok {
		return errors.New("not found")
	}
	conf.RemindersSent++
	now := time.Now().UTC()
	conf.LastReminderAt = &now
	return nil
}

func (m *mockConfirmationStore) ListPending(_ context.Context, recipientID string) ([]Confirmation, error) {
	var results []Confirmation
	for _, c := range m.confs {
		if c.RecipientID == recipientID && c.Status == ConfirmPending {
			results = append(results, *c)
		}
	}
	return results, nil
}

func (m *mockConfirmationStore) ListByInstance(_ context.Context, instanceID string) ([]Confirmation, error) {
	var results []Confirmation
	for _, c := range m.confs {
		if c.InstanceID == instanceID {
			results = append(results, *c)
		}
	}
	return results, nil
}

func (m *mockConfirmationStore) FindOverdue(_ context.Context) ([]Confirmation, error) {
	return nil, nil
}

// mockAuditStoreForConfirm captures audit entries for verification.
type mockAuditStoreForConfirm struct {
	entries []*AuditEntry
}

func (m *mockAuditStoreForConfirm) Append(_ context.Context, entry *AuditEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockAuditStoreForConfirm) QueryByInstance(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func (m *mockAuditStoreForConfirm) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func (m *mockAuditStoreForConfirm) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func (m *mockAuditStoreForConfirm) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func TestConfirm_Success_Executor(t *testing.T) {
	store := newMockConfirmationStore()
	audit := &mockAuditStoreForConfirm{}
	ct := NewConfirmationTracker(store, nil, nil, audit)

	conf := &Confirmation{
		ID:          "conf_test_1",
		InstanceID:  "inst_1",
		RecipientID: "user_exec",
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmPending,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	err := ct.Confirm(context.Background(), "conf_test_1", "user_exec", "done processing")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if store.updatedStatus["conf_test_1"] != ConfirmConfirmed {
		t.Errorf("expected status ConfirmConfirmed, got %s", store.updatedStatus["conf_test_1"])
	}
	if store.updatedNotes["conf_test_1"] != "done processing" {
		t.Errorf("expected notes 'done processing', got '%s'", store.updatedNotes["conf_test_1"])
	}
	if len(audit.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(audit.entries))
	}
	entry := audit.entries[0]
	if entry.EventType != "executor_confirmed" {
		t.Errorf("expected event type 'executor_confirmed', got '%s'", entry.EventType)
	}
	if entry.ActorID != "user_exec" {
		t.Errorf("expected actor 'user_exec', got '%s'", entry.ActorID)
	}
	if entry.InstanceID != "inst_1" {
		t.Errorf("expected instance 'inst_1', got '%s'", entry.InstanceID)
	}
}

func TestConfirm_Success_Notifier(t *testing.T) {
	store := newMockConfirmationStore()
	audit := &mockAuditStoreForConfirm{}
	ct := NewConfirmationTracker(store, nil, nil, audit)

	conf := &Confirmation{
		ID:          "conf_test_2",
		InstanceID:  "inst_2",
		RecipientID: "user_notif",
		Type:        ConfirmTypeNotifier,
		Status:      ConfirmPending,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	err := ct.Confirm(context.Background(), "conf_test_2", "user_notif", "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(audit.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(audit.entries))
	}
	if audit.entries[0].EventType != "notifier_acknowledged" {
		t.Errorf("expected event type 'notifier_acknowledged', got '%s'", audit.entries[0].EventType)
	}
}

func TestConfirm_NotFound(t *testing.T) {
	store := newMockConfirmationStore()
	ct := NewConfirmationTracker(store, nil, nil, nil)

	err := ct.Confirm(context.Background(), "nonexistent", "user1", "")
	if !errors.Is(err, ErrConfirmationNotFound) {
		t.Errorf("expected ErrConfirmationNotFound, got: %v", err)
	}
}

func TestConfirm_RecipientMismatch(t *testing.T) {
	store := newMockConfirmationStore()
	ct := NewConfirmationTracker(store, nil, nil, nil)

	conf := &Confirmation{
		ID:          "conf_test_3",
		InstanceID:  "inst_3",
		RecipientID: "user_a",
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmPending,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	err := ct.Confirm(context.Background(), "conf_test_3", "user_b", "notes")
	if !errors.Is(err, ErrRecipientMismatch) {
		t.Errorf("expected ErrRecipientMismatch, got: %v", err)
	}
}

func TestConfirm_AlreadyProcessed(t *testing.T) {
	store := newMockConfirmationStore()
	ct := NewConfirmationTracker(store, nil, nil, nil)

	conf := &Confirmation{
		ID:          "conf_test_4",
		InstanceID:  "inst_4",
		RecipientID: "user_c",
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmConfirmed,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	err := ct.Confirm(context.Background(), "conf_test_4", "user_c", "notes")
	if !errors.Is(err, ErrAlreadyConfirmed) {
		t.Errorf("expected ErrAlreadyConfirmed, got: %v", err)
	}
}

func TestConfirm_AutoClosedAlsoRejected(t *testing.T) {
	store := newMockConfirmationStore()
	ct := NewConfirmationTracker(store, nil, nil, nil)

	conf := &Confirmation{
		ID:          "conf_test_5",
		InstanceID:  "inst_5",
		RecipientID: "user_d",
		Type:        ConfirmTypeNotifier,
		Status:      ConfirmAutoClosed,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	err := ct.Confirm(context.Background(), "conf_test_5", "user_d", "")
	if !errors.Is(err, ErrAlreadyConfirmed) {
		t.Errorf("expected ErrAlreadyConfirmed, got: %v", err)
	}
}

func TestConfirm_ExecutorNotesTruncatedTo2000Runes(t *testing.T) {
	store := newMockConfirmationStore()
	audit := &mockAuditStoreForConfirm{}
	ct := NewConfirmationTracker(store, nil, nil, audit)

	conf := &Confirmation{
		ID:          "conf_test_6",
		InstanceID:  "inst_6",
		RecipientID: "user_e",
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmPending,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	longNotes := strings.Repeat("A", 2500)
	err := ct.Confirm(context.Background(), "conf_test_6", "user_e", longNotes)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	storedNotes := store.updatedNotes["conf_test_6"]
	if len([]rune(storedNotes)) != 2000 {
		t.Errorf("expected notes truncated to 2000 runes, got %d runes", len([]rune(storedNotes)))
	}
}

func TestConfirm_NotifierNotesCleared(t *testing.T) {
	store := newMockConfirmationStore()
	audit := &mockAuditStoreForConfirm{}
	ct := NewConfirmationTracker(store, nil, nil, audit)

	conf := &Confirmation{
		ID:          "conf_test_7",
		InstanceID:  "inst_7",
		RecipientID: "user_f",
		Type:        ConfirmTypeNotifier,
		Status:      ConfirmPending,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	longNotes := strings.Repeat("x", 3000)
	err := ct.Confirm(context.Background(), "conf_test_7", "user_f", longNotes)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	storedNotes := store.updatedNotes["conf_test_7"]
	if storedNotes != "" {
		t.Errorf("expected notifier notes to be empty, got %d chars", len(storedNotes))
	}
}

func TestConfirm_ExecutorNotesExactly2000RunesNotTruncated(t *testing.T) {
	store := newMockConfirmationStore()
	audit := &mockAuditStoreForConfirm{}
	ct := NewConfirmationTracker(store, nil, nil, audit)

	conf := &Confirmation{
		ID:          "conf_test_8",
		InstanceID:  "inst_8",
		RecipientID: "user_g",
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmPending,
		CreatedAt:   time.Now().UTC(),
	}
	store.confs[conf.ID] = conf

	exactNotes := strings.Repeat("X", 2000)
	err := ct.Confirm(context.Background(), "conf_test_8", "user_g", exactNotes)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	storedNotes := store.updatedNotes["conf_test_8"]
	if len([]rune(storedNotes)) != 2000 {
		t.Errorf("expected notes exactly 2000 runes, got %d runes", len([]rune(storedNotes)))
	}
}
