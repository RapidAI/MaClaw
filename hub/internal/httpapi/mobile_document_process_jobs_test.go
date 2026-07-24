package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMobileDocumentProcessShouldAsync(t *testing.T) {
	if !mobileDocumentProcessShouldAsync(true, "short") {
		t.Fatal("async flag should force async")
	}
	if mobileDocumentProcessShouldAsync(false, "short") {
		t.Fatal("short draft should stay sync")
	}
	// Build a large markdown over threshold.
	var b strings.Builder
	for utf8.RuneCountInString(b.String()) < mobileDocProcessAsyncRuneThreshold {
		b.WriteString("段落内容用于触发异步文档处理阈值。")
	}
	if !mobileDocumentProcessShouldAsync(false, b.String()) {
		t.Fatal("large draft should auto async")
	}
}

func TestMobileDocumentProcessAsyncEnqueue(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-doc-proc@example.com")
	ownerID := enroll.UserID

	draftID := "draft_proc_async_1"
	mobileDocuments.Lock()
	mobileDocuments.drafts[draftID] = mobileDocumentDraftRecord{
		ID: draftID, OwnerID: ownerID, TenantID: enroll.TenantID, Title: "长文",
		Markdown:  "# 标题\n\n正文第一段。",
		UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, draftID)
		mobileDocuments.Unlock()
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/drafts/"+draftID+"/process",
		strings.NewReader(`{"action":"summarize","async":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("draftId", draftID)
	rec := httptest.NewRecorder()
	MobileDocumentProcessHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	jobID, _ := body["job_id"].(string)
	if jobID == "" {
		t.Fatalf("missing job_id: %#v", body)
	}
	t.Cleanup(func() {
		mobileDocumentProcessJobs.Lock()
		delete(mobileDocumentProcessJobs.jobs, jobID)
		mobileDocumentProcessJobs.Unlock()
	})

	// Wait for background process.
	deadline := time.Now().Add(2 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		getReq := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/process-jobs/"+jobID, nil)
		getReq.Header.Set("Authorization", "Bearer "+token)
		getReq.SetPathValue("jobId", jobID)
		getRec := httptest.NewRecorder()
		MobileDocumentProcessJobStatusHandler(identity).ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		status, _ = got["status"].(string)
		if status == mobileDocProcessStatusReady {
			if got["draft"] == nil {
				t.Fatalf("ready job missing draft: %#v", got)
			}
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if status != mobileDocProcessStatusReady {
		t.Fatalf("expected ready, status=%q", status)
	}

	// Unified jobs list includes document_process.
	jobsReq := httptest.NewRequest(http.MethodGet, "/api/mobile/jobs", nil)
	jobsReq.Header.Set("Authorization", "Bearer "+token)
	jobsRec := httptest.NewRecorder()
	MobileJobsHandler(identity).ServeHTTP(jobsRec, jobsReq)
	if jobsRec.Code != http.StatusOK {
		t.Fatalf("jobs=%d body=%s", jobsRec.Code, jobsRec.Body.String())
	}
	var jobsBody map[string]any
	if err := json.Unmarshal(jobsRec.Body.Bytes(), &jobsBody); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range jobsBody["jobs"].([]any) {
		item := raw.(map[string]any)
		if item["job_id"] == jobID && item["kind"] == "document_process" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("document_process job missing from list: %#v", jobsBody)
	}
}

func TestMobileDocumentProcessSyncStillWorks(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-doc-proc-sync@example.com")
	draftID := "draft_proc_sync_1"
	mobileDocuments.Lock()
	mobileDocuments.drafts[draftID] = mobileDocumentDraftRecord{
		ID: draftID, OwnerID: enroll.UserID, TenantID: enroll.TenantID, Title: "短文",
		Markdown:  "# Hi\n\n短正文",
		UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, draftID)
		mobileDocuments.Unlock()
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/drafts/"+draftID+"/process",
		strings.NewReader(`{"action":"polish"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("draftId", draftID)
	rec := httptest.NewRecorder()
	MobileDocumentProcessHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "processed" {
		t.Fatalf("body=%#v", body)
	}
	if body["draft"] == nil {
		t.Fatal("missing draft")
	}
}
