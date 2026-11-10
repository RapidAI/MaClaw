package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestCompanionRemoteSnapshotServesStale(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	principal := &auth.ViewerPrincipal{UserID: "stale-remote-user", Email: "stale-remote@example.com"}
	const ownerID = "stale-remote-user"
	companionRemoteSnapCache.Lock()
	gen := companionRemoteSnapCache.gen
	companionRemoteSnapCache.items[ownerID] = companionRemoteSnapshot{
		gen:    gen,
		at:     time.Now().Add(-time.Hour),
		shares: []map[string]any{{"token": "cd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "chat_revision": float64(9)}},
		stamp:  42,
	}
	companionRemoteSnapCache.Unlock()
	t.Cleanup(func() {
		companionRemoteSnapCache.Lock()
		delete(companionRemoteSnapCache.items, ownerID)
		delete(companionRemoteSnapCache.refreshing, ownerID)
		companionRemoteSnapCache.Unlock()
	})
	_, stamp := companionRemoteShareSnapshot(context.Background(), identity, principal, ownerID)
	if stamp != 42 {
		t.Fatalf("stale snapshot was rebuilt on the request, stamp %d", stamp)
	}
}

func TestCompanionGroupCrossTenantInviteNotifiesAndShowsShares(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken := issueViewerTokenForTenant(t, identity, "tenant-a", "owner-group-cross@example.com")
	readerCtx := auth.WithTenant(context.Background(), "tenant-b")
	reader, err := identity.ManualBindForTenant(readerCtx, "tenant-b", "reader-group-cross@example.com")
	if err != nil {
		t.Fatal(err)
	}
	readerToken, err := identity.IssueViewerTokenForUser(readerCtx, reader.ID)
	if err != nil {
		t.Fatal(err)
	}
	inTenant, _ := identity.LookupUserByEmail(auth.WithTenant(context.Background(), "tenant-a"), reader.Email)
	if inTenant != nil {
		t.Fatalf("reader is visible inside the group tenant: %+v", inTenant)
	}

	groupID := createCompanionGroupAs(t, identity, ownerToken, "跨租户组")
	created := postCompanionShareWithFields(t, identity, ownerToken, "group", "", groupID, "0", "已分享.pdf", "", []byte("%PDF-cross-tenant"))
	if created.Code != http.StatusOK {
		t.Fatalf("share: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)

	notices := pendingInviteNotifications(t, svc)
	if len(notices) != 1 {
		t.Fatalf("notices: %d", len(notices))
	}
	if len(notices[0].AudienceIDs) != 1 || notices[0].AudienceIDs[0] != reader.ID {
		t.Fatalf("audience %v want %s", notices[0].AudienceIDs, reader.ID)
	}
	if strings.Contains(listCompanionShares(t, identity, readerToken), token) {
		t.Fatal("pending member can already see the group document")
	}

	accept := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", nil)
	accept.SetPathValue("id", groupID)
	accept.Header.Set("Authorization", "Bearer "+readerToken)
	acceptRec := httptest.NewRecorder()
	CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(acceptRec, accept)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", acceptRec.Code, acceptRec.Body.String())
	}
	received := listCompanionShares(t, identity, readerToken)
	if !strings.Contains(received, token) {
		t.Fatalf("accepted reader cannot see the document: %s", received)
	}
	if strings.Contains(received, "hub_origin") || strings.Contains(received, "cgr_") {
		t.Fatalf("same-hub share was treated as foreign: %s", received)
	}
	if strings.Contains(listCompanionGroups(t, identity, ownerToken), "cgr_") {
		t.Fatal("group view leaked an invite secret")
	}
}

func TestCompanionGroupRelayInviteAndGrantImport(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "origin-owner@example.com")
	contact := "away-reader@example.com"

	var mu sync.Mutex
	var relayed, revoked []map[string]string
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if strings.HasSuffix(r.URL.Path, "/revoke") {
			revoked = append(revoked, body)
		} else {
			relayed = append(relayed, body)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer home.Close()
	companionHomeHubLookup = func(context.Context, string) (companionHomeHub, bool) {
		return companionHomeHub{HubID: "hub-b", BaseURL: home.URL}, true
	}
	companionLocalOriginFn = func() string { return "http://origin.example" }

	groupID := createCompanionGroupAs(t, identity, ownerToken, "跨 Hub 组")
	created := postCompanionShareWithFields(t, identity, ownerToken, "group", "", groupID, "0", "已有.pdf", "", []byte("%PDF-remote"))
	if created.Code != http.StatusOK {
		t.Fatalf("share: %d %s", created.Code, created.Body.String())
	}
	token := companionShareTokenFrom(t, created)
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, contact)

	mu.Lock()
	if len(relayed) != 1 {
		mu.Unlock()
		t.Fatalf("relayed %d invites", len(relayed))
	}
	secret := relayed[0]["secret"]
	origin := relayed[0]["origin_hub"]
	mu.Unlock()
	if origin != "http://origin.example" || relayed[0]["group_id"] != groupID || relayed[0]["contact"] != contact {
		t.Fatalf("relay body: %+v", relayed[0])
	}
	if !companionReaderGrantPattern.MatchString(secret) {
		t.Fatalf("secret: %q", secret)
	}
	if strings.Contains(listCompanionGroups(t, identity, ownerToken), secret) {
		t.Fatal("owner group view contains the invite secret")
	}

	decision := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/remote-decision", strings.NewReader(fmt.Sprintf(`{"contact":%q,"secret":%q,"action":"accept"}`, contact, secret)))
	decision.SetPathValue("id", groupID)
	decisionRec := httptest.NewRecorder()
	CompanionGroupRemoteDecisionHandler(identity).ServeHTTP(decisionRec, decision)
	if decisionRec.Code != http.StatusOK || !strings.Contains(decisionRec.Body.String(), secret) {
		t.Fatalf("decision: %d %s", decisionRec.Code, decisionRec.Body.String())
	}

	remote := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/groups/"+groupID+"/remote-shares?contact="+contact, nil)
	remote.SetPathValue("id", groupID)
	remote.Header.Set(companionReaderGrantHeader, secret)
	remoteRec := httptest.NewRecorder()
	CompanionGroupRemoteSharesHandler().ServeHTTP(remoteRec, remote)
	if remoteRec.Code != http.StatusOK || !strings.Contains(remoteRec.Body.String(), token) {
		t.Fatalf("remote shares: %d %s", remoteRec.Code, remoteRec.Body.String())
	}

	imported := postCompanionShareImport(t, identity, "", token, fmt.Sprintf(`{"reader_grant":%q}`, secret))
	if imported.Code != http.StatusOK || string(imported.Body.Bytes()) != "%PDF-remote" {
		t.Fatalf("grant import: %d %s", imported.Code, imported.Body.String())
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+contact+`"}`))
	remove.SetPathValue("id", groupID)
	remove.Header.Set("Authorization", "Bearer "+ownerToken)
	removeRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(removeRec, remove)
	if removeRec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", removeRec.Code, removeRec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(revoked) != 1 || revoked[0]["secret"] != secret || revoked[0]["group_id"] != groupID {
		t.Fatalf("revoke: %+v", revoked)
	}
}

func TestCompanionGroupHomeInviteListsRemoteShares(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)
	readerToken, reader := issueViewerToken(t, identity, "home-reader@example.com")

	secret, err := newCompanionReaderGrant()
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := newCompanionGroupID()
	if err != nil {
		t.Fatal(err)
	}
	token := "cd_" + strings.Repeat("ab", 16)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/invite-proof"):
			if r.Header.Get(companionReaderGrantHeader) != secret {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"status":"pending"}`))
		case strings.HasSuffix(r.URL.Path, "/remote-decision"):
			_, _ = w.Write([]byte(`{"ok":true}`))
		case strings.HasSuffix(r.URL.Path, "/remote-shares"):
			if r.Header.Get(companionReaderGrantHeader) != secret {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"shares":[{"token":%q,"name":"already.pdf","revision":1}]}`, token)
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()

	body := fmt.Sprintf(`{"group_id":%q,"name":"对方的组","owner":"用户 A","contact":%q,"origin_hub":%q,"secret":%q}`, groupID, reader.Email, origin.URL, secret)
	inbound := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/remote-invites", strings.NewReader(body))
	inboundRec := httptest.NewRecorder()
	CompanionGroupRemoteInviteHandler(identity).ServeHTTP(inboundRec, inbound)
	if inboundRec.Code != http.StatusOK {
		t.Fatalf("inbound: %d %s", inboundRec.Code, inboundRec.Body.String())
	}
	if !strings.Contains(listCompanionInvites(t, identity, readerToken), groupID) {
		t.Fatal("invite list omitted the remote invitation")
	}
	notices := pendingInviteNotifications(t, svc)
	if len(notices) != 1 || len(notices[0].AudienceIDs) != 1 || notices[0].AudienceIDs[0] != reader.UserID {
		t.Fatalf("notices: %+v", notices)
	}

	accept := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", nil)
	accept.SetPathValue("id", groupID)
	accept.Header.Set("Authorization", "Bearer "+readerToken)
	acceptRec := httptest.NewRecorder()
	CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(acceptRec, accept)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", acceptRec.Code, acceptRec.Body.String())
	}
	received := listCompanionShares(t, identity, readerToken)
	originURL := companionHubBaseURL(origin.URL)
	if !strings.Contains(received, token) || !strings.Contains(received, originURL) || !strings.Contains(received, secret) {
		t.Fatalf("received shares: %s", received)
	}
}
