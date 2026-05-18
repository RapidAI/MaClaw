package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAsyncImportSkillJobLifecycle(t *testing.T) {
	svc, principal, token, server := newAsyncSkillTestServer(t)
	_ = svc

	archive := buildTestSkillArchive(t, map[string]string{
		"demo-skill/skill.yaml": "name: demo-skill\ndescription: demo skill\nstatus: active\ntype: knowledge\ncontent: hello from async import\n",
	})
	body := bytes.NewBufferString(`{"zip_base64":"` + base64.StdEncoding.EncodeToString(archive) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/import?async=true", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("async import status = %d body = %s", w.Code, w.Body.String())
	}
	var job asyncJobView
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatalf("decode async job: %v", err)
	}
	if job.ID == "" || job.Kind != "skill.import" {
		t.Fatalf("unexpected job payload: %#v", job)
	}

	final := waitForAsyncJob(t, server, token, job.ID)
	if final.Status != asyncJobStatusSucceeded {
		t.Fatalf("expected succeeded job, got %#v", final)
	}
	var result struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("decode job result: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("unexpected job result items: %#v", result)
	}
	items, err := svc.ListSkills(context.Background(), principal)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(items) != 1 || items[0].Name != "demo-skill" {
		t.Fatalf("unexpected installed skills: %#v", items)
	}
}

func TestAsyncImportSkillJobFailure(t *testing.T) {
	_, _, token, server := newAsyncSkillTestServer(t)
	body := bytes.NewBufferString(`{"zip_base64":"not-base64"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/import?async=true", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("async import failure status = %d body = %s", w.Code, w.Body.String())
	}
	var job asyncJobView
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatalf("decode async failure job: %v", err)
	}
	final := waitForAsyncJob(t, server, token, job.ID)
	if final.Status != asyncJobStatusFailed || final.Error == "" {
		t.Fatalf("expected failed job, got %#v", final)
	}
}

func TestAsyncJobRedactsResultAndError(t *testing.T) {
	dataRoot := t.TempDir()
	m := newAsyncJobManager(dataRoot)
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	jsonEscapedRoot, _ := json.Marshal(dataRoot)
	job := m.createUserJob("test.success", p, func(context.Context) (any, error) {
		return map[string]any{
			"message":      "token=secret-token path=" + dataRoot,
			"escaped_path": string(jsonEscapedRoot),
			"endpoint_url": "https://user:pass@example.test/api?api_key=url-secret&trace=ok",
			"api_secret":   "plain-secret-value",
			"nested": map[string]string{
				"auth_header": "Bearer nested-secret-value",
			},
			"author": "visible-author",
		}, nil
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := m.getUserJob(job.ID, p)
		if !ok {
			t.Fatalf("job not found")
		}
		if got.Status == asyncJobStatusSucceeded {
			text := string(got.Result)
			if strings.Contains(text, "secret-token") || strings.Contains(text, "url-secret") || strings.Contains(text, "plain-secret-value") || strings.Contains(text, "nested-secret-value") || strings.Contains(text, "user:pass") || strings.Contains(text, dataRoot) || strings.Contains(text, filepath.ToSlash(dataRoot)) || strings.Contains(text, string(jsonEscapedRoot)) {
				t.Fatalf("expected redacted async result, got %s", text)
			}
			if !strings.Contains(text, "trace=ok") {
				t.Fatalf("expected benign URL query to remain visible, got %s", text)
			}
			if !strings.Contains(text, "visible-author") {
				t.Fatalf("expected benign field to remain visible, got %s", text)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	failed := m.createUserJob("test.failure", p, func(context.Context) (any, error) {
		return nil, errors.New("api_secret=secret-value path=" + dataRoot)
	})
	for time.Now().Before(deadline) {
		got, ok := m.getUserJob(failed.ID, p)
		if !ok {
			t.Fatalf("failed job not found")
		}
		if got.Status == asyncJobStatusFailed {
			if strings.Contains(got.Error, "secret-value") || strings.Contains(got.Error, dataRoot) || strings.Contains(got.Error, filepath.ToSlash(dataRoot)) {
				t.Fatalf("expected redacted async error, got %q", got.Error)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for async redaction jobs")
}

func TestAsyncJobSnapshotRedactsLegacyPersistedPayload(t *testing.T) {
	dataRoot := t.TempDir()
	m := newAsyncJobManager(dataRoot)
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	outsidePath := filepath.Join(filepath.Dir(dataRoot), "outside-secret", "legacy-job.md")
	payload, err := json.Marshal(map[string]any{"message": "token=legacy-token path=" + dataRoot, "file_path": outsidePath, "nested": map[string]string{"root_path": outsidePath}})
	if err != nil {
		t.Fatalf("marshal legacy payload: %v", err)
	}
	job := &asyncJobRecord{asyncJobView: asyncJobView{ID: "job_legacy", Kind: "legacy", Status: asyncJobStatusSucceeded, TenantID: p.TenantID, UserID: p.UserID, Result: payload, Error: "api_secret=legacy-secret path=" + dataRoot}}
	got := m.snapshot(job)
	text := string(got.Result) + " " + got.Error
	for _, leaked := range []string{"legacy-token", "legacy-secret", dataRoot, filepath.ToSlash(dataRoot), filepath.Dir(outsidePath), filepath.ToSlash(filepath.Dir(outsidePath))} {
		if strings.Contains(text, leaked) {
			t.Fatalf("expected legacy snapshot redaction for %q, got %s", leaked, text)
		}
	}
	if !strings.Contains(string(got.Result), filepath.Base(outsidePath)) {
		t.Fatalf("expected path basename to remain for diagnostics, got %s", got.Result)
	}
}

func newAsyncSkillTestServer(t *testing.T) (*agentservice.Service, agentservice.Principal, string, *HTTPServer) {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	return svc, principal, token, NewHTTPServer(svc, "admin-secret", nil)
}

func waitForAsyncJob(t *testing.T, server *HTTPServer, token, jobID string) asyncJobView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get job status = %d body = %s", w.Code, w.Body.String())
		}
		var job asyncJobView
		if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
			t.Fatalf("decode job poll: %v", err)
		}
		if job.Status == asyncJobStatusSucceeded || job.Status == asyncJobStatusFailed {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job %s", jobID)
	return asyncJobView{}
}

func buildTestSkillArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestDeleteAsyncJobsEndpointBulkDelete(t *testing.T) {
	_, principal, token, server := newAsyncSkillTestServer(t)
	job1 := server.jobs.createUserJob("skill.import", principal, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "one"}, nil
	})
	time.Sleep(10 * time.Millisecond)
	job2 := server.jobs.createUserJob("mcp.start", principal, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "two"}, nil
	})
	final1 := waitForAsyncJob(t, server, token, job1.ID)
	_ = waitForAsyncJob(t, server, token, job2.ID)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs?status=succeeded&before="+final1.CreatedAt.Add(time.Millisecond).Format(time.RFC3339Nano), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk delete status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Status  string         `json:"status"`
		Deleted int            `json:"deleted"`
		Items   []asyncJobView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode bulk delete: %v", err)
	}
	if out.Status != "deleted" || out.Deleted != 1 || len(out.Items) != 1 || out.Items[0].ID != job1.ID {
		t.Fatalf("unexpected bulk delete payload: %#v", out)
	}
	if _, ok := server.jobs.getUserJob(job1.ID, principal); ok {
		t.Fatalf("expected first job to be deleted")
	}
	if _, ok := server.jobs.getUserJob(job2.ID, principal); !ok {
		t.Fatalf("expected second job to remain")
	}
}

func TestDeleteAsyncJobsEndpointRequiresFilterOrAll(t *testing.T) {
	_, _, token, server := newAsyncSkillTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body = %s", w.Code, w.Body.String())
	}
}
