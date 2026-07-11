package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMobileDocumentDraftsListHandler(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-drafts-list@example.com")
	ownerID := enroll.UserID

	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.drafts["d_list_1"] = mobileDocumentDraftRecord{
		ID: "d_list_1", OwnerID: ownerID, Title: "周报", Template: "report",
		Markdown: "# 周报\n\n内容", UpdatedAt: now,
	}
	mobileDocuments.drafts["d_other"] = mobileDocumentDraftRecord{
		ID: "d_other", OwnerID: "other", Title: "他人", Markdown: "x", UpdatedAt: now,
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "d_list_1")
		delete(mobileDocuments.drafts, "d_other")
		mobileDocuments.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/drafts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileDocumentDraftsListHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	count, _ := body["count"].(float64)
	if count < 1 {
		t.Fatalf("body=%#v", body)
	}
	for _, raw := range body["drafts"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == "d_other" {
			t.Fatal("leaked other owner draft")
		}
		if item["id"] == "d_list_1" {
			if item["markdown"] != nil {
				t.Fatal("list should omit markdown by default")
			}
			if item["title"] != "周报" {
				t.Fatalf("title=%v", item["title"])
			}
		}
	}

	// Get single draft with body.
	getReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/drafts/d_list_1", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getReq.SetPathValue("draftId", "d_list_1")
	getRec := httptest.NewRecorder()
	MobileDocumentDraftsListHandler(identity).ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var one map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	draft, _ := one["draft"].(map[string]any)
	if draft["markdown"] == nil || draft["title"] != "周报" {
		t.Fatalf("draft=%#v", one)
	}
}
