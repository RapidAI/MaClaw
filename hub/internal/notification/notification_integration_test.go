package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Mock WSBroadcaster — captures broadcast calls for verification
// ---------------------------------------------------------------------------

type mockWSBroadcaster struct {
	mu        sync.Mutex
	envelopes []capturedEnvelope
}

type capturedEnvelope struct {
	MachineIDs []string // nil for BroadcastToAll
	RawData    []byte
	Payload    NotificationPushPayload
}

func newMockWSBroadcaster() *mockWSBroadcaster {
	return &mockWSBroadcaster{}
}

func (m *mockWSBroadcaster) BroadcastToMachines(machineIDs []string, envelope []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ce := capturedEnvelope{
		MachineIDs: machineIDs,
		RawData:    envelope,
	}
	// Parse the envelope to extract payload
	var env struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(envelope, &env); err == nil {
		var p NotificationPushPayload
		_ = json.Unmarshal(env.Payload, &p)
		ce.Payload = p
	}
	m.envelopes = append(m.envelopes, ce)
	return nil
}

func (m *mockWSBroadcaster) BroadcastToAll(envelope []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ce := capturedEnvelope{
		MachineIDs: nil, // nil indicates BroadcastToAll
		RawData:    envelope,
	}
	var env struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(envelope, &env); err == nil {
		var p NotificationPushPayload
		_ = json.Unmarshal(env.Payload, &p)
		ce.Payload = p
	}
	m.envelopes = append(m.envelopes, ce)
	return nil
}

func (m *mockWSBroadcaster) getEnvelopes() []capturedEnvelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]capturedEnvelope, len(m.envelopes))
	copy(result, m.envelopes)
	return result
}

func (m *mockWSBroadcaster) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envelopes = nil
}

// ---------------------------------------------------------------------------
// Helper: create in-memory SQLite database with notification schema
// ---------------------------------------------------------------------------

func setupTestDB(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	// Create minimal machines table needed for audience resolution
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS machines (
		id       TEXT PRIMARY KEY,
		user_id  TEXT NOT NULL DEFAULT '',
		status   TEXT NOT NULL DEFAULT 'online'
	)`)
	if err != nil {
		t.Fatalf("create machines table: %v", err)
	}

	return db, store
}

func setupTestService(t *testing.T) (*Service, *Store, *mockWSBroadcaster) {
	t.Helper()
	_, store := setupTestDB(t)
	broadcaster := newMockWSBroadcaster()
	svc := NewService(store, broadcaster, nil)
	return svc, store, broadcaster
}

// ---------------------------------------------------------------------------
// Task 17.1 — Hub admin API 端到端测试
// Flow: Create → Publish → Pull unread → Mark read → Revoke → Verify gone
// Validates: FR-1, FR-3, FR-4, FR-5, FR-6
// ---------------------------------------------------------------------------

func TestIntegration_HubAdminEndToEnd(t *testing.T) {
	svc, _, broadcaster := setupTestService(t)
	ctx := context.Background()
	machineID := "machine-001"

	// Step 1: Create notification
	req := CreateRequest{
		Title:        "系统维护公告",
		Content:      "将于今晚 22:00 进行系统维护，预计持续 2 小时。",
		Category:     CategoryMaintenance,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		CreatedBy:    "admin-1",
	}

	notif, err := svc.CreateNotification(ctx, req)
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if notif.ID == "" {
		t.Fatal("expected non-empty notification ID")
	}
	if notif.Status != StatusDraft {
		t.Fatalf("expected status=draft, got %s", notif.Status)
	}

	// Step 2: Publish notification
	if err := svc.PublishNotification(ctx, notif.ID); err != nil {
		t.Fatalf("PublishNotification: %v", err)
	}

	// Verify push envelope was sent (action=new)
	envelopes := broadcaster.getEnvelopes()
	if len(envelopes) == 0 {
		t.Fatal("expected at least one broadcast after publish")
	}
	lastEnv := envelopes[len(envelopes)-1]
	if lastEnv.Payload.Action != "new" {
		t.Fatalf("expected action=new, got %s", lastEnv.Payload.Action)
	}
	if lastEnv.Payload.Notification == nil {
		t.Fatal("expected notification in push payload")
	}
	if lastEnv.Payload.Notification.Title != "系统维护公告" {
		t.Fatalf("unexpected title in envelope: %s", lastEnv.Payload.Notification.Title)
	}

	// Step 3: Pull unread notifications for machine
	unread, err := svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread notification, got %d", len(unread))
	}
	if unread[0].Title != "系统维护公告" {
		t.Fatalf("unexpected unread title: %s", unread[0].Title)
	}
	if unread[0].IsRead {
		t.Fatal("unread notification should have IsRead=false")
	}

	// Step 4: Mark read
	if err := svc.MarkRead(ctx, machineID, notif.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	// Verify no longer in unread list
	unread, err = svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine after mark read: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected 0 unread after mark read, got %d", len(unread))
	}

	// Step 5: Revoke notification
	broadcaster.reset()
	if err := svc.RevokeNotification(ctx, notif.ID); err != nil {
		t.Fatalf("RevokeNotification: %v", err)
	}

	// Verify revoke envelope was sent
	envelopes = broadcaster.getEnvelopes()
	if len(envelopes) == 0 {
		t.Fatal("expected broadcast after revoke")
	}
	revokeEnv := envelopes[len(envelopes)-1]
	if revokeEnv.Payload.Action != "revoke" {
		t.Fatalf("expected action=revoke, got %s", revokeEnv.Payload.Action)
	}
	if revokeEnv.Payload.NotifID != notif.ID {
		t.Fatalf("expected notification_id=%s in revoke payload, got %s", notif.ID, revokeEnv.Payload.NotifID)
	}

	// Step 6: Verify revoked notification doesn't appear in unread
	// Create a new machine that hasn't read anything
	unread, err = svc.GetUnreadForMachine(ctx, "machine-new", 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine for new machine: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected 0 unread for revoked notification, got %d", len(unread))
	}

	// Verify notification status is revoked
	retrieved, err := svc.GetByID(ctx, notif.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if retrieved.Status != StatusRevoked {
		t.Fatalf("expected status=revoked, got %s", retrieved.Status)
	}
}

// ---------------------------------------------------------------------------
// Task 17.2 — HubCenter 级联端到端测试
// Flow: Create from cascade → Hub stores it → Client pulls and verifies
// Validates: FR-2, FR-3
// ---------------------------------------------------------------------------

func TestIntegration_HubCenterCascade(t *testing.T) {
	svc, store, broadcaster := setupTestService(t)
	ctx := context.Background()
	machineID := "machine-002"

	// Simulate HubCenter cascade push
	cascadeNotif := &Notification{
		ID:           "hc-notif-001",
		Title:        "全网安全补丁通知",
		Content:      "请所有用户尽快更新到最新版本，修复 CVE-2026-1234 漏洞。",
		Category:     CategorySecurityAlert,
		Priority:     PriorityUrgent,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		IMPush:       true,
		CreatedBy:    "hubcenter-admin-1",
	}

	req := CascadeRequest{
		Notification: cascadeNotif,
	}

	if err := svc.CreateFromCascade(ctx, req); err != nil {
		t.Fatalf("CreateFromCascade: %v", err)
	}

	// Verify notification was stored with source=hubcenter
	found, err := store.FindBySource(ctx, "hubcenter", "hc-notif-001")
	if err != nil {
		t.Fatalf("FindBySource: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find cascaded notification by source")
	}
	if found.Source != "hubcenter" {
		t.Fatalf("expected source=hubcenter, got %s", found.Source)
	}
	if found.SourceID != "hc-notif-001" {
		t.Fatalf("expected source_id=hc-notif-001, got %s", found.SourceID)
	}
	if found.Status != StatusPublished {
		t.Fatalf("cascade notification should be published immediately, got %s", found.Status)
	}
	if found.Title != "全网安全补丁通知" {
		t.Fatalf("unexpected title: %s", found.Title)
	}

	// Verify push envelope was sent
	envelopes := broadcaster.getEnvelopes()
	if len(envelopes) == 0 {
		t.Fatal("expected broadcast after cascade create")
	}
	lastEnv := envelopes[len(envelopes)-1]
	if lastEnv.Payload.Action != "new" {
		t.Fatalf("expected action=new, got %s", lastEnv.Payload.Action)
	}

	// Client pulls unread notifications
	unread, err := svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread from cascade, got %d", len(unread))
	}
	if unread[0].Title != "全网安全补丁通知" {
		t.Fatalf("unexpected title: %s", unread[0].Title)
	}
	if unread[0].Priority != string(PriorityUrgent) {
		t.Fatalf("expected priority=urgent, got %s", unread[0].Priority)
	}

	// Verify idempotency — cascade same notification again should update, not duplicate
	cascadeNotif.Title = "全网安全补丁通知（已更新）"
	req2 := CascadeRequest{Notification: cascadeNotif}
	if err := svc.CreateFromCascade(ctx, req2); err != nil {
		t.Fatalf("CreateFromCascade (idempotent): %v", err)
	}

	// Should still be 1 notification (updated, not duplicated)
	unread, err = svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine after idempotent cascade: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread after idempotent cascade, got %d", len(unread))
	}
}

// ---------------------------------------------------------------------------
// Task 17.3 — WebSocket 推送验证测试
// Flow: Create → Verify push envelope (action=new) → Revoke → Verify revoke envelope
// Validates: FR-3, NFR-2
// ---------------------------------------------------------------------------

func TestIntegration_WebSocketPushVerification(t *testing.T) {
	svc, _, broadcaster := setupTestService(t)
	ctx := context.Background()

	// Create and publish notification
	req := CreateRequest{
		Title:        "功能更新：AI 助手新版本",
		Content:      "新增通知系统功能，点击铃铛查看通知。",
		Category:     CategoryFeatureUpdate,
		Priority:     PriorityImportant,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		CreatedBy:    "admin-2",
	}

	notif, err := svc.CreateNotification(ctx, req)
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	broadcaster.reset()

	// Publish — should trigger push
	if err := svc.PublishNotification(ctx, notif.ID); err != nil {
		t.Fatalf("PublishNotification: %v", err)
	}

	// Verify push envelope structure (action=new)
	envelopes := broadcaster.getEnvelopes()
	if len(envelopes) != 1 {
		t.Fatalf("expected exactly 1 envelope after publish, got %d", len(envelopes))
	}

	pushEnv := envelopes[0]

	// Verify it was BroadcastToAll (MachineIDs is nil for AudienceAll)
	if pushEnv.MachineIDs != nil {
		t.Fatalf("expected BroadcastToAll (nil machineIDs) for AudienceAll, got %v", pushEnv.MachineIDs)
	}

	// Verify payload content
	if pushEnv.Payload.Action != "new" {
		t.Fatalf("expected action=new, got %s", pushEnv.Payload.Action)
	}
	if pushEnv.Payload.Notification == nil {
		t.Fatal("expected notification in new action payload")
	}
	if pushEnv.Payload.Notification.ID != notif.ID {
		t.Fatalf("notification ID mismatch: %s vs %s", pushEnv.Payload.Notification.ID, notif.ID)
	}
	if pushEnv.Payload.Notification.Status != StatusPublished {
		t.Fatalf("expected published status in envelope, got %s", pushEnv.Payload.Notification.Status)
	}

	// Verify raw envelope JSON structure
	var rawEnv struct {
		Type    string          `json:"type"`
		Ts      int64           `json:"ts"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(pushEnv.RawData, &rawEnv); err != nil {
		t.Fatalf("unmarshal raw envelope: %v", err)
	}
	if rawEnv.Type != NotificationEnvelopeType {
		t.Fatalf("expected type=%s, got %s", NotificationEnvelopeType, rawEnv.Type)
	}
	if rawEnv.Ts == 0 {
		t.Fatal("expected non-zero timestamp in envelope")
	}

	// Revoke — should trigger revoke envelope
	broadcaster.reset()
	if err := svc.RevokeNotification(ctx, notif.ID); err != nil {
		t.Fatalf("RevokeNotification: %v", err)
	}

	envelopes = broadcaster.getEnvelopes()
	if len(envelopes) != 1 {
		t.Fatalf("expected exactly 1 envelope after revoke, got %d", len(envelopes))
	}

	revokeEnv := envelopes[0]
	if revokeEnv.Payload.Action != "revoke" {
		t.Fatalf("expected action=revoke, got %s", revokeEnv.Payload.Action)
	}
	if revokeEnv.Payload.NotifID != notif.ID {
		t.Fatalf("expected notification_id=%s in revoke, got %s", notif.ID, revokeEnv.Payload.NotifID)
	}
	if revokeEnv.Payload.Notification != nil {
		t.Fatal("revoke action should not contain full notification object")
	}
}

// ---------------------------------------------------------------------------
// Task 17.4 — 客户端断线重连同步测试
// Flow: Create → Simulate offline → Reconnect → Pull unread → Verify
// Validates: FR-3, NFR-3
// ---------------------------------------------------------------------------

func TestIntegration_ClientReconnectSync(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()
	machineID := "machine-offline-001"

	// Step 1: Create and publish a notification while client is "offline"
	// (The client simply doesn't pull — simulating offline state)
	req1 := CreateRequest{
		Title:        "维护通知 #1",
		Content:      "第一次维护",
		Category:     CategoryMaintenance,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		CreatedBy:    "admin-1",
	}
	notif1, err := svc.CreateNotification(ctx, req1)
	if err != nil {
		t.Fatalf("CreateNotification #1: %v", err)
	}
	if err := svc.PublishNotification(ctx, notif1.ID); err != nil {
		t.Fatalf("PublishNotification #1: %v", err)
	}

	// Step 2: Create another notification while still offline
	req2 := CreateRequest{
		Title:        "功能更新 #2",
		Content:      "新功能上线",
		Category:     CategoryFeatureUpdate,
		Priority:     PriorityImportant,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		CreatedBy:    "admin-1",
	}
	notif2, err := svc.CreateNotification(ctx, req2)
	if err != nil {
		t.Fatalf("CreateNotification #2: %v", err)
	}
	if err := svc.PublishNotification(ctx, notif2.ID); err != nil {
		t.Fatalf("PublishNotification #2: %v", err)
	}

	// Step 3: Create an expired notification (should NOT appear on reconnect)
	pastTime := time.Now().Add(-1 * time.Hour)
	req3 := CreateRequest{
		Title:        "已过期通知 #3",
		Content:      "这条通知已过期",
		Category:     CategorySystemAnnouncement,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		ExpireAt:     &pastTime,
		CreatedBy:    "admin-1",
	}
	notif3, err := svc.CreateNotification(ctx, req3)
	if err != nil {
		t.Fatalf("CreateNotification #3: %v", err)
	}
	if err := svc.PublishNotification(ctx, notif3.ID); err != nil {
		t.Fatalf("PublishNotification #3: %v", err)
	}

	// Step 4: Create a revoked notification (should NOT appear on reconnect)
	req4 := CreateRequest{
		Title:        "已撤回通知 #4",
		Content:      "这条通知已撤回",
		Category:     CategorySystemAnnouncement,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		AudienceIDs:  []string{},
		CreatedBy:    "admin-1",
	}
	notif4, err := svc.CreateNotification(ctx, req4)
	if err != nil {
		t.Fatalf("CreateNotification #4: %v", err)
	}
	if err := svc.PublishNotification(ctx, notif4.ID); err != nil {
		t.Fatalf("PublishNotification #4: %v", err)
	}
	if err := svc.RevokeNotification(ctx, notif4.ID); err != nil {
		t.Fatalf("RevokeNotification #4: %v", err)
	}

	// Step 5: Simulate client reconnect — pull unread notifications
	unread, err := svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine (reconnect): %v", err)
	}

	// Should see only the 2 valid published notifications (not expired, not revoked)
	if len(unread) != 2 {
		t.Fatalf("expected 2 unread notifications on reconnect, got %d", len(unread))
	}

	// Verify the correct notifications are present (ordered by created_at DESC)
	titles := make(map[string]bool)
	for _, n := range unread {
		titles[n.Title] = true
	}
	if !titles["维护通知 #1"] {
		t.Error("missing '维护通知 #1' in reconnect unread list")
	}
	if !titles["功能更新 #2"] {
		t.Error("missing '功能更新 #2' in reconnect unread list")
	}
	if titles["已过期通知 #3"] {
		t.Error("expired notification should not appear in reconnect unread list")
	}
	if titles["已撤回通知 #4"] {
		t.Error("revoked notification should not appear in reconnect unread list")
	}

	// Step 6: Verify that after reading one, reconnect shows only unread
	if err := svc.MarkRead(ctx, machineID, notif1.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	unread, err = svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine after mark read: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread after marking one read, got %d", len(unread))
	}
	if unread[0].Title != "功能更新 #2" {
		t.Fatalf("expected '功能更新 #2', got '%s'", unread[0].Title)
	}

	// Step 7: Mark all read, verify empty on next reconnect
	if err := svc.MarkAllRead(ctx, machineID); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}

	unread, err = svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine after mark all read: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected 0 unread after mark all read, got %d", len(unread))
	}
}
