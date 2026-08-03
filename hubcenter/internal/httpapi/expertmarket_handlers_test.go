package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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
	payload := map[string]any{"format": "maclaw-expert-package", "version": 1, "expert_package_id": id, "expert": map[string]any{"name": name, "description": "a tested market expert", "icon": "🧪"}}
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
func fmtInt(v int64) string { return strconv.FormatInt(v, 10) }

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
	// The account endpoint is the source for “My submissions”; it must read
	// from the authoritative connection so a just-committed submission appears
	// even when a deployed read replica is behind.
	account := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/account", nil)
	account.Header.Set("Authorization", "Bearer "+sellerSession.Token)
	accountRec := httptest.NewRecorder()
	h.GetExpertMarketAccount(accountRec, account)
	if accountRec.Code != http.StatusOK || !bytes.Contains(accountRec.Body.Bytes(), []byte(`"id":"`+listing.ID+`"`)) {
		t.Fatalf("seller account should include its submitted expert: status=%d body=%s", accountRec.Code, accountRec.Body.String())
	}
	approve := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/"+listing.ID+"/approve", strings.NewReader(`{"reason":"content reviewed"}`))
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
	_, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "listing", seller.ID, seller.Email, "pkgexp-unlisted", "Unlisted", "", "", "1", 5, "listed", path, len(data), now, now)
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
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "delete-listing", seller.ID, seller.Email, "pkgexp-delete", "Archived expert", "", "", "1", 5, "listed", path, len(data), now, now); err != nil {
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

func TestExpertMarketModerationRequiresReasonAndInstallationAudit(t *testing.T) {
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
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "audit-listing", seller.ID, seller.Email, "pkgexp-audit", "Audited expert", "", "", "1", 1, "pending_review", path, len(data), now, now); err != nil {
		t.Fatal(err)
	}
	approve := httptest.NewRequest(http.MethodPost, "/", nil)
	approve.SetPathValue("id", "audit-listing")
	approveRec := httptest.NewRecorder()
	h.AdminApproveExpertMarketListing(approveRec, approve)
	if approveRec.Code != http.StatusBadRequest {
		t.Fatalf("approve without reason=%d", approveRec.Code)
	}
	if _, err := h.store.DB().Exec(`UPDATE sm_expert_market_listings SET status='listed' WHERE id='audit-listing'`); err != nil {
		t.Fatal(err)
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
		`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`,
		"admin-listing", seller.ID, seller.Email, "pkgexp-admin-list", "Reviewed expert", "", "", "1", 0, "pending_review", "", 0, now, now,
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
	if _, err := writeDB.Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "primary-listing", "seller", "seller@example.test", "pkgexp-primary-list", "Primary listing", "", "", "1", 0, "listed", "", 0, now, now); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.AdminListExpertMarketListings(rec, httptest.NewRequest(http.MethodGet, "/?status=listed", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"total":1`)) {
		t.Fatalf("admin list must use primary DB: status=%d body=%s", rec.Code, rec.Body.String())
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
		`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`,
		"listed-delete-guard", seller.ID, seller.Email, "pkgexp-delete-guard", "Listed expert", "", "", "1", 0, "listed", "", 0, now, now,
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
		`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`,
		"event-listing", seller.ID, seller.Email, "pkgexp-event-list", "Event expert", "", "", "1", 0, "listed", "", 0, now, now,
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct{ id, action string }{{"event-a", "approved"}, {"event-b", "unlisted"}} {
		if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, 'administrator', ?, '', ?)`, event.id, "event-listing", event.action, now); err != nil {
			t.Fatal(err)
		}
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
	if len(payload.Events) != 2 || payload.Events[0]["id"] != "event-b" || payload.Events[1]["id"] != "event-a" {
		t.Fatalf("events ordering=%+v", payload.Events)
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
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "purge-listing", seller.ID, seller.Email, "pkgexp-purge", "Purgeable expert", "", "", "1", 0, "deleted", path, len(data), now, now); err != nil {
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
	if _, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?)`, "blocked-purge-listing", seller.ID, seller.Email, "pkgexp-purge-failure", "Blocked purge", "", "", "1", 0, "deleted", blockedPath, 4, now, now); err != nil {
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
