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
		ID: "q_d1", OwnerID: owner, Title: "A", Markdown: strings.Repeat("x", 100), UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.uploads["q_u1"] = mobileDocumentUploadRecord{
		TaskID: "q_u1", OwnerID: owner, SourceBytes: []byte(strings.Repeat("y", 50)),
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

	used := mobileDocumentQuotaUsedBytes(owner)
	if used != 150 {
		t.Fatalf("used=%d want 150", used)
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
	if body["document_quota_used_bytes"] != float64(150) {
		t.Fatalf("body=%#v", body)
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
		ID: "q_fill", OwnerID: owner, Title: "fill", Markdown: big, UpdatedAt: time.Now().UTC(),
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
		ID: "qb1", OwnerID: enroll.UserID, Markdown: "hello", UpdatedAt: time.Now().UTC(),
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
