package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
)

func storeCompanionShareForNotifyTest(share companionDocShare) {
	companionDocShares.Lock()
	companionDocShares.items[share.Token] = share
	companionDocShares.Unlock()
}

func TestCompanionShareNotificationReachesAcceptedGroupMembers(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, owner := issueViewerToken(t, identity, "share-notify-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "share-notify-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "通知论文组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")

	share := companionDocShare{
		Token:      "cd_33333333333333333333333333333333",
		Name:       "论文.pdf",
		OwnerID:    owner.UserID,
		OwnerLabel: owner.Email,
		Visibility: "group",
		GroupID:    groupID,
		TenantID:   owner.TenantID,
	}
	storeCompanionShareForNotifyTest(share)
	refreshCompanionShareNotification(share)

	notice, err := svc.FindBySource(context.Background(), companionShareNotificationSource, share.Token)
	if err != nil {
		t.Fatalf("find notice: %v", err)
	}
	if notice == nil || notice.Status != notification.StatusPublished {
		t.Fatalf("share notice = %+v", notice)
	}
	if notice.AudienceType != notification.AudienceUser ||
		len(notice.AudienceIDs) != 1 ||
		notice.AudienceIDs[0] != reader.UserID {
		t.Fatalf("audience = %s %v (owner %s reader %s)", notice.AudienceType, notice.AudienceIDs, owner.UserID, reader.UserID)
	}
	if !strings.Contains(notice.Content, "通知论文组") || !strings.Contains(notice.Content, "论文.pdf") {
		t.Fatalf("content = %q", notice.Content)
	}

	// A later audience edit replaces the notice; public has no recipients.
	share.Visibility = "public"
	share.GroupID = ""
	storeCompanionShareForNotifyTest(share)
	refreshCompanionShareNotification(share)
	if gone, _ := svc.FindBySource(context.Background(), companionShareNotificationSource, share.Token); gone != nil {
		t.Fatalf("public share kept a notice: %+v", gone)
	}
}

func TestCompanionShareNotificationReachesPersonAndIsRevoked(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	_, owner := issueViewerToken(t, identity, "person-notify-owner@example.com")
	_, reader := issueViewerToken(t, identity, "person-notify-reader@example.com")
	share := companionDocShare{
		Token:      "cd_44444444444444444444444444444444",
		Name:       "指定论文.pdf",
		OwnerID:    owner.UserID,
		OwnerLabel: owner.Email,
		Visibility: "person",
		Target:     reader.Email,
		TenantID:   owner.TenantID,
	}
	storeCompanionShareForNotifyTest(share)
	refreshCompanionShareNotification(share)

	notice, err := svc.FindBySource(context.Background(), companionShareNotificationSource, share.Token)
	if err != nil || notice == nil {
		t.Fatalf("person notice = %+v %v", notice, err)
	}
	if len(notice.AudienceIDs) != 1 || notice.AudienceIDs[0] != reader.UserID {
		t.Fatalf("person audience = %v", notice.AudienceIDs)
	}

	revokeCompanionShareNotification(share.Token)
	if gone, _ := svc.FindBySource(context.Background(), companionShareNotificationSource, share.Token); gone != nil {
		t.Fatalf("revoked notice remained: %+v", gone)
	}
}

func TestCreateAndDeleteCompanionShareDriveItsNotification(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	ownerToken, owner := issueViewerToken(t, identity, "handler-notify-owner@example.com")
	readerToken, reader := issueViewerToken(t, identity, "handler-notify-reader@example.com")
	groupID := createCompanionGroupAs(t, identity, ownerToken, "处理通知组")
	addCompanionGroupMemberAs(t, identity, ownerToken, groupID, reader.Email)
	postCompanionInviteDecision(t, identity, readerToken, groupID, "accept")

	rec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "通知.pdf", []byte("%PDF-notify"))
	if rec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	token := companionShareTokenFrom(t, rec)
	var notice *notification.Notification
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := svc.FindBySource(context.Background(), companionShareNotificationSource, token)
		if err == nil && got != nil {
			notice = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if notice == nil {
		t.Fatal("the created share raised no notification")
	}
	if len(notice.AudienceIDs) != 1 || notice.AudienceIDs[0] != reader.UserID || notice.AudienceIDs[0] == owner.UserID {
		t.Fatalf("audience = %v", notice.AudienceIDs)
	}

	if del := deleteCompanionShareAs(t, identity, ownerToken, token); del.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", del.Code, del.Body.String())
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := svc.FindBySource(context.Background(), companionShareNotificationSource, token)
		if err == nil && got == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the deleted share kept its notification")
}

func TestEnsureCompanionShareNotificationDoesNotStack(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	_, owner := issueViewerToken(t, identity, "ensure-owner@example.com")
	_, reader := issueViewerToken(t, identity, "ensure-reader@example.com")
	share := companionDocShare{
		Token:      "cd_66666666666666666666666666666666",
		Name:       "重复分享.pdf",
		OwnerID:    owner.UserID,
		OwnerLabel: owner.Email,
		Visibility: "person",
		Target:     reader.Email,
		TenantID:   owner.TenantID,
	}
	storeCompanionShareForNotifyTest(share)
	ensureCompanionShareNotification(share)
	ensureCompanionShareNotification(share)
	items, err := svc.ListNotifications(context.Background(), notification.ListFilter{Status: notification.StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range items {
		if item.Source == companionShareNotificationSource && item.SourceID == share.Token {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("share notices = %d, want 1", count)
	}
}

func TestCompanionShareHostTransferRequiresAcceptance(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "host-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "host-nominee@example.com")
	const token = "cd_77777777777777777777777777777777"
	share := companionDocShare{
		Token:      token,
		Name:       "主持人转移.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "person",
		Target:     nominee.Email,
		TenantID:   admin.TenantID,
		Importers:  map[string]string{nominee.UserID: "nominee"},
	}
	storeCompanionShareForNotifyTest(share)

	nominated := postCompanionHostAs(t, identity, adminToken, token, nominee.Email)
	if nominated.Code != http.StatusOK {
		t.Fatalf("nominate: %d %s", nominated.Code, nominated.Body.String())
	}
	// The seat has not moved yet.
	if got, _ := companionShareByToken(token); companionShareHostID(got) != admin.UserID {
		t.Fatalf("host moved before acceptance: %s", companionShareHostID(got))
	}
	// A non-admin cannot nominate.
	if rec := postCompanionHostAs(t, identity, nomineeToken, token, nominee.Email); rec.Code != http.StatusForbidden {
		t.Fatalf("nominee nominate: %d %s", rec.Code, rec.Body.String())
	}
	// Accepting moves the seat and keeps the admin.
	accepted := decideCompanionHostAs(t, identity, nomineeToken, token, "accept")
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", accepted.Code, accepted.Body.String())
	}
	moved, _ := companionShareByToken(token)
	if companionShareHostID(moved) != nominee.UserID {
		t.Fatalf("host after accept = %s", companionShareHostID(moved))
	}
	if !companionShareIsAdmin(moved, admin.UserID) || companionShareIsAdmin(moved, nominee.UserID) {
		t.Fatalf("admin rights moved with the host seat")
	}
	// The admin takes it back.
	revoked := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/shares/"+token+"/host", nil)
	revoked.SetPathValue("token", token)
	revoked.Header.Set("Authorization", "Bearer "+adminToken)
	revokeRec := httptest.NewRecorder()
	CompanionDocumentShareHostHandler(identity).ServeHTTP(revokeRec, revoked)
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", revokeRec.Code, revokeRec.Body.String())
	}
	back, _ := companionShareByToken(token)
	if companionShareHostID(back) != admin.UserID {
		t.Fatalf("host after revoke = %s", companionShareHostID(back))
	}
}

func TestCompanionShareHostDeclineKeepsTheSeat(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "decline-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "decline-nominee@example.com")
	const token = "cd_88888888888888888888888888888888"
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "拒绝转移.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "person",
		Target:     nominee.Email,
		TenantID:   admin.TenantID,
	})
	if rec := postCompanionHostAs(t, identity, adminToken, token, nominee.Email); rec.Code != http.StatusOK {
		t.Fatalf("nominate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := decideCompanionHostAs(t, identity, nomineeToken, token, "decline"); rec.Code != http.StatusOK {
		t.Fatalf("decline: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := companionShareByToken(token)
	if companionShareHostID(got) != admin.UserID || strings.TrimSpace(got.PendingHostID) != "" {
		t.Fatalf("decline changed the seat: host=%s pending=%s", companionShareHostID(got), got.PendingHostID)
	}
	// Only the nominee can answer; a second decision finds nothing pending.
	if rec := decideCompanionHostAs(t, identity, nomineeToken, token, "accept"); rec.Code != http.StatusForbidden {
		t.Fatalf("second decision: %d %s", rec.Code, rec.Body.String())
	}
}

func postCompanionHostAs(t *testing.T, identity *auth.IdentityService, viewer, token, contact string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/host", strings.NewReader(`{"contact":"`+contact+`"}`))
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentShareHostHandler(identity).ServeHTTP(rec, req)
	return rec
}

func decideCompanionHostAs(t *testing.T, identity *auth.IdentityService, viewer, token, decision string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/host/"+decision, nil)
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	if decision == "accept" {
		CompanionDocumentShareHostAcceptHandler(identity).ServeHTTP(rec, req)
	} else {
		CompanionDocumentShareHostDeclineHandler(identity).ServeHTTP(rec, req)
	}
	return rec
}

func TestCompanionShareHostTransferNotifiesNomineeAndParticipants(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "notice-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "notice-nominee@example.com")
	otherToken, other := issueViewerToken(t, identity, "notice-other@example.com")
	const token = "cd_99999999999999999999999999999999"
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "通知转移.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "person",
		Target:     nominee.Email,
		TenantID:   admin.TenantID,
		Importers:  map[string]string{nominee.UserID: "nominee", other.UserID: "other"},
	})
	// The other importer also has access; a person share still notifies it
	// only if it is the target, so use the group path for a second reader.
	if rec := postCompanionHostAs(t, identity, adminToken, token, nominee.Email); rec.Code != http.StatusOK {
		t.Fatalf("nominate: %d %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	var request *notification.Notification
	for time.Now().Before(deadline) {
		got, err := svc.FindBySource(context.Background(), notification.SourceCompanionHostTransfer, token)
		if err == nil && got != nil {
			request = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request == nil {
		t.Fatal("the nominee received no transfer request")
	}
	if len(request.AudienceIDs) != 1 || request.AudienceIDs[0] != nominee.UserID {
		t.Fatalf("request audience = %v", request.AudienceIDs)
	}
	var notice *notification.Notification
	for time.Now().Before(deadline) {
		got, err := svc.FindBySource(context.Background(), notification.SourceCompanionHostTransferNotice, token)
		if err == nil && got != nil {
			notice = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if notice == nil {
		t.Fatal("participants were not told about the nomination")
	}
	found := false
	for _, id := range notice.AudienceIDs {
		if id == nominee.UserID {
			t.Fatalf("the nominee got both the request and the notice: %v", notice.AudienceIDs)
		}
		if id == admin.UserID {
			found = true
		}
	}
	if !found {
		t.Fatalf("admin missing from the participant notice: %v", notice.AudienceIDs)
	}

	// Accepting replaces the request with an outcome notice for everyone.
	if rec := decideCompanionHostAs(t, identity, nomineeToken, token, "accept"); rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if gone, _ := svc.FindBySource(context.Background(), notification.SourceCompanionHostTransfer, token); gone == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gone, _ := svc.FindBySource(context.Background(), notification.SourceCompanionHostTransfer, token); gone != nil {
		t.Fatal("the accepted request notice was not withdrawn")
	}
	_ = otherToken
}

func TestCompanionShareHostSeatClearedWhenTheHostLosesAccess(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "lost-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "lost-nominee@example.com")
	const token = "cd_aaaa1111111111111111111111111111"
	groupID := createCompanionGroupAs(t, identity, adminToken, "丢失访问组")
	addCompanionGroupMemberAs(t, identity, adminToken, groupID, nominee.Email)
	postCompanionInviteDecision(t, identity, nomineeToken, groupID, "accept")
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "丢失访问.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "group",
		GroupID:    groupID,
		TenantID:   admin.TenantID,
	})
	if rec := postCompanionHostAs(t, identity, adminToken, token, nominee.Email); rec.Code != http.StatusOK {
		t.Fatalf("nominate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := decideCompanionHostAs(t, identity, nomineeToken, token, "accept"); rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	seated, _ := companionShareByToken(token)
	if companionShareHostID(seated) != nominee.UserID {
		t.Fatalf("host = %s", companionShareHostID(seated))
	}
	// Opening the share records the reader as an importer. That must not keep
	// the seat after they leave the group: group access is membership, not import.
	if seated.Importers == nil {
		seated.Importers = map[string]string{}
	}
	seated.Importers[nominee.UserID] = "imported"
	storeCompanionShareForNotifyTest(seated)

	// The admin removes the member; the seat must not stay with someone who
	// can no longer open the document.
	removeMember(t, identity, adminToken, groupID, nominee.Email)
	got, _ := companionShareByToken(token)
	if companionShareHostID(got) != admin.UserID {
		t.Fatalf("host after removal = %s", companionShareHostID(got))
	}
	if strings.TrimSpace(got.PendingHostID) != "" {
		t.Fatalf("pending survived removal: %s", got.PendingHostID)
	}
}

func removeMember(t *testing.T, identity *auth.IdentityService, viewer, groupID, contact string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+contact+`"}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove member: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCompanionShareHostCancelDoesNotRevokeAnAcceptedSeat(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "cancel-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "cancel-nominee@example.com")
	const token = "cd_bbbb2222222222222222222222222222"
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "取消不会收回.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "person",
		Target:     nominee.Email,
		TenantID:   admin.TenantID,
		Importers:  map[string]string{nominee.UserID: "nominee"},
	})
	if rec := postCompanionHostAs(t, identity, adminToken, token, nominee.Email); rec.Code != http.StatusOK {
		t.Fatalf("nominate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := decideCompanionHostAs(t, identity, nomineeToken, token, "accept"); rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	// The dialog still says 取消转移. That request must not take the seat back.
	if rec := deleteCompanionHostAs(t, identity, adminToken, token, "cancel"); rec.Code != http.StatusConflict {
		t.Fatalf("stale cancel: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := companionShareByToken(token)
	if companionShareHostID(got) != nominee.UserID {
		t.Fatalf("stale cancel moved the host: %s", companionShareHostID(got))
	}
	if rec := deleteCompanionHostAs(t, identity, adminToken, token, "revoke"); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	got, _ = companionShareByToken(token)
	if companionShareHostID(got) != admin.UserID {
		t.Fatalf("host after revoke = %s", companionShareHostID(got))
	}
}

func TestCompanionShareHostResultNoticeDoesNotClobberANewerNomination(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "stale-admin@example.com")
	nominee, _ := issueViewerToken(t, identity, "stale-nominee@example.com")
	const token = "cd_cccc3333333333333333333333333333"
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "过期结果.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "person",
		Target:     "stale-nominee@example.com",
		TenantID:   admin.TenantID,
	})
	_ = nominee
	if rec := postCompanionHostAs(t, identity, adminToken, token, "stale-nominee@example.com"); rec.Code != http.StatusOK {
		t.Fatalf("nominate: %d %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	var request *notification.Notification
	for time.Now().Before(deadline) {
		got, err := svc.FindBySource(context.Background(), notification.SourceCompanionHostTransfer, token)
		if err == nil && got != nil {
			request = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request == nil {
		t.Fatal("the nominee received no transfer request")
	}
	current, _ := companionShareByToken(token)
	// An outcome decided before this nomination must not withdraw it.
	notifyCompanionHostTransferResult(current, "过期的主持人变更", companionHostSeatStamp{})
	if gone, _ := svc.FindBySource(context.Background(), notification.SourceCompanionHostTransfer, token); gone == nil || gone.ID != request.ID {
		t.Fatalf("stale result removed the live request: %+v", gone)
	}
}

func TestCompanionShareAudienceChangeReclaimsHostWhoCannotOpen(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "audience-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "audience-nominee@example.com")
	_, reader := issueViewerToken(t, identity, "audience-reader@example.com")
	const token = "cd_dddd4444444444444444444444444444"
	groupID := createCompanionGroupAs(t, identity, adminToken, "改范围组")
	addCompanionGroupMemberAs(t, identity, adminToken, groupID, nominee.Email)
	postCompanionInviteDecision(t, identity, nomineeToken, groupID, "accept")
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:            token,
		Name:             "改范围.pdf",
		OwnerID:          admin.UserID,
		OwnerLabel:       admin.Email,
		Visibility:       "group",
		GroupID:          groupID,
		TenantID:         admin.TenantID,
		HostID:           nominee.UserID,
		HostLabel:        nominee.Email,
		Importers:        map[string]string{nominee.UserID: "imported"},
		PendingHostID:    reader.UserID,
		PendingHostLabel: reader.Email,
		PendingHostSince: time.Now().UTC(),
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/shares/"+token, strings.NewReader(`{"visibility":"person","target":"`+reader.Email+`","days":0}`))
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	CompanionDocumentShareUpdateHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"host":true`) {
		t.Fatalf("update hid the seat: %s", rec.Body.String())
	}
	got, _ := companionShareByToken(token)
	if companionShareHostID(got) != admin.UserID {
		t.Fatalf("host after audience change = %s", companionShareHostID(got))
	}
	if strings.TrimSpace(got.PendingHostID) != "" {
		t.Fatalf("pending survived the audience change: %s", got.PendingHostID)
	}
}

func TestCompanionShareDissolvedGroupClearsHostAndNominationTogether(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "dissolve-admin@example.com")
	_, hostUser := issueViewerToken(t, identity, "dissolve-host@example.com")
	_, pendingUser := issueViewerToken(t, identity, "dissolve-pending@example.com")
	const token = "cd_eeee5555555555555555555555555555"
	groupID := createCompanionGroupAs(t, identity, adminToken, "解散席位组")
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:            token,
		Name:             "解散.pdf",
		OwnerID:          admin.UserID,
		OwnerLabel:       admin.Email,
		Visibility:       "group",
		GroupID:          groupID,
		TenantID:         admin.TenantID,
		HostID:           hostUser.UserID,
		HostLabel:        hostUser.Email,
		PendingHostID:    pendingUser.UserID,
		PendingHostLabel: pendingUser.Email,
		PendingHostSince: time.Now().UTC(),
		Importers:        map[string]string{hostUser.UserID: "imported", pendingUser.UserID: "imported"},
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID, nil)
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dissolve: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := companionShareByToken(token)
	if companionShareHostID(got) != admin.UserID {
		t.Fatalf("host after dissolve = %s", companionShareHostID(got))
	}
	if strings.TrimSpace(got.PendingHostID) != "" {
		t.Fatalf("pending survived dissolve: %s", got.PendingHostID)
	}
}

// A nomination can outlive the membership that made the nominee a participant:
// the removal handler reconciles the seat, but it can interleave with an accept
// that is already in flight. The seat must not move to someone who can no
// longer open the document.
func TestCompanionShareHostAcceptRefusesANomineeWhoLostAccess(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "race-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "race-nominee@example.com")
	const token = "cd_ffff6666666666666666666666666666"
	groupID := createCompanionGroupAs(t, identity, adminToken, "竞态席位组")
	addCompanionGroupMemberAs(t, identity, adminToken, groupID, nominee.Email)
	postCompanionInviteDecision(t, identity, nomineeToken, groupID, "accept")
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "竞态.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "group",
		GroupID:    groupID,
		TenantID:   admin.TenantID,
		Importers:  map[string]string{nominee.UserID: "imported"},
	})
	if rec := postCompanionHostAs(t, identity, adminToken, token, nominee.Email); rec.Code != http.StatusOK {
		t.Fatalf("nominate: %d %s", rec.Code, rec.Body.String())
	}
	// The membership ends without the seat reconcile running first.
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group := companionDocGroups.items[groupID]
	kept := make([]companionDocGroupMember, 0, len(group.Members))
	for _, member := range group.Members {
		if strings.TrimSpace(member.UserID) == nominee.UserID || strings.TrimSpace(member.Contact) == nominee.Email {
			continue
		}
		kept = append(kept, member)
	}
	group.Members = kept
	companionDocGroups.items[groupID] = group
	companionDocGroups.Unlock()

	rec := decideCompanionHostAs(t, identity, nomineeToken, token, "accept")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("accept without access: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "分享范围") {
		t.Fatalf("accept without access said something else: %s", rec.Body.String())
	}
	got, _ := companionShareByToken(token)
	if companionShareHostID(got) != admin.UserID {
		t.Fatalf("the seat moved to a nominee without access: %s", companionShareHostID(got))
	}
	// The nomination is withdrawn as well: this nominee can never accept, and a
	// pending request would keep failing in their bell.
	if strings.TrimSpace(got.PendingHostID) != "" {
		t.Fatalf("a refused accept left the nomination pending: %s", got.PendingHostID)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := svc.FindBySource(context.Background(), notification.SourceCompanionHostTransferNotice, token)
		if err == nil && got != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the participants were not told the transfer was cancelled")
}

func TestCompanionShareHostNominationRequiresOpenAccess(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "open-admin@example.com")
	nomineeToken, nominee := issueViewerToken(t, identity, "open-nominee@example.com")
	outsiderToken, outsider := issueViewerToken(t, identity, "open-outsider@example.com")
	_ = outsiderToken
	const token = "cd_aaaa7777777777777777777777777777"
	groupID := createCompanionGroupAs(t, identity, adminToken, "提名范围组")
	addCompanionGroupMemberAs(t, identity, adminToken, groupID, nominee.Email)
	postCompanionInviteDecision(t, identity, nomineeToken, groupID, "accept")
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "提名范围.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "group",
		GroupID:    groupID,
		TenantID:   admin.TenantID,
		Importers:  map[string]string{nominee.UserID: "imported"},
	})
	removeMember(t, identity, adminToken, groupID, nominee.Email)

	rec := postCompanionHostAs(t, identity, adminToken, token, nominee.Email)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "不能打开") {
		t.Fatalf("departed importer: %d %s", rec.Code, rec.Body.String())
	}
	rec = postCompanionHostAs(t, identity, adminToken, token, outsider.Email)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "参与者") {
		t.Fatalf("outsider: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := companionShareByToken(token)
	if strings.TrimSpace(got.PendingHostID) != "" {
		t.Fatalf("a refused nomination was stored: %s", got.PendingHostID)
	}
}

func TestCompanionShareHostRevokeIgnoresTheAdminSeat(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	withCompanionGroupNotifier(t, identity, svc)

	adminToken, admin := issueViewerToken(t, identity, "seat-admin@example.com")
	const token = "cd_bbbb8888888888888888888888888888"
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "管理员席位.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "private",
		TenantID:   admin.TenantID,
		HostID:     admin.UserID,
		HostLabel:  admin.Email,
	})
	if rec := deleteCompanionHostAs(t, identity, adminToken, token, "revoke"); rec.Code != http.StatusBadRequest {
		t.Fatalf("revoke admin seat: %d %s", rec.Code, rec.Body.String())
	}
	storeCompanionShareForNotifyTest(companionDocShare{
		Token:      token,
		Name:       "管理员席位.pdf",
		OwnerID:    admin.UserID,
		OwnerLabel: admin.Email,
		Visibility: "private",
		TenantID:   admin.TenantID,
		ExpiresAt:  time.Now().UTC().Add(-time.Hour),
		HostID:     "someone-else",
		HostLabel:  "someone-else@example.com",
	})
	if rec := deleteCompanionHostAs(t, identity, adminToken, token, "revoke"); rec.Code != http.StatusGone {
		t.Fatalf("revoke expired: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := companionShareByToken(token)
	if strings.TrimSpace(got.HostID) != "someone-else" {
		t.Fatalf("expired revoke moved the host: %s", got.HostID)
	}
}

func TestCompanionShareSyncRejectionKeepsAReclaimedSeat(t *testing.T) {
	now := time.Now().UTC()
	share := companionDocShare{OwnerID: "admin", HostID: "host"}
	status, _, msg := companionShareSyncRejection(share, true, "admin", now)
	if status != http.StatusForbidden || !strings.Contains(msg, "主持人") {
		t.Fatalf("reclaimed seat: %d %s", status, msg)
	}
	if status, _, msg = companionShareSyncRejection(share, true, "host", now); status != 0 {
		t.Fatalf("current host rejected: %d %s", status, msg)
	}
	if status, _, _ = companionShareSyncRejection(companionDocShare{}, false, "host", now); status != http.StatusNotFound {
		t.Fatalf("missing share: %d", status)
	}
	share.ExpiresAt = now.Add(-time.Hour)
	if status, _, _ = companionShareSyncRejection(share, true, "host", now); status != http.StatusGone {
		t.Fatalf("expired share: %d", status)
	}
}

func TestCompanionHostTransferRevokeClearsEveryCopy(t *testing.T) {
	resetCompanionDocShares(t)
	_, _, _ = newHTTPAPITestServices(t)
	svc := newCompanionGroupNotificationService(t)
	const token = "cd_cccc9999999999999999999999999999"
	ctx := context.Background()
	for range 2 {
		created, err := svc.CreateNotification(ctx, notification.CreateRequest{
			Title:        "主持人转移",
			Content:      "旧的转移",
			Category:     notification.CategoryCustom,
			Priority:     notification.PriorityImportant,
			AudienceType: notification.AudienceUser,
			AudienceIDs:  []string{"user-a"},
			CreatedBy:    "companion-share",
			Source:       notification.SourceCompanionHostTransfer,
			SourceID:     token,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := svc.PublishNotification(ctx, created.ID); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	revokeCompanionHostTransferLocked(ctx, svc, token)
	if got, err := svc.FindBySource(ctx, notification.SourceCompanionHostTransfer, token); err != nil || got != nil {
		t.Fatalf("an older transfer request stayed in the bell: %+v %v", got, err)
	}
}

func deleteCompanionHostAs(t *testing.T, identity *auth.IdentityService, viewer, token, action string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/mobile/documents/shares/" + token + "/host"
	if action != "" {
		target += "?action=" + action
	}
	req := httptest.NewRequest(http.MethodDelete, target, nil)
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentShareHostHandler(identity).ServeHTTP(rec, req)
	return rec
}
