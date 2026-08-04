package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMobileDocumentDraftDeleteHandler(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "doc-del@example.com")
	mobileDocuments.Lock()
	mobileDocuments.drafts["d_del_1"] = mobileDocumentDraftRecord{
		ID: "d_del_1", OwnerID: enroll.UserID, TenantID: enroll.TenantID, Title: "T", Markdown: "# T\n", UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.drafts["d_other"] = mobileDocumentDraftRecord{
		ID: "d_other", OwnerID: "someone-else", Title: "X", Markdown: "x", UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "d_del_1")
		delete(mobileDocuments.drafts, "d_other")
		mobileDocuments.Unlock()
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/drafts/d_del_1", nil)
	req.SetPathValue("draftId", "d_del_1")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileDocumentDraftUpdateHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	mobileDocuments.Lock()
	_, still := mobileDocuments.drafts["d_del_1"]
	other, otherOK := mobileDocuments.drafts["d_other"]
	mobileDocuments.Unlock()
	if still {
		t.Fatal("draft should be deleted")
	}
	if !otherOK || other.OwnerID != "someone-else" {
		t.Fatal("other owner's draft must remain")
	}

	// Foreign draft: respond 200 already_gone (do not leak existence) and keep it.
	bad := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/drafts/d_other", nil)
	bad.SetPathValue("draftId", "d_other")
	bad.Header.Set("Authorization", "Bearer "+token)
	badRec := httptest.NewRecorder()
	MobileDocumentDraftUpdateHandler(identity).ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusOK {
		t.Fatalf("expected 200 already_gone for foreign draft, got %d body=%s", badRec.Code, badRec.Body.String())
	}
	mobileDocuments.Lock()
	_, otherStill := mobileDocuments.drafts["d_other"]
	mobileDocuments.Unlock()
	if !otherStill {
		t.Fatal("other owner's draft must remain")
	}

	// Missing draft: idempotent 200.
	miss := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/drafts/missing", nil)
	miss.SetPathValue("draftId", "missing")
	miss.Header.Set("Authorization", "Bearer "+token)
	missRec := httptest.NewRecorder()
	MobileDocumentDraftUpdateHandler(identity).ServeHTTP(missRec, miss)
	if missRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for missing draft, got %d", missRec.Code)
	}
}

func TestMobileDocumentDraftsListHandler(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-drafts-list@example.com")
	ownerID := enroll.UserID

	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.drafts["d_list_1"] = mobileDocumentDraftRecord{
		ID: "d_list_1", OwnerID: ownerID, TenantID: enroll.TenantID, Title: "周报", Template: "report",
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

func TestMobileDocumentDraftsListReportsOriginalAndStoredSizes(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-drafts-compressed-size@example.com")
	clearMobileStateForTest(t)
	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.drafts["compressed-size-draft"] = mobileDocumentDraftRecord{
		ID: "compressed-size-draft", OwnerID: enroll.UserID, TenantID: enroll.TenantID,
		Title: "legacy", Markdown: mobileDocumentUnsupportedPreviewText, UpdatedAt: now,
		SourceFilename: "legacy.doc", SourcePath: "offline/blob.bin", SourceSize: 123,
		SourceEncoding: "gzip", SourceOriginalSize: 4096,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/drafts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileDocumentDraftsListHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Drafts []map[string]any `json:"drafts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, item := range body.Drafts {
		if item["id"] != "compressed-size-draft" {
			continue
		}
		if item["source_size"] != float64(4096) || item["source_storage_size"] != float64(123) {
			t.Fatalf("sizes=%#v", item)
		}
		return
	}
	t.Fatal("compressed draft missing from list")
}

func TestMobileDocumentDraftsListLabelsGeneratedMeetingResultsWithParentRecording(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-drafts-meeting-parent@example.com")
	const recordingID = "meeting-document-list-parent"
	const draftID = "meeting-document-list-transcript"
	now := time.Now().UTC()

	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[recordingID] = mobileMeetingRecording{
		ID: recordingID, OwnerID: enroll.UserID, TenantID: enroll.TenantID,
		Status: "ready", TranscriptDraftID: draftID, UpdatedAt: now,
	}
	mobileMeetingRecordings.Unlock()
	mobileDocuments.Lock()
	mobileDocuments.drafts[draftID] = mobileDocumentDraftRecord{
		ID: draftID, OwnerID: enroll.UserID, TenantID: enroll.TenantID,
		Title: "Meeting transcript", Markdown: "Transcript", UpdatedAt: now,
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, recordingID)
		mobileMeetingRecordings.Unlock()
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, draftID)
		mobileDocuments.Unlock()
	})

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/drafts", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	MobileDocumentDraftsListHandler(identity).ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listBody struct {
		Drafts []map[string]any `json:"drafts"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	for _, item := range listBody.Drafts {
		if item["id"] == draftID && item["managed_by_recording_id"] != recordingID {
			t.Fatalf("list parent=%#v", item)
		}
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/drafts/"+draftID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getReq.SetPathValue("draftId", draftID)
	getResp := httptest.NewRecorder()
	MobileDocumentDraftsListHandler(identity).ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var getBody struct {
		Draft map[string]any `json:"draft"`
	}
	if err := json.Unmarshal(getResp.Body.Bytes(), &getBody); err != nil {
		t.Fatal(err)
	}
	if getBody.Draft["managed_by_recording_id"] != recordingID {
		t.Fatalf("get parent=%#v", getBody.Draft)
	}
}

func TestMobileDocumentLibraryKeepsDraftsTenantIsolated(t *testing.T) {
	owner := "document-library-shared-owner"
	now := time.Now().UTC()
	first := mobileDocumentDraftRecord{ID: "draft-tenant-a", OwnerID: owner, TenantID: "tenant-a", Title: "Tenant A", Markdown: "# A", UpdatedAt: now}
	second := mobileDocumentDraftRecord{ID: "draft-tenant-b", OwnerID: owner, TenantID: "tenant-b", Title: "Tenant B", Markdown: "# B", UpdatedAt: now}
	mobileDocuments.Lock()
	mobileDocuments.drafts[first.ID] = first
	mobileDocuments.drafts[second.ID] = second
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, first.ID)
		delete(mobileDocuments.drafts, second.ID)
		mobileDocuments.Unlock()
	})

	items := mobileLibraryItems(owner, "tenant-a", true, false)
	if len(items) != 1 || items[0]["id"] != first.ID {
		t.Fatalf("library documents = %#v", items)
	}
	if _, ok := mobileLibraryItemByID(owner, "tenant-a", second.ID); ok {
		t.Fatal("a tenant must not retrieve another tenant's document draft")
	}
}
