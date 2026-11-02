package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func expertMarketShareMultipart(t *testing.T, archive []byte, price int64, targets string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("version", "1.0.0")
	_ = mw.WriteField("price", fmtInt(price))
	_ = mw.WriteField("visibility", "users")
	_ = mw.WriteField("platform_distribution", "true")
	_ = mw.WriteField("share_targets", targets)
	if len(archive) > 0 {
		part, err := mw.CreateFormFile("package", "expert.zip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(archive); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

func submitExpertShare(t *testing.T, h *SkillMarketHandlers, token string, archive []byte, targets string) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := expertMarketShareMultipart(t, archive, 9, targets)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(rec, req)
	return rec
}

func TestExpertMarketSpecifiedUserShareRejectsBadTargets(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "share-owner", "share-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	archive := expertMarketArchive(t, "pkgexp-share-invalid", "Invalid share")
	cases := []struct {
		name    string
		targets string
		want    string
	}{
		{"empty", "", "at least one"},
		{"invalid", "not a contact", "invalid phone number or email"},
		{"self", "share-owner@example.test", "yourself"},
		{"long", "123456789012345678901", "invalid phone number or email"},
	}
	for _, tc := range cases {
		rec := submitExpertShare(t, h, session.Token, archive, tc.targets)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s status=%d body=%s", tc.name, rec.Code, rec.Body.String())
		}
	}
	var tooMany strings.Builder
	for i := 0; i < expertMarketMaxShareTargets+1; i++ {
		if i > 0 {
			tooMany.WriteByte(',')
		}
		fmt.Fprintf(&tooMany, "1390000%04d", i)
	}
	rec := submitExpertShare(t, h, session.Token, archive, tooMany.String())
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "too many") {
		t.Fatalf("limit status=%d body=%s", rec.Code, rec.Body.String())
	}
	phoneOwner, err := users.EnsureAccountWithID(ctx, "share-phone-owner", "phone:13300001111")
	if err != nil {
		t.Fatal(err)
	}
	phoneSession, err := auth.CreateSessionForUser(ctx, phoneOwner.ID, phoneOwner.Email)
	if err != nil {
		t.Fatal(err)
	}
	selfPhone := submitExpertShare(t, h, phoneSession.Token, archive, "+86 13300001111")
	if selfPhone.Code != http.StatusBadRequest || !strings.Contains(selfPhone.Body.String(), "yourself") {
		t.Fatalf("phone self status=%d body=%s", selfPhone.Code, selfPhone.Body.String())
	}
	self0086 := submitExpertShare(t, h, phoneSession.Token, archive, "0086 13300001111")
	if self0086.Code != http.StatusBadRequest || !strings.Contains(self0086.Body.String(), "yourself") {
		t.Fatalf("0086 self status=%d body=%s", self0086.Code, self0086.Body.String())
	}
	var listings int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_listings`).Scan(&listings); err != nil || listings != 0 {
		t.Fatalf("listings=%d err=%v", listings, err)
	}
}

func TestExpertMarketSpecifiedUserShareMatchesPhoneOrEmailGlobally(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "share-global-owner", "share-global-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	emailUser, err := users.EnsureAccountWithID(ctx, "share-email-user", "friend@example.test")
	if err != nil {
		t.Fatal(err)
	}
	phoneUser, err := users.EnsureAccountWithID(ctx, "share-phone-user", "phone:13301168516")
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := users.EnsureAccountWithID(ctx, "share-stranger", "stranger@example.test")
	if err != nil {
		t.Fatal(err)
	}
	ownerSession, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	emailSession, err := auth.CreateSessionForUser(ctx, emailUser.ID, emailUser.Email)
	if err != nil {
		t.Fatal(err)
	}
	phoneSession, err := auth.CreateSessionForUser(ctx, phoneUser.ID, phoneUser.Email)
	if err != nil {
		t.Fatal(err)
	}
	strangerSession, err := auth.CreateSessionForUser(ctx, stranger.ID, stranger.Email)
	if err != nil {
		t.Fatal(err)
	}
	rec := submitExpertShare(t, h, ownerSession.Token, expertMarketArchive(t, "pkgexp-share-global", "Shared analyst"), "friend@example.test, Friend@Example.test\n13301168516, 133-0116-8516, +86 13301168516, +86 13900001111, 13900001111, 008613900001111")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", rec.Code, rec.Body.String())
	}
	var created expertMarketAdminListing
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Visibility != "users" || created.Status != "shared" || created.PlatformDistribution || created.Price != 0 {
		t.Fatalf("listing=%+v", created)
	}
	if strings.Join(created.ShareTargets, ",") != "friend@example.test,13301168516,13900001111" {
		t.Fatalf("targets=%v", created.ShareTargets)
	}
	var enabled int
	var storedUser string
	if err := h.store.DB().QueryRow(`SELECT platform_distribution FROM sm_expert_market_listings WHERE id=?`, created.ID).Scan(&enabled); err != nil || enabled != 0 {
		t.Fatalf("platform_distribution=%d err=%v", enabled, err)
	}
	if err := h.store.DB().QueryRow(`SELECT user_id FROM sm_expert_market_share_targets WHERE listing_id=? AND contact=?`, created.ID, "phone:13900001111").Scan(&storedUser); err != nil || storedUser != "" {
		t.Fatalf("unknown contact user_id=%q err=%v", storedUser, err)
	}
	if err := h.store.DB().QueryRow(`SELECT user_id FROM sm_expert_market_share_targets WHERE listing_id=? AND contact=?`, created.ID, emailUser.Email).Scan(&storedUser); err != nil || storedUser != emailUser.ID {
		t.Fatalf("email target user_id=%q err=%v", storedUser, err)
	}

	catalogue := httptest.NewRecorder()
	h.ListExpertMarketListings(catalogue, httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts", nil))
	if catalogue.Code != http.StatusOK || !strings.Contains(catalogue.Body.String(), `"total":0`) {
		t.Fatalf("catalogue=%d %s", catalogue.Code, catalogue.Body.String())
	}
	detail := httptest.NewRecorder()
	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+created.ID, nil)
	detailReq.SetPathValue("id", created.ID)
	h.GetExpertMarketListing(detail, detailReq)
	if detail.Code != http.StatusNotFound {
		t.Fatalf("detail=%d %s", detail.Code, detail.Body.String())
	}
	adminList := httptest.NewRecorder()
	h.AdminListExpertMarketListings(adminList, httptest.NewRequest(http.MethodGet, "/api/v1/admin/expert-market/experts?visibility=users", nil))
	if adminList.Code != http.StatusOK || !strings.Contains(adminList.Body.String(), `"share_targets":["friend@example.test","13301168516","13900001111"]`) {
		t.Fatalf("admin=%d %s", adminList.Code, adminList.Body.String())
	}

	assertShareDownload(t, h, created.ID, emailSession.Token, http.StatusOK)
	assertShareDownload(t, h, created.ID, phoneSession.Token, http.StatusOK)
	assertShareDownload(t, h, created.ID, ownerSession.Token, http.StatusOK)
	denied := assertShareDownload(t, h, created.ID, strangerSession.Token, http.StatusForbidden)
	if !strings.Contains(denied, "not shared") {
		t.Fatalf("stranger body=%s", denied)
	}

	emailAccount := expertShareAccount(t, h, emailSession.Token)
	if len(emailAccount.Received) != 1 || emailAccount.Received[0].ID != created.ID || len(emailAccount.Purchases) != 0 || len(emailAccount.Received[0].ShareTargets) != 0 {
		t.Fatalf("email account=%+v", emailAccount)
	}
	ownerAccount := expertShareAccount(t, h, ownerSession.Token)
	if len(ownerAccount.Uploads) != 1 || len(ownerAccount.Uploads[0].ShareTargets) != 3 || len(ownerAccount.Received) != 0 {
		t.Fatalf("owner account uploads=%d targets=%v received=%d", len(ownerAccount.Uploads), ownerAccount.Uploads[0].ShareTargets, len(ownerAccount.Received))
	}

	lateUser, err := users.EnsureAccountWithID(ctx, "share-late-phone", "phone:13900001111")
	if err != nil {
		t.Fatal(err)
	}
	lateSession, err := auth.CreateSessionForUser(ctx, lateUser.ID, lateUser.Email)
	if err != nil {
		t.Fatal(err)
	}
	lateAccount := expertShareAccount(t, h, lateSession.Token)
	if len(lateAccount.Received) != 1 || lateAccount.Received[0].ID != created.ID {
		t.Fatalf("late account received=%+v", lateAccount.Received)
	}
	assertShareDownload(t, h, created.ID, lateSession.Token, http.StatusOK)

	reportBody := strings.NewReader(`{"status":"installed","local_expert_id":"local-shared","version":"1.0.0"}`)
	report := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+created.ID+"/installations", reportBody)
	report.SetPathValue("id", created.ID)
	report.Header.Set("Authorization", "Bearer "+emailSession.Token)
	reportRec := httptest.NewRecorder()
	h.ReportExpertMarketInstallation(reportRec, report)
	if reportRec.Code != http.StatusCreated {
		t.Fatalf("install report=%d %s", reportRec.Code, reportRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/expert-market/experts/"+created.ID+"/private", nil)
	deleteReq.SetPathValue("id", created.ID)
	deleteReq.Header.Set("Authorization", "Bearer "+ownerSession.Token)
	deleteRec := httptest.NewRecorder()
	h.DeletePrivateExpertMarketListing(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", deleteRec.Code, deleteRec.Body.String())
	}
	if len(expertShareAccount(t, h, emailSession.Token).Received) != 0 {
		t.Fatal("recipient still sees a deleted share")
	}
	var remaining int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_share_targets WHERE listing_id=?`, created.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("targets remaining=%d err=%v", remaining, err)
	}
}

type expertShareAccountView struct {
	Uploads   []expertMarketAdminListing `json:"uploads"`
	Purchases []expertMarketAdminListing `json:"purchases"`
	Received  []expertMarketAdminListing `json:"received"`
}

func expertShareAccount(t *testing.T, h *SkillMarketHandlers, token string) expertShareAccountView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/account", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.GetExpertMarketAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account=%d %s", rec.Code, rec.Body.String())
	}
	var view expertShareAccountView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func assertShareDownload(t *testing.T, h *SkillMarketHandlers, id, token string, want int) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+id+"/download", nil)
	req.SetPathValue("id", id)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(rec, req)
	if rec.Code != want {
		t.Fatalf("download=%d want %d body=%s", rec.Code, want, rec.Body.String())
	}
	return rec.Body.String()
}

func TestExpertMarketAdminRemovesSpecifiedUserShare(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "share-admin-owner", "share-admin-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	friend, err := users.EnsureAccountWithID(ctx, "share-admin-friend", "share-admin-friend@example.test")
	if err != nil {
		t.Fatal(err)
	}
	ownerSession, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	friendSession, err := auth.CreateSessionForUser(ctx, friend.ID, friend.Email)
	if err != nil {
		t.Fatal(err)
	}
	rec := submitExpertShare(t, h, ownerSession.Token, expertMarketArchive(t, "pkgexp-share-admin", "Admin takedown"), "share-admin-friend@example.test, 13900002222")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", rec.Code, rec.Body.String())
	}
	var created expertMarketAdminListing
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	blank, err := h.expertMarketSharedWith(ctx, created.ID, "", "nobody@example.test")
	if err != nil || blank {
		t.Fatalf("blank user matched an unlinked target: matched=%v err=%v", blank, err)
	}
	linked, err := h.expertMarketSharedWith(ctx, created.ID, friend.ID, friend.Email)
	if err != nil || !linked {
		t.Fatalf("recipient match=%v err=%v", linked, err)
	}

	publicID := "share-admin-public"
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, publicID, owner.ID, owner.Email, "pkgexp-share-admin-public", "Public", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	publicReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/experts/"+publicID+"/private", strings.NewReader(`{"reason":"should not apply"}`))
	publicReq.Header.Set("Content-Type", "application/json")
	publicReq.SetPathValue("id", publicID)
	publicRec := httptest.NewRecorder()
	h.AdminDeletePrivateExpertMarketListing(publicRec, publicReq)
	if publicRec.Code != http.StatusConflict {
		t.Fatalf("public delete=%d %s", publicRec.Code, publicRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/experts/"+created.ID+"/private", strings.NewReader(`{"reason":"policy removal"}`))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteReq.SetPathValue("id", created.ID)
	deleteRec := httptest.NewRecorder()
	h.AdminDeletePrivateExpertMarketListing(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK || !strings.Contains(deleteRec.Body.String(), `"visibility":"users"`) {
		t.Fatalf("admin delete=%d %s", deleteRec.Code, deleteRec.Body.String())
	}
	var visibility, status, zipPath string
	if err := h.store.DB().QueryRow(`SELECT visibility, status, zip_path FROM sm_expert_market_listings WHERE id=?`, created.ID).Scan(&visibility, &status, &zipPath); err != nil || visibility != "users" || status != "deleted" || zipPath == "" {
		t.Fatalf("after delete visibility=%s status=%s path=%s err=%v", visibility, status, zipPath, err)
	}
	var remaining int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_share_targets WHERE listing_id=?`, created.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("targets remaining=%d err=%v", remaining, err)
	}
	if len(expertShareAccount(t, h, friendSession.Token).Received) != 0 {
		t.Fatal("recipient still sees an admin-deleted share")
	}
	assertShareDownload(t, h, created.ID, friendSession.Token, http.StatusGone)

	againReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/experts/"+created.ID+"/private", strings.NewReader(`{"reason":"policy removal"}`))
	againReq.Header.Set("Content-Type", "application/json")
	againReq.SetPathValue("id", created.ID)
	again := httptest.NewRecorder()
	h.AdminDeletePrivateExpertMarketListing(again, againReq)
	if again.Code != http.StatusOK {
		t.Fatalf("repeat delete=%d %s", again.Code, again.Body.String())
	}

	purgeReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/experts/"+created.ID+"/private/purge", strings.NewReader(`{"reason":"retention elapsed"}`))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeReq.SetPathValue("id", created.ID)
	purgeRec := httptest.NewRecorder()
	h.AdminPurgePrivateExpertMarketListing(purgeRec, purgeReq)
	if purgeRec.Code != http.StatusOK {
		t.Fatalf("purge=%d %s", purgeRec.Code, purgeRec.Body.String())
	}
	if _, err := os.Stat(zipPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("package still exists after purge: %v", err)
	}
	if err := h.store.DB().QueryRow(`SELECT visibility, status FROM sm_expert_market_listings WHERE id=?`, created.ID).Scan(&visibility, &status); err != nil || visibility != "users" || status != "purged" {
		t.Fatalf("after purge visibility=%s status=%s err=%v", visibility, status, err)
	}
}

func TestExpertMarketPublishSpecifiedUserShareStaysFree(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "share-publish-owner", "share-publish-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	friend, err := users.EnsureAccountWithID(ctx, "share-publish-friend", "share-publish-friend@example.test")
	if err != nil {
		t.Fatal(err)
	}
	ownerSession, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	friendSession, err := auth.CreateSessionForUser(ctx, friend.ID, friend.Email)
	if err != nil {
		t.Fatal(err)
	}
	rec := submitExpertShare(t, h, ownerSession.Token, expertMarketArchive(t, "pkgexp-share-publish", "Publish share"), "share-publish-friend@example.test")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", rec.Code, rec.Body.String())
	}
	var created expertMarketAdminListing
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`UPDATE sm_expert_market_listings SET price=9, platform_distribution=1 WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}
	publish := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+created.ID+"/publish", nil)
	publish.SetPathValue("id", created.ID)
	publish.Header.Set("Authorization", "Bearer "+ownerSession.Token)
	publishRec := httptest.NewRecorder()
	h.PublishExpertMarketListing(publishRec, publish)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish=%d %s", publishRec.Code, publishRec.Body.String())
	}
	var price, distribution int
	var visibility, status string
	if err := h.store.DB().QueryRow(`SELECT price, platform_distribution, visibility, status FROM sm_expert_market_listings WHERE id=?`, created.ID).Scan(&price, &distribution, &visibility, &status); err != nil || price != 0 || distribution != 0 || visibility != "public" || status != "pending_review" {
		t.Fatalf("price=%d distribution=%d visibility=%s status=%s err=%v", price, distribution, visibility, status, err)
	}
	var remaining int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_share_targets WHERE listing_id=?`, created.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("targets remaining=%d err=%v", remaining, err)
	}
	if len(expertShareAccount(t, h, friendSession.Token).Received) != 0 {
		t.Fatal("recipient still sees a share that was submitted for public review")
	}
}
