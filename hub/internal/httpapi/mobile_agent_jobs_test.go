package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMobileAgentJobsHandlerRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/jobs", strings.NewReader(`{"query":"hi"}`))
	MobileAgentJobsHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestMobileAgentJobsCreateAndGetWithoutLLM(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-agent-job@example.com")
	ownerID := enroll.UserID

	// Clean any leftover jobs for isolation.
	mobileAgentJobs.Lock()
	for id, j := range mobileAgentJobs.jobs {
		if j.OwnerID == ownerID {
			delete(mobileAgentJobs.jobs, id)
		}
	}
	mobileAgentJobs.Unlock()

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/jobs", strings.NewReader(
		`{"query":"请分析本周风险","messages":[{"role":"user","content":"ctx"}]}`,
	))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	// No official LLM → job should still enqueue then fail in background.
	MobileAgentJobsHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	jobID, _ := created["job_id"].(string)
	if jobID == "" {
		t.Fatalf("missing job_id: %#v", created)
	}
	if created["status"] != mobileAgentJobStatusQueued && created["status"] != mobileAgentJobStatusRunning {
		// Accept queued (immediate response) or racing to running.
		if created["kind"] != "assistant" {
			t.Fatalf("created=%#v", created)
		}
	}

	// Wait for background runner to mark failed (no LLM).
	deadline := time.Now().Add(3 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		getReq := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/jobs/"+jobID, nil)
		getReq.Header.Set("Authorization", "Bearer "+token)
		// PathValue is set by ServeMux; call path-aware handler via set.
		getReq.SetPathValue("jobId", jobID)
		getRec := httptest.NewRecorder()
		MobileAgentJobsHandler(identity).ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(getRec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		status, _ = body["status"].(string)
		if status == mobileAgentJobStatusFailed || status == mobileAgentJobStatusReady {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != mobileAgentJobStatusFailed {
		t.Fatalf("expected failed without LLM, status=%q", status)
	}

	// Unified jobs list includes assistant kind.
	jobsReq := httptest.NewRequest(http.MethodGet, "/api/mobile/jobs", nil)
	jobsReq.Header.Set("Authorization", "Bearer "+token)
	jobsRec := httptest.NewRecorder()
	MobileJobsHandler(identity).ServeHTTP(jobsRec, jobsReq)
	if jobsRec.Code != http.StatusOK {
		t.Fatalf("jobs status=%d body=%s", jobsRec.Code, jobsRec.Body.String())
	}
	var jobsBody map[string]any
	if err := json.Unmarshal(jobsRec.Body.Bytes(), &jobsBody); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range jobsBody["jobs"].([]any) {
		item := raw.(map[string]any)
		if item["job_id"] == jobID && item["kind"] == "assistant" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("assistant job missing from unified list: %#v", jobsBody)
	}

	t.Cleanup(func() {
		mobileAgentJobs.Lock()
		delete(mobileAgentJobs.jobs, jobID)
		mobileAgentJobs.Unlock()
	})
}

func TestMobileSearchAsyncEnqueuesJob(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-search-async@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", strings.NewReader(
		`{"query":"长任务请后台执行","async":true}`,
	))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileSearchHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["async"] != true {
		t.Fatalf("body=%#v", body)
	}
	jobID, _ := body["job_id"].(string)
	if jobID == "" {
		t.Fatalf("missing job_id: %#v", body)
	}
	t.Cleanup(func() {
		mobileAgentJobs.Lock()
		delete(mobileAgentJobs.jobs, jobID)
		mobileAgentJobs.Unlock()
	})
}
