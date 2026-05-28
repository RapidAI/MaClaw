package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestPublicKnowledgeLibraryCreatesReadableScope(t *testing.T) {
	ctx := t.Context()
	access := newKnowledgeAccessService(newFileKVStore(t.TempDir() + "/knowledge_access.json"))
	library, err := access.CreatePublicLibrary(ctx, "tenant-a", "Tax Docs")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	if library.OwnerID == "" || library.TenantID != "tenant-a" || library.Name != "Tax Docs" {
		t.Fatalf("unexpected library: %#v", library)
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	scopes := access.ResolveForUser(ctx, "tenant-a", "user-a")
	if !hasPublicKnowledgeScope(scopes, library.TenantID, library.OwnerID) {
		t.Fatalf("expected public library scope in resolved access: %#v", scopes)
	}
}

func TestPublicKnowledgeLibraryEnsureIsIdempotent(t *testing.T) {
	ctx := t.Context()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	first, created, err := access.EnsurePublicLibrary(ctx, "tenant-a", "Ops Docs")
	if err != nil || !created {
		t.Fatalf("EnsurePublicLibrary first created=%v err=%v", created, err)
	}
	second, created, err := access.EnsurePublicLibrary(ctx, "tenant-a", "ops docs")
	if err != nil || created {
		t.Fatalf("EnsurePublicLibrary second created=%v err=%v", created, err)
	}
	if second.ID != first.ID || second.OwnerID != first.OwnerID {
		t.Fatalf("expected duplicate name to return existing library: first=%#v second=%#v", first, second)
	}
	libraries, err := access.ListPublicLibraries(ctx)
	if err != nil {
		t.Fatalf("ListPublicLibraries: %v", err)
	}
	if len(libraries) != 1 {
		t.Fatalf("expected one public library after idempotent ensure, got %#v", libraries)
	}
}

func TestPublicKnowledgeLibraryEnsureConcurrentUnique(t *testing.T) {
	ctx := t.Context()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	const workers = 24
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			library, _, err := access.EnsurePublicLibrary(ctx, "tenant-a", "Ops Docs")
			if err != nil {
				errs <- err
				return
			}
			ids <- library.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("EnsurePublicLibrary concurrent: %v", err)
		}
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
			continue
		}
		if id != first {
			t.Fatalf("expected concurrent ensure to return same library ID, got %q and %q", first, id)
		}
	}
	libraries, err := access.ListPublicLibraries(ctx)
	if err != nil {
		t.Fatalf("ListPublicLibraries: %v", err)
	}
	if len(libraries) != 1 || libraries[0].ID != first {
		t.Fatalf("expected one public library after concurrent ensure, got %#v", libraries)
	}
}

func TestPublicKnowledgeLibraryViewsIncludeSourceCounts(t *testing.T) {
	ctx := t.Context()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, "tenant-a", "Ops Docs")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	if _, err := store.SaveText(ctx, knowledge.TextSaveRequest{TenantID: library.TenantID, OwnerID: library.OwnerID, Title: "Runbook", Text: "public runbook"}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	server := &HTTPServer{knowledgeMgr: &knowledgeStoreManager{store: store, access: access}}
	views, err := server.publicKnowledgeLibraryViews(ctx, []publicKnowledgeLibrary{library})
	if err != nil {
		t.Fatalf("publicKnowledgeLibraryViews: %v", err)
	}
	if len(views) != 1 || views[0].SourceCount != 1 || views[0].DistilledSources == 0 || views[0].LatestSourceAt == nil {
		t.Fatalf("unexpected view: %#v", views)
	}
}

func TestPublicKnowledgeLibraryDeleteRemovesUserScopes(t *testing.T) {
	ctx := t.Context()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, "tenant-a", "Ops Docs")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	if _, ok, removed, err := access.DeletePublicLibrary(ctx, library.ID); err != nil || !ok || removed != 1 {
		t.Fatalf("DeletePublicLibrary ok=%v err=%v", ok, err)
	}
	scopes := access.ResolveForUser(ctx, "tenant-a", "user-a")
	if hasPublicKnowledgeScope(scopes, library.TenantID, library.OwnerID) {
		t.Fatalf("expected deleted public library scope removed: %#v", scopes)
	}
}

func TestKnowledgeAccessRejectsUnknownPublicLibraryScope(t *testing.T) {
	ctx := t.Context()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: "tenant-public", OwnerID: "public:missing", Name: "Missing"}}})
	if err == nil || !strings.Contains(err.Error(), "unknown public knowledge library") {
		t.Fatalf("expected unknown public library scope error, got %v", err)
	}
}

func TestKnowledgeAccessResolveDropsUnregisteredPublicScope(t *testing.T) {
	ctx := t.Context()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	scopes := access.filterRegisteredPublicKnowledgeScopes(ctx, []knowledgeScope{
		{TenantID: "tenant-a", OwnerID: "user-a", Name: "self"},
		{TenantID: "tenant-public", OwnerID: "public:missing", Name: "stale"},
	})
	if hasPublicKnowledgeScope(scopes, "tenant-public", "public:missing") {
		t.Fatalf("expected unregistered public scope to be dropped: %#v", scopes)
	}
	if !hasPublicKnowledgeScope(scopes, "tenant-a", "user-a") {
		t.Fatalf("expected non-public scope to remain: %#v", scopes)
	}
}

func TestAdminPublicKnowledgeCreateDeleteAndImportAudit(t *testing.T) {
	ctx := t.Context()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: access})

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/public-knowledge-libraries", strings.NewReader(`{"tenant_id":"`+tenant.ID+`","name":"Ops Docs"}`))
	createReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create public library status = %d body = %s", createResp.Code, createResp.Body.String())
	}
	var library publicKnowledgeLibrary
	if err := json.Unmarshal(createResp.Body.Bytes(), &library); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/public-knowledge-libraries/"+library.ID+"/import/text", strings.NewReader(`{"text":"ops runbook"}`))
	importReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	importReq.Header.Set("Content-Type", "application/json")
	importResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(importResp, importReq)
	if importResp.Code != http.StatusCreated {
		t.Fatalf("import text status = %d body = %s", importResp.Code, importResp.Body.String())
	}
	var importOut struct {
		Status   string `json:"status"`
		SourceID string `json:"source_id"`
		Kind     string `json:"kind"`
	}
	if err := json.Unmarshal(importResp.Body.Bytes(), &importOut); err != nil {
		t.Fatalf("decode import text response: %v", err)
	}
	if importOut.Status != "completed" || importOut.SourceID == "" || importOut.Kind == "" {
		t.Fatalf("unexpected import text response: %#v", importOut)
	}
	if err := access.SetUser(ctx, "tenant-a", "user-a", &knowledgeAccessConfig{Enabled: true, ReadScopes: []knowledgeScope{{TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name}}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/public-knowledge-libraries/"+library.ID, nil)
	deleteReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	deleteResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete public library status = %d body = %s", deleteResp.Code, deleteResp.Body.String())
	}
	var deleted struct {
		DeletedSources int `json:"deleted_sources"`
		RemovedScopes  int `json:"removed_scopes"`
	}
	if err := json.Unmarshal(deleteResp.Body.Bytes(), &deleted); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deleted.DeletedSources != 1 || deleted.RemovedScopes != 1 {
		t.Fatalf("unexpected delete response: %#v", deleted)
	}

	for _, action := range []string{"admin.public_knowledge_library_created", "admin.public_knowledge_import_text", "admin.public_knowledge_library_deleted"} {
		events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: action})
		if err != nil {
			t.Fatalf("ListAuditEvents %s: %v", action, err)
		}
		if len(events) != 1 || events[0].ResourceID != library.ID || events[0].Metadata["library_id"] != library.ID {
			t.Fatalf("unexpected audit events for %s: %#v", action, events)
		}
	}
}

func TestAdminPublicKnowledgeCreateDuplicateIsIdempotent(t *testing.T) {
	ctx := t.Context()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})

	create := func(name string) (int, publicKnowledgeLibrary) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/public-knowledge-libraries", strings.NewReader(`{"tenant_id":"`+tenant.ID+`","name":"`+name+`"}`))
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		var library publicKnowledgeLibrary
		if err := json.Unmarshal(w.Body.Bytes(), &library); err != nil {
			t.Fatalf("decode create response: %v body=%s", err, w.Body.String())
		}
		return w.Code, library
	}
	firstStatus, first := create("Ops Docs")
	secondStatus, second := create("ops docs")
	if firstStatus != http.StatusCreated || secondStatus != http.StatusOK || first.ID != second.ID {
		t.Fatalf("unexpected duplicate create result: first=%d %#v second=%d %#v", firstStatus, first, secondStatus, second)
	}
	events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.public_knowledge_library_created"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected only first create to write audit event, got %#v", events)
	}
}

func TestAdminPublicKnowledgeImportFileAcceptsMultipleFiles(t *testing.T) {
	ctx := t.Context()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, tenant.ID, "Ops Docs")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: access})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, text := range map[string]string{"one.txt": "first public knowledge", "two.txt": "second public knowledge"} {
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write([]byte(text)); err != nil {
			t.Fatalf("write multipart file: %v", err)
		}
	}
	if err := writer.WriteField("topic_hint", "ops"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/public-knowledge-libraries/"+library.ID+"/import/file", &body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("multi file import status = %d body = %s", w.Code, w.Body.String())
	}
	var accepted struct {
		JobID     string   `json:"job_id"`
		FileCount int      `json:"file_count"`
		Filenames []string `json:"filenames"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if accepted.JobID == "" || accepted.FileCount != 2 || len(accepted.Filenames) != 2 {
		t.Fatalf("unexpected import response: %#v", accepted)
	}
	waitForAdminJobSuccess(t, server, accepted.JobID)
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: library.TenantID, OwnerID: library.OwnerID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected two public sources from multi file import, got %#v", sources)
	}
}

func TestUserKnowledgeImportFileAcceptsMultipleFiles(t *testing.T) {
	ctx := t.Context()
	secret := "test-token-secret-0123456789012345"
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: secret}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})
	token, _, err := agentservice.NewTokenManager(secret, time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, text := range map[string]string{"alpha.txt": "alpha user knowledge", "beta.txt": "beta user knowledge"} {
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write([]byte(text)); err != nil {
			t.Fatalf("write multipart file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/import/file", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("user multi file import status = %d body = %s", w.Code, w.Body.String())
	}
	var accepted struct {
		JobID     string   `json:"job_id"`
		FileCount int      `json:"file_count"`
		Filenames []string `json:"filenames"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if accepted.JobID == "" || accepted.FileCount != 2 || len(accepted.Filenames) != 2 {
		t.Fatalf("unexpected import response: %#v", accepted)
	}
	waitForAdminJobSuccess(t, server, accepted.JobID)
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{TenantID: tenant.ID, OwnerID: user.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected two user sources from multi file import, got %#v", sources)
	}
}

func TestKnowledgeMultipartUploadLimitsFilesAndPerFileSize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    map[string]string
		maxBytes int64
		maxFiles int
		wantErr  string
	}{
		{name: "too many files", files: map[string]string{"one.txt": "1", "two.txt": "2"}, maxBytes: 1024, maxFiles: 1, wantErr: "too many files"},
		{name: "file too large", files: map[string]string{"big.txt": strings.Repeat("x", 12)}, maxBytes: 8, maxFiles: 2, wantErr: "file too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for name, text := range tc.files {
				part, err := writer.CreateFormFile("file", name)
				if err != nil {
					t.Fatalf("CreateFormFile: %v", err)
				}
				if _, err := part.Write([]byte(text)); err != nil {
					t.Fatalf("write multipart file: %v", err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("multipart close: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/upload", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err := req.ParseMultipartForm(1024); err != nil {
				t.Fatalf("ParseMultipartForm: %v", err)
			}
			_, err := saveKnowledgeMultipartFiles(req, t.TempDir(), "limit-*", tc.maxBytes, tc.maxFiles)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
		})
	}
}

func waitForAdminJobSuccess(t *testing.T, server *HTTPServer, jobID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := server.jobs.getAnyJob(jobID)
		if ok && job.Status == asyncJobStatusSucceeded {
			return
		}
		if ok && (job.Status == asyncJobStatusFailed || job.Status == asyncJobStatusCanceled) {
			t.Fatalf("job %s ended with %s: %s", jobID, job.Status, job.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish in time", jobID)
}

func TestAdminPublicKnowledgeCreateRequiresExistingTenant(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{store: store, access: newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/public-knowledge-libraries", strings.NewReader(`{"tenant_id":"missing","name":"Ops Docs"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("create public library with missing tenant status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminPublicKnowledgeAccessAttachDetach(t *testing.T) {
	ctx := t.Context()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, tenant.ID, "Ops Docs")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: access})

	attachReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/"+user.ID+"/public-libraries/"+library.ID, nil)
	attachReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	attachResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(attachResp, attachReq)
	if attachResp.Code != http.StatusOK {
		t.Fatalf("attach public library status = %d body = %s", attachResp.Code, attachResp.Body.String())
	}
	var attached knowledgeAccessConfig
	if err := json.Unmarshal(attachResp.Body.Bytes(), &attached); err != nil {
		t.Fatalf("decode attach response: %v", err)
	}
	if !attached.Enabled || !hasPublicKnowledgeScope(attached.ReadScopes, library.TenantID, library.OwnerID) {
		t.Fatalf("expected attached public scope: %#v", attached)
	}

	attachAgainReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/"+user.ID+"/public-libraries/"+library.ID, nil)
	attachAgainReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	attachAgainResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(attachAgainResp, attachAgainReq)
	if attachAgainResp.Code != http.StatusOK {
		t.Fatalf("attach public library again status = %d body = %s", attachAgainResp.Code, attachAgainResp.Body.String())
	}
	var attachedAgain knowledgeAccessConfig
	if err := json.Unmarshal(attachAgainResp.Body.Bytes(), &attachedAgain); err != nil {
		t.Fatalf("decode second attach response: %v", err)
	}
	if countPublicKnowledgeScope(attachedAgain.ReadScopes, library.TenantID, library.OwnerID) != 1 {
		t.Fatalf("expected public scope to remain unique: %#v", attachedAgain)
	}

	detachReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/"+user.ID+"/public-libraries/"+library.ID, nil)
	detachReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	detachResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(detachResp, detachReq)
	if detachResp.Code != http.StatusOK {
		t.Fatalf("detach public library status = %d body = %s", detachResp.Code, detachResp.Body.String())
	}
	var detached knowledgeAccessConfig
	if err := json.Unmarshal(detachResp.Body.Bytes(), &detached); err != nil {
		t.Fatalf("decode detach response: %v", err)
	}
	if hasPublicKnowledgeScope(detached.ReadScopes, library.TenantID, library.OwnerID) {
		t.Fatalf("expected public scope removed: %#v", detached)
	}

	attachEvents, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_access_public_library_attached"})
	if err != nil {
		t.Fatalf("ListAuditEvents attach: %v", err)
	}
	detachEvents, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.knowledge_access_public_library_detached"})
	if err != nil {
		t.Fatalf("ListAuditEvents detach: %v", err)
	}
	if len(attachEvents) != 2 || len(detachEvents) != 1 || attachEvents[0].Metadata["library_id"] != library.ID || detachEvents[0].Metadata["library_id"] != library.ID {
		t.Fatalf("unexpected public knowledge access audit events: attach=%#v detach=%#v", attachEvents, detachEvents)
	}
}

func TestAdminPublicKnowledgeAccessAttachRequiresExistingUser(t *testing.T) {
	ctx := t.Context()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, tenant.ID, "Ops Docs")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: access})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/missing/public-libraries/"+library.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("attach public library with missing user status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminPublicKnowledgeAccessAttachCrossTenantWithoutGlobalSwitch(t *testing.T) {
	ctx := t.Context()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateTenant target: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser target: %v", err)
	}
	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(t.TempDir(), "knowledge_access.json")))
	library, err := access.CreatePublicLibrary(ctx, "tenant-public", "Policy Docs")
	if err != nil {
		t.Fatalf("CreatePublicLibrary: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", &knowledgeStoreManager{access: access})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/knowledge-access/tenants/"+tenant.ID+"/users/"+user.ID+"/public-libraries/"+library.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cross-tenant public attach status = %d body = %s", w.Code, w.Body.String())
	}
	resolved := access.ResolveForUser(ctx, tenant.ID, user.ID)
	if !hasPublicKnowledgeScope(resolved, library.TenantID, library.OwnerID) {
		t.Fatalf("expected cross-tenant public scope to resolve without global switch: %#v", resolved)
	}
	if !hasPublicKnowledgeScope(resolved, tenant.ID, user.ID) {
		t.Fatalf("expected own scope to remain resolved with public scope: %#v", resolved)
	}
}

func hasPublicKnowledgeScope(scopes []knowledgeScope, tenantID, ownerID string) bool {
	for _, scope := range scopes {
		if scope.TenantID == tenantID && scope.OwnerID == ownerID {
			return true
		}
	}
	return false
}

func countPublicKnowledgeScope(scopes []knowledgeScope, tenantID, ownerID string) int {
	count := 0
	for _, scope := range scopes {
		if scope.TenantID == tenantID && scope.OwnerID == ownerID {
			count++
		}
	}
	return count
}
