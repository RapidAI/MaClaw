package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMobileJobsHandlerRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/jobs", nil)
	MobileJobsHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestMobileJobsHandlerListsOwnedJobs(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-jobs@example.com")
	ownerID := enroll.UserID
	if ownerID == "" {
		t.Fatal("expected enroll user id")
	}

	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.uploads["up_job_1"] = mobileDocumentUploadRecord{
		TaskID: "up_job_1", OwnerID: ownerID, TenantID: enroll.TenantID, Filename: "a.pdf",
		Status: "processing", Message: "ocr", UpdatedAt: now,
	}
	mobileDocuments.uploads["up_other"] = mobileDocumentUploadRecord{
		TaskID: "up_other", OwnerID: "someone-else", Filename: "x.pdf",
		Status: "processing", UpdatedAt: now,
	}
	mobileDocuments.exports["exp_job_1"] = mobileDocumentExportRecord{
		JobID: "exp_job_1", OwnerID: ownerID, TenantID: enroll.TenantID, Format: "pdf",
		Status: "ready", CreatedAt: now.Add(-time.Minute),
	}
	mobileDocuments.Unlock()

	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks["emp_job_1"] = mobileDigitalEmployeeTaskRecord{
		TaskID: "emp_job_1", OwnerID: ownerID, TenantID: enroll.TenantID, EmployeeID: "e1",
		Prompt: "整理本周周报", Status: "running", UpdatedAt: now.Add(-time.Second),
	}
	mobileDigitalEmployeeTasks.Unlock()

	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.uploads, "up_job_1")
		delete(mobileDocuments.uploads, "up_other")
		delete(mobileDocuments.exports, "exp_job_1")
		mobileDocuments.Unlock()
		mobileDigitalEmployeeTasks.Lock()
		delete(mobileDigitalEmployeeTasks.tasks, "emp_job_1")
		mobileDigitalEmployeeTasks.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileJobsHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	count, _ := body["count"].(float64)
	if count < 3 {
		t.Fatalf("expected >=3 jobs, body=%#v", body)
	}
	jobs, _ := body["jobs"].([]any)
	kinds := map[string]bool{}
	for _, raw := range jobs {
		item, _ := raw.(map[string]any)
		kinds[item["kind"].(string)] = true
		if item["job_id"] == "up_other" {
			t.Fatalf("must not leak other owner job: %#v", item)
		}
	}
	for _, want := range []string{"document_upload", "document_export", "digital_employee"} {
		if !kinds[want] {
			t.Fatalf("missing kind %s in %#v", want, body)
		}
	}
	active, _ := body["active_count"].(float64)
	if active < 1 {
		t.Fatalf("expected active jobs, body=%#v", body)
	}
}

func TestMobileDocumentJobCollectorsKeepTenantsIsolated(t *testing.T) {
	ownerID := "document-job-shared-owner"
	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-job-tenant-a"] = mobileDocumentUploadRecord{TaskID: "upload-job-tenant-a", OwnerID: ownerID, TenantID: "tenant-a", Status: "queued", UpdatedAt: now}
	mobileDocuments.uploads["upload-job-tenant-b"] = mobileDocumentUploadRecord{TaskID: "upload-job-tenant-b", OwnerID: ownerID, TenantID: "tenant-b", Status: "queued", UpdatedAt: now}
	mobileDocuments.exports["export-job-tenant-a"] = mobileDocumentExportRecord{JobID: "export-job-tenant-a", OwnerID: ownerID, TenantID: "tenant-a", Format: "pdf", Status: "ready", CreatedAt: now}
	mobileDocuments.exports["export-job-tenant-b"] = mobileDocumentExportRecord{JobID: "export-job-tenant-b", OwnerID: ownerID, TenantID: "tenant-b", Format: "pdf", Status: "ready", CreatedAt: now}
	mobileDocuments.Unlock()
	mobileDocumentProcessJobs.Lock()
	mobileDocumentProcessJobs.jobs["process-job-tenant-a"] = mobileDocumentProcessJobRecord{JobID: "process-job-tenant-a", OwnerID: ownerID, TenantID: "tenant-a", Status: mobileDocProcessStatusQueued, CreatedAt: now, UpdatedAt: now}
	mobileDocumentProcessJobs.jobs["process-job-tenant-b"] = mobileDocumentProcessJobRecord{JobID: "process-job-tenant-b", OwnerID: ownerID, TenantID: "tenant-b", Status: mobileDocProcessStatusQueued, CreatedAt: now, UpdatedAt: now}
	mobileDocumentProcessJobs.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.uploads, "upload-job-tenant-a")
		delete(mobileDocuments.uploads, "upload-job-tenant-b")
		delete(mobileDocuments.exports, "export-job-tenant-a")
		delete(mobileDocuments.exports, "export-job-tenant-b")
		mobileDocuments.Unlock()
		mobileDocumentProcessJobs.Lock()
		delete(mobileDocumentProcessJobs.jobs, "process-job-tenant-a")
		delete(mobileDocumentProcessJobs.jobs, "process-job-tenant-b")
		mobileDocumentProcessJobs.Unlock()
	})

	for name, jobs := range map[string][]mobileJobItem{
		"uploads":   mobileCollectDocumentUploadJobs(ownerID, "tenant-a"),
		"exports":   mobileCollectDocumentExportJobs(ownerID, "tenant-a"),
		"processes": mobileCollectDocumentProcessJobs(ownerID, "tenant-a"),
	} {
		if len(jobs) != 1 || !strings.HasSuffix(jobs[0].JobID, "tenant-a") {
			t.Fatalf("%s tenant isolation failed: %#v", name, jobs)
		}
	}
}

func TestMobileOperationalJobCollectorsKeepTenantsIsolated(t *testing.T) {
	ownerID := "operational-job-shared-owner"
	now := time.Now().UTC()
	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks["employee-job-tenant-a"] = mobileDigitalEmployeeTaskRecord{TaskID: "employee-job-tenant-a", OwnerID: ownerID, TenantID: "tenant-a", Status: "queued", CreatedAt: now, UpdatedAt: now}
	mobileDigitalEmployeeTasks.tasks["employee-job-tenant-b"] = mobileDigitalEmployeeTaskRecord{TaskID: "employee-job-tenant-b", OwnerID: ownerID, TenantID: "tenant-b", Status: "queued", CreatedAt: now, UpdatedAt: now}
	mobileDigitalEmployeeTasks.Unlock()
	mobileBackendSSHTasks.Lock()
	mobileBackendSSHTasks.tasks["ssh-job-tenant-a"] = mobileBackendSSHTaskRecord{TaskID: "ssh-job-tenant-a", OwnerID: ownerID, TenantID: "tenant-a", Status: "queued", CreatedAt: now, UpdatedAt: now}
	mobileBackendSSHTasks.tasks["ssh-job-tenant-b"] = mobileBackendSSHTaskRecord{TaskID: "ssh-job-tenant-b", OwnerID: ownerID, TenantID: "tenant-b", Status: "queued", CreatedAt: now, UpdatedAt: now}
	mobileBackendSSHTasks.Unlock()
	mobileBackendSSHFileOperations.Lock()
	mobileBackendSSHFileOperations.operations["ssh-file-tenant-a"] = mobileBackendSSHFileOperationRecord{OperationID: "ssh-file-tenant-a", OwnerID: ownerID, TenantID: "tenant-a", Status: "queued", CreatedAt: now, UpdatedAt: now}
	mobileBackendSSHFileOperations.operations["ssh-file-tenant-b"] = mobileBackendSSHFileOperationRecord{OperationID: "ssh-file-tenant-b", OwnerID: ownerID, TenantID: "tenant-b", Status: "queued", CreatedAt: now, UpdatedAt: now}
	mobileBackendSSHFileOperations.Unlock()
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions["ssh-session-tenant-a"] = mobileBackendSSHSessionRecord{SessionID: "ssh-session-tenant-a", OwnerID: ownerID, TenantID: "tenant-a", Status: "ready", CreatedAt: now, UpdatedAt: now}
	mobileBackendSSHSessions.sessions["ssh-session-tenant-b"] = mobileBackendSSHSessionRecord{SessionID: "ssh-session-tenant-b", OwnerID: ownerID, TenantID: "tenant-b", Status: "ready", CreatedAt: now, UpdatedAt: now}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileDigitalEmployeeTasks.Lock()
		delete(mobileDigitalEmployeeTasks.tasks, "employee-job-tenant-a")
		delete(mobileDigitalEmployeeTasks.tasks, "employee-job-tenant-b")
		mobileDigitalEmployeeTasks.Unlock()
		mobileBackendSSHTasks.Lock()
		delete(mobileBackendSSHTasks.tasks, "ssh-job-tenant-a")
		delete(mobileBackendSSHTasks.tasks, "ssh-job-tenant-b")
		mobileBackendSSHTasks.Unlock()
		mobileBackendSSHFileOperations.Lock()
		delete(mobileBackendSSHFileOperations.operations, "ssh-file-tenant-a")
		delete(mobileBackendSSHFileOperations.operations, "ssh-file-tenant-b")
		mobileBackendSSHFileOperations.Unlock()
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, "ssh-session-tenant-a")
		delete(mobileBackendSSHSessions.sessions, "ssh-session-tenant-b")
		mobileBackendSSHSessions.Unlock()
	})

	for name, jobs := range map[string][]mobileJobItem{
		"employees": mobileCollectDigitalEmployeeJobs(ownerID, "tenant-a"),
		"ssh":       mobileCollectBackendSSHJobs(ownerID, "tenant-a"),
	} {
		if len(jobs) == 0 {
			t.Fatalf("%s jobs unexpectedly empty", name)
		}
		for _, job := range jobs {
			if strings.HasSuffix(job.JobID, "tenant-b") {
				t.Fatalf("%s leaked another tenant job: %#v", name, jobs)
			}
		}
	}
}

func TestMobileJobIsActive(t *testing.T) {
	if !mobileJobIsActive("running") || !mobileJobIsActive("processing") {
		t.Fatal("running/processing should be active")
	}
	if mobileJobIsActive("ready") || mobileJobIsActive("failed") {
		t.Fatal("terminal statuses should not be active")
	}
	if !mobileJobIsActive("kill_requested") {
		t.Fatal("kill_requested still active")
	}
}

func TestMobileCollectBackendSSHJobsIncludesOpenSessions(t *testing.T) {
	owner := "job-owner-ssh-sess"
	sessionID := "mobssh_job_sess_1"
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID: sessionID, OwnerID: owner, TenantID: "t1",
		ServerProfileID: "edge", ExecMode: mobileSSHExecHub,
		Status: "ready", State: "hub_connected", Message: "live",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
	})
	jobs := mobileCollectBackendSSHJobs(owner, "t1")
	found := false
	for _, j := range jobs {
		if j.JobID == sessionID && j.Kind == "ssh_session" {
			found = true
			if j.DeepLink != "/servers" {
				t.Fatalf("deep_link=%s", j.DeepLink)
			}
		}
	}
	if !found {
		t.Fatalf("expected open hub_exec session in jobs, got %#v", jobs)
	}
}
