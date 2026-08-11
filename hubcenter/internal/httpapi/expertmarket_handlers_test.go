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
