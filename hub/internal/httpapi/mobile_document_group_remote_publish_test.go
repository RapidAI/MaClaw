package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
)

// newCompanionFederationMux mounts every cross-hub endpoint on one mux. The
// in-memory stores are shared inside a test, so two httptest servers on this
// mux play the group origin and a member's home hub at the same time.
func newCompanionFederationMux(identity *auth.IdentityService) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/mobile/documents/groups/{id}/invite-proof", CompanionGroupInviteProofHandler())
	mux.HandleFunc("POST /api/mobile/documents/groups/{id}/remote-decision", CompanionGroupRemoteDecisionHandler(identity))
	mux.HandleFunc("/api/mobile/documents/groups/{id}/remote-shares", CompanionGroupRemoteSharesHandler())
	mux.HandleFunc("POST /api/mobile/documents/groups/remote-invites", CompanionGroupRemoteInviteHandler(identity))
	mux.HandleFunc("POST /api/mobile/documents/groups/remote-invites/revoke", CompanionGroupRemoteRevokeHandler())
	mux.HandleFunc("POST /api/mobile/documents/groups/remote-share-notices", CompanionGroupRemoteShareNoticesHandler(identity))
	return mux
}

func publishRemoteGroupShare(t *testing.T, identity *auth.IdentityService, bearer, groupID, filename string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("days", "0")
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/remote-share", &buf)
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	CompanionGroupRemoteSharePublishHandler(identity).ServeHTTP(rec, req)
	return rec
}

func storedCompanionRemoteInvite(t *testing.T, origin, groupID, contact string) companionRemoteInvite {
	t.Helper()
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	defer companionRemoteInvites.Unlock()
	invite, ok := companionRemoteInvites.items[companionRemoteInviteKey(origin, groupID, contact)]
	if !ok {
		t.Fatalf("remote invite %s/%s/%s was not stored", origin, groupID, contact)
	}
	return invite
}

// A member whose account lives on another hub publishes into the group
// through their own hub: the home hub forwards the upload to the origin with
// the invite secret, the origin stores the share under the synthetic remote
// owner, and the reply carries the grant the desktop saves for later calls.
func TestCompanionRemoteMemberPublishesGroupShare(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)

	mux := newCompanionFederationMux(identity)
	origin := httptest.NewServer(mux)
	defer origin.Close()
	home := httptest.NewServer(mux)
	defer home.Close()

	companionHomeHubLookup = func(context.Context, string) (companionHomeHub, bool) {
		return companionHomeHub{HubID: "hub-b", BaseURL: home.URL}, true
	}
	companionLocalOriginFn = func() string { return origin.URL }

	ownerToken, _ := issueViewerToken(t, identity, "remote-publish-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "remote-publish-reader@example.com")
	strangerToken, _ := issueViewerToken(t, identity, "remote-publish-stranger@example.com")

	groupID := createCompanionGroupAs(t, identity, ownerToken, "跨 Hub 发布组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	contact := normalizeCompanionContact(reader.Email)
	invite := storedCompanionRemoteInvite(t, companionHubBaseURL(origin.URL), groupID, contact)
	if invite.Status != "pending" {
		t.Fatalf("invite status %q, want pending", invite.Status)
	}

	accept := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", nil)
	accept.SetPathValue("id", groupID)
	accept.Header.Set("Authorization", "Bearer "+readerToken)
	acceptRec := httptest.NewRecorder()
	CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(acceptRec, accept)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", acceptRec.Code, acceptRec.Body.String())
	}
	// Shared in-memory stores make the accept land on the local member row
	// instead of the relayed invite row, which is what a real home hub would
	// settle through companionDecideRemoteInvite. Mirror that end state here.
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	key := companionRemoteInviteKey(companionHubBaseURL(origin.URL), groupID, contact)
	invite = companionRemoteInvites.items[key]
	invite.Status = "accepted"
	companionRemoteInvites.items[key] = invite
	companionRemoteInvites.Unlock()

	payload := []byte("%PDF-remote-published")
	published := publishRemoteGroupShare(t, identity, readerToken, groupID, "远端发布.pdf", payload)
	if published.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", published.Code, published.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(published.Body.Bytes(), &view); err != nil {
		t.Fatalf("publish view: %v (%s)", err, published.Body.String())
	}
	token, _ := view["token"].(string)
	if !companionShareTokenPattern.MatchString(token) {
		t.Fatalf("token: %q", token)
	}
	for _, key := range []string{"mine", "admin", "host"} {
		if view[key] != true {
			t.Fatalf("view[%s] = %v, want true (%s)", key, view[key], published.Body.String())
		}
	}
	if view["hub_origin"] != companionHubBaseURL(origin.URL) {
		t.Fatalf("hub_origin: %v", view["hub_origin"])
	}
	if view["reader_grant"] != invite.Secret {
		t.Fatalf("reader_grant: %v want %q", view["reader_grant"], invite.Secret)
	}

	loadCompanionDocShares()
	companionDocShares.Lock()
	stored, ok := companionDocShares.items[token]
	companionDocShares.Unlock()
	if !ok {
		t.Fatalf("share %s not stored", token)
	}
	if stored.RemoteOwner != contact || stored.OwnerID != companionRemoteShareOwnerID(contact, groupID) {
		t.Fatalf("remote owner: %q / %q", stored.RemoteOwner, stored.OwnerID)
	}
	if stored.Visibility != "group" || stored.GroupID != groupID {
		t.Fatalf("audience: %q / %q", stored.Visibility, stored.GroupID)
	}

	// The member's own share list on the origin marks the row as theirs.
	remote := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/groups/"+groupID+"/remote-shares?contact="+contact, nil)
	remote.SetPathValue("id", groupID)
	remote.Header.Set(companionReaderGrantHeader, invite.Secret)
	remoteRec := httptest.NewRecorder()
	CompanionGroupRemoteSharesHandler().ServeHTTP(remoteRec, remote)
	if remoteRec.Code != http.StatusOK || !strings.Contains(remoteRec.Body.String(), token) || !strings.Contains(remoteRec.Body.String(), `"mine":true`) {
		t.Fatalf("remote shares: %d %s", remoteRec.Code, remoteRec.Body.String())
	}

	// The home hub's received list carries the row with its origin and grant.
	received := listCompanionShares(t, identity, readerToken)
	if !strings.Contains(received, token) || !strings.Contains(received, companionHubBaseURL(origin.URL)) || !strings.Contains(received, invite.Secret) {
		t.Fatalf("received shares: %s", received)
	}

	// A member of no group cannot publish through the relay, and a caller
	// without the secret cannot publish directly on the origin.
	if rec := publishRemoteGroupShare(t, identity, strangerToken, groupID, "外人.pdf", payload); rec.Code != http.StatusNotFound {
		t.Fatalf("outsider publish: %d %s", rec.Code, rec.Body.String())
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("days", "0")
	_ = writer.WriteField("contact", contact)
	part, err := writer.CreateFormFile("file", "direct.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	direct := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/remote-shares?contact="+contact, &buf)
	direct.SetPathValue("id", groupID)
	direct.Header.Set("Content-Type", writer.FormDataContentType())
	directRec := httptest.NewRecorder()
	CompanionGroupRemoteSharesHandler().ServeHTTP(directRec, direct)
	if directRec.Code != http.StatusNotFound {
		t.Fatalf("secretless direct publish: %d %s", directRec.Code, directRec.Body.String())
	}
}

// A share notice for a remote member crosses hubs: the origin relays create
// and revoke to the member's home hub, which raises and withdraws the local
// companion_share bell entry for the invited user.
func TestCompanionRemoteShareNoticeRelayCreatesLocalNotification(t *testing.T) {
	resetCompanionDocShares(t)
	trustCompanionOriginsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	var mu sync.Mutex
	var relayed []map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/groups/remote-share-notices", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var body map[string]string
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		relayed = append(relayed, body)
		mu.Unlock()
		CompanionGroupRemoteShareNoticesHandler(identity).ServeHTTP(w, r)
	})
	home := httptest.NewServer(mux)
	defer home.Close()

	const originBase = "http://origin.example"
	companionLocalOriginFn = func() string { return originBase }
	companionHomeHubLookup = func(context.Context, string) (companionHomeHub, bool) {
		return companionHomeHub{HubID: "hub-b", BaseURL: home.URL}, true
	}

	ownerToken, _ := issueViewerToken(t, identity, "notice-relay-owner@example.com")
	_, reader := issueViewerToken(t, identity, "notice-relay-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "通知 relay 组")

	// The remote member has no account on the origin, so the local notify
	// path cannot reach them; the relay is the only way the bell entry lands.
	contact := "away-remote-member@example.com"
	secret, err := newCompanionReaderGrant()
	if err != nil {
		t.Fatal(err)
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group := companionDocGroups.items[groupID]
	group.Members = append(group.Members, companionDocGroupMember{
		Contact:      contact,
		Status:       "accepted",
		InvitedAt:    time.Now().UTC(),
		HomeHubURL:   home.URL,
		InviteSecret: secret,
	})
	companionDocGroups.items[groupID] = group
	companionDocGroups.Unlock()
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	companionRemoteInvites.items[companionRemoteInviteKey(originBase, groupID, contact)] = companionRemoteInvite{
		GroupID:   groupID,
		Name:      "通知 relay 组",
		Owner:     "群主",
		Contact:   contact,
		OriginHub: originBase,
		Secret:    secret,
		Status:    "accepted",
		UserID:    reader.UserID,
	}
	companionRemoteInvites.Unlock()

	created := postCompanionShareWithFields(t, identity, ownerToken, "group", "", groupID, "0", "公告.pdf", "", []byte("%PDF-relayed"))
	if created.Code != http.StatusOK {
		t.Fatalf("share: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)

	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if ok() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	findNotice := func(action string) map[string]string {
		mu.Lock()
		defer mu.Unlock()
		for _, body := range relayed {
			if body["action"] == action && body["token"] == token {
				return body
			}
		}
		return nil
	}
	findNotification := func() *notification.Notification {
		items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
		if err != nil {
			return nil
		}
		for _, item := range items {
			if item.Source == companionShareNotificationSource && item.SourceID == token {
				return item
			}
		}
		return nil
	}

	var relayedCreate map[string]string
	waitFor("create relay", func() bool {
		relayedCreate = findNotice("create")
		return relayedCreate != nil
	})
	if relayedCreate["group_id"] != groupID || relayedCreate["secret"] != secret || relayedCreate["origin_hub"] != originBase {
		t.Fatalf("relayed notice body: %+v", relayedCreate)
	}

	var notice *notification.Notification
	waitFor("relayed bell entry", func() bool {
		notice = findNotification()
		return notice != nil
	})
	if len(notice.AudienceIDs) != 1 || notice.AudienceIDs[0] != reader.UserID {
		t.Fatalf("audience %v want %s", notice.AudienceIDs, reader.UserID)
	}

	// Deleting the share relays a revoke and withdraws the bell entry.
	if rec := deleteCompanionShareAs(t, identity, ownerToken, token); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	waitFor("revoke relay", func() bool { return findNotice("revoke") != nil })
	waitFor("relayed bell entry to be withdrawn", func() bool { return findNotification() == nil })
}

// Removing a remote member must relay the share-notice revokes before the
// invite revoke: the receiving hub proves the secret through the invite row,
// so a notice revoke that lands after that row is gone would 404 and strand
// the bell entry.
func TestCompanionRemoteMemberRemovalRevokesShareNotices(t *testing.T) {
	resetCompanionDocShares(t)
	trustCompanionOriginsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	var mu sync.Mutex
	var calls []string
	record := func(tag string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			calls = append(calls, tag)
			mu.Unlock()
			h(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/groups/remote-share-notices", record("share-notice", CompanionGroupRemoteShareNoticesHandler(identity)))
	mux.HandleFunc("/api/mobile/documents/groups/remote-invites/revoke", record("invite-revoke", CompanionGroupRemoteRevokeHandler()))
	home := httptest.NewServer(mux)
	defer home.Close()

	const originBase = "http://origin.example"
	companionLocalOriginFn = func() string { return originBase }

	ownerToken, _ := issueViewerToken(t, identity, "removal-owner@example.com")
	_, reader := issueViewerToken(t, identity, "removal-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "移除 relay 组")

	contact := "away-removed-member@example.com"
	secret, err := newCompanionReaderGrant()
	if err != nil {
		t.Fatal(err)
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group := companionDocGroups.items[groupID]
	group.Members = append(group.Members, companionDocGroupMember{
		Contact:      contact,
		Status:       "accepted",
		InvitedAt:    time.Now().UTC(),
		HomeHubURL:   home.URL,
		InviteSecret: secret,
	})
	companionDocGroups.items[groupID] = group
	companionDocGroups.Unlock()
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	companionRemoteInvites.items[companionRemoteInviteKey(originBase, groupID, contact)] = companionRemoteInvite{
		GroupID:   groupID,
		Name:      "移除 relay 组",
		Owner:     "群主",
		Contact:   contact,
		OriginHub: originBase,
		Secret:    secret,
		Status:    "accepted",
		UserID:    reader.UserID,
	}
	companionRemoteInvites.Unlock()

	created := postCompanionShareWithFields(t, identity, ownerToken, "group", "", groupID, "0", "公告.pdf", "", []byte("%PDF-removed"))
	if created.Code != http.StatusOK {
		t.Fatalf("share: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)

	findNotification := func() *notification.Notification {
		items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
		if err != nil {
			return nil
		}
		for _, item := range items {
			if item.Source == companionShareNotificationSource && item.SourceID == token {
				return item
			}
		}
		return nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for findNotification() == nil && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if findNotification() == nil {
		t.Fatal("relayed share notice was never created")
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+contact+`"}`))
	remove.SetPathValue("id", groupID)
	remove.Header.Set("Authorization", "Bearer "+ownerToken)
	removeRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(removeRec, remove)
	if removeRec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", removeRec.Code, removeRec.Body.String())
	}

	// The relays run inside the remove call, so the order is already fixed:
	// share-notice revoke lands while the invite row still proves the secret.
	mu.Lock()
	got := append([]string(nil), calls...)
	mu.Unlock()
	noticeIdx, revokeIdx := -1, -1
	for i, call := range got {
		if call == "share-notice" && noticeIdx < 0 {
			noticeIdx = i
		}
		if call == "invite-revoke" && revokeIdx < 0 {
			revokeIdx = i
		}
	}
	if noticeIdx < 0 || revokeIdx < 0 || noticeIdx > revokeIdx {
		t.Fatalf("relay order %v, want share-notice before invite-revoke", got)
	}
	deadline = time.Now().Add(5 * time.Second)
	for findNotification() != nil && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if findNotification() != nil {
		t.Fatal("share notice survived the member's removal")
	}
}

// A member who leaves on their own hub must not strand share notices: the
// home hub tells the origin first, so the revokes it relays back are verified
// while the invite row still exists.
func TestCompanionRemoteMemberLeaveRevokesShareNotices(t *testing.T) {
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

	ownerToken, _ := issueViewerToken(t, identity, "leave-owner@example.com")
	_, reader := issueViewerToken(t, identity, "leave-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "退群 relay 组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	contact := normalizeCompanionContact(reader.Email)
	invite := storedCompanionRemoteInvite(t, companionHubBaseURL(origin.URL), groupID, contact)
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	key := companionRemoteInviteKey(companionHubBaseURL(origin.URL), groupID, contact)
	invite.Status = "accepted"
	companionRemoteInvites.items[key] = invite
	companionRemoteInvites.Unlock()
	// The accepted member row the decision endpoint flips on the origin.
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

	created := postCompanionShareWithFields(t, identity, ownerToken, "group", "", groupID, "0", "公告.pdf", "", []byte("%PDF-leave"))
	if created.Code != http.StatusOK {
		t.Fatalf("share: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)

	findNotification := func() *notification.Notification {
		items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
		if err != nil {
			return nil
		}
		for _, item := range items {
			if item.Source == companionShareNotificationSource && item.SourceID == token {
				return item
			}
		}
		return nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for findNotification() == nil && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if findNotification() == nil {
		t.Fatal("relayed share notice was never created")
	}

	leave := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/leave", nil)
	leave.SetPathValue("id", groupID)
	leaveRec := httptest.NewRecorder()
	if !companionLeaveRemoteInvite(leave.Context(), leaveRec, reader.UserID, map[string]struct{}{contact: {}}, groupID) {
		t.Fatal("leave was not routed to the remote invite")
	}
	if leaveRec.Code != http.StatusOK {
		t.Fatalf("leave: %d %s", leaveRec.Code, leaveRec.Body.String())
	}
	if n := findNotification(); n != nil {
		t.Fatal("share notice survived the member's leave")
	}
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	_, stillThere := companionRemoteInvites.items[key]
	companionRemoteInvites.Unlock()
	if stillThere {
		t.Fatal("remote invite row survived the leave")
	}
}

// Two remote members can share one home hub and one origin group. The notice
// row is keyed by the share token, so the second member's relay must merge
// into the same audience, and one member leaving must not take the other's
// bell entry down with it.
func TestCompanionRemoteShareNoticeMergesAudiences(t *testing.T) {
	resetCompanionDocShares(t)
	trustCompanionOriginsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/mobile/documents/groups/remote-share-notices",
		CompanionGroupRemoteShareNoticesHandler(identity))
	home := httptest.NewServer(mux)
	defer home.Close()

	const originBase = "http://origin-audience.example"
	const groupID = "cg_" + "1234567890abcdef1234567890abcdef"
	companionHomeHubLookup = func(context.Context, string) (companionHomeHub, bool) {
		return companionHomeHub{HubID: "hub-b", BaseURL: home.URL}, true
	}

	_, readerA := issueViewerToken(t, identity, "audience-a@example.com")
	_, readerB := issueViewerToken(t, identity, "audience-b@example.com")
	secretA, err := newCompanionReaderGrant()
	if err != nil {
		t.Fatal(err)
	}
	secretB, err := newCompanionReaderGrant()
	if err != nil {
		t.Fatal(err)
	}
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	for contact, invite := range map[string]companionRemoteInvite{
		"audience-a@example.com": {Secret: secretA, UserID: readerA.UserID},
		"audience-b@example.com": {Secret: secretB, UserID: readerB.UserID},
	} {
		invite.GroupID = groupID
		invite.Name = "共读组"
		invite.OriginHub = originBase
		invite.Contact = contact
		invite.Status = "accepted"
		companionRemoteInvites.items[companionRemoteInviteKey(originBase, groupID, contact)] = invite
	}
	companionRemoteInvites.Unlock()

	const token = "cd_" + "abcdef1234567890abcdef1234567890"
	relay := func(action, secret string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{
			"action":     action,
			"group_id":   groupID,
			"token":      token,
			"name":       "共读.pdf",
			"owner":      "群主",
			"origin_hub": originBase,
			"secret":     secret,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/remote-share-notices", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	audience := func() []string {
		items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
		if err != nil {
			return nil
		}
		for _, item := range items {
			if item.Source == companionShareNotificationSource && item.SourceID == token {
				return item.AudienceIDs
			}
		}
		return nil
	}
	contains := func(list []string, want string) bool {
		for _, got := range list {
			if got == want {
				return true
			}
		}
		return false
	}

	if rec := relay("create", secretA); rec.Code != http.StatusOK {
		t.Fatalf("create A: %d %s", rec.Code, rec.Body.String())
	}
	if rec := relay("create", secretB); rec.Code != http.StatusOK {
		t.Fatalf("create B: %d %s", rec.Code, rec.Body.String())
	}
	got := audience()
	if len(got) != 2 || !contains(got, readerA.UserID) || !contains(got, readerB.UserID) {
		t.Fatalf("audience after two creates = %v", got)
	}

	// Member A leaving pulls only A out; B's entry survives.
	if rec := relay("revoke", secretA); rec.Code != http.StatusOK {
		t.Fatalf("revoke A: %d %s", rec.Code, rec.Body.String())
	}
	got = audience()
	if len(got) != 1 || got[0] != readerB.UserID {
		t.Fatalf("audience after A's revoke = %v", got)
	}

	if rec := relay("revoke", secretB); rec.Code != http.StatusOK {
		t.Fatalf("revoke B: %d %s", rec.Code, rec.Body.String())
	}
	if got := audience(); got != nil {
		t.Fatalf("audience after both revokes = %v", got)
	}
}
