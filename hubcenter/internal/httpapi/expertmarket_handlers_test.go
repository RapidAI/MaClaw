package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

func newExpertMarketTestHandler(t *testing.T) (*SkillMarketHandlers, *skillmarket.UserService, *skillmarket.AuthService, *skillmarket.CreditsService) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	users := skillmarket.NewUserService(store, nil)
	auth := skillmarket.NewAuthService(store, nil, "")
	credits := skillmarket.NewCreditsService(store)
	return NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: users, AuthSvc: auth, CreditsSvc: credits, DataDir: t.TempDir()}), users, auth, credits
}

func expertMarketArchive(t *testing.T, id, name string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	file, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"format": "maclaw-expert-package", "version": 1, "expert_package_id": id, "expert": map[string]any{"name": name, "description": "a tested market expert", "icon": "馃И"}}
	payload["expert"].(map[string]any)["system_prompt"] = "Provide carefully reviewed expert assistance."
	if err := json.NewEncoder(file).Encode(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func expertMarketMultipart(t *testing.T, archive []byte, price int64) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("version", "1.0.0")
	_ = mw.WriteField("price", fmtInt(price))
	part, err := mw.CreateFormFile("package", "expert.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err = mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

func expertMarketMultipartWithVisibility(t *testing.T, archive []byte, price int64, visibility string, platformDistribution bool) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("version", "1.0.0")
	_ = mw.WriteField("price", fmtInt(price))
	_ = mw.WriteField("visibility", visibility)
	_ = mw.WriteField("platform_distribution", strconv.FormatBool(platformDistribution))
	part, err := mw.CreateFormFile("package", "expert.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err = mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}
func fmtInt(v int64) string { return strconv.FormatInt(v, 10) }

func TestExpertMarketPrivateShareCannotEnablePlatformDistribution(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-distribution-seller", "private-distribution@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := expertMarketMultipartWithVisibility(t, expertMarketArchive(t, "pkgexp-private-distribution", "Private distribution"), 0, "private", true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", rec.Code, rec.Body.String())
	}
	var listing expertMarketAdminListing
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.PlatformDistribution {
		t.Fatalf("private listing returned platform distribution consent: %+v", listing)
	}
	var enabled int
	if err := h.store.DB().QueryRow(`SELECT platform_distribution FROM sm_expert_market_listings WHERE id=?`, listing.ID).Scan(&enabled); err != nil || enabled != 0 {
		t.Fatalf("private listing platform_distribution=%d err=%v, want 0", enabled, err)
	}
}

func TestExpertMarketPublicShareReturnsPlatformDistributionAcrossReadAPIs(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "public-distribution-seller", "public-distribution@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := expertMarketMultipartWithVisibility(t, expertMarketArchive(t, "pkgexp-public-distribution", "Public distribution"), 0, "public", true)
	submit := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	submit.Header.Set("Authorization", "Bearer "+session.Token)
	submit.Header.Set("Content-Type", contentType)
	submitRec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(submitRec, submit)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", submitRec.Code, submitRec.Body.String())
	}
	var created expertMarketAdminListing
	if err := json.Unmarshal(submitRec.Body.Bytes(), &created); err != nil || !created.PlatformDistribution {
		t.Fatalf("created listing=%+v err=%v, want distribution enabled", created, err)
	}
	if _, err := h.store.DB().Exec(`UPDATE sm_expert_market_listings SET status='listed' WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}

	account := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/account", nil)
	account.Header.Set("Authorization", "Bearer "+session.Token)
	accountRec := httptest.NewRecorder()
	h.GetExpertMarketAccount(accountRec, account)
	if accountRec.Code != http.StatusOK || !bytes.Contains(accountRec.Body.Bytes(), []byte(`"platform_distribution":true`)) {
		t.Fatalf("account=%d %s, want enabled distribution", accountRec.Code, accountRec.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts", nil)
	list.Header.Set("Authorization", "Bearer "+session.Token)
	listRec := httptest.NewRecorder()
	h.ListExpertMarketListings(listRec, list)
	if listRec.Code != http.StatusOK || !bytes.Contains(listRec.Body.Bytes(), []byte(`"platform_distribution":true`)) {
		t.Fatalf("list=%d %s, want enabled distribution", listRec.Code, listRec.Body.String())
	}

	detail := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+created.ID, nil)
	detail.SetPathValue("id", created.ID)
	detail.Header.Set("Authorization", "Bearer "+session.Token)
	detailRec := httptest.NewRecorder()
	h.GetExpertMarketListing(detailRec, detail)
	if detailRec.Code != http.StatusOK || !bytes.Contains(detailRec.Body.Bytes(), []byte(`"platform_distribution":true`)) {
		t.Fatalf("detail=%d %s, want enabled distribution", detailRec.Code, detailRec.Body.String())
	}
}

func TestExpertMarketAccountUsesUserIDAcrossBoundLoginContacts(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	userID := "expert-bound-contact-owner"
	_, err := users.EnsureAccountWithID(ctx, userID, "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	firstSession, err := auth.CreateSessionForUser(ctx, userID, "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := expertMarketMultipartWithVisibility(t, expertMarketArchive(t, "pkgexp-bound-contact-owner", "Bound contact expert"), 0, "private", false)
	submit := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	submit.Header.Set("Authorization", "Bearer "+firstSession.Token)
	submit.Header.Set("Content-Type", contentType)
	submitRec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(submitRec, submit)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", submitRec.Code, submitRec.Body.String())
	}

	// The Hub can authenticate this same principal through a phone on a later
	// login. The session contact is intentionally different; asset ownership
	// remains keyed by the immutable user ID.
	secondSession, err := auth.CreateSessionForUser(ctx, userID, "phone:17000000000")
	if err != nil {
		t.Fatal(err)
	}
	account := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/account", nil)
	account.Header.Set("Authorization", "Bearer "+secondSession.Token)
	accountRec := httptest.NewRecorder()
	h.GetExpertMarketAccount(accountRec, account)
	if accountRec.Code != http.StatusOK || !bytes.Contains(accountRec.Body.Bytes(), []byte(`"name":"Bound contact expert"`)) {
		t.Fatalf("phone-contact account=%d %s, want submission owned by user ID", accountRec.Code, accountRec.Body.String())
	}
	if !bytes.Contains(accountRec.Body.Bytes(), []byte(`"email":"owner@example.test"`)) {
		t.Fatalf("account response=%s, want the canonical contact without changing ownership", accountRec.Body.String())
	}
}

func TestExpertMarketSubmissionAuditActorUsesUserID(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	userID := "expert-audit-user-id"
	owner, err := users.EnsureAccountWithID(ctx, userID, "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := expertMarketMultipartWithVisibility(t, expertMarketArchive(t, "pkgexp-audit-user-id", "Audit actor expert"), 0, "public", true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", rec.Code, rec.Body.String())
	}
	var actor string
	if err := h.store.DB().QueryRow(`SELECT actor FROM sm_expert_market_events ORDER BY created_at DESC LIMIT 1`).Scan(&actor); err != nil || actor != userID {
		t.Fatalf("event actor=%q err=%v, want user ID %q", actor, err, userID)
	}
}

func TestExpertMarketMakePrivateClearsPlatformDistribution(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "make-private-distribution-seller", "make-private-distribution@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`, platform_distribution) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'public', 'listed', ?, 0, 0, 0, 0, '', ?, ?, 1)`, "make-private-distribution", seller.ID, seller.Email, "pkgexp-make-private-distribution", "Make private distribution", "", "", "1.0.0", 0, filepath.Join(h.expertMarketDir(), "unused.zip"), now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/make-private-distribution/make-private", nil)
	req.SetPathValue("id", "make-private-distribution")
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.MakeExpertMarketListingPrivate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("make private=%d %s", rec.Code, rec.Body.String())
	}
	var visibility, status string
	var enabled int
	if err := h.store.DB().QueryRow(`SELECT visibility, status, platform_distribution FROM sm_expert_market_listings WHERE id='make-private-distribution'`).Scan(&visibility, &status, &enabled); err != nil || visibility != "private" || status != "private" || enabled != 0 {
		t.Fatalf("private listing visibility=%q status=%q platform_distribution=%d err=%v", visibility, status, enabled, err)
	}
}

func TestExpertMarketSubmitReviewPurchaseAndDownload(t *testing.T) {
	h, users, auth, credits := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "expert-seller", "seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := users.EnsureAccountWithID(ctx, "expert-buyer", "buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := credits.TopUp(ctx, buyer.ID, 100); err != nil {
		t.Fatal(err)
	}
	sellerSession, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	buyerSession, err := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	archive := expertMarketArchive(t, "pkgexp-test-expert", "Test Expert")
	body, contentType := expertMarketMultipart(t, archive, 25)
	submit := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	submit.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	submit.Header.Set("Content-Type", contentType)
	submitRec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(submitRec, submit)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", submitRec.Code, submitRec.Body.String())
	}
	var listing expertMarketAdminListing
	if err := json.Unmarshal(submitRec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Status != "pending_review" {
		t.Fatalf("status=%q", listing.Status)
	}
	// The account endpoint is the source for 鈥淢y submissions鈥? it must read
	// from the authoritative connection so a just-committed submission appears
	// even when a deployed read replica is behind.
	account := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/account", nil)
	account.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	accountRec := httptest.NewRecorder()
	h.GetExpertMarketAccount(accountRec, account)
	if accountRec.Code != http.StatusOK || !bytes.Contains(accountRec.Body.Bytes(), []byte(`"id":"`+listing.ID+`"`)) {
		t.Fatalf("seller account should include its submitted expert: status=%d body=%s", accountRec.Code, accountRec.Body.String())
	}
	// Review notes are optional; approving with an empty body must still publish.
	approve := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/"+listing.ID+"/approve", nil)
	approve.SetPathValue("id", listing.ID)
	approveRec := httptest.NewRecorder()
	h.AdminApproveExpertMarketListing(approveRec, approve)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve=%d %s", approveRec.Code, approveRec.Body.String())
	}
	var approved map[string]string
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approved); err != nil || approved["status"] != "listed" {
		t.Fatalf("approval must publish listing: response=%s err=%v", approveRec.Body.String(), err)
	}
	var reviewNote string
	if err := h.store.DB().QueryRow(`SELECT review_note FROM sm_expert_market_listings WHERE id=?`, listing.ID).Scan(&reviewNote); err != nil || reviewNote != "" {
		t.Fatalf("optional review note=%q err=%v", reviewNote, err)
	}
	var approvalEvents int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_events WHERE listing_id=? AND action='approved'`, listing.ID).Scan(&approvalEvents); err != nil || approvalEvents != 1 {
		t.Fatalf("approval audit count=%d err=%v", approvalEvents, err)
	}
	purchase := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+listing.ID+"/purchase", nil)
	purchase.SetPathValue("id", listing.ID)
	purchase.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(purchaseRec, purchase)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase=%d %s", purchaseRec.Code, purchaseRec.Body.String())
	}
	if balance, err := credits.GetBalance(ctx, buyer.ID); err != nil || balance != 75 {
		t.Fatalf("balance=%d err=%v", balance, err)
	}
	// A repeated click must resolve as owned, not debit Credits a second time.
	retry := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+listing.ID+"/purchase", nil)
	retry.SetPathValue("id", listing.ID)
	retry.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	retryRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(retryRec, retry)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("repeat purchase=%d %s", retryRec.Code, retryRec.Body.String())
	}
	if balance, err := credits.GetBalance(ctx, buyer.ID); err != nil || balance != 75 {
		t.Fatalf("repeat balance=%d err=%v", balance, err)
	}
	listed := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts", nil)
	listed.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	listedRec := httptest.NewRecorder()
	h.ListExpertMarketListings(listedRec, listed)
	if listedRec.Code != http.StatusOK || !bytes.Contains(listedRec.Body.Bytes(), []byte(`"owned":true`)) {
		t.Fatalf("list should expose buyer entitlement: status=%d body=%s", listedRec.Code, listedRec.Body.String())
	}
	ownerList := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts", nil)
	ownerList.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	ownerListRec := httptest.NewRecorder()
	h.ListExpertMarketListings(ownerListRec, ownerList)
	if ownerListRec.Code != http.StatusOK || !bytes.Contains(ownerListRec.Body.Bytes(), []byte(`"owned":true`)) {
		t.Fatalf("list should expose publisher package access: status=%d body=%s", ownerListRec.Code, ownerListRec.Body.String())
	}
	download := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+listing.ID+"/download", nil)
	download.SetPathValue("id", listing.ID)
	download.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	downloadRec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(downloadRec, download)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download=%d %s", downloadRec.Code, downloadRec.Body.String())
	}
	data, err := io.ReadAll(downloadRec.Result().Body)
	if err != nil || !bytes.Equal(data, archive) {
		t.Fatalf("download data differs err=%v", err)
	}
}

func TestExpertMarketOwnerPurchaseIsIdempotentOwned(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "owner-purchase", "owner-purchase@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	archive := expertMarketArchive(t, "pkgexp-owner-purchase", "Owner purchase")
	path := filepath.Join(h.expertMarketDir(), "owner-purchase.zip")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "owner-purchase-listing", seller.ID, seller.Email, "pkgexp-owner-purchase", "Owner purchase", "", "", "1", 0, "public", "listed", path, len(archive), now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/owner-purchase-listing/purchase", nil)
	req.SetPathValue("id", "owner-purchase-listing")
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"owned"`)) {
		t.Fatalf("owner purchase=%d %s", rec.Code, rec.Body.String())
	}
	var purchases int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id='owner-purchase-listing'`).Scan(&purchases); err != nil || purchases != 0 {
		t.Fatalf("owner must not receive a redundant purchase row: count=%d err=%v", purchases, err)
	}
}

func TestExpertMarketSubmissionRollsBackWhenAuditWriteFails(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-submit-audit-rollback", "seller-submit-audit-rollback@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if _, err := h.store.DB().Exec(`CREATE TRIGGER fail_expert_submission_audit BEFORE INSERT ON sm_expert_market_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	archive := expertMarketArchive(t, "pkgexp-submit-audit-rollback", "Submission audit rollback")
	body, contentType := expertMarketMultipart(t, archive, 0)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("submission with failed audit=%d %s", rec.Code, rec.Body.String())
	}
	var listings int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_listings WHERE source_expert_id='pkgexp-submit-audit-rollback'`).Scan(&listings); err != nil {
		t.Fatal(err)
	}
	if listings != 0 {
		t.Fatalf("listing persisted after failed audit: %d", listings)
	}
	entries, err := os.ReadDir(h.expertMarketDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("package bytes persisted after failed audit: %d files", len(entries))
	}
}

func TestExpertMarketPrivateVisibilitySkipsReviewAndCanBeRepublished(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-seller", "private@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := users.EnsureAccountWithID(ctx, "private-buyer", "buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	sellerSession, _ := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	buyerSession, _ := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	// Rebuild the test form with the explicit visibility selection.
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.WriteField("version", "1.0.0")
	_ = mw.WriteField("price", "0")
	_ = mw.WriteField("visibility", "private")
	part, err := mw.CreateFormFile("package", "expert.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(expertMarketArchive(t, "pkgexp-private", "Private Expert"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	submit := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", &form)
	submit.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	submit.Header.Set("Content-Type", mw.FormDataContentType())
	submitRec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(submitRec, submit)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("private submit=%d %s", submitRec.Code, submitRec.Body.String())
	}
	var listing expertMarketAdminListing
	if err := json.Unmarshal(submitRec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Visibility != "private" || listing.Status != "private" {
		t.Fatalf("private listing visibility=%q status=%q", listing.Visibility, listing.Status)
	}
	list := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts", nil)
	list.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	listRec := httptest.NewRecorder()
	h.ListExpertMarketListings(listRec, list)
	if listRec.Code != http.StatusOK || bytes.Contains(listRec.Body.Bytes(), []byte(listing.ID)) {
		t.Fatalf("private listing leaked into public market: status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	purchase := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+listing.ID+"/purchase", nil)
	purchase.SetPathValue("id", listing.ID)
	purchase.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(purchaseRec, purchase)
	if purchaseRec.Code != http.StatusNotFound {
		t.Fatalf("private listing must not be purchasable: status=%d body=%s", purchaseRec.Code, purchaseRec.Body.String())
	}
	// Defend the details endpoint too. A private record must remain hidden even
	// if a legacy/manual update leaves it with a listed status.
	if _, err := h.store.DB().Exec(`UPDATE sm_expert_market_listings SET status='listed' WHERE id=?`, listing.ID); err != nil {
		t.Fatal(err)
	}
	detail := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+listing.ID, nil)
	detail.SetPathValue("id", listing.ID)
	detail.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	detailRec := httptest.NewRecorder()
	h.GetExpertMarketListing(detailRec, detail)
	if detailRec.Code != http.StatusNotFound {
		t.Fatalf("private listing must not expose public details: status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
	if _, err := h.store.DB().Exec(`UPDATE sm_expert_market_listings SET status='private' WHERE id=?`, listing.ID); err != nil {
		t.Fatal(err)
	}
	publish := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+listing.ID+"/publish", nil)
	publish.SetPathValue("id", listing.ID)
	publish.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	publishRec := httptest.NewRecorder()
	h.PublishExpertMarketListing(publishRec, publish)
	if publishRec.Code != http.StatusOK || !bytes.Contains(publishRec.Body.Bytes(), []byte(`"status":"pending_review"`)) {
		t.Fatalf("publish=%d %s", publishRec.Code, publishRec.Body.String())
	}
}

func TestExpertMarketAccountKeepsListingsWithTheAuthenticatedUserID(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "shared-identity-seller", "seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	firstSession, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := expertMarketMultipart(t, expertMarketArchive(t, "pkgexp-shared-identity", "Shared identity expert"), 0)
	submit := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", body)
	submit.Header.Set("Authorization", "Bearer "+firstSession.Token)
	submit.Header.Set("Content-Type", contentType)
	submitRec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(submitRec, submit)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("submit=%d %s", submitRec.Code, submitRec.Body.String())
	}

	// A subsequent login may present a different verified contact (for example,
	// phone versus email) but keeps the same Hub user ID. Ownership must remain
	// attached to that ID, not the contact stored with the original session.
	secondSession, err := auth.CreateSessionForUser(ctx, seller.ID, "seller-phone-login@example.test")
	if err != nil {
		t.Fatal(err)
	}
	account := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/account", nil)
	account.Header.Set("Authorization", "Bearer "+secondSession.Token)
	accountRec := httptest.NewRecorder()
	h.GetExpertMarketAccount(accountRec, account)
	if accountRec.Code != http.StatusOK || !bytes.Contains(accountRec.Body.Bytes(), []byte(`"source_expert_id":"pkgexp-shared-identity"`)) {
		t.Fatalf("same user ID must retain submitted experts across login contacts: status=%d body=%s", accountRec.Code, accountRec.Body.String())
	}
	var ownerID string
	if err := h.store.DB().QueryRow(`SELECT owner_user_id FROM sm_expert_market_listings WHERE source_expert_id='pkgexp-shared-identity'`).Scan(&ownerID); err != nil || ownerID != seller.ID {
		t.Fatalf("listing owner id=%q err=%v, want %q", ownerID, err, seller.ID)
	}
}

func TestExpertMarketOwnerCanDeletePrivateShare(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-delete-seller", "private-delete@example.test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := users.EnsureAccountWithID(ctx, "private-delete-other", "private-delete-other@example.test")
	if err != nil {
		t.Fatal(err)
	}
	sellerSession, _ := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	otherSession, _ := auth.CreateSessionForUser(ctx, other.ID, other.Email)
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.WriteField("version", "1.0.0")
	_ = mw.WriteField("price", "0")
	_ = mw.WriteField("visibility", "private")
	part, err := mw.CreateFormFile("package", "expert.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(expertMarketArchive(t, "pkgexp-private-delete", "Private deletion"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	submit := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", &form)
	submit.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	submit.Header.Set("Content-Type", mw.FormDataContentType())
	submitRec := httptest.NewRecorder()
	h.SubmitExpertMarketListing(submitRec, submit)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("private submit=%d %s", submitRec.Code, submitRec.Body.String())
	}
	var listing expertMarketAdminListing
	if err := json.Unmarshal(submitRec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	var packagePath string
	if err := h.store.DB().QueryRow(`SELECT zip_path FROM sm_expert_market_listings WHERE id=?`, listing.ID).Scan(&packagePath); err != nil {
		t.Fatal(err)
	}
	otherDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/expert-market/experts/"+listing.ID+"/private", nil)
	otherDelete.SetPathValue("id", listing.ID)
	otherDelete.Header.Set("Authorization", "Bearer "+otherSession.Token)
	otherDeleteRec := httptest.NewRecorder()
	h.DeletePrivateExpertMarketListing(otherDeleteRec, otherDelete)
	if otherDeleteRec.Code != http.StatusNotFound {
		t.Fatalf("non-owner delete=%d %s", otherDeleteRec.Code, otherDeleteRec.Body.String())
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/expert-market/experts/"+listing.ID+"/private", nil)
	deleteReq.SetPathValue("id", listing.ID)
	deleteReq.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	deleteRec := httptest.NewRecorder()
	h.DeletePrivateExpertMarketListing(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK || !bytes.Contains(deleteRec.Body.Bytes(), []byte(`"status":"deleted"`)) {
		t.Fatalf("delete private=%d %s", deleteRec.Code, deleteRec.Body.String())
	}
	var remaining int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_listings WHERE id=?`, listing.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("private listing still exists: count=%d err=%v", remaining, err)
	}
	if _, err := os.Stat(packagePath); !os.IsNotExist(err) {
		t.Fatalf("private package was not removed: %v", err)
	}
}

func TestExpertMarketPrivateDeleteNeverRemovesPackageOutsideMarketStorage(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-delete-path-seller", "private-delete-path@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := os.CreateTemp(t.TempDir(), "not-a-market-package-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	foreignPath := foreign.Name()
	if _, err := foreign.WriteString("must remain"); err != nil {
		t.Fatal(err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "private-foreign-path", seller.ID, seller.Email, "pkgexp-private-foreign-path", "Private external path", "", "", "1", 0, "private", "private", foreignPath, 12, now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/expert-market/experts/private-foreign-path/private", nil)
	req.SetPathValue("id", "private-foreign-path")
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.DeletePrivateExpertMarketListing(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete private=%d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(foreignPath); err != nil {
		t.Fatalf("deletion must not remove an external package path: %v", err)
	}
}

func TestExpertMarketPrivateDeletePreservesMalformedEntitlements(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-delete-entitlement-seller", "private-delete-entitlement@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := users.EnsureAccountWithID(ctx, "private-delete-entitlement-buyer", "private-delete-entitlement-buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(h.expertMarketDir(), "private-entitlement.zip")
	if err := os.WriteFile(packagePath, []byte("private package"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := expertMarketNow()
	const listingID = "private-entitlement-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-private-entitlement", "Private entitlement", "", "", "1", 0, "private", "private", packagePath, 15, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_purchases (id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, 0, 'active', ?)`, "private-entitlement-purchase", listingID, buyer.ID, buyer.Email, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/expert-market/experts/"+listingID+"/private", nil)
	req.SetPathValue("id", listingID)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.DeletePrivateExpertMarketListing(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete private with entitlement=%d %s", rec.Code, rec.Body.String())
	}
	var remaining, purchases int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_listings WHERE id=?`, listingID).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("listing must remain: count=%d err=%v", remaining, err)
	}
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND status='active'`, listingID).Scan(&purchases); err != nil || purchases != 1 {
		t.Fatalf("entitlement must remain: count=%d err=%v", purchases, err)
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("package must remain: %v", err)
	}
}

func TestExpertMarketPrivateListingNeverDownloadsForMalformedEntitlement(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-download-seller", "private-download-seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := users.EnsureAccountWithID(ctx, "private-download-buyer", "private-download-buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyerSession, err := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(h.expertMarketDir(), "private-download.zip")
	if err := os.WriteFile(packagePath, []byte("private package"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := expertMarketNow()
	const listingID = "private-download-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-private-download", "Private download", "", "", "1", 0, "private", "private", packagePath, 15, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_purchases (id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, 0, 'active', ?)`, "private-download-purchase", listingID, buyer.ID, buyer.Email, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+listingID+"/download", nil)
	req.SetPathValue("id", listingID)
	req.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	rec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("private download=%d %s", rec.Code, rec.Body.String())
	}
}

func TestExpertMarketPrivateListingOwnerCanRestoreDownloadedPackage(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-owner-restore", "private-owner-restore@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	packageData := []byte("private owner package")
	packagePath := filepath.Join(h.expertMarketDir(), "private-owner-restore.zip")
	if err := os.WriteFile(packagePath, packageData, 0o600); err != nil {
		t.Fatal(err)
	}
	now := expertMarketNow()
	const listingID = "private-owner-restore-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-private-owner-restore", "Private owner restore", "", "", "1", 0, "private", "private", packagePath, int64(len(packageData)), now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+listingID+"/download", nil)
	req.SetPathValue("id", listingID)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), packageData) {
		t.Fatalf("owner private restore=%d body=%q", rec.Code, rec.Body.Bytes())
	}
}

func TestExpertMarketDownloadNeverServesPackageOutsideMarketStorage(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "download-foreign-seller", "download-foreign@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := os.CreateTemp(t.TempDir(), "not-a-market-package-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	foreignPath := foreign.Name()
	if _, err := foreign.WriteString("must not be served"); err != nil {
		t.Fatal(err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	const listingID = "download-foreign-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-download-foreign", "External package", "", "", "1", 0, "private", "private", foreignPath, 19, now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+listingID+"/download", nil)
	req.SetPathValue("id", listingID)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("foreign package download=%d %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("must not be served")) {
		t.Fatal("download response disclosed a file outside market storage")
	}
}

func TestExpertMarketAdminPrivateOperations(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "admin-private-owner", "admin-private-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	target, err := users.EnsureAccountWithID(ctx, "admin-private-target", "admin-private-target@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now, listingID := expertMarketNow(), "admin-private-operations"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, owner.ID, owner.Email, "pkgexp-admin-private-ops", "Private operations", "owner transfer and admin publication", "", "1", 0, "private", "private", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	for _, keyword := range []string{"", "t"} {
		listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/expert-market/users?keyword="+keyword, nil)
		listRec := httptest.NewRecorder()
		h.AdminListExpertMarketUsers(listRec, listReq)
		if listRec.Code != http.StatusBadRequest {
			t.Fatalf("short user query %q status=%d body=%s", keyword, listRec.Code, listRec.Body.String())
		}
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/expert-market/users?keyword=target", nil)
	listRec := httptest.NewRecorder()
	h.AdminListExpertMarketUsers(listRec, listReq)
	if listRec.Code != http.StatusOK || !bytes.Contains(listRec.Body.Bytes(), []byte(target.Email)) {
		t.Fatalf("target users=%d %s", listRec.Code, listRec.Body.String())
	}

	transferReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/"+listingID+"/transfer-owner", strings.NewReader(`{"target_user_id":"`+target.ID+`","expected_owner_id":"`+owner.ID+`","reason":"operations handoff"}`))
	transferReq.Header.Set("Content-Type", "application/json")
	transferReq.SetPathValue("id", listingID)
	transferRec := httptest.NewRecorder()
	h.AdminTransferExpertMarketOwner(transferRec, transferReq)
	if transferRec.Code != http.StatusOK {
		t.Fatalf("transfer=%d %s", transferRec.Code, transferRec.Body.String())
	}
	var gotOwner, gotEmail string
	if err := h.store.DB().QueryRow(`SELECT owner_user_id, owner_email FROM sm_expert_market_listings WHERE id=?`, listingID).Scan(&gotOwner, &gotEmail); err != nil || gotOwner != target.ID || gotEmail != target.Email {
		t.Fatalf("owner=%q email=%q err=%v", gotOwner, gotEmail, err)
	}
	var transferAudits int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_audit_events WHERE listing_id=? AND action='transfer_owner'`, listingID).Scan(&transferAudits); err != nil || transferAudits != 1 {
		t.Fatalf("transfer audit=%d err=%v", transferAudits, err)
	}

	publishReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/"+listingID+"/submit-publication", strings.NewReader(`{"reason":"review requested"}`))
	publishReq.Header.Set("Content-Type", "application/json")
	publishReq.SetPathValue("id", listingID)
	publishRec := httptest.NewRecorder()
	h.AdminSubmitExpertMarketPublication(publishRec, publishReq)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publication=%d %s", publishRec.Code, publishRec.Body.String())
	}
	var visibility, status string
	if err := h.store.DB().QueryRow(`SELECT visibility, status FROM sm_expert_market_listings WHERE id=?`, listingID).Scan(&visibility, &status); err != nil || visibility != "public" || status != "pending_review" {
		t.Fatalf("publication visibility=%q status=%q err=%v", visibility, status, err)
	}
}

func TestExpertMarketAdminSearchEscapesLikeMetacharacters(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "search-owner", "search-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	for _, item := range []struct{ id, name string }{{"search-percent", "100% verified"}, {"search-plain", "100x verified"}} {
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, item.id, owner.ID, owner.Email, "pkgexp-"+item.id, item.name, "", "", "1", 0, "private", "private", "", 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/expert-market/experts?keyword=%25", nil)
	rec := httptest.NewRecorder()
	h.AdminListExpertMarketListings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search=%d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Experts []expertMarketAdminListing `json:"experts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Experts) != 1 || payload.Experts[0].ID != "search-percent" {
		t.Fatalf("literal percent search=%+v", payload.Experts)
	}
}

func TestExpertMarketAdminPrivateDeletePreventsOwnerDownload(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "admin-delete-owner", "admin-delete-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	listingID, packagePath := "admin-private-delete", filepath.Join(h.expertMarketDir(), "admin-private-delete.zip")
	if err := os.WriteFile(packagePath, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, owner.ID, owner.Email, "pkgexp-admin-private-delete", "Private delete", "", "", "1", 0, "private", "private", packagePath, 7, now, now); err != nil {
		t.Fatal(err)
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/experts/"+listingID+"/private", strings.NewReader(`{"reason":"policy removal"}`))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteReq.SetPathValue("id", listingID)
	deleteRec := httptest.NewRecorder()
	h.AdminDeletePrivateExpertMarketListing(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("private delete=%d %s", deleteRec.Code, deleteRec.Body.String())
	}
	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts/"+listingID+"/download", nil)
	downloadReq.Header.Set("Authorization", "Bearer "+session.Token)
	downloadReq.SetPathValue("id", listingID)
	downloadRec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusGone {
		t.Fatalf("deleted private download=%d %s", downloadRec.Code, downloadRec.Body.String())
	}
	purgeReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/experts/"+listingID+"/private/purge", strings.NewReader(`{"reason":"retention elapsed"}`))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeReq.SetPathValue("id", listingID)
	purgeRec := httptest.NewRecorder()
	h.AdminPurgePrivateExpertMarketListing(purgeRec, purgeReq)
	if purgeRec.Code != http.StatusOK {
		t.Fatalf("private purge=%d %s", purgeRec.Code, purgeRec.Body.String())
	}
	if _, err := os.Stat(packagePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private package still exists after purge: %v", err)
	}
	var status string
	if err := h.store.DB().QueryRow(`SELECT status FROM sm_expert_market_listings WHERE id=?`, listingID).Scan(&status); err != nil || status != "purged" {
		t.Fatalf("private purge status=%q err=%v", status, err)
	}
	var audits int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_audit_events WHERE listing_id=? AND action='purge_private'`, listingID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("private purge audit=%d err=%v", audits, err)
	}
}

func TestExpertMarketPrivatePurgeKeepsPackageWhenAuditWriteFails(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "private-purge-audit-owner", "private-purge-audit-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(h.expertMarketDir(), "private-purge-audit-failure.zip")
	if err := os.WriteFile(packagePath, []byte("private package"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "private-purge-audit-failure", owner.ID, owner.Email, "pkgexp-private-purge-audit-failure", "Private purge audit failure", "", "", "1", 0, "private", "deleted", packagePath, 15, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`CREATE TRIGGER fail_private_purge_audit BEFORE INSERT ON sm_expert_market_audit_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"retention elapsed"}`))
	req.SetPathValue("id", "private-purge-audit-failure")
	rec := httptest.NewRecorder()
	h.AdminPurgePrivateExpertMarketListing(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("purge=%d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("package must remain after failed audit: %v", err)
	}
	var status, zipPath string
	if err := h.store.DB().QueryRow(`SELECT status, zip_path FROM sm_expert_market_listings WHERE id='private-purge-audit-failure'`).Scan(&status, &zipPath); err != nil || status != "deleted" || zipPath != packagePath {
		t.Fatalf("listing status=%q path=%q err=%v", status, zipPath, err)
	}
}

func TestExpertMarketPrivateListingNeverAcceptsInstallationReportForMalformedEntitlement(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-install-seller", "private-install-seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := users.EnsureAccountWithID(ctx, "private-install-buyer", "private-install-buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyerSession, err := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	const listingID = "private-install-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-private-install", "Private install", "", "", "1", 0, "private", "private", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_purchases (id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, 0, 'active', ?)`, "private-install-purchase", listingID, buyer.ID, buyer.Email, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+listingID+"/installations", strings.NewReader(`{"status":"failed","failure_stage":"download"}`))
	req.SetPathValue("id", listingID)
	req.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	rec := httptest.NewRecorder()
	h.ReportExpertMarketInstallation(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("private installation report=%d %s", rec.Code, rec.Body.String())
	}
	var reports int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_installations WHERE listing_id=?`, listingID).Scan(&reports); err != nil || reports != 0 {
		t.Fatalf("private installation report persisted: count=%d err=%v", reports, err)
	}
}

func TestExpertMarketAccountDoesNotExposePrivateListingThroughMalformedEntitlement(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-account-seller", "private-account-seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := users.EnsureAccountWithID(ctx, "private-account-buyer", "private-account-buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyerSession, err := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	const listingID = "private-account-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-private-account", "Private account expert", "", "", "1", 0, "private", "private", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_purchases (id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, 0, 'active', ?)`, "private-account-purchase", listingID, buyer.ID, buyer.Email, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/account", nil)
	req.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	rec := httptest.NewRecorder()
	h.GetExpertMarketAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account=%d %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(listingID)) || bytes.Contains(rec.Body.Bytes(), []byte("Private account expert")) {
		t.Fatalf("private listing leaked through account purchases: %s", rec.Body.String())
	}
}

func TestExpertMarketApprovalDoesNotPublishPrivateLegacyListing(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-review-seller", "private-review@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "private-review-listing", seller.ID, seller.Email, "pkgexp-private-review", "Private review", "", "", "1", 0, "private", "pending_review", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	approve := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/private-review-listing/approve", nil)
	approve.SetPathValue("id", "private-review-listing")
	approveRec := httptest.NewRecorder()
	h.AdminApproveExpertMarketListing(approveRec, approve)
	if approveRec.Code != http.StatusConflict {
		t.Fatalf("private listing approval=%d body=%s", approveRec.Code, approveRec.Body.String())
	}
	var visibility, status string
	if err := h.store.DB().QueryRow(`SELECT visibility, status FROM sm_expert_market_listings WHERE id=?`, "private-review-listing").Scan(&visibility, &status); err != nil {
		t.Fatal(err)
	}
	if visibility != "private" || status != "pending_review" {
		t.Fatalf("approval changed private legacy row: visibility=%q status=%q", visibility, status)
	}
}

func TestExpertMarketAdminLifecycleDoesNotAlterPrivateLegacyListing(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "private-admin-seller", "private-admin@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "private-admin-listing", seller.ID, seller.Email, "pkgexp-private-admin", "Private admin", "", "", "1", 0, "private", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
	}{
		{name: "unlist", handler: h.AdminUnlistExpertMarketListing, method: http.MethodPost},
		{name: "delete", handler: h.AdminDeleteExpertMarketListing, method: http.MethodDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", strings.NewReader(`{"reason":"moderation"}`))
			req.SetPathValue("id", "private-admin-listing")
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("%s private listing=%d body=%s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
	var visibility, status string
	if err := h.store.DB().QueryRow(`SELECT visibility, status FROM sm_expert_market_listings WHERE id=?`, "private-admin-listing").Scan(&visibility, &status); err != nil {
		t.Fatal(err)
	}
	if visibility != "private" || status != "listed" {
		t.Fatalf("admin lifecycle changed private legacy row: visibility=%q status=%q", visibility, status)
	}
}

func TestExpertMarketUnlistStopsNewDistributionAndMakePrivateChangesVisibility(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "visibility-seller", "visibility@example.test")
	sellerSession, _ := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "visibility-listing", seller.ID, seller.Email, "pkgexp-visibility", "Visibility Expert", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	unlist := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/visibility-listing/withdraw", nil)
	unlist.SetPathValue("id", "visibility-listing")
	unlist.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	unlistRec := httptest.NewRecorder()
	h.WithdrawExpertMarketListing(unlistRec, unlist)
	if unlistRec.Code != http.StatusOK || !bytes.Contains(unlistRec.Body.Bytes(), []byte(`"status":"unlisted"`)) {
		t.Fatalf("unlist=%d %s", unlistRec.Code, unlistRec.Body.String())
	}
	private := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/visibility-listing/make-private", nil)
	private.SetPathValue("id", "visibility-listing")
	private.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	privateRec := httptest.NewRecorder()
	h.MakeExpertMarketListingPrivate(privateRec, private)
	if privateRec.Code != http.StatusOK || !bytes.Contains(privateRec.Body.Bytes(), []byte(`"visibility":"private"`)) {
		t.Fatalf("make private=%d %s", privateRec.Code, privateRec.Body.String())
	}
}

func TestExpertMarketUnlistedBlocksNewPurchaseButAllowsEntitledDownload(t *testing.T) {
	h, users, auth, credits := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller", "seller@example.test")
	buyer, _ := users.EnsureAccountWithID(ctx, "buyer", "buyer@example.test")
	_ = credits.TopUp(ctx, buyer.ID, 10)
	sellerSession, _ := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	buyerSession, _ := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	h.ensureExpertMarketSchema()
	data := expertMarketArchive(t, "pkgexp-unlisted", "Unlisted")
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.expertMarketDir(), "listing.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "listing", seller.ID, seller.Email, "pkgexp-unlisted", "Unlisted", "", "", "1", 5, "public", "listed", path, len(data), now, now)
	if err != nil {
		t.Fatal(err)
	}
	purchase := httptest.NewRequest(http.MethodPost, "/", nil)
	purchase.SetPathValue("id", "listing")
	purchase.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(purchaseRec, purchase)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase=%d", purchaseRec.Code)
	}
	withdraw := httptest.NewRequest(http.MethodPost, "/", nil)
	withdraw.SetPathValue("id", "listing")
	withdraw.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	withdrawRec := httptest.NewRecorder()
	h.WithdrawExpertMarketListing(withdrawRec, withdraw)
	if withdrawRec.Code != http.StatusOK {
		t.Fatalf("withdraw=%d", withdrawRec.Code)
	}
	download := httptest.NewRequest(http.MethodGet, "/", nil)
	download.SetPathValue("id", "listing")
	download.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	downloadRec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(downloadRec, download)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download=%d %s", downloadRec.Code, downloadRec.Body.String())
	}
	secondBuyer, err := users.EnsureAccountWithID(ctx, "second-buyer", "second@example.test")
	if err != nil {
		t.Fatal(err)
	}
	_ = credits.TopUp(ctx, secondBuyer.ID, 10)
	secondSession, err := auth.CreateSessionForUser(ctx, secondBuyer.ID, secondBuyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	blocked := httptest.NewRequest(http.MethodPost, "/", nil)
	blocked.SetPathValue("id", "listing")
	blocked.Header.Set("Authorization", "Bearer "+secondSession.Token)
	blockedRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(blockedRec, blocked)
	if blockedRec.Code != http.StatusNotFound {
		t.Fatalf("new purchase after unlist=%d %s", blockedRec.Code, blockedRec.Body.String())
	}
}

func TestExpertMarketDeleteRetainsEntitledDownloadButPurgeRequiresNoEntitlements(t *testing.T) {
	h, users, auth, credits := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-delete", "seller-delete@example.test")
	buyer, _ := users.EnsureAccountWithID(ctx, "buyer-delete", "buyer-delete@example.test")
	_ = credits.TopUp(ctx, buyer.ID, 10)
	sellerSession, _ := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	buyerSession, _ := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	h.ensureExpertMarketSchema()
	data := expertMarketArchive(t, "pkgexp-delete", "Archived expert")
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.expertMarketDir(), "delete.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "delete-listing", seller.ID, seller.Email, "pkgexp-delete", "Archived expert", "", "", "1", 5, "public", "listed", path, len(data), now, now); err != nil {
		t.Fatal(err)
	}
	purchase := httptest.NewRequest(http.MethodPost, "/", nil)
	purchase.SetPathValue("id", "delete-listing")
	purchase.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(purchaseRec, purchase)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase=%d %s", purchaseRec.Code, purchaseRec.Body.String())
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"policy archive"}`))
	deleteReq.SetPathValue("id", "delete-listing")
	unlist := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"reason":"temporarily unavailable"}`))
	unlist.SetPathValue("id", "delete-listing")
	unlistRec := httptest.NewRecorder()
	h.AdminUnlistExpertMarketListing(unlistRec, unlist)
	if unlistRec.Code != http.StatusOK {
		t.Fatalf("unlist=%d %s", unlistRec.Code, unlistRec.Body.String())
	}
	deleteRec := httptest.NewRecorder()
	h.AdminDeleteExpertMarketListing(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", deleteRec.Code, deleteRec.Body.String())
	}
	download := httptest.NewRequest(http.MethodGet, "/", nil)
	download.SetPathValue("id", "delete-listing")
	download.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	downloadRec := httptest.NewRecorder()
	h.DownloadExpertMarketListing(downloadRec, download)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("archived download=%d %s", downloadRec.Code, downloadRec.Body.String())
	}
	purge := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"remove bytes"}`))
	purge.SetPathValue("id", "delete-listing")
	purgeRec := httptest.NewRecorder()
	h.AdminPurgeExpertMarketListing(purgeRec, purge)
	if purgeRec.Code != http.StatusConflict {
		t.Fatalf("purge with entitlement=%d %s", purgeRec.Code, purgeRec.Body.String())
	}
	_ = sellerSession // seller session verifies the test follows normal account setup.
}

func TestExpertMarketOptionalModerationNoteAndInstallationAudit(t *testing.T) {
	h, users, auth, credits := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-audit", "seller-audit@example.test")
	buyer, _ := users.EnsureAccountWithID(ctx, "buyer-audit", "buyer-audit@example.test")
	_ = credits.TopUp(ctx, buyer.ID, 10)
	buyerSession, _ := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	h.ensureExpertMarketSchema()
	data := expertMarketArchive(t, "pkgexp-audit", "Audited expert")
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.expertMarketDir(), "audit.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "audit-listing", seller.ID, seller.Email, "pkgexp-audit", "Audited expert", "", "", "1", 1, "public", "pending_review", path, len(data), now, now); err != nil {
		t.Fatal(err)
	}
	approve := httptest.NewRequest(http.MethodPost, "/", nil)
	approve.SetPathValue("id", "audit-listing")
	approveRec := httptest.NewRecorder()
	h.AdminApproveExpertMarketListing(approveRec, approve)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve without reason=%d", approveRec.Code)
	}
	var reviewNote string
	if err := h.store.DB().QueryRow(`SELECT review_note FROM sm_expert_market_listings WHERE id='audit-listing'`).Scan(&reviewNote); err != nil || reviewNote != "" {
		t.Fatalf("optional review note=%q err=%v", reviewNote, err)
	}
	purchase := httptest.NewRequest(http.MethodPost, "/", nil)
	purchase.SetPathValue("id", "audit-listing")
	purchase.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(purchaseRec, purchase)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase=%d", purchaseRec.Code)
	}
	report := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"status":"failed","failure_stage":"dependencies","error_message":"skill missing"}`))
	report.SetPathValue("id", "audit-listing")
	report.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	reportRec := httptest.NewRecorder()
	h.ReportExpertMarketInstallation(reportRec, report)
	if reportRec.Code != http.StatusCreated {
		t.Fatalf("installation report=%d %s", reportRec.Code, reportRec.Body.String())
	}
	var stage string
	if err := h.store.DB().QueryRow(`SELECT failure_stage FROM sm_expert_market_installations WHERE listing_id='audit-listing'`).Scan(&stage); err != nil || stage != "dependencies" {
		t.Fatalf("stage=%q err=%v", stage, err)
	}
}

func TestExpertMarketInstallationReportRejectsTrailingOrOversizedInput(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "installation-validation-owner", "installation-validation@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	const listingID = "installation-validation-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, owner.ID, owner.Email, "pkgexp-installation-validation", "Installation validation", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"status":"installed","local_expert_id":"local"}{}`,
		`{"status":"failed","error_message":"` + strings.Repeat("x", 2049) + `"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+listingID+"/installations", strings.NewReader(body))
		req.SetPathValue("id", listingID)
		req.Header.Set("Authorization", "Bearer "+session.Token)
		rec := httptest.NewRecorder()
		h.ReportExpertMarketInstallation(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("installation validation body=%q status=%d response=%s", body[:min(len(body), 80)], rec.Code, rec.Body.String())
		}
	}
	var reports int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_installations WHERE listing_id=?`, listingID).Scan(&reports); err != nil || reports != 0 {
		t.Fatalf("invalid reports persisted: count=%d err=%v", reports, err)
	}
}

func TestExpertMarketInstallationReportUpsertsRetryState(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	owner, err := users.EnsureAccountWithID(ctx, "installation-retry-owner", "installation-retry@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	const listingID = "installation-retry-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, owner.ID, owner.Email, "pkgexp-installation-retry", "Installation retry", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"status":"failed","version":"1.0","failure_stage":"download","error_message":"network unavailable"}`,
		`{"status":"installed","local_expert_id":"local-retry","version":"1.1"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/"+listingID+"/installations", strings.NewReader(body))
		req.SetPathValue("id", listingID)
		req.Header.Set("Authorization", "Bearer "+session.Token)
		rec := httptest.NewRecorder()
		h.ReportExpertMarketInstallation(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("installation report=%d %s", rec.Code, rec.Body.String())
		}
	}
	var reports int
	var status, localID, version, stage, message string
	if err := h.store.DB().QueryRow(`SELECT COUNT(*), MAX(status), MAX(local_expert_id), MAX(version), MAX(failure_stage), MAX(error_message) FROM sm_expert_market_installations WHERE listing_id=? AND user_id=?`, listingID, owner.ID).Scan(&reports, &status, &localID, &version, &stage, &message); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || status != "installed" || localID != "local-retry" || version != "1.1" || stage != "" || message != "" {
		t.Fatalf("installation retry state reports=%d status=%q local=%q version=%q stage=%q message=%q", reports, status, localID, version, stage, message)
	}
}

func TestExpertMarketRejectAllowsEmptyReviewNote(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-reject", "seller-reject@example.test")
	h.ensureExpertMarketSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "reject-listing", seller.ID, seller.Email, "pkgexp-reject", "Rejected expert", "", "", "1", 0, "public", "pending_review", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	reject := httptest.NewRequest(http.MethodPost, "/", nil)
	reject.SetPathValue("id", "reject-listing")
	rejectRec := httptest.NewRecorder()
	h.AdminRejectExpertMarketListing(rejectRec, reject)
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("reject without note=%d %s", rejectRec.Code, rejectRec.Body.String())
	}
	var status, reviewNote string
	if err := h.store.DB().QueryRow(`SELECT status, review_note FROM sm_expert_market_listings WHERE id='reject-listing'`).Scan(&status, &reviewNote); err != nil || status != "rejected" || reviewNote != "" {
		t.Fatalf("status=%q review note=%q err=%v", status, reviewNote, err)
	}
}

func TestExpertMarketReviewRollsBackWhenAuditWriteFails(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-review-rollback", "seller-review-rollback@example.test")
	h.ensureExpertMarketSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "review-rollback-listing", seller.ID, seller.Email, "pkgexp-review-rollback", "Review rollback", "", "", "1", 0, "public", "pending_review", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`CREATE TRIGGER fail_expert_review_audit BEFORE INSERT ON sm_expert_market_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	approve := httptest.NewRequest(http.MethodPost, "/", nil)
	approve.SetPathValue("id", "review-rollback-listing")
	approveRec := httptest.NewRecorder()
	h.AdminApproveExpertMarketListing(approveRec, approve)
	if approveRec.Code != http.StatusInternalServerError {
		t.Fatalf("approve with failed audit=%d %s", approveRec.Code, approveRec.Body.String())
	}
	var status string
	if err := h.store.DB().QueryRow(`SELECT status FROM sm_expert_market_listings WHERE id='review-rollback-listing'`).Scan(&status); err != nil || status != "pending_review" {
		t.Fatalf("status after failed audit=%q err=%v", status, err)
	}
}

func TestExpertMarketAdminLifecycleRollsBackWhenAuditWriteFails(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-admin-audit-rollback", "seller-admin-audit-rollback@example.test")
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "admin-audit-rollback", seller.ID, seller.Email, "pkgexp-admin-audit-rollback", "Admin audit rollback", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`CREATE TRIGGER fail_expert_admin_audit BEFORE INSERT ON sm_expert_market_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"reason":"policy"}`))
	req.SetPathValue("id", "admin-audit-rollback")
	rec := httptest.NewRecorder()
	h.AdminUnlistExpertMarketListing(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("unlist with failed audit=%d %s", rec.Code, rec.Body.String())
	}
	var status string
	if err := h.store.DB().QueryRow(`SELECT status FROM sm_expert_market_listings WHERE id='admin-audit-rollback'`).Scan(&status); err != nil || status != "listed" {
		t.Fatalf("status after failed admin audit=%q err=%v", status, err)
	}
}

func TestExpertMarketStructuredAuditIncludesPublicLifecycleTransitions(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-public-audit", "seller-public-audit@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	insert := func(id, status string) {
		t.Helper()
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, id, seller.ID, seller.Email, "pkgexp-"+id, id, "", "", "1", 0, "public", status, "", 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insert("public-audit-approve", "pending_review")
	insert("public-audit-unlist", "listed")
	insert("public-audit-delete", "unlisted")
	insert("public-audit-purge", "deleted")
	tests := []struct {
		id      string
		action  string
		from    string
		to      string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{id: "public-audit-approve", action: "approved", from: "pending_review", to: "listed", handler: h.AdminApproveExpertMarketListing},
		{id: "public-audit-unlist", action: "unlisted", from: "listed", to: "unlisted", handler: h.AdminUnlistExpertMarketListing},
		{id: "public-audit-delete", action: "deleted", from: "unlisted", to: "deleted", handler: h.AdminDeleteExpertMarketListing},
		{id: "public-audit-purge", action: "purged", from: "deleted", to: "purged", handler: h.AdminPurgeExpertMarketListing},
	}
	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"reason":"policy review"}`))
			req.Header.Set("X-Request-ID", "audit-request-"+tc.action)
			req.SetPathValue("id", tc.id)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s=%d %s", tc.action, rec.Code, rec.Body.String())
			}
			var action, reason, beforeJSON, afterJSON, requestID string
			if err := h.store.DB().QueryRow(`SELECT action, reason, before_json, after_json, request_id FROM sm_expert_market_audit_events WHERE listing_id=?`, tc.id).Scan(&action, &reason, &beforeJSON, &afterJSON, &requestID); err != nil {
				t.Fatal(err)
			}
			var before, after map[string]string
			if err := json.Unmarshal([]byte(beforeJSON), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(afterJSON), &after); err != nil {
				t.Fatal(err)
			}
			if action != tc.action || reason != "policy review" || requestID != "audit-request-"+tc.action || before["visibility"] != "public" || after["visibility"] != "public" || before["status"] != tc.from || after["status"] != tc.to {
				t.Fatalf("audit action=%q reason=%q request=%q before=%v after=%v", action, reason, requestID, before, after)
			}
		})
	}
}

func TestExpertMarketOwnerLifecycleRollsBackWhenAuditWriteFails(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-owner-audit-rollback", "seller-owner-audit-rollback@example.test")
	session, err := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	insert := func(id, sourceID, visibility, status string) {
		t.Helper()
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, id, seller.ID, seller.Email, sourceID, id, "", "", "1", 0, visibility, status, "", 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insert("owner-withdraw-rollback", "pkgexp-owner-withdraw-rollback", "public", "listed")
	insert("owner-private-rollback", "pkgexp-owner-private-rollback", "public", "listed")
	insert("owner-publish-rollback", "pkgexp-owner-publish-rollback", "private", "private")
	if _, err := h.store.DB().Exec(`CREATE TRIGGER fail_expert_owner_audit BEFORE INSERT ON sm_expert_market_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		id         string
		handler    func(http.ResponseWriter, *http.Request)
		visibility string
		status     string
	}{
		{name: "withdraw", id: "owner-withdraw-rollback", handler: h.WithdrawExpertMarketListing, visibility: "public", status: "listed"},
		{name: "make private", id: "owner-private-rollback", handler: h.MakeExpertMarketListingPrivate, visibility: "public", status: "listed"},
		{name: "publish", id: "owner-publish-rollback", handler: h.PublishExpertMarketListing, visibility: "private", status: "private"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetPathValue("id", tc.id)
			req.Header.Set("Authorization", "Bearer "+session.Token)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("%s with failed audit=%d body=%s", tc.name, rec.Code, rec.Body.String())
			}
			var visibility, status string
			if err := h.store.DB().QueryRow(`SELECT visibility, status FROM sm_expert_market_listings WHERE id=?`, tc.id).Scan(&visibility, &status); err != nil {
				t.Fatal(err)
			}
			if visibility != tc.visibility || status != tc.status {
				t.Fatalf("%s changed row after failed audit: visibility=%q status=%q", tc.name, visibility, status)
			}
		})
	}
}

func TestExpertMarketLifecycleActionsStillRequireReason(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-lifecycle", "seller-lifecycle@example.test")
	h.ensureExpertMarketSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	insert := func(id, sourceID, status string) {
		t.Helper()
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, id, seller.ID, seller.Email, sourceID, id, "", "", "1", 0, "public", status, "", 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insert("listed-requires-reason", "pkgexp-listed-reason", "listed")
	insert("unlisted-requires-reason", "pkgexp-unlisted-reason", "unlisted")
	insert("deleted-requires-reason", "pkgexp-deleted-reason", "deleted")

	tests := []struct {
		name    string
		id      string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "unlist", id: "listed-requires-reason", handler: h.AdminUnlistExpertMarketListing},
		{name: "delete", id: "unlisted-requires-reason", handler: h.AdminDeleteExpertMarketListing},
		{name: "purge", id: "deleted-requires-reason", handler: h.AdminPurgeExpertMarketListing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.SetPathValue("id", tc.id)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestExpertMarketAdminLifecycleRejectsMalformedAndOversizedRequests(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-lifecycle-validation", "seller-lifecycle-validation@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	insert := func(id, status string) {
		t.Helper()
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, id, seller.ID, seller.Email, "pkgexp-"+id, id, "", "", "1", 0, "public", status, "", 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insert("validation-approve", "pending_review")
	insert("validation-unlist", "listed")
	insert("validation-delete", "unlisted")
	insert("validation-purge", "deleted")
	tests := []struct {
		name    string
		id      string
		body    string
		handler func(http.ResponseWriter, *http.Request)
		status  string
	}{
		{name: "approve malformed", id: "validation-approve", body: `{"reason":`, handler: h.AdminApproveExpertMarketListing, status: "pending_review"},
		{name: "unlist trailing payload", id: "validation-unlist", body: `{"reason":"reviewed"}{}`, handler: h.AdminUnlistExpertMarketListing, status: "listed"},
		{name: "delete malformed", id: "validation-delete", body: `not-json`, handler: h.AdminDeleteExpertMarketListing, status: "unlisted"},
		{name: "purge oversized reason", id: "validation-purge", body: `{"reason":"` + strings.Repeat("x", 2049) + `"}`, handler: h.AdminPurgeExpertMarketListing, status: "deleted"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.SetPathValue("id", tc.id)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var status string
			if err := h.store.DB().QueryRow(`SELECT status FROM sm_expert_market_listings WHERE id=?`, tc.id).Scan(&status); err != nil || status != tc.status {
				t.Fatalf("listing status=%q err=%v", status, err)
			}
		})
	}
}

func TestExpertMarketAdminListReflectsModerationDecision(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-admin-list", "seller-admin-list@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(
		`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`,
		"admin-listing", seller.ID, seller.Email, "pkgexp-admin-list", "Reviewed expert", "", "", "1", 0, "public", "pending_review", "", 0, now, now,
	); err != nil {
		t.Fatal(err)
	}

	approve := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"reason":"review complete"}`))
	approve.SetPathValue("id", "admin-listing")
	approveRec := httptest.NewRecorder()
	h.AdminApproveExpertMarketListing(approveRec, approve)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve=%d %s", approveRec.Code, approveRec.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/?status=listed", nil)
	listRec := httptest.NewRecorder()
	h.AdminListExpertMarketListings(listRec, list)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list=%d %s", listRec.Code, listRec.Body.String())
	}
	var payload struct {
		Experts []expertMarketAdminListing `json:"experts"`
		Total   int                        `json:"total"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Experts) != 1 || payload.Experts[0].ID != "admin-listing" || payload.Experts[0].Status != "listed" {
		t.Fatalf("admin list did not reflect approval: %+v", payload)
	}
}

func TestExpertMarketAdminListFiltersByVisibility(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-admin-visibility", "seller-admin-visibility@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	insert := func(id, sourceID, visibility string) {
		t.Helper()
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, id, seller.ID, seller.Email, sourceID, id, "", "", "1", 0, visibility, "listed", "", 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insert("admin-public-listing", "pkgexp-admin-public", "public")
	insert("admin-private-listing", "pkgexp-admin-private", "private")

	req := httptest.NewRequest(http.MethodGet, "/?status=listed&visibility=public", nil)
	rec := httptest.NewRecorder()
	h.AdminListExpertMarketListings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered list=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Experts []expertMarketAdminListing `json:"experts"`
		Total   int                        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Experts) != 1 || payload.Experts[0].ID != "admin-public-listing" {
		t.Fatalf("visibility filter returned %+v", payload)
	}

	invalid := httptest.NewRecorder()
	h.AdminListExpertMarketListings(invalid, httptest.NewRequest(http.MethodGet, "/?visibility=team", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid visibility=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestExpertMarketSchemaCreatesPublicCatalogueIndexes(t *testing.T) {
	h, _, _, _ := newExpertMarketTestHandler(t)
	h.ensureExpertMarketSchema()
	rows, err := h.store.DB().Query(`SELECT name FROM sqlite_master WHERE type='index' AND name IN ('idx_sm_expert_market_public_created', 'idx_sm_expert_market_public_downloads', 'idx_sm_expert_market_public_sales')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexes := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		indexes[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"idx_sm_expert_market_public_created", "idx_sm_expert_market_public_downloads", "idx_sm_expert_market_public_sales"} {
		if !indexes[name] {
			t.Fatalf("missing public catalogue index %q", name)
		}
	}
}

func TestExpertMarketAdminListUsesPrimaryForConsistentTotal(t *testing.T) {
	writeDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer writeDB.Close()
	readDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer readDB.Close()
	store, err := skillmarket.NewStore(writeDB, readDB)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensureExpertMarketSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := writeDB.Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "primary-listing", "seller", "seller@example.test", "pkgexp-primary-list", "Primary listing", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.AdminListExpertMarketListings(rec, httptest.NewRequest(http.MethodGet, "/?status=listed", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"total":1`)) {
		t.Fatalf("admin list must use primary DB: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExpertMarketPublicListUsesPrimaryForConsistentResults(t *testing.T) {
	writeDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer writeDB.Close()
	readDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer readDB.Close()
	store, err := skillmarket.NewStore(writeDB, readDB)
	if err != nil {
		t.Fatal(err)
	}
	// The public-list handler authenticates before it queries. Point its auth
	// dependencies at the primary too, because the deliberately stale read DB
	// only models a lagging listing replica, not account storage.
	primaryStore, err := skillmarket.NewStore(writeDB, writeDB)
	if err != nil {
		t.Fatal(err)
	}
	users := skillmarket.NewUserService(primaryStore, nil)
	auth := skillmarket.NewAuthService(primaryStore, nil, "")
	viewer, err := users.EnsureAccountWithID(context.Background(), "primary-catalogue-viewer", "viewer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(context.Background(), viewer.ID, viewer.Email)
	if err != nil {
		t.Fatal(err)
	}
	// Initialize the Expert Market schema through a handler that uses the
	// primary as both connections. Otherwise the once-per-write-DB schema guard
	// would run only against the intentionally stale read replica.
	schemaHandler := NewSkillMarketHandlers(SkillMarketConfig{Store: primaryStore, UserSvc: users, AuthSvc: auth, DataDir: t.TempDir()})
	schemaHandler.ensureExpertMarketSchema()
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: users, AuthSvc: auth, DataDir: t.TempDir()})
	now := expertMarketNow()
	if _, err := writeDB.Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "primary-catalogue-listing", "seller", "seller@example.test", "pkgexp-primary-catalogue", "Primary catalogue", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/experts", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.ListExpertMarketListings(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"id":"primary-catalogue-listing"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"total":1`)) {
		t.Fatalf("public list must use primary DB consistently: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExpertMarketPurchaseDownloadAndInstallationUsePrimaryState(t *testing.T) {
	writeDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer writeDB.Close()
	readDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer readDB.Close()
	store, err := skillmarket.NewStore(writeDB, readDB)
	if err != nil {
		t.Fatal(err)
	}
	primaryStore, err := skillmarket.NewStore(writeDB, writeDB)
	if err != nil {
		t.Fatal(err)
	}
	users := skillmarket.NewUserService(primaryStore, nil)
	auth := skillmarket.NewAuthService(primaryStore, nil, "")
	credits := skillmarket.NewCreditsService(primaryStore)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "primary-state-seller", "seller-primary@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := users.EnsureAccountWithID(ctx, "primary-state-buyer", "buyer-primary@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := credits.TopUp(ctx, buyer.ID, 10); err != nil {
		t.Fatal(err)
	}
	buyerSession, err := auth.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	schemaHandler := NewSkillMarketHandlers(SkillMarketConfig{Store: primaryStore, UserSvc: users, AuthSvc: auth, CreditsSvc: credits, DataDir: t.TempDir()})
	schemaHandler.ensureExpertMarketSchema()
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: users, AuthSvc: auth, CreditsSvc: credits, DataDir: t.TempDir()})
	now := expertMarketNow()
	if _, err := writeDB.Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "primary-state-listing", seller.ID, seller.Email, "pkgexp-primary-state", "Primary state", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}

	purchase := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/primary-state-listing/purchase", nil)
	purchase.SetPathValue("id", "primary-state-listing")
	purchase.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchaseExpertMarketListing(purchaseRec, purchase)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase must read current primary listing: status=%d body=%s", purchaseRec.Code, purchaseRec.Body.String())
	}

	installation := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts/primary-state-listing/installations", strings.NewReader(`{"status":"failed","failure_stage":"download"}`))
	installation.SetPathValue("id", "primary-state-listing")
	installation.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	installationRec := httptest.NewRecorder()
	h.ReportExpertMarketInstallation(installationRec, installation)
	if installationRec.Code != http.StatusCreated {
		t.Fatalf("installation report must see current primary entitlement: status=%d body=%s", installationRec.Code, installationRec.Body.String())
	}
}

func TestExpertMarketAdminDeleteRequiresUnlistedStatus(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-delete-guard", "seller-delete-guard@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(
		`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`,
		"listed-delete-guard", seller.ID, seller.Email, "pkgexp-delete-guard", "Listed expert", "", "", "1", 0, "public", "listed", "", 0, now, now,
	); err != nil {
		t.Fatal(err)
	}

	remove := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"remove"}`))
	remove.SetPathValue("id", "listed-delete-guard")
	removeRec := httptest.NewRecorder()
	h.AdminDeleteExpertMarketListing(removeRec, remove)
	if removeRec.Code != http.StatusConflict {
		t.Fatalf("delete listed=%d %s", removeRec.Code, removeRec.Body.String())
	}
}

func TestExpertMarketAdminEventsUseWriteDBAndStableOrdering(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-event-list", "seller-event-list@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(
		`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`,
		"event-listing", seller.ID, seller.Email, "pkgexp-event-list", "Event expert", "", "", "1", 0, "public", "listed", "", 0, now, now,
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct{ id, action string }{{"event-a", "approved"}, {"event-b", "unlisted"}} {
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, 'administrator', ?, '', ?)`, event.id, "event-listing", event.action, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_audit_events (id, listing_id, actor_admin_id, action, reason, created_at) VALUES ('audit-event-c', ?, 'admin-id', 'transfer_owner', 'handoff', ?)`, "event-listing", now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetPathValue("id", "event-listing")
	rec := httptest.NewRecorder()
	h.AdminListExpertMarketEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("events=%d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Events []map[string]string `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 3 || payload.Events[0]["id"] != "event-b" || payload.Events[1]["id"] != "event-a" || payload.Events[2]["id"] != "audit-event-c" || payload.Events[2]["source"] != "structured" || payload.Events[2]["before_json"] != "{}" || payload.Events[2]["after_json"] != "{}" || payload.Events[2]["request_id"] != "" || payload.Events[0]["source"] != "legacy" {
		t.Fatalf("events ordering=%+v", payload.Events)
	}
}

func TestExpertMarketAdminEventsOrderChronologicallyAndExposeStructuredAudit(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-event-details", "seller-event-details@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "event-details-listing", seller.ID, seller.Email, "pkgexp-event-details", "Event details", "", "", "1", 0, "public", "listed", "", 0, "2026-08-15T09:00:00Z", "2026-08-15T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES ('event-whole-second', 'event-details-listing', 'administrator', 'unlisted', 'legacy', '2026-08-15T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_audit_events (id, listing_id, actor_admin_id, action, reason, before_json, after_json, request_id, created_at) VALUES ('event-fractional-second', 'event-details-listing', 'admin-1', 'listed', 'approved', '{"status":"pending_review"}', '{"status":"listed"}', 'request-123', '2026-08-15T09:00:00.100Z')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetPathValue("id", "event-details-listing")
	rec := httptest.NewRecorder()
	h.AdminListExpertMarketEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("events=%d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Events []map[string]string `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 2 || payload.Events[0]["id"] != "event-fractional-second" || payload.Events[0]["source"] != "structured" || payload.Events[0]["before_json"] != `{"status":"pending_review"}` || payload.Events[0]["after_json"] != `{"status":"listed"}` || payload.Events[0]["request_id"] != "request-123" || payload.Events[1]["id"] != "event-whole-second" {
		t.Fatalf("events=%+v", payload.Events)
	}

	missing := httptest.NewRequest(http.MethodGet, "/", nil)
	missing.SetPathValue("id", "missing-event-listing")
	missingRec := httptest.NewRecorder()
	h.AdminListExpertMarketEvents(missingRec, missing)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing listing events=%d %s", missingRec.Code, missingRec.Body.String())
	}
}

func TestExpertMarketAdminEventsArePaginatedAndBounded(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-event-pagination", "seller-event-pagination@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	const listingID = "event-pagination-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-event-pagination", "Event pagination", "", "", "1", 0, "public", "listed", "", 0, "2026-08-15T09:00:00Z", "2026-08-15T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 55; i++ {
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, 'administrator', 'reviewed', '', ?)`, fmt.Sprintf("event-page-%03d", i), listingID, fmt.Sprintf("2026-08-15T09:%02d:00Z", i)); err != nil {
			t.Fatal(err)
		}
	}
	request := func(rawQuery string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
		req.SetPathValue("id", listingID)
		rec := httptest.NewRecorder()
		h.AdminListExpertMarketEvents(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("events query %q status=%d response=%s", rawQuery, rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	first := request("")
	firstEvents := first["events"].([]any)
	if len(firstEvents) != 50 || !first["has_more"].(bool) || int(first["page"].(float64)) != 1 || int(first["page_size"].(float64)) != 50 {
		t.Fatalf("default event page=%+v", first)
	}
	second := request("page=2&page_size=50")
	secondEvents := second["events"].([]any)
	if len(secondEvents) != 5 || second["has_more"].(bool) || int(second["page"].(float64)) != 2 || int(second["page_size"].(float64)) != 50 {
		t.Fatalf("second event page=%+v", second)
	}
	custom := request("page=1&page_size=2")
	if events := custom["events"].([]any); len(events) != 2 || !custom["has_more"].(bool) {
		t.Fatalf("custom event page=%+v", custom)
	}
}

func TestExpertMarketAdminEventsHasMoreUsesLookahead(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "seller-event-lookahead", "seller-event-lookahead@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	const listingID = "event-lookahead-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, seller.ID, seller.Email, "pkgexp-event-lookahead", "Event lookahead", "", "", "1", 0, "public", "listed", "", 0, "2026-08-15T09:00:00Z", "2026-08-15T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, 'administrator', 'reviewed', '', ?)`, fmt.Sprintf("event-lookahead-%d", i), listingID, fmt.Sprintf("2026-08-15T09:00:0%dZ", i)); err != nil {
			t.Fatal(err)
		}
	}
	request := func(page int) map[string]any {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?page=%d&page_size=2", page), nil)
		req.SetPathValue("id", listingID)
		rec := httptest.NewRecorder()
		h.AdminListExpertMarketEvents(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("events page %d status=%d response=%s", page, rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	first, second, third := request(1), request(2), request(3)
	if len(first["events"].([]any)) != 2 || !first["has_more"].(bool) {
		t.Fatalf("first exact-multiple event page=%+v", first)
	}
	if len(second["events"].([]any)) != 2 || second["has_more"].(bool) {
		t.Fatalf("last exact-multiple event page=%+v", second)
	}
	if len(third["events"].([]any)) != 0 || third["has_more"].(bool) {
		t.Fatalf("empty event page=%+v", third)
	}
}

func TestExpertMarketListEndpointsRejectInvalidPages(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	viewer, err := users.EnsureAccountWithID(ctx, "invalid-page-viewer", "invalid-page@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.CreateSessionForUser(ctx, viewer.ID, viewer.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	now := expertMarketNow()
	const listingID = "invalid-page-listing"
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, listingID, viewer.ID, viewer.Email, "pkgexp-invalid-page", "Invalid page", "", "", "1", 0, "public", "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		path    string
		admin   bool
	}{
		{name: "public listings", handler: h.ListExpertMarketListings, path: "/?page=100001"},
		{name: "admin listings", handler: h.AdminListExpertMarketListings, path: "/?page=-1", admin: true},
		{name: "admin users", handler: h.AdminListExpertMarketUsers, path: "/?keyword=ex&page=invalid", admin: true},
		{name: "admin events", handler: h.AdminListExpertMarketEvents, path: "/?page=999999999", admin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if !tc.admin {
				req.Header.Set("Authorization", "Bearer "+session.Token)
			}
			if tc.name == "admin events" {
				req.SetPathValue("id", listingID)
			}
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid page status=%d response=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestExpertMarketListingIDRejectsMalformedRouteValues(t *testing.T) {
	for _, rawID := range []string{"", " ", "../outside", "expert/other", strings.Repeat("x", 129)} {
		t.Run(strconv.Quote(rawID), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.SetPathValue("id", rawID)
			rec := httptest.NewRecorder()
			if id, ok := expertMarketListingID(rec, req); ok || id != "" || rec.Code != http.StatusNotFound {
				t.Fatalf("route id %q accepted: id=%q ok=%v status=%d", rawID, id, ok, rec.Code)
			}
		})
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetPathValue("id", "expert_valid-1.0")
	rec := httptest.NewRecorder()
	if id, ok := expertMarketListingID(rec, req); !ok || id != "expert_valid-1.0" {
		t.Fatalf("valid route id rejected: id=%q ok=%v", id, ok)
	}
}

func TestExpertMarketAuditRequestIDIsBounded(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("X-Request-ID", "  request-123  ")
	if got := expertMarketRequestID(request); got != "request-123" {
		t.Fatalf("trimmed request ID=%q", got)
	}
	request.Header.Set("X-Request-ID", strings.Repeat("x", expertMarketMaxRequestIDRunes+1))
	if got := expertMarketRequestID(request); got != "" {
		t.Fatalf("oversized request ID stored=%q", got)
	}
}

func TestExpertMarketPurgesUnentitledArchivedPackage(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-purge", "seller-purge@example.test")
	h.ensureExpertMarketSchema()
	data := expertMarketArchive(t, "pkgexp-purge", "Purgeable expert")
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.expertMarketDir(), "purge.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "purge-listing", seller.ID, seller.Email, "pkgexp-purge", "Purgeable expert", "", "", "1", 0, "public", "deleted", path, len(data), now, now); err != nil {
		t.Fatal(err)
	}
	purge := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"safe to remove"}`))
	purge.SetPathValue("id", "purge-listing")
	purgeRec := httptest.NewRecorder()
	h.AdminPurgeExpertMarketListing(purgeRec, purge)
	if purgeRec.Code != http.StatusOK {
		t.Fatalf("purge=%d %s", purgeRec.Code, purgeRec.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("archive remains after purge, err=%v", err)
	}
	var status, zipPath string
	if err := h.store.DB().QueryRow(`SELECT status, zip_path FROM sm_expert_market_listings WHERE id='purge-listing'`).Scan(&status, &zipPath); err != nil || status != "purged" || zipPath != "" {
		t.Fatalf("tombstone status=%q path=%q err=%v", status, zipPath, err)
	}
}

func TestExpertMarketPublicPurgeKeepsPackageWhenAuditWriteFails(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "public-purge-audit-owner", "public-purge-audit-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(h.expertMarketDir(), "public-purge-audit-failure.zip")
	if err := os.WriteFile(packagePath, []byte("public package"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := expertMarketNow()
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "public-purge-audit-failure", seller.ID, seller.Email, "pkgexp-public-purge-audit-failure", "Public purge audit failure", "", "", "1", 0, "public", "deleted", packagePath, 14, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB().Exec(`CREATE TRIGGER fail_public_purge_audit BEFORE INSERT ON sm_expert_market_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"retention elapsed"}`))
	req.SetPathValue("id", "public-purge-audit-failure")
	rec := httptest.NewRecorder()
	h.AdminPurgeExpertMarketListing(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("purge=%d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("package must remain after failed audit: %v", err)
	}
	var status, zipPath string
	if err := h.store.DB().QueryRow(`SELECT status, zip_path FROM sm_expert_market_listings WHERE id='public-purge-audit-failure'`).Scan(&status, &zipPath); err != nil || status != "deleted" || zipPath != packagePath {
		t.Fatalf("listing status=%q path=%q err=%v", status, zipPath, err)
	}
}

func TestExpertMarketPurgeNeverRemovesPackageOutsideMarketStorage(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-purge-foreign", "seller-purge-foreign@example.test")
	h.ensureExpertMarketSchema()
	foreign, err := os.CreateTemp(t.TempDir(), "not-a-market-package-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	foreignPath := foreign.Name()
	if _, err := foreign.WriteString("must remain"); err != nil {
		t.Fatal(err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "purge-foreign-listing", seller.ID, seller.Email, "pkgexp-purge-foreign", "External archive", "", "", "1", 0, "public", "deleted", foreignPath, 11, now, now); err != nil {
		t.Fatal(err)
	}
	purge := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"remove bytes"}`))
	purge.SetPathValue("id", "purge-foreign-listing")
	purgeRec := httptest.NewRecorder()
	h.AdminPurgeExpertMarketListing(purgeRec, purge)
	if purgeRec.Code != http.StatusInternalServerError {
		t.Fatalf("purge=%d %s", purgeRec.Code, purgeRec.Body.String())
	}
	if _, err := os.Stat(foreignPath); err != nil {
		t.Fatalf("purge must not remove an external package path: %v", err)
	}
	var status string
	if err := h.store.DB().QueryRow(`SELECT status FROM sm_expert_market_listings WHERE id='purge-foreign-listing'`).Scan(&status); err != nil || status != "deleted" {
		t.Fatalf("listing status=%q err=%v", status, err)
	}
}

func TestExpertMarketPurgeNeverFollowsDirectorySymlinkOutsideMarketStorage(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-purge-symlink", "seller-purge-symlink@example.test")
	h.ensureExpertMarketSchema()
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	foreignDir := t.TempDir()
	foreignPath := filepath.Join(foreignDir, "not-a-market-package.zip")
	if err := os.WriteFile(foreignPath, []byte("must remain"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(h.expertMarketDir(), "outside-link")
	if err := os.Symlink(foreignDir, linkPath); err != nil {
		t.Skipf("directory symlinks are unavailable on this test host: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "purge-symlink-listing", seller.ID, seller.Email, "pkgexp-purge-symlink", "Symlinked archive", "", "", "1", 0, "public", "deleted", filepath.Join(linkPath, filepath.Base(foreignPath)), 11, now, now); err != nil {
		t.Fatal(err)
	}
	purge := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"remove bytes"}`))
	purge.SetPathValue("id", "purge-symlink-listing")
	purgeRec := httptest.NewRecorder()
	h.AdminPurgeExpertMarketListing(purgeRec, purge)
	if purgeRec.Code != http.StatusInternalServerError {
		t.Fatalf("purge=%d %s", purgeRec.Code, purgeRec.Body.String())
	}
	if _, err := os.Stat(foreignPath); err != nil {
		t.Fatalf("purge must not remove a package through a directory symlink: %v", err)
	}
	var status string
	if err := h.store.DB().QueryRow(`SELECT status FROM sm_expert_market_listings WHERE id='purge-symlink-listing'`).Scan(&status); err != nil || status != "deleted" {
		t.Fatalf("listing status=%q err=%v", status, err)
	}
}

func TestRemoveExpertMarketPackageRejectsNonRegularTargets(t *testing.T) {
	h, _, _, _ := newExpertMarketTestHandler(t)
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	directoryPath := filepath.Join(h.expertMarketDir(), "not-a-package")
	if err := os.Mkdir(directoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.removeExpertMarketPackage(directoryPath); err == nil {
		t.Fatal("package cleanup must reject a directory")
	}
	if info, err := os.Stat(directoryPath); err != nil || !info.IsDir() {
		t.Fatalf("directory target changed after rejected cleanup: info=%v err=%v", info, err)
	}

	packagePath := filepath.Join(h.expertMarketDir(), "real-package.zip")
	if err := os.WriteFile(packagePath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(h.expertMarketDir(), "package-link.zip")
	if err := os.Symlink(packagePath, linkPath); err != nil {
		t.Skipf("file symlinks are unavailable on this test host: %v", err)
	}
	if err := h.removeExpertMarketPackage(linkPath); err == nil {
		t.Fatal("package cleanup must reject a symlink")
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("symlink target changed after rejected cleanup: %v", err)
	}
	if info, err := os.Lstat(linkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink changed after rejected cleanup: info=%v err=%v", info, err)
	}
}

func TestExpertMarketPurgeKeepsDeletedListingWhenPackageRemovalFails(t *testing.T) {
	h, users, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, _ := users.EnsureAccountWithID(ctx, "seller-purge-failure", "seller-purge-failure@example.test")
	h.ensureExpertMarketSchema()
	blockedPath := filepath.Join(h.expertMarketDir(), "blocked-package")
	if err := os.MkdirAll(blockedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blockedPath, "archive.zip"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "blocked-purge-listing", seller.ID, seller.Email, "pkgexp-purge-failure", "Blocked purge", "", "", "1", 0, "public", "deleted", blockedPath, 4, now, now); err != nil {
		t.Fatal(err)
	}
	purge := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"reason":"remove bytes"}`))
	purge.SetPathValue("id", "blocked-purge-listing")
	purgeRec := httptest.NewRecorder()
	h.AdminPurgeExpertMarketListing(purgeRec, purge)
	if purgeRec.Code != http.StatusInternalServerError {
		t.Fatalf("purge=%d %s", purgeRec.Code, purgeRec.Body.String())
	}
	var status, zipPath string
	if err := h.store.DB().QueryRow(`SELECT status, zip_path FROM sm_expert_market_listings WHERE id='blocked-purge-listing'`).Scan(&status, &zipPath); err != nil || status != "deleted" || zipPath != blockedPath {
		t.Fatalf("listing status=%q path=%q err=%v", status, zipPath, err)
	}
	if _, err := os.Stat(filepath.Join(blockedPath, "archive.zip")); err != nil {
		t.Fatalf("archive changed after failed purge: %v", err)
	}
}

func TestExpertMarketRejectsIncompleteOrUnexpectedPackageContent(t *testing.T) {
	makeArchive := func(t *testing.T, manifest map[string]any, extraName string) []byte {
		t.Helper()
		var out bytes.Buffer
		zw := zip.NewWriter(&out)
		file, err := zw.Create("manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(file).Encode(manifest); err != nil {
			t.Fatal(err)
		}
		if extraName != "" {
			if _, err := zw.Create(extraName); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	base := map[string]any{"format": "maclaw-expert-package", "version": 1, "expert_package_id": "pkgexp-validation", "expert": map[string]any{"name": "Validated", "description": "", "system_prompt": "Work safely."}}
	withoutPrompt := map[string]any{"format": "maclaw-expert-package", "version": 1, "expert_package_id": "pkgexp-no-prompt", "expert": map[string]any{"name": "Incomplete"}}
	if _, _, _, _, err := expertMarketManifest(makeArchive(t, withoutPrompt, "")); err == nil {
		t.Fatal("missing prompt package should be rejected")
	}
	if _, _, _, _, err := expertMarketManifest(makeArchive(t, base, "notes.txt")); err == nil {
		t.Fatal("unexpected package content should be rejected")
	}
	archiveWithDirectory := func(t *testing.T) []byte {
		t.Helper()
		var out bytes.Buffer
		zw := zip.NewWriter(&out)
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: "skills/"}); err != nil {
			t.Fatal(err)
		}
		file, err := zw.Create("manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(file).Encode(base); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	if _, _, _, _, err := expertMarketManifest(archiveWithDirectory(t)); err == nil {
		t.Fatal("directory entry should be rejected to match the desktop importer")
	}
	archiveWithSymlink := func(t *testing.T) []byte {
		t.Helper()
		var out bytes.Buffer
		zw := zip.NewWriter(&out)
		header := &zip.FileHeader{Name: "linked-skill"}
		header.SetMode(os.ModeSymlink | 0o777)
		if _, err := zw.CreateHeader(header); err != nil {
			t.Fatal(err)
		}
		file, err := zw.Create("manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(file).Encode(base); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	if _, _, _, _, err := expertMarketManifest(archiveWithSymlink(t)); err == nil {
		t.Fatal("symbolic link should be rejected to match the desktop importer")
	}
	builtin := map[string]any{"format": "maclaw-expert-package", "version": 1, "expert_package_id": "pkgexp-builtin", "expert": map[string]any{"id": "builtin-reviewer", "name": "Builtin", "system_prompt": "Work safely.", "builtin": true}}
	if _, _, _, _, err := expertMarketManifest(makeArchive(t, builtin, "")); err == nil {
		t.Fatal("built-in expert package should be rejected to match the desktop importer")
	}
}
