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
		tenant_id TEXT NOT NULL DEFAULT '',
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

func seedMachineUser(t *testing.T, store *Store, machineID, userID, tenantID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active'
	)`); err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT OR REPLACE INTO users (id, tenant_id, status) VALUES (?, ?, 'active')`, userID, tenantID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT OR REPLACE INTO machines (id, tenant_id, user_id, status) VALUES (?, ?, ?, 'online')`, machineID, tenantID, userID); err != nil {
		t.Fatalf("seed machine: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 17.1 — Hub admin API 端到端测试
// Flow: Create → Publish → Pull unread → Mark read → Revoke → Verify gone
// Validates: FR-1, FR-3, FR-4, FR-5, FR-6
// ---------------------------------------------------------------------------

func TestIntegration_HubAdminEndToEnd(t *testing.T) {
	svc, store, broadcaster := setupTestService(t)
	ctx := context.Background()
	machineID := "machine-001"
	seedMachineUser(t, store, machineID, "user-001", "tenant_default")

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
	seedMachineUser(t, store, "machine-new", "user-new", "tenant_default")
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
	seedMachineUser(t, store, machineID, "user-002", "tenant_default")

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
		Action:       "new",
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

	broadcaster.reset()
	if err := svc.RevokeFromCascade(ctx, "hc-notif-001"); err != nil {
		t.Fatalf("RevokeFromCascade: %v", err)
	}

	revoked, err := store.FindBySource(ctx, "hubcenter", "hc-notif-001")
	if err != nil {
		t.Fatalf("FindBySource after revoke: %v", err)
	}
	if revoked == nil {
		t.Fatal("expected cascaded notification after revoke")
	}
	if revoked.Status != StatusRevoked {
		t.Fatalf("expected cascaded notification status=revoked, got %s", revoked.Status)
	}

	envelopes = broadcaster.getEnvelopes()
	if len(envelopes) != 1 {
		t.Fatalf("expected 1 revoke envelope after cascade revoke, got %d", len(envelopes))
	}
	if envelopes[0].Payload.Action != "revoke" {
		t.Fatalf("expected action=revoke after cascade revoke, got %s", envelopes[0].Payload.Action)
	}
	if envelopes[0].Payload.NotifID != found.ID {
		t.Fatalf("expected local notification_id=%s in cascade revoke, got %s", found.ID, envelopes[0].Payload.NotifID)
	}

	unread, err = svc.GetUnreadForMachine(ctx, machineID, 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine after cascade revoke: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected no unread after cascade revoke, got %d", len(unread))
	}
}

func TestReadStatsCountsAudienceUsers(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()

	if _, err := store.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active'
	)`); err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO users (id, tenant_id, status) VALUES
		('user-a1', 'tenant_a', 'active'),
		('user-a2', 'tenant_a', 'active'),
		('user-b1', 'tenant_b', 'active'),
		('user-deleted', 'tenant_b', 'deleted')
	`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	allNotif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "All users",
		Content:      "hello everyone",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceAll,
		CreatedBy:    "admin",
	})
	if err != nil {
		t.Fatalf("CreateNotification all: %v", err)
	}
	if err := svc.PublishNotification(ctx, allNotif.ID); err != nil {
		t.Fatalf("PublishNotification all: %v", err)
	}
	allStats, err := svc.GetReadStats(ctx, allNotif.ID)
	if err != nil {
		t.Fatalf("GetReadStats all: %v", err)
	}
	if allStats.TotalPush != 3 {
		t.Fatalf("all users total push = %d, want 3", allStats.TotalPush)
	}

	tenantNotif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Tenant A",
		Content:      "hello tenant a",
		Category:     CategorySystemAnnouncement,
		AudienceType: AudienceTenant,
		AudienceIDs:  []string{"tenant_a"},
		CreatedBy:    "admin",
	})
	if err != nil {
		t.Fatalf("CreateNotification tenant: %v", err)
	}
	if err := svc.PublishNotification(ctx, tenantNotif.ID); err != nil {
		t.Fatalf("PublishNotification tenant: %v", err)
	}
	tenantStats, err := svc.GetReadStats(ctx, tenantNotif.ID)
	if err != nil {
		t.Fatalf("GetReadStats tenant: %v", err)
	}
	if tenantStats.TotalPush != 2 {
		t.Fatalf("tenant users total push = %d, want 2", tenantStats.TotalPush)
	}
}

func TestGetUnreadForMachineFiltersByAudience(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()
	seedMachineUser(t, store, "machine-a", "user-a", "tenant_a")
	seedMachineUser(t, store, "machine-b", "user-b", "tenant_b")

	createAndPublish := func(title string, audience AudienceType, ids []string) {
		t.Helper()
		notif, err := svc.CreateNotification(ctx, CreateRequest{
			Title:        title,
			Content:      "audience test",
			Category:     CategorySystemAnnouncement,
			AudienceType: audience,
			AudienceIDs:  ids,
			CreatedBy:    "admin",
		})
		if err != nil {
			t.Fatalf("CreateNotification %s: %v", title, err)
		}
		if err := svc.PublishNotification(ctx, notif.ID); err != nil {
			t.Fatalf("PublishNotification %s: %v", title, err)
		}
	}

	createAndPublish("all-users", AudienceAll, nil)
	createAndPublish("tenant-a-only", AudienceTenant, []string{"tenant_a"})
	createAndPublish("tenant-b-only", AudienceTenant, []string{"tenant_b"})
	createAndPublish("user-a-only", AudienceUser, []string{"user-a"})

	unreadA, err := svc.GetUnreadForMachine(ctx, "machine-a", 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine machine-a: %v", err)
	}
	titlesA := map[string]bool{}
	for _, item := range unreadA {
		titlesA[item.Title] = true
	}
	for _, title := range []string{"all-users", "tenant-a-only", "user-a-only"} {
		if !titlesA[title] {
			t.Fatalf("machine-a missing %q in unread list: %+v", title, titlesA)
		}
	}
	if titlesA["tenant-b-only"] {
		t.Fatalf("machine-a should not see tenant-b notification: %+v", titlesA)
	}

	unreadB, err := svc.GetUnreadForMachine(ctx, "machine-b", 10)
	if err != nil {
		t.Fatalf("GetUnreadForMachine machine-b: %v", err)
	}
	titlesB := map[string]bool{}
	for _, item := range unreadB {
		titlesB[item.Title] = true
	}
	if !titlesB["all-users"] || !titlesB["tenant-b-only"] {
		t.Fatalf("machine-b missing expected notifications: %+v", titlesB)
	}
	if titlesB["tenant-a-only"] || titlesB["user-a-only"] {
		t.Fatalf("machine-b should not see tenant/user-a notifications: %+v", titlesB)
	}
}

func TestMarkAllReadOnlyMarksVisibleAudience(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()
	seedMachineUser(t, store, "machine-a", "user-a", "tenant_a")
	seedMachineUser(t, store, "machine-b", "user-b", "tenant_b")

	publish := func(title string, audience AudienceType, ids []string) string {
		t.Helper()
		notif, err := svc.CreateNotification(ctx, CreateRequest{
			Title:        title,
			Content:      "mark all read audience test",
			Category:     CategorySystemAnnouncement,
			AudienceType: audience,
			AudienceIDs:  ids,
			CreatedBy:    "admin",
		})
		if err != nil {
			t.Fatalf("CreateNotification %s: %v", title, err)
		}
		if err := svc.PublishNotification(ctx, notif.ID); err != nil {
			t.Fatalf("PublishNotification %s: %v", title, err)
		}
		return notif.ID
	}

	allID := publish("all-users", AudienceAll, nil)
	tenantAID := publish("tenant-a-only", AudienceTenant, []string{"tenant_a"})
	tenantBID := publish("tenant-b-only", AudienceTenant, []string{"tenant_b"})

	if err := svc.MarkAllRead(ctx, "machine-a"); err != nil {
		t.Fatalf("MarkAllRead machine-a: %v", err)
	}

	assertRead := func(notificationID string, want int) {
		t.Helper()
		var got int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_notification_reads WHERE notification_id = ? AND machine_id = 'machine-a'`, notificationID).Scan(&got); err != nil {
			t.Fatalf("count reads for %s: %v", notificationID, err)
		}
		if got != want {
			t.Fatalf("read count for %s = %d, want %d", notificationID, got, want)
		}
	}

	assertRead(allID, 1)
	assertRead(tenantAID, 1)
	assertRead(tenantBID, 0)
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

func TestIntegration_DeleteNotificationRequiresInactiveStatus(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()

	notif, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Delete lifecycle test",
		Content:      "Only inactive notifications should be physically deleted.",
		Category:     CategorySystemAnnouncement,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		CreatedBy:    "admin-delete-test",
	})
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if err := svc.PublishNotification(ctx, notif.ID); err != nil {
		t.Fatalf("PublishNotification: %v", err)
	}

	if err := svc.DeleteNotification(ctx, notif.ID); err == nil {
		t.Fatal("expected published notification delete to be rejected")
	}

	if err := svc.RevokeNotification(ctx, notif.ID); err != nil {
		t.Fatalf("RevokeNotification: %v", err)
	}
	if err := svc.DeleteNotification(ctx, notif.ID); err != nil {
		t.Fatalf("DeleteNotification after revoke: %v", err)
	}

	deleted, err := store.GetByID(ctx, notif.ID)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected notification to be deleted, got status=%s", deleted.Status)
	}

	draft, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Delete draft test",
		Content:      "Draft notifications have not reached clients and can be cleaned up.",
		Category:     CategorySystemAnnouncement,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		CreatedBy:    "admin-delete-test",
	})
	if err != nil {
		t.Fatalf("CreateNotification draft: %v", err)
	}
	if err := svc.DeleteNotification(ctx, draft.ID); err != nil {
		t.Fatalf("DeleteNotification draft: %v", err)
	}

	expired, err := svc.CreateNotification(ctx, CreateRequest{
		Title:        "Delete expired test",
		Content:      "Expired notifications no longer reach clients and can be cleaned up.",
		Category:     CategoryMaintenance,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		CreatedBy:    "admin-delete-test",
	})
	if err != nil {
		t.Fatalf("CreateNotification expired: %v", err)
	}
	if err := store.UpdateStatus(ctx, expired.ID, StatusExpired); err != nil {
		t.Fatalf("UpdateStatus expired: %v", err)
	}
	if err := svc.DeleteNotification(ctx, expired.ID); err != nil {
		t.Fatalf("DeleteNotification expired: %v", err)
	}
}

func TestIntegration_ClientReconnectSync(t *testing.T) {
	svc, store, _ := setupTestService(t)
	ctx := context.Background()
	machineID := "machine-offline-001"
	seedMachineUser(t, store, machineID, "user-offline-001", "tenant_default")

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
