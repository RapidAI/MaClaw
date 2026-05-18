package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// --- Mock Stores for Confirmation Integration Tests ---
// Prefixed with "ci" to avoid conflicts with other test files in this package.

// ciConfirmStore is an in-memory ConfirmationStore for confirmation integration tests.
type ciConfirmStore struct {
	mu      sync.Mutex
	records map[string]*Confirmation
	order   []string // insertion order for FindOverdue
}

func newCIConfirmStore() *ciConfirmStore {
	return &ciConfirmStore{
		records: make(map[string]*Confirmation),
	}
}

func (s *ciConfirmStore) Create(_ context.Context, conf *Confirmation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conf.ID == "" {
		conf.ID = fmt.Sprintf("conf_%d", len(s.records)+1)
	}
	if conf.CreatedAt.IsZero() {
		conf.CreatedAt = time.Now().UTC()
	}
	if conf.Status == "" {
		conf.Status = ConfirmPending
	}
	c := *conf
	s.records[c.ID] = &c
	s.order = append(s.order, c.ID)
	conf.ID = c.ID
	return nil
}

func (s *ciConfirmStore) Get(_ context.Context, id string) (*Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.records[id]
	if !ok {
		return nil, nil
	}
	copy := *c
	return &copy, nil
}

func (s *ciConfirmStore) UpdateStatus(_ context.Context, id string, status ConfirmationStatus, notes string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.records[id]
	if !ok {
		return fmt.Errorf("confirmation %s not found", id)
	}
	c.Status = status
	c.Notes = notes
	now := time.Now().UTC()
	switch status {
	case ConfirmConfirmed:
		c.ConfirmedAt = &now
	case ConfirmAutoClosed:
		c.AutoClosedAt = &now
	}
	return nil
}

func (s *ciConfirmStore) IncrementReminders(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.records[id]
	if !ok {
		return fmt.Errorf("confirmation %s not found", id)
	}
	c.RemindersSent++
	now := time.Now().UTC()
	c.LastReminderAt = &now
	return nil
}

func (s *ciConfirmStore) ListPending(_ context.Context, recipientID string) ([]Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []Confirmation
	for _, id := range s.order {
		c := s.records[id]
		if c.Status == ConfirmPending && c.RecipientID == recipientID {
			results = append(results, *c)
		}
	}
	return results, nil
}

func (s *ciConfirmStore) ListByInstance(_ context.Context, instanceID string) ([]Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []Confirmation
	for _, id := range s.order {
		c := s.records[id]
		if c.InstanceID == instanceID {
			results = append(results, *c)
		}
	}
	return results, nil
}

func (s *ciConfirmStore) FindOverdue(_ context.Context) ([]Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	var results []Confirmation
	for _, id := range s.order {
		c := s.records[id]
		if c.Status != ConfirmPending {
			continue
		}
		deadline := c.CreatedAt.Add(time.Duration(c.TimeoutHours) * time.Hour)
		if now.After(deadline) {
			results = append(results, *c)
		}
	}
	return results, nil
}

// ciInstanceStore is a minimal InstanceStore for confirmation integration tests.
type ciInstanceStore struct {
	mu        sync.Mutex
	instances map[string]*WorkflowInstance
}

func newCIInstanceStore() *ciInstanceStore {
	return &ciInstanceStore{
		instances: make(map[string]*WorkflowInstance),
	}
}

func (s *ciInstanceStore) Get(_ context.Context, id string) (*WorkflowInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return nil, nil
	}
	return inst, nil
}

func (s *ciInstanceStore) addInstance(inst *WorkflowInstance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.ID] = inst
}

func (s *ciInstanceStore) Create(_ context.Context, _ *WorkflowInstance) error { return nil }
func (s *ciInstanceStore) UpdateStatus(_ context.Context, _ string, _ InstanceStatus) error {
	return nil
}
func (s *ciInstanceStore) UpdateCurrentNode(_ context.Context, _, _ string) error { return nil }
func (s *ciInstanceStore) UpdateInstanceData(_ context.Context, _ string, _ map[string]interface{}) error {
	return nil
}
func (s *ciInstanceStore) CreateNodeExecution(_ context.Context, _ *NodeExecution) error { return nil }
func (s *ciInstanceStore) UpdateNodeExecution(_ context.Context, _ string, _ NodeStatus, _ json.RawMessage, _ string) error {
	return nil
}
func (s *ciInstanceStore) GetPendingApprovals(_ context.Context, _ string) ([]NodeExecution, error) {
	return nil, nil
}
func (s *ciInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *ciInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *ciInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *ciInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// ciAuditStore records audit entries for confirmation integration tests.
type ciAuditStore struct {
	mu      sync.Mutex
	entries []AuditEntry
}

func newCIAuditStore() *ciAuditStore {
	return &ciAuditStore{}
}

func (s *ciAuditStore) Append(_ context.Context, entry *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC().Truncate(time.Millisecond)
	}
	s.entries = append(s.entries, *entry)
	return nil
}

func (s *ciAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *ciAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *ciAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (s *ciAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *ciAuditStore) findByEventType(eventType string) []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []AuditEntry
	for _, e := range s.entries {
		if e.EventType == eventType {
			results = append(results, e)
		}
	}
	return results
}

// ciHubNotifier records in-app notifications for confirmation integration tests.
type ciHubNotifier struct {
	mu    sync.Mutex
	sent  []ciSentNotification
	fails bool
}

type ciSentNotification struct {
	RecipientID string
	Notif       *InAppNotification
}

func (n *ciHubNotifier) Send(_ context.Context, recipientID string, notif *InAppNotification) error {
	if n.fails {
		return fmt.Errorf("hub notifier unavailable")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, ciSentNotification{RecipientID: recipientID, Notif: notif})
	return nil
}

func (n *ciHubNotifier) sentCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.sent)
}

// ciIMPusher records IM push notifications for confirmation integration tests.
type ciIMPusher struct {
	mu        sync.Mutex
	sent      []ciPushRecord
	connected map[string]bool
}

type ciPushRecord struct {
	RecipientID string
	Msg         *IMPushMessage
}

func newCIIMPusher() *ciIMPusher {
	return &ciIMPusher{connected: make(map[string]bool)}
}

func (p *ciIMPusher) Push(_ context.Context, recipientID string, msg *IMPushMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, ciPushRecord{RecipientID: recipientID, Msg: msg})
	return nil
}

func (p *ciIMPusher) IsConnected(_ context.Context, recipientID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connected[recipientID]
}

// --- Test Helpers ---

func buildCITestTracker() (*ConfirmationTracker, *ciConfirmStore, *ciInstanceStore, *ciAuditStore, *ciHubNotifier, *ciIMPusher) {
	confStore := newCIConfirmStore()
	instStore := newCIInstanceStore()
	auditStore := newCIAuditStore()
	hubNotifier := &ciHubNotifier{}
	imPusher := newCIIMPusher()

	dispatcher := NewNotificationDispatcher(hubNotifier, imPusher, auditStore, nil)

	tracker := NewConfirmationTracker(confStore, instStore, dispatcher, auditStore)
	return tracker, confStore, instStore, auditStore, hubNotifier, imPusher
}

// --- Integration Tests ---

func TestIntegration_Confirmation_ReminderFlow(t *testing.T) {
	tracker, confStore, instStore, auditStore, hubNotifier, _ := buildCITestTracker()
	ctx := context.Background()

	instStore.addInstance(&WorkflowInstance{
		ID:     "inst_1",
		Status: InstanceCompleted,
		InstanceData: map[string]interface{}{
			"workflow_name": "请假审批",
			"initiator_id":  "user_initiator",
		},
	})

	pastTime := time.Now().UTC().Add(-50 * time.Hour)
	executorConf := &Confirmation{
		ID: "conf_exec_1", InstanceID: "inst_1", RecipientID: "user_executor",
		Type: ConfirmTypeExecutor, Status: ConfirmPending,
		TimeoutHours: 48, MaxReminders: 3, RemindersSent: 0, ReminderIntervalHours: 24,
		CreatedAt: pastTime,
	}
	notifierConf := &Confirmation{
		ID: "conf_notif_1", InstanceID: "inst_1", RecipientID: "user_notifier",
		Type: ConfirmTypeNotifier, Status: ConfirmPending,
		TimeoutHours: 72, MaxReminders: 2, RemindersSent: 0, ReminderIntervalHours: 24,
		CreatedAt: pastTime,
	}

	if err := confStore.Create(ctx, executorConf); err != nil {
		t.Fatalf("create executor confirmation: %v", err)
	}
	if err := confStore.Create(ctx, notifierConf); err != nil {
		t.Fatalf("create notifier confirmation: %v", err)
	}

	tracker.processOverdueConfirmations(ctx)

	if hubNotifier.sentCount() == 0 {
		t.Fatal("expected at least one reminder notification to be sent")
	}

	execConf, err := confStore.Get(ctx, "conf_exec_1")
	if err != nil {
		t.Fatalf("get executor confirmation: %v", err)
	}
	if execConf.RemindersSent != 1 {
		t.Errorf("expected executor reminders_sent=1, got %d", execConf.RemindersSent)
	}
	if execConf.LastReminderAt == nil {
		t.Error("expected executor last_reminder_at to be set")
	}

	notifConf, err := confStore.Get(ctx, "conf_notif_1")
	if err != nil {
		t.Fatalf("get notifier confirmation: %v", err)
	}
	if notifConf.RemindersSent != 0 {
		t.Errorf("expected notifier reminders_sent=0 (not overdue yet), got %d", notifConf.RemindersSent)
	}

	confStore.mu.Lock()
	confStore.records["conf_notif_1"].CreatedAt = time.Now().UTC().Add(-80 * time.Hour)
	confStore.mu.Unlock()

	tracker.processOverdueConfirmations(ctx)

	notifConf, _ = confStore.Get(ctx, "conf_notif_1")
	if notifConf.RemindersSent != 1 {
		t.Errorf("expected notifier reminders_sent=1 after second process, got %d", notifConf.RemindersSent)
	}

	escalations := auditStore.findByEventType("escalation_triggered")
	if len(escalations) != 0 {
		t.Errorf("expected no escalation events yet, got %d", len(escalations))
	}
}

func TestIntegration_Confirmation_ExecutorEscalation(t *testing.T) {
	tracker, confStore, instStore, auditStore, hubNotifier, _ := buildCITestTracker()
	ctx := context.Background()

	instStore.addInstance(&WorkflowInstance{
		ID:     "inst_2",
		Status: InstanceCompleted,
		InstanceData: map[string]interface{}{
			"workflow_name": "采购审批",
			"initiator_id":  "user_boss",
		},
	})

	pastTime := time.Now().UTC().Add(-100 * time.Hour)
	lastReminder := time.Now().UTC().Add(-25 * time.Hour)
	conf := &Confirmation{
		ID: "conf_exec_escalate", InstanceID: "inst_2", RecipientID: "user_executor_2",
		Type: ConfirmTypeExecutor, Status: ConfirmPending,
		TimeoutHours: 48, MaxReminders: 3, RemindersSent: 3, ReminderIntervalHours: 24,
		LastReminderAt: &lastReminder, CreatedAt: pastTime,
	}

	if err := confStore.Create(ctx, conf); err != nil {
		t.Fatalf("create confirmation: %v", err)
	}

	initialNotifCount := hubNotifier.sentCount()
	tracker.processOverdueConfirmations(ctx)

	if hubNotifier.sentCount() <= initialNotifCount {
		t.Fatal("expected escalation notification to be sent")
	}

	escalations := auditStore.findByEventType("escalation_triggered")
	if len(escalations) == 0 {
		t.Fatal("expected escalation_triggered audit event")
	}
	if escalations[0].InstanceID != "inst_2" {
		t.Errorf("expected instance_id=inst_2, got %s", escalations[0].InstanceID)
	}
	if escalations[0].ActorID != "user_executor_2" {
		t.Errorf("expected actor_id=user_executor_2, got %s", escalations[0].ActorID)
	}

	updatedConf, _ := confStore.Get(ctx, "conf_exec_escalate")
	if updatedConf.Status != ConfirmAutoClosed {
		t.Errorf("expected confirmation status=auto_closed after escalation, got %s", updatedConf.Status)
	}
}

func TestIntegration_Confirmation_NotifierAutoClose(t *testing.T) {
	tracker, confStore, instStore, auditStore, hubNotifier, _ := buildCITestTracker()
	ctx := context.Background()

	instStore.addInstance(&WorkflowInstance{
		ID:     "inst_3",
		Status: InstanceCompleted,
		InstanceData: map[string]interface{}{
			"workflow_name": "报销审批",
			"initiator_id":  "user_finance",
		},
	})

	pastTime := time.Now().UTC().Add(-100 * time.Hour)
	lastReminder := time.Now().UTC().Add(-25 * time.Hour)
	conf := &Confirmation{
		ID: "conf_notif_autoclose", InstanceID: "inst_3", RecipientID: "user_notifier_2",
		Type: ConfirmTypeNotifier, Status: ConfirmPending,
		TimeoutHours: 72, MaxReminders: 2, RemindersSent: 2, ReminderIntervalHours: 24,
		LastReminderAt: &lastReminder, CreatedAt: pastTime,
	}

	if err := confStore.Create(ctx, conf); err != nil {
		t.Fatalf("create confirmation: %v", err)
	}

	initialNotifCount := hubNotifier.sentCount()
	tracker.processOverdueConfirmations(ctx)

	updatedConf, _ := confStore.Get(ctx, "conf_notif_autoclose")
	if updatedConf.Status != ConfirmAutoClosed {
		t.Errorf("expected status=auto_closed, got %s", updatedConf.Status)
	}

	autoClosedEvents := auditStore.findByEventType("auto_closed")
	if len(autoClosedEvents) == 0 {
		t.Fatal("expected auto_closed audit event")
	}
	if autoClosedEvents[0].Details != "notifier_timeout" {
		t.Errorf("expected details=notifier_timeout, got %s", autoClosedEvents[0].Details)
	}
	if autoClosedEvents[0].InstanceID != "inst_3" {
		t.Errorf("expected instance_id=inst_3, got %s", autoClosedEvents[0].InstanceID)
	}

	if hubNotifier.sentCount() != initialNotifCount {
		t.Errorf("expected no notification sent for notifier auto-close, but got %d new notifications",
			hubNotifier.sentCount()-initialNotifCount)
	}
}

func TestIntegration_Confirmation_IntervalRespected(t *testing.T) {
	tracker, confStore, instStore, _, hubNotifier, _ := buildCITestTracker()
	ctx := context.Background()

	instStore.addInstance(&WorkflowInstance{
		ID:     "inst_4",
		Status: InstanceCompleted,
		InstanceData: map[string]interface{}{
			"workflow_name": "出差审批",
			"initiator_id":  "user_admin",
		},
	})

	pastTime := time.Now().UTC().Add(-50 * time.Hour)
	lastReminder := time.Now().UTC().Add(-1 * time.Hour)
	conf := &Confirmation{
		ID: "conf_interval_test", InstanceID: "inst_4", RecipientID: "user_exec_interval",
		Type: ConfirmTypeExecutor, Status: ConfirmPending,
		TimeoutHours: 48, MaxReminders: 3, RemindersSent: 1, ReminderIntervalHours: 24,
		LastReminderAt: &lastReminder, CreatedAt: pastTime,
	}

	if err := confStore.Create(ctx, conf); err != nil {
		t.Fatalf("create confirmation: %v", err)
	}

	initialNotifCount := hubNotifier.sentCount()
	tracker.processOverdueConfirmations(ctx)

	if hubNotifier.sentCount() != initialNotifCount {
		t.Errorf("expected no reminder sent (interval not reached), but got %d new notifications",
			hubNotifier.sentCount()-initialNotifCount)
	}

	updatedConf, _ := confStore.Get(ctx, "conf_interval_test")
	if updatedConf.RemindersSent != 1 {
		t.Errorf("expected reminders_sent=1 (unchanged), got %d", updatedConf.RemindersSent)
	}
}

func TestIntegration_Confirmation_ConfirmMethod(t *testing.T) {
	tracker, confStore, _, auditStore, _, _ := buildCITestTracker()
	ctx := context.Background()

	conf := &Confirmation{
		ID: "conf_confirm_test", InstanceID: "inst_5", RecipientID: "user_exec_confirm",
		Type: ConfirmTypeExecutor, Status: ConfirmPending,
		TimeoutHours: 48, MaxReminders: 3, RemindersSent: 0, ReminderIntervalHours: 24,
		CreatedAt: time.Now().UTC(),
	}

	if err := confStore.Create(ctx, conf); err != nil {
		t.Fatalf("create confirmation: %v", err)
	}

	err := tracker.Confirm(ctx, "conf_confirm_test", "user_exec_confirm", "已完成操作")
	if err != nil {
		t.Fatalf("Confirm failed: %v", err)
	}

	updatedConf, _ := confStore.Get(ctx, "conf_confirm_test")
	if updatedConf.Status != ConfirmConfirmed {
		t.Errorf("expected status=confirmed, got %s", updatedConf.Status)
	}
	if updatedConf.Notes != "已完成操作" {
		t.Errorf("expected notes='已完成操作', got '%s'", updatedConf.Notes)
	}

	confirmEvents := auditStore.findByEventType("executor_confirmed")
	if len(confirmEvents) == 0 {
		t.Fatal("expected executor_confirmed audit event")
	}
	if confirmEvents[0].ActorID != "user_exec_confirm" {
		t.Errorf("expected actor_id=user_exec_confirm, got %s", confirmEvents[0].ActorID)
	}

	err = tracker.Confirm(ctx, "conf_confirm_test", "user_exec_confirm", "再次确认")
	if err != ErrAlreadyConfirmed {
		t.Errorf("expected ErrAlreadyConfirmed on second confirm, got %v", err)
	}
}
