package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestMobileDocumentQuotaUsedBytes(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "quota-user@example.com")
	owner := enroll.UserID

	mobileDocuments.Lock()
	mobileDocuments.drafts["q_d1"] = mobileDocumentDraftRecord{
		ID: "q_d1", OwnerID: owner, TenantID: enroll.TenantID, Title: "A", Markdown: strings.Repeat("x", 100),
		Images: []mobileDocumentDraftImage{{ID: "img1", SourceSize: 25}}, UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.uploads["q_u1"] = mobileDocumentUploadRecord{
		TaskID: "q_u1", OwnerID: owner, TenantID: enroll.TenantID, SourceBytes: []byte(strings.Repeat("y", 50)),
	}
	mobileDocuments.drafts["q_other"] = mobileDocumentDraftRecord{
		ID: "q_other", OwnerID: "other", Markdown: strings.Repeat("z", 1000),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "q_d1")
		delete(mobileDocuments.drafts, "q_other")
		delete(mobileDocuments.uploads, "q_u1")
		mobileDocuments.Unlock()
	})

	used := mobileDocumentQuotaUsedBytes(owner, enroll.TenantID)
	if used != 175 {
		t.Fatalf("used=%d want 175", used)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/quota", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileDocumentQuotaHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["document_quota_used_bytes"] != float64(175) {
		t.Fatalf("body=%#v", body)
	}
}

func TestMobileDocumentQuotaKeepsTenantsIsolated(t *testing.T) {
	owner := "quota-shared-owner"
	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.drafts["quota-tenant-a"] = mobileDocumentDraftRecord{ID: "quota-tenant-a", OwnerID: owner, TenantID: "tenant-a", Markdown: strings.Repeat("a", 20), UpdatedAt: now}
	mobileDocuments.uploads["quota-upload-tenant-a"] = mobileDocumentUploadRecord{TaskID: "quota-upload-tenant-a", OwnerID: owner, TenantID: "tenant-a", SourceBytes: []byte(strings.Repeat("a", 10))}
	mobileDocuments.drafts["quota-tenant-b"] = mobileDocumentDraftRecord{ID: "quota-tenant-b", OwnerID: owner, TenantID: "tenant-b", Markdown: strings.Repeat("b", 200), UpdatedAt: now}
	mobileDocuments.uploads["quota-upload-tenant-b"] = mobileDocumentUploadRecord{TaskID: "quota-upload-tenant-b", OwnerID: owner, TenantID: "tenant-b", SourceBytes: []byte(strings.Repeat("b", 100))}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "quota-tenant-a")
		delete(mobileDocuments.uploads, "quota-upload-tenant-a")
		delete(mobileDocuments.drafts, "quota-tenant-b")
		delete(mobileDocuments.uploads, "quota-upload-tenant-b")
		mobileDocuments.Unlock()
	})
	if used := mobileDocumentQuotaUsedBytes(owner, "tenant-a"); used != 30 {
		t.Fatalf("tenant-a used=%d want 30", used)
	}
	if used := mobileDocumentQuotaUsedBytes(owner, "tenant-b"); used != 300 {
		t.Fatalf("tenant-b used=%d want 300", used)
	}
	if runes := mobileDocumentDraftRuneEstimate(owner, "tenant-a"); runes != 20 {
		t.Fatalf("tenant-a runes=%d want 20", runes)
	}
}

func TestMobileDocumentQuotaDeduplicatesOnlySharedBlobPath(t *testing.T) {
	owner := "quota-shared-blob-owner"
	tenant := "quota-shared-blob-tenant"
	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.drafts["quota-shared-draft"] = mobileDocumentDraftRecord{
		ID: "quota-shared-draft", OwnerID: owner, TenantID: tenant,
		Markdown: "body", SourcePath: "owner/upload/shared.bin", SourceSize: 40, UpdatedAt: now,
	}
	mobileDocuments.uploads["quota-shared-upload"] = mobileDocumentUploadRecord{
		TaskID: "quota-shared-upload", DraftID: "quota-shared-draft", OwnerID: owner, TenantID: tenant,
		SourcePath: "owner/upload/shared.bin", SourceSize: 40,
	}
	mobileDocuments.uploads["quota-distinct-upload"] = mobileDocumentUploadRecord{
		TaskID: "quota-distinct-upload", DraftID: "quota-shared-draft", OwnerID: owner, TenantID: tenant,
		SourcePath: "owner/upload/distinct.bin", SourceSize: 30,
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "quota-shared-draft")
		delete(mobileDocuments.uploads, "quota-shared-upload")
		delete(mobileDocuments.uploads, "quota-distinct-upload")
		mobileDocuments.Unlock()
	})

	// Markdown 4 + shared blob 40 + separately stored upload blob 30.
	if used, _ := mobileDocumentQuotaScan(owner, tenant, false); used != 74 {
		t.Fatalf("used=%d want 74", used)
	}
}

func TestMobileDocumentDraftCreateRespectsQuota(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "quota-create@example.com")
	owner := enroll.UserID

	// Leave only a few free bytes so a normal create fails.
	big := strings.Repeat("A", 100*1024*1024-20)
	mobileDocuments.Lock()
	mobileDocuments.drafts["q_fill"] = mobileDocumentDraftRecord{
		ID: "q_fill", OwnerID: owner, TenantID: enroll.TenantID, Title: "fill", Markdown: big, UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "q_fill")
		mobileDocuments.Unlock()
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/drafts",
		strings.NewReader(`{"title":"will fail","template":"report","content":"hello world this exceeds remaining"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileDocumentDraftHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("status=%d body=%s want 507", rec.Code, rec.Body.String())
	}
}

func TestMobileDocumentQuotaIgnoresMissingBlobOriginals(t *testing.T) {
	owner := "quota-ghost-owner"
	t.Setenv(mobileBlobDirEnv, t.TempDir())
	mobileDocuments.Lock()
	mobileDocuments.drafts["ghost_d"] = mobileDocumentDraftRecord{
		ID: "ghost_d", OwnerID: owner, Markdown: "abcd", // 4
		SourcePath: "missing/ghost.bin", SourceSize: 10_000, // should be repaired away
	}
	mobileDocuments.uploads["ghost_u"] = mobileDocumentUploadRecord{
		TaskID: "ghost_u", OwnerID: owner,
		SourcePath: "missing/up.bin", SourceSize: 20_000, // repaired away
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "ghost_d")
		delete(mobileDocuments.uploads, "ghost_u")
		mobileDocuments.Unlock()
	})

	// Fast path still counts ghost SourceSize (metadata trusted).
	fast, repairedFast := mobileDocumentQuotaScan(owner, "default", false)
	if repairedFast {
		t.Fatal("fast scan must not repair")
	}
	if fast < 10_000 {
		t.Fatalf("fast used=%d want inflated ghost size", fast)
	}
	// Repair path drops missing blobs.
	used, repaired := mobileDocumentQuotaScan(owner, "default", true)
	if !repaired {
		t.Fatal("expected repair for missing blobs")
	}
	if used != 4 {
		t.Fatalf("used=%d want 4 (markdown only)", used)
	}
	// Write check should pass after repair-on-over-limit path.
	if err := mobileCheckDocumentQuota(owner, "default", 100, 1000); err != nil {
		t.Fatalf("check after ghost repair: %v", err)
	}
	mobileDocuments.Lock()
	d := mobileDocuments.drafts["ghost_d"]
	u := mobileDocuments.uploads["ghost_u"]
	mobileDocuments.Unlock()
	if d.SourcePath != "" || d.SourceSize != 0 {
		t.Fatalf("draft original not cleared: %#v", d)
	}
	if u.SourcePath != "" || u.SourceSize != 0 {
		t.Fatalf("upload original not cleared: %#v", u)
	}
}

func TestMobileEffectiveDocumentQuotaPaidBoost(t *testing.T) {
	// Without system wiring, effective quota is free baseline.
	p := &auth.ViewerPrincipal{UserID: "u", Email: "u@example.com", TenantID: "t"}
	if got := mobileEffectiveDocumentQuota(context.Background(), p); got != 100*1024*1024 {
		t.Fatalf("free limit=%d", got)
	}
	// paidBoost helper still used by bootstrap path.
	if mobileDocumentQuotaLimitForPrincipal(p, true) != 500*1024*1024 {
		t.Fatal("paid boost")
	}
}

func TestMobileBootstrapIncludesQuotaUsed(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "quota-boot@example.com")
	mobileDocuments.Lock()
	mobileDocuments.drafts["qb1"] = mobileDocumentDraftRecord{
		ID: "qb1", OwnerID: enroll.UserID, TenantID: enroll.TenantID, Markdown: "hello", UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "qb1")
		mobileDocuments.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileBootstrapHandler(identity, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	limits, _ := body["limits"].(map[string]any)
	if limits["document_quota_used_bytes"] != float64(5) {
		t.Fatalf("limits=%#v want used=5", limits)
	}
}
