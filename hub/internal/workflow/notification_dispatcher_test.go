package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// --- Mock implementations for notification dispatcher tests ---

type ndMockHubNotifier struct {
	mu      sync.Mutex
	sent    []*InAppNotification
	sentTo  []string
	failFor map[string]error
}

func newNDMockHubNotifier() *ndMockHubNotifier {
	return &ndMockHubNotifier{failFor: make(map[string]error)}
}

func (m *ndMockHubNotifier) Send(_ context.Context, recipientID string, notif *InAppNotification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.failFor[recipientID]; ok {
		return err
	}
	m.sent = append(m.sent, notif)
	m.sentTo = append(m.sentTo, recipientID)
	return nil
}

type ndMockIMPusher struct {
	mu        sync.Mutex
	pushed    []*IMPushMessage
	pushedTo  []string
	connected map[string]bool
	failFor   map[string]error
}

func newNDMockIMPusher() *ndMockIMPusher {
	return &ndMockIMPusher{connected: make(map[string]bool), failFor: make(map[string]error)}
}

func (m *ndMockIMPusher) Push(_ context.Context, recipientID string, msg *IMPushMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.failFor[recipientID]; ok {
		return err
	}
	m.pushed = append(m.pushed, msg)
	m.pushedTo = append(m.pushedTo, recipientID)
	return nil
}

func (m *ndMockIMPusher) IsConnected(_ context.Context, recipientID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected[recipientID]
}

type ndMockAuditStore struct {
	mu      sync.Mutex
	entries []*AuditEntry
}

func (m *ndMockAuditStore) Append(_ context.Context, entry *AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}
func (m *ndMockAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *ndMockAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *ndMockAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *ndMockAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]AuditEntry, int, error) {
	return nil, 0, nil
}

type ndMockNotifStore struct {
	mu      sync.Mutex
	records []*Notification
}

func (m *ndMockNotifStore) Create(_ context.Context, notif *Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, notif)
	return nil
}
func (m *ndMockNotifStore) Get(_ context.Context, _ string) (*Notification, error) { return nil, nil }
func (m *ndMockNotifStore) ListByInstance(_ context.Context, _ string) ([]Notification, error) {
	return nil, nil
}
func (m *ndMockNotifStore) ListByRecipient(_ context.Context, _ string) ([]Notification, error) {
	return nil, nil
}
func (m *ndMockNotifStore) MarkDelivered(_ context.Context, _ string) error   { return nil }
func (m *ndMockNotifStore) MarkFailed(_ context.Context, _ string, _ string) error { return nil }

// --- Tests ---

func TestDispatch_BothChannelsSuccess(t *testing.T) {
	hub := newNDMockHubNotifier()
	im := newNDMockIMPusher()
	im.connected["user1"] = true
	audit := &ndMockAuditStore{}
	store := &ndMockNotifStore{}
	d := NewNotificationDispatcher(hub, im, audit, store)

	notif := &WorkflowNotification{
		InstanceID: "inst1", Type: NotifTypeResultExecutor, RecipientID: "user1",
		WorkflowName: "请假审批", Result: "approved", FormDataSummary: "年假1天",
		InitiatorName: "张三", InstanceURL: "https://hub.example.com/instances/inst1",
	}
	err := d.Dispatch(context.Background(), notif)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(hub.sentTo) != 1 || hub.sentTo[0] != "user1" {
		t.Errorf("expected hub sent to user1, got: %v", hub.sentTo)
	}
	if len(im.pushedTo) != 1 || im.pushedTo[0] != "user1" {
		t.Errorf("expected IM push to user1, got: %v", im.pushedTo)
	}
	if len(store.records) != 2 {
		t.Errorf("expected 2 notification records, got: %d", len(store.records))
	}
	if len(audit.entries) != 0 {
		t.Errorf("expected no audit entries, got: %d", len(audit.entries))
	}
	if notif.DeliveredAt == nil {
		t.Error("expected DeliveredAt to be set")
	}
	if notif.DeliveryChannel != "hub_inapp,im" {
		t.Errorf("expected channel 'hub_inapp,im', got: %s", notif.DeliveryChannel)
	}
}

func TestDispatch_IMNotConnected_HubOnlySuccess(t *testing.T) {
	hub := newNDMockHubNotifier()
	im := newNDMockIMPusher()
	audit := &ndMockAuditStore{}
	store := &ndMockNotifStore{}
	d := NewNotificationDispatcher(hub, im, audit, store)

	notif := &WorkflowNotification{
		InstanceID: "inst1", Type: NotifTypeNotifier, RecipientID: "user1",
		WorkflowName: "请假审批", Result: "approved", InstanceURL: "https://hub.example.com/inst1",
	}
	err := d.Dispatch(context.Background(), notif)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(hub.sentTo) != 1 {
		t.Errorf("expected 1 hub notification, got: %d", len(hub.sentTo))
	}
	if len(im.pushedTo) != 0 {
		t.Errorf("expected no IM push, got: %d", len(im.pushedTo))
	}
	if len(store.records) != 1 {
		t.Errorf("expected 1 notification record, got: %d", len(store.records))
	}
	if len(audit.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got: %d", len(audit.entries))
	}
	if audit.entries[0].EventType != "im_delivery_failed" {
		t.Errorf("expected 'im_delivery_failed', got: %s", audit.entries[0].EventType)
	}
	if notif.DeliveryChannel != "hub_inapp" {
		t.Errorf("expected channel 'hub_inapp', got: %s", notif.DeliveryChannel)
	}
}

func TestDispatch_IMPushFails_NonFatal(t *testing.T) {
	hub := newNDMockHubNotifier()
	im := newNDMockIMPusher()
	im.connected["user1"] = true
	im.failFor["user1"] = errors.New("feishu timeout")
	audit := &ndMockAuditStore{}
	store := &ndMockNotifStore{}
	d := NewNotificationDispatcher(hub, im, audit, store)

	notif := &WorkflowNotification{
		InstanceID: "inst1", Type: NotifTypeReminder, RecipientID: "user1",
		WorkflowName: "请假审批", InstanceURL: "https://hub.example.com/inst1",
	}
	err := d.Dispatch(context.Background(), notif)
	if err != nil {
		t.Fatalf("expected no error (IM failure non-fatal), got: %v", err)
	}
	if len(hub.sentTo) != 1 {
		t.Errorf("expected 1 hub notification, got: %d", len(hub.sentTo))
	}
	if len(audit.entries) != 1 || audit.entries[0].EventType != "im_delivery_failed" {
		t.Errorf("expected im_delivery_failed audit entry")
	}
	if notif.DeliveryChannel != "hub_inapp" {
		t.Errorf("expected channel 'hub_inapp', got: %s", notif.DeliveryChannel)
	}
}

func TestDispatch_HubFails_ReturnsError(t *testing.T) {
	hub := newNDMockHubNotifier()
	hub.failFor["user1"] = errors.New("hub service unavailable")
	im := newNDMockIMPusher()
	im.connected["user1"] = true
	audit := &ndMockAuditStore{}
	store := &ndMockNotifStore{}
	d := NewNotificationDispatcher(hub, im, audit, store)

	notif := &WorkflowNotification{
		InstanceID: "inst1", Type: NotifTypeResultExecutor, RecipientID: "user1",
		WorkflowName: "请假审批", InstanceURL: "https://hub.example.com/inst1",
	}
	err := d.Dispatch(context.Background(), notif)
	if err == nil {
		t.Fatal("expected error when hub fails")
	}
	if notif.DeliveredAt != nil {
		t.Error("expected DeliveredAt to be nil when hub fails")
	}
}

func TestDispatchBatch_AllSuccess(t *testing.T) {
	hub := newNDMockHubNotifier()
	im := newNDMockIMPusher()
	im.connected["user1"] = true
	im.connected["user2"] = true
	audit := &ndMockAuditStore{}
	store := &ndMockNotifStore{}
	d := NewNotificationDispatcher(hub, im, audit, store)

	notifs := []*WorkflowNotification{
		{InstanceID: "inst1", Type: NotifTypeResultExecutor, RecipientID: "user1", WorkflowName: "审批", InstanceURL: "u1"},
		{InstanceID: "inst1", Type: NotifTypeNotifier, RecipientID: "user2", WorkflowName: "审批", InstanceURL: "u2"},
	}
	err := d.DispatchBatch(context.Background(), notifs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	hub.mu.Lock()
	hubCount := len(hub.sentTo)
	hub.mu.Unlock()
	if hubCount != 2 {
		t.Errorf("expected 2 hub notifications, got: %d", hubCount)
	}
}

func TestDispatchBatch_PartialFailure(t *testing.T) {
	hub := newNDMockHubNotifier()
	hub.failFor["user1"] = errors.New("hub error")
	im := newNDMockIMPusher()
	im.connected["user2"] = true
	audit := &ndMockAuditStore{}
	store := &ndMockNotifStore{}
	d := NewNotificationDispatcher(hub, im, audit, store)

	notifs := []*WorkflowNotification{
		{InstanceID: "inst1", Type: NotifTypeResultExecutor, RecipientID: "user1", WorkflowName: "审批", InstanceURL: "u1"},
		{InstanceID: "inst1", Type: NotifTypeNotifier, RecipientID: "user2", WorkflowName: "审批", InstanceURL: "u2"},
	}
	err := d.DispatchBatch(context.Background(), notifs)
	if err == nil {
		t.Fatal("expected error for partial failure")
	}
	hub.mu.Lock()
	sentCount := len(hub.sentTo)
	hub.mu.Unlock()
	if sentCount != 1 {
		t.Errorf("expected 1 successful hub notification, got: %d", sentCount)
	}
}

func TestDispatchBatch_Empty(t *testing.T) {
	d := NewNotificationDispatcher(newNDMockHubNotifier(), newNDMockIMPusher(), &ndMockAuditStore{}, &ndMockNotifStore{})
	if err := d.DispatchBatch(context.Background(), nil); err != nil {
		t.Fatalf("expected no error for empty batch, got: %v", err)
	}
}

func TestDispatch_GeneratesID(t *testing.T) {
	d := NewNotificationDispatcher(newNDMockHubNotifier(), newNDMockIMPusher(), &ndMockAuditStore{}, &ndMockNotifStore{})
	notif := &WorkflowNotification{
		InstanceID: "inst1", Type: NotifTypeNotifier, RecipientID: "user1",
		WorkflowName: "审批", InstanceURL: "url",
	}
	_ = d.Dispatch(context.Background(), notif)
	if notif.ID == "" {
		t.Error("expected notification ID to be generated")
	}
	if notif.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestBuildInAppNotification_ExecutorType(t *testing.T) {
	notif := &WorkflowNotification{
		Type: NotifTypeResultExecutor, WorkflowName: "请假审批", Result: "approved",
		FormDataSummary: "年假1天", InitiatorName: "张三", InstanceURL: "https://hub/inst/1",
	}
	inApp := buildInAppNotification(notif)
	if inApp.Title != "【待执行】请假审批 - approved" {
		t.Errorf("unexpected title: %s", inApp.Title)
	}
	if inApp.URL != "https://hub/inst/1" {
		t.Errorf("unexpected URL: %s", inApp.URL)
	}
	if inApp.Type != "result_executor" {
		t.Errorf("unexpected type: %s", inApp.Type)
	}
}

func TestBuildIMPushMessage_WithdrawalType(t *testing.T) {
	notif := &WorkflowNotification{
		Type: NotifTypeWithdrawal, WorkflowName: "请假审批", InstanceURL: "https://hub/inst/1",
	}
	msg := buildIMPushMessage(notif)
	if msg.Title != "【已撤回】请假审批" {
		t.Errorf("unexpected title: %s", msg.Title)
	}
	if msg.Body != "发起人已撤回此审批流程，无需进一步操作。" {
		t.Errorf("unexpected body: %s", msg.Body)
	}
}
