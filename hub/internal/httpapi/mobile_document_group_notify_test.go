package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
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
	if addRec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate invite: %d %s", addRec.Code, addRec.Body.String())
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
