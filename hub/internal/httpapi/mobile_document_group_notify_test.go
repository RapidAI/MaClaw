package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
	"github.com/RapidAI/CodeClaw/hub/internal/store"

	_ "modernc.org/sqlite"
)

// newCompanionGroupNotificationService gives a real notification service so the
// invite delivery path is exercised end to end, not stubbed.
func newCompanionGroupNotificationService(t *testing.T) *notification.Service {
	t.Helper()
	svc, _ := newCompanionGroupNotificationServiceAndDB(t)
	return svc
}

// newCompanionGroupNotificationServiceAndDB also returns the database so a test
// can register a machine, which is what the unread pull joins on.
func newCompanionGroupNotificationServiceAndDB(t *testing.T) (*notification.Service, *sql.DB) {
	t.Helper()
	svc, db, _ := newCompanionGroupNotificationServiceWithBroadcaster(t, nil)
	return svc, db
}

// newCompanionGroupNotificationServiceWithBroadcaster wires a recording push
// target so the WebSocket envelope can be inspected.
func newCompanionGroupNotificationServiceWithBroadcaster(t *testing.T, broadcaster notification.WSBroadcaster) (*notification.Service, *sql.DB, notification.WSBroadcaster) {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/notifications.db")
	if err != nil {
		t.Fatalf("open notification db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Delivery is asynchronous while tests poll the same database. SQLite
	// needs a busy wait here, as the Hub database has.
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000"} {
		if _, err := db.Exec(pragma); err != nil {
			t.Fatalf("configure notification db: %v", err)
		}
	}
	// Publishing resolves the audience's machines, and the unread pull joins
	// machines to users. The Hub database has both; the isolated test database
	// needs the same shape.
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS machines (id TEXT PRIMARY KEY, user_id TEXT, status TEXT, tenant_id TEXT)`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, tenant_id TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	store := notification.NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init notification schema: %v", err)
	}
	return notification.NewService(store, broadcaster, nil), db, broadcaster
}

// registerInviteeMachine makes the invitee's desktop a pull target.
func registerInviteeMachine(t *testing.T, db *sql.DB, userID, machineID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO machines (id, user_id, status, tenant_id) VALUES (?, ?, 'active', '')`, machineID, userID); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO users (id, tenant_id) VALUES (?, '')`, userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func withCompanionGroupNotifier(t *testing.T, identity *auth.IdentityService, svc *notification.Service) {
	t.Helper()
	ConfigureCompanionGroupInviteNotifier(identity, svc)
	t.Cleanup(func() { ConfigureCompanionGroupInviteNotifier(nil, nil) })
}

// pendingInviteNotifications returns the live invite notices for one person.
func pendingInviteNotifications(t *testing.T, svc *notification.Service) []*notification.Notification {
	t.Helper()
	items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	out := make([]*notification.Notification, 0, len(items))
	for _, item := range items {
		if item.Source == companionGroupInviteNotificationSource {
			out = append(out, item)
		}
	}
	return out
}

// TestCompanionGroupInviteRaisesNotificationForOfflineInvitee is the delivery
// guarantee: inviting someone who is not connected still stores a notification,
// which is what their next connect pulls.
func TestCompanionGroupInviteRaisesNotificationForOfflineInvitee(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "notify-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "notify-reader@example.com")

	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	notices := pendingInviteNotifications(t, svc)
	if len(notices) != 1 {
		t.Fatalf("want 1 pending invite notice, got %d", len(notices))
	}
	notice := notices[0]
	if notice.AudienceType != notification.AudienceUser {
		t.Fatalf("audience type %q", notice.AudienceType)
	}
	if len(notice.AudienceIDs) != 1 || notice.AudienceIDs[0] != reader.UserID {
		t.Fatalf("audience %v want [%s]", notice.AudienceIDs, reader.UserID)
	}
	if notice.SourceID != companionGroupInviteSourceID(groupID, reader.UserID) {
		t.Fatalf("source id %q", notice.SourceID)
	}
	if !strings.Contains(notice.Content, "论文组") {
		t.Fatalf("content %q", notice.Content)
	}

	// The invite list carries the notice id, which is how a client ties the two.
	var listed struct {
		Invites []struct {
			ID             string `json:"id"`
			NotificationID string `json:"notification_id"`
		} `json:"invites"`
	}
	if err := json.Unmarshal([]byte(listCompanionInvites(t, identity, readerToken)), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Invites) != 1 || listed.Invites[0].NotificationID != notice.ID {
		t.Fatalf("invite notice id: %+v want %s", listed.Invites, notice.ID)
	}
}

// recordingBroadcaster captures the WebSocket envelopes the notification
// service would push to connected machines.
type recordingBroadcaster struct {
	mu         sync.Mutex
	machineIDs []string
	envelopes  [][]byte
}

func (r *recordingBroadcaster) BroadcastToMachines(machineIDs []string, envelope []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.machineIDs = append(r.machineIDs, machineIDs...)
	r.envelopes = append(r.envelopes, append([]byte(nil), envelope...))
	return nil
}

func (r *recordingBroadcaster) BroadcastToAll(envelope []byte) error {
	return r.BroadcastToMachines(nil, envelope)
}

// TestCompanionGroupInvitePushCarriesDeliveryFields pins the online half of the
// contract: the pushed envelope names the invitee's machine and carries the
// source fields the desktop uses to raise its system notification.
func TestCompanionGroupInvitePushCarriesDeliveryFields(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	broadcaster := &recordingBroadcaster{}
	svc, db, _ := newCompanionGroupNotificationServiceWithBroadcaster(t, broadcaster)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "push-owner@example.com")
	_, reader := issueViewerToken(t, identity, "push-reader@example.com")
	registerInviteeMachine(t, db, reader.UserID, "machine-push-1")

	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	broadcaster.mu.Lock()
	envelopes := append([][]byte(nil), broadcaster.envelopes...)
	machineIDs := append([]string(nil), broadcaster.machineIDs...)
	broadcaster.mu.Unlock()

	if len(envelopes) != 1 {
		t.Fatalf("envelopes %d want 1", len(envelopes))
	}
	if len(machineIDs) != 1 || machineIDs[0] != "machine-push-1" {
		t.Fatalf("push targets %v want [machine-push-1]", machineIDs)
	}
	var envelope struct {
		Type    string `json:"type"`
		Payload struct {
			Action       string `json:"action"`
			Notification struct {
				ID           string   `json:"id"`
				Source       string   `json:"source"`
				SourceID     string   `json:"source_id"`
				AudienceType string   `json:"audience_type"`
				AudienceIDs  []string `json:"audience_ids"`
			} `json:"notification"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(envelopes[0], &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Type != "notification.push" || envelope.Payload.Action != "new" {
		t.Fatalf("envelope %s/%s", envelope.Type, envelope.Payload.Action)
	}
	notice := envelope.Payload.Notification
	if notice.ID == "" || notice.Source != notification.SourceCompanionGroupInvite {
		t.Fatalf("pushed notice %+v", notice)
	}
	if notice.SourceID != companionGroupInviteSourceID(groupID, reader.UserID) {
		t.Fatalf("source id %q", notice.SourceID)
	}
	if notice.AudienceType != string(notification.AudienceUser) || len(notice.AudienceIDs) != 1 || notice.AudienceIDs[0] != reader.UserID {
		t.Fatalf("audience %s %v", notice.AudienceType, notice.AudienceIDs)
	}
}

// TestCompanionGroupInviteNotificationPulledByInviteeMachine is the offline
// delivery guarantee end to end: the notice is returned by the same unread pull
// a desktop runs after reconnecting, and it stops once the invitation is settled.
func TestCompanionGroupInviteNotificationPulledByInviteeMachine(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc, db := newCompanionGroupNotificationServiceAndDB(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "pull-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "pull-reader@example.com")
	registerInviteeMachine(t, db, reader.UserID, "machine-reader-1")

	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	unread, err := svc.GetUnreadForMachine(context.Background(), "machine-reader-1", 10)
	if err != nil {
		t.Fatalf("unread pull: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("unread %d want 1", len(unread))
	}
	if unread[0].Source != notification.SourceCompanionGroupInvite {
		t.Fatalf("source %q", unread[0].Source)
	}
	if unread[0].SourceID != companionGroupInviteSourceID(groupID, reader.UserID) {
		t.Fatalf("source id %q", unread[0].SourceID)
	}
	if !strings.Contains(unread[0].Content, "论文组") {
		t.Fatalf("content %q", unread[0].Content)
	}

	// Deciding the invitation takes the notice away from the same pull.
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")
	unread, err = svc.GetUnreadForMachine(context.Background(), "machine-reader-1", 10)
	if err != nil {
		t.Fatalf("unread pull after accept: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("accepted invitation still pulled %d notices", len(unread))
	}
}

// TestCompanionGroupInviteHiddenFromAdminList pins the admin console boundary:
// an invitation delivery row belongs to one reader, not to the admin notice list.
func TestCompanionGroupInviteHiddenFromAdminList(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "admin-list-owner@example.com")
	_, reader := issueViewerToken(t, identity, "admin-list-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	// One admin notice shares the table.
	adminNotice, err := svc.CreateNotification(context.Background(), notification.CreateRequest{
		Title:        "系统维护",
		Content:      "今晚维护。",
		Category:     notification.CategoryMaintenance,
		Priority:     notification.PriorityNormal,
		AudienceType: notification.AudienceUser,
		AudienceIDs:  []string{reader.UserID},
		CreatedBy:    "admin",
	})
	if err != nil {
		t.Fatalf("create admin notice: %v", err)
	}
	if err := svc.PublishNotification(context.Background(), adminNotice.ID); err != nil {
		t.Fatalf("publish admin notice: %v", err)
	}

	items, err := svc.ListNotifications(context.Background(), notification.ListFilter{
		Limit:          50,
		ExcludeSources: []string{notification.SourceCompanionGroupInvite},
	})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(items) != 1 || items[0].ID != adminNotice.ID {
		for _, item := range items {
			t.Logf("listed %s source=%s", item.ID, item.Source)
		}
		t.Fatalf("admin list returned %d notices, want only the admin one", len(items))
	}
}

// TestCompanionGroupInviteAcceptMatchesVerifiedIdentity covers an account whose
// invited address is a verified identity instead of the account email. A
// phone-registered account keeps a synthesized email such as "phone:1709...",
// so this is the common shape for that kind of account. The invitation is
// addressed to the identity, and accepting it must settle the invitation.
func TestCompanionGroupInviteAcceptMatchesVerifiedIdentity(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "identity-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "identity-reader@example.com")
	alias := "alias-reader@example.com"
	user, err := identity.UsersRepo().GetByID(context.Background(), reader.UserID)
	if err != nil || user == nil {
		t.Fatalf("reader user: %+v %v", user, err)
	}
	now := time.Now().UTC()
	if err := identity.UsersRepo().UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:         user.ID + "_alias_email",
		TenantID:   user.TenantID,
		UserID:     user.ID,
		Type:       "email",
		Value:      alias,
		Verified:   true,
		VerifiedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("add verified identity: %v", err)
	}

	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, alias)

	// The list must recognize the identity-addressed invitation too, otherwise
	// the bell only has the delivery notice and every action on it fails.
	if body := listCompanionInvites(t, identity, readerToken); !strings.Contains(body, groupID) {
		t.Fatalf("identity-addressed invite missing from the list: %s", body)
	}

	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")
	if status, ok := companionMemberStatus(groupID, alias); !ok || status != "accepted" {
		t.Fatalf("alias row after accept: %q %v", status, ok)
	}
}

// TestCompanionGroupInviteIgnoresUnverifiedIdentity keeps the user-id
// credential safe: an address nobody verified is only a claim, so it must not
// deliver someone else's invitation to that account.
func TestCompanionGroupInviteIgnoresUnverifiedIdentity(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "unverified-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "unverified-reader@example.com")
	unverified := "unverified-alias@example.com"
	user, err := identity.UsersRepo().GetByID(context.Background(), reader.UserID)
	if err != nil || user == nil {
		t.Fatalf("reader user: %+v %v", user, err)
	}
	now := time.Now().UTC()
	if err := identity.UsersRepo().UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:        user.ID + "_unverified_email",
		TenantID:  user.TenantID,
		UserID:    user.ID,
		Type:      "email",
		Value:     unverified,
		Verified:  false,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add unverified identity: %v", err)
	}

	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, unverified)

	if body := listCompanionInvites(t, identity, readerToken); strings.Contains(body, groupID) {
		t.Fatalf("unverified identity saw the invite: %s", body)
	}
	if got := pendingInviteNotifications(t, svc); len(got) != 0 {
		t.Fatalf("unverified identity got %d notices", len(got))
	}
	// The row is still pending for the admin, but this account cannot settle it.
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", strings.NewReader(`{}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+readerToken)
	rec := httptest.NewRecorder()
	CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unverified accept: %d %s", rec.Code, rec.Body.String())
	}
}

// TestCompanionGroupInviteNotificationRevokedOnAccept covers the "stop nagging
// once decided" half: an accepted invitation withdraws its notice.
func TestCompanionGroupInviteNotificationRevokedOnAccept(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "revoke-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "revoke-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	if len(pendingInviteNotifications(t, svc)) != 1 {
		t.Fatal("invite notice was not created")
	}

	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")
	if got := pendingInviteNotifications(t, svc); len(got) != 0 {
		t.Fatalf("accepted invite kept %d notices", len(got))
	}
}

// TestCompanionGroupInviteNotificationRevokedOnRemoval covers an admin
// withdrawing the invitation.
func TestCompanionGroupInviteNotificationRevokedOnRemoval(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "remove-owner@example.com")
	_, reader := issueViewerToken(t, identity, "remove-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	remove := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+reader.Email+`"}`))
	remove.SetPathValue("id", groupID)
	remove.Header.Set("Authorization", "Bearer "+ownerToken)
	removeRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(removeRec, remove)
	if removeRec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", removeRec.Code, removeRec.Body.String())
	}
	if got := pendingInviteNotifications(t, svc); len(got) != 0 {
		t.Fatalf("removed invite kept %d notices", len(got))
	}
}

// TestCompanionGroupInviteIgnoreSilencesWithoutDeciding pins the difference
// between ignore and decline: the member row survives for the admin, the
// invitee stops being notified, and inviting again re-arms the notice.
func TestCompanionGroupInviteIgnoreSilencesWithoutDeciding(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "ignore-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "ignore-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	postCompanionInviteDecision(t, identity, readerToken, groupID, "ignore")

	if body := listCompanionInvites(t, identity, readerToken); strings.Contains(body, groupID) {
		t.Fatalf("ignored invite still listed: %s", body)
	}
	if got := pendingInviteNotifications(t, svc); len(got) != 0 {
		t.Fatalf("ignored invite kept %d notices", len(got))
	}
	// The admin still sees the person as pending, so the row is not lost.
	if status, ok := companionMemberStatus(groupID, strings.ToLower(reader.Email)); !ok || status != "pending" {
		t.Fatalf("member row after ignore: %q %v", status, ok)
	}

	// Inviting again is how the admin reaches someone who ignored.
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	if body := listCompanionInvites(t, identity, readerToken); !strings.Contains(body, groupID) {
		t.Fatalf("re-invite was not delivered: %s", body)
	}
	if got := pendingInviteNotifications(t, svc); len(got) != 1 {
		t.Fatalf("re-invite notices %d want 1", len(got))
	}
}

// TestCompanionGroupInviteNotificationNotDuplicated keeps a re-invite to a still
// pending person from stacking notices in their bell.
func TestCompanionGroupInviteNotificationNotDuplicated(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "dup-owner@example.com")
	_, reader := issueViewerToken(t, identity, "dup-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	add := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+reader.Email+`"}`))
	add.SetPathValue("id", groupID)
	add.Header.Set("Authorization", "Bearer "+ownerToken)
	addRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(addRec, add)
	if addRec.Code != http.StatusOK {
		t.Fatalf("resend invite: %d %s", addRec.Code, addRec.Body.String())
	}
	if got := pendingInviteNotifications(t, svc); len(got) != 1 {
		t.Fatalf("duplicate invite made %d notices", len(got))
	}
}

// TestCompanionGroupDissolveRevokesInviteNotifications covers a dissolved group.
func TestCompanionGroupDissolveRevokesInviteNotifications(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "dissolve-owner@example.com")
	_, reader := issueViewerToken(t, identity, "dissolve-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	del := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID, nil)
	del.SetPathValue("id", groupID)
	del.Header.Set("Authorization", "Bearer "+ownerToken)
	delRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(delRec, del)
	if delRec.Code != http.StatusOK {
		t.Fatalf("dissolve: %d %s", delRec.Code, delRec.Body.String())
	}
	if got := pendingInviteNotifications(t, svc); len(got) != 0 {
		t.Fatalf("dissolved group kept %d notices", len(got))
	}
}

func createCompanionGroupAs(t *testing.T, identity *auth.IdentityService, token, name string) string {
	t.Helper()
	create := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups", strings.NewReader(`{"name":"`+name+`"}`))
	create.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	CompanionDocumentGroupsHandler(identity).ServeHTTP(rec, create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create group: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	groupID, _ := created["id"].(string)
	if groupID == "" {
		t.Fatalf("create group id: %s", rec.Body.String())
	}
	return groupID
}

func addCompanionGroupMemberAs(t *testing.T, identity *auth.IdentityService, token, groupID, contact string) {
	t.Helper()
	add := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+contact+`"}`))
	add.SetPathValue("id", groupID)
	add.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(rec, add)
	if rec.Code != http.StatusOK {
		t.Fatalf("add member: %d %s", rec.Code, rec.Body.String())
	}
}

// companionDigestNotifications returns the live digest notices addressed to one
// member of one group. The merged design means this must never exceed one row.
func companionDigestNotifications(t *testing.T, svc *notification.Service, groupID, userID string) []*notification.Notification {
	t.Helper()
	items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	sourceID := companionGroupDigestSourceID(groupID, userID)
	out := make([]*notification.Notification, 0, 1)
	for _, item := range items {
		if item.Source == companionGroupDigestNotificationSource && item.SourceID == sourceID {
			out = append(out, item)
		}
	}
	return out
}

// waitCompanionDigest polls until want digest rows are live or the deadline
// passes. Digest creation runs off the accept goroutine, so a direct read can
// legitimately observe zero for a few milliseconds.
func waitCompanionDigest(t *testing.T, svc *notification.Service, groupID, userID string, want int) []*notification.Notification {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []*notification.Notification
	for time.Now().Before(deadline) {
		got = companionDigestNotifications(t, svc, groupID, userID)
		if len(got) == want {
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
	return got
}

// TestCompanionGroupDigestMergesHistoricalShares is the whole point of the
// mechanism: several documents shared before someone joined raise ONE merged
// notice that names the group and the count, never one notice per share.
func TestCompanionGroupDigestMergesHistoricalShares(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "digest-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "历史文档组")

	for _, name := range []string{"一.pdf", "二.pdf"} {
		rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, name, []byte("%PDF-"+name))
		if rec.Code != http.StatusOK {
			t.Fatalf("share %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	// A non-group share of the same owner must not inflate the count.
	if rec := postCompanionShareAs(t, identity, ownerToken, "public", "", "0", "", "公开.pdf", []byte("%PDF-public")); rec.Code != http.StatusOK {
		t.Fatalf("public share: %d %s", rec.Code, rec.Body.String())
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")

	digests := waitCompanionDigest(t, svc, groupID, reader.UserID, 1)
	if len(digests) != 1 {
		t.Fatalf("digest count %d want 1", len(digests))
	}
	digest := digests[0]
	if !strings.Contains(digest.Content, "历史文档组") || !strings.Contains(digest.Content, "2") {
		t.Fatalf("digest content %q", digest.Content)
	}
	if len(digest.AudienceIDs) != 1 || digest.AudienceIDs[0] != reader.UserID {
		t.Fatalf("audience %v want [%s]", digest.AudienceIDs, reader.UserID)
	}
	// The merged notice never produces per-share rows for the same member.
	items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	for _, item := range items {
		if item.Source == companionShareNotificationSource {
			for _, id := range item.AudienceIDs {
				if id == reader.UserID {
					t.Fatalf("reader also got a per-share notice %s", item.SourceID)
				}
			}
		}
	}
}

// TestCompanionGroupDigestSkipsEmptyGroup: joining a group with no readable
// documents has nothing to merge, so no notice is raised.
func TestCompanionGroupDigestSkipsEmptyGroup(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "empty-digest-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "empty-digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "空组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := companionDigestNotifications(t, svc, groupID, reader.UserID); len(got) != 0 {
			t.Fatalf("empty group raised %d digests", len(got))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestCompanionGroupDigestRevokedOnLeave: losing membership takes the merged
// notice away, because the documents it points at are no longer readable.
func TestCompanionGroupDigestRevokedOnLeave(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "leave-digest-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "leave-digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "退群组")
	if rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "文档.pdf", []byte("%PDF-doc")); rec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")
	if got := waitCompanionDigest(t, svc, groupID, reader.UserID, 1); len(got) != 1 {
		t.Fatalf("no digest before leave")
	}

	leave := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/leave", nil)
	leave.SetPathValue("id", groupID)
	leave.Header.Set("Authorization", "Bearer "+readerToken)
	leaveRec := httptest.NewRecorder()
	CompanionDocumentGroupLeaveHandler(identity).ServeHTTP(leaveRec, leave)
	if leaveRec.Code != http.StatusOK {
		t.Fatalf("leave: %d %s", leaveRec.Code, leaveRec.Body.String())
	}
	if got := waitCompanionDigest(t, svc, groupID, reader.UserID, 0); len(got) != 0 {
		t.Fatalf("digest survived the leave")
	}
}

// TestCompanionGroupDigestRevokedOnDissolve covers the admin killing the whole
// group: every member's digest dies with it.
func TestCompanionGroupDigestRevokedOnDissolve(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "dissolve-digest-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "dissolve-digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "解散组")
	if rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "文档.pdf", []byte("%PDF-doc")); rec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")
	if got := waitCompanionDigest(t, svc, groupID, reader.UserID, 1); len(got) != 1 {
		t.Fatalf("no digest before dissolve")
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID, nil)
	del.SetPathValue("id", groupID)
	del.Header.Set("Authorization", "Bearer "+ownerToken)
	delRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(delRec, del)
	if delRec.Code != http.StatusOK {
		t.Fatalf("dissolve: %d %s", delRec.Code, delRec.Body.String())
	}
	if got := waitCompanionDigest(t, svc, groupID, reader.UserID, 0); len(got) != 0 {
		t.Fatalf("digest survived the dissolve")
	}
}

// TestCompanionGroupDigestRefreshedOnRejoin: leaving and coming back must not
// stack a second notice. The re-join digest replaces the old row and counts
// everything shared up to the new join — including what arrived while the
// member was away.
func TestCompanionGroupDigestRefreshedOnRejoin(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "rejoin-digest-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "rejoin-digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "回归组")
	if rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "旧文档.pdf", []byte("%PDF-old")); rec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")
	first := waitCompanionDigest(t, svc, groupID, reader.UserID, 1)
	if len(first) != 1 {
		t.Fatalf("no digest after first join")
	}

	leave := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/leave", nil)
	leave.SetPathValue("id", groupID)
	leave.Header.Set("Authorization", "Bearer "+readerToken)
	leaveRec := httptest.NewRecorder()
	CompanionDocumentGroupLeaveHandler(identity).ServeHTTP(leaveRec, leave)
	if leaveRec.Code != http.StatusOK {
		t.Fatalf("leave: %d %s", leaveRec.Code, leaveRec.Body.String())
	}
	if got := waitCompanionDigest(t, svc, groupID, reader.UserID, 0); len(got) != 0 {
		t.Fatalf("digest survived the leave")
	}

	// One more document arrives while they are out; the new join counts it.
	if rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "新文档.pdf", []byte("%PDF-new")); rec.Code != http.StatusOK {
		t.Fatalf("second share: %d %s", rec.Code, rec.Body.String())
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")

	deadline := time.Now().Add(5 * time.Second)
	for {
		got := companionDigestNotifications(t, svc, groupID, reader.UserID)
		if len(got) == 1 && strings.Contains(got[0].Content, "2") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rejoin digests %d, want one naming 2 documents", len(got))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestCompanionRemoteGroupDigestCountsFetchedShares covers the member whose
// group lives on another hub: the digest counts what the remote-shares fetch
// returns for the just-accepted invite, not the local share table.
func TestCompanionRemoteGroupDigestCountsFetchedShares(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	mux := newCompanionFederationMux(identity)
	origin := httptest.NewServer(mux)
	defer origin.Close()
	home := httptest.NewServer(mux)
	defer home.Close()
	companionLocalOriginFn = func() string { return origin.URL }
	companionHomeHubLookup = func(context.Context, string) (companionHomeHub, bool) {
		return companionHomeHub{HubID: "hub-b", BaseURL: home.URL}, true
	}

	ownerToken, _ := issueViewerToken(t, identity, "remote-digest-owner@example.com")
	_, reader := issueViewerToken(t, identity, "remote-digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "跨 Hub 组")
	for _, name := range []string{"甲.pdf", "乙.pdf", "丙.pdf"} {
		rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, name, []byte("%PDF-"+name))
		if rec.Code != http.StatusOK {
			t.Fatalf("share %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	contact := normalizeCompanionContact(reader.Email)
	invite := storedCompanionRemoteInvite(t, companionHubBaseURL(origin.URL), groupID, contact)
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	key := companionRemoteInviteKey(companionHubBaseURL(origin.URL), groupID, contact)
	invite.Status = "accepted"
	invite.UserID = reader.UserID
	companionRemoteInvites.items[key] = invite
	companionRemoteInvites.Unlock()
	// The remote-shares endpoint proves the member through the origin's group
	// row, which the accept flip would have set on the real member hub.
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group := companionDocGroups.items[groupID]
	for i, member := range group.Members {
		if member.Contact == contact {
			group.Members[i].Status = "accepted"
		}
	}
	companionDocGroups.items[groupID] = group
	companionDocGroups.Unlock()

	companionRemoteGroupJoinedShareDigest(invite, reader.UserID)
	digests := waitCompanionDigest(t, svc, groupID, reader.UserID, 1)
	if len(digests) != 1 {
		t.Fatalf("remote digest count %d want 1", len(digests))
	}
	if !strings.Contains(digests[0].Content, "3") || !strings.Contains(digests[0].Content, "跨 Hub 组") {
		t.Fatalf("remote digest content %q", digests[0].Content)
	}
}

// TestCompanionGroupDigestSkippedWhenMembershipEnded covers the async race:
// creation runs off the accept goroutine, so a member who already left (or a
// group already dissolved) must not get a notice written after its revoke.
func TestCompanionGroupDigestSkippedWhenMembershipEnded(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "race-digest-owner@example.com")
	_, reader := issueViewerToken(t, identity, "race-digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "竞态组")
	if rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "文档.pdf", []byte("%PDF-doc")); rec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	// The membership is already gone when the digest writer runs.
	companionGroupJoinedShareDigestLocal(companionDocGroup{ID: groupID, Name: "竞态组"}, reader.UserID, time.Now().UTC())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := companionDigestNotifications(t, svc, groupID, reader.UserID); len(got) != 0 {
			t.Fatalf("dead membership got %d digests", len(got))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestCompanionGroupDigestSkippedOnSecondContactAccept: an account that
// already joined can hold another pending invitation on a different address.
// Settling it must not rewrite the join digest — the documents it names were
// already news, and a delete-recreate would resurface a read notice.
func TestCompanionGroupDigestSkippedOnSecondContactAccept(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, _ := issueViewerToken(t, identity, "alias-digest-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "alias-digest-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "别名组")
	if rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "文档.pdf", []byte("%PDF-doc")); rec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")
	first := waitCompanionDigest(t, svc, groupID, reader.UserID, 1)
	if len(first) != 1 {
		t.Fatalf("no digest after first accept")
	}

	// A second invitation on another of the account's addresses lands as a
	// fresh pending row keyed on the same user id.
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group := companionDocGroups.items[groupID]
	group.Members = append(group.Members, companionDocGroupMember{
		Contact:   "alias-reader@example.com",
		Status:    "pending",
		UserID:    reader.UserID,
		InvitedAt: time.Now().UTC(),
	})
	companionDocGroups.items[groupID] = group
	if err := persistCompanionDocGroupsLocked(); err != nil {
		companionDocGroups.Unlock()
		t.Fatalf("persist: %v", err)
	}
	companionDocGroups.Unlock()

	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")

	// A spurious refresh would delete and recreate the row under a new id;
	// the surviving id proves no rewrite ran.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := companionDigestNotifications(t, svc, groupID, reader.UserID)
		if len(got) != 1 || got[0].ID != first[0].ID {
			t.Fatalf("second-contact accept rewrote the digest: %d rows", len(got))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestCompanionRemoteGroupDigestSkippedOnSecondContactAccept mirrors the local
// guard: remote invites live one row per invited address, so an account can
// accept a second invitation while already inside. The pending row must
// settle, but the digest delivered at the first join must not be rewritten.
func TestCompanionRemoteGroupDigestSkippedOnSecondContactAccept(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)
	readerToken, reader := issueViewerToken(t, identity, "remote-alias-reader@example.com")

	secretA, err := newCompanionReaderGrant()
	if err != nil {
		t.Fatal(err)
	}
	secretB, err := newCompanionReaderGrant()
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := newCompanionGroupID()
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/invite-proof"):
			_, _ = w.Write([]byte(`{"ok":true,"status":"pending"}`))
		case strings.HasSuffix(r.URL.Path, "/remote-decision"):
			_, _ = w.Write([]byte(`{"ok":true}`))
		case strings.HasSuffix(r.URL.Path, "/remote-shares"):
			_, _ = w.Write([]byte(`{"shares":[{"token":"cd_` + strings.Repeat("ab", 16) + `","name":"历史.pdf","revision":1}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()

	postRemoteInvite := func(contact, secret string) {
		t.Helper()
		body := fmt.Sprintf(`{"group_id":%q,"name":"跨 Hub 组","owner":"群主","contact":%q,"origin_hub":%q,"secret":%q}`, groupID, contact, origin.URL, secret)
		inbound := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/remote-invites", strings.NewReader(body))
		inboundRec := httptest.NewRecorder()
		CompanionGroupRemoteInviteHandler(identity).ServeHTTP(inboundRec, inbound)
		if inboundRec.Code != http.StatusOK {
			t.Fatalf("inbound invite %s: %d %s", contact, inboundRec.Code, inboundRec.Body.String())
		}
	}
	postRemoteInvite(reader.Email, secretA)
	// The relayed invite must name a resolvable contact: register the alias
	// account so the inbound handler accepts it, then point the row at the
	// reader — the same user id two contacts land on.
	_, _ = issueViewerToken(t, identity, "alias-remote@example.com")
	postRemoteInvite("alias-remote@example.com", secretB)
	// The alias row is the same account's second address; the relay resolves
	// the user id on a real home hub.
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	aliasKey := companionRemoteInviteKey(companionHubBaseURL(origin.URL), groupID, normalizeCompanionContact("alias-remote@example.com"))
	alias, ok := companionRemoteInvites.items[aliasKey]
	if !ok {
		companionRemoteInvites.Unlock()
		t.Fatalf("alias invite row missing")
	}
	alias.UserID = reader.UserID
	companionRemoteInvites.items[aliasKey] = alias
	companionRemoteInvites.Unlock()

	postAccept := func() *httptest.ResponseRecorder {
		t.Helper()
		accept := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", nil)
		accept.SetPathValue("id", groupID)
		accept.Header.Set("Authorization", "Bearer "+readerToken)
		rec := httptest.NewRecorder()
		CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(rec, accept)
		return rec
	}
	if rec := postAccept(); rec.Code != http.StatusOK {
		t.Fatalf("first accept: %d %s", rec.Code, rec.Body.String())
	}
	first := waitCompanionDigest(t, svc, groupID, reader.UserID, 1)
	if len(first) != 1 {
		t.Fatalf("no digest after first remote accept")
	}
	if rec := postAccept(); rec.Code != http.StatusOK {
		t.Fatalf("second accept: %d %s", rec.Code, rec.Body.String())
	}

	// A rewrite would delete and recreate the row under a new id.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := companionDigestNotifications(t, svc, groupID, reader.UserID)
		if len(got) != 1 || got[0].ID != first[0].ID {
			t.Fatalf("second-contact accept rewrote the remote digest: %d rows", len(got))
		}
		time.Sleep(25 * time.Millisecond)
	}
	// The second invitation is real: settling it must not be skipped just
	// because the account already holds a seat.
	companionRemoteInvites.Lock()
	pending := 0
	for _, item := range companionRemoteInvites.items {
		if item.GroupID == groupID && item.Status == "pending" {
			pending++
		}
	}
	companionRemoteInvites.Unlock()
	if pending != 0 {
		t.Fatalf("second accept left %d pending invites", pending)
	}
}
