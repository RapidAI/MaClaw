package workermemory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestWorkerMemorySaveAndRecallIsOwnerIsolated(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	postMemory(t, mux, map[string]any{
		"worker_id": "worker-a",
		"content":   "Customer Alpha prefers weekly production risk summaries.",
		"category":  "project_knowledge",
		"tags":      []string{"customer-alpha"},
	})
	postMemory(t, mux, map[string]any{
		"worker_id": "worker-b",
		"content":   "Customer Beta prefers daily quality alerts.",
		"category":  "project_knowledge",
	})

	req := httptest.NewRequest(http.MethodGet, "/client/iworker/memories?worker_id=worker-a&query=production&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Memories []MemoryDTO `json:"memories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(resp.Memories) != 1 {
		t.Fatalf("memories len = %d, want 1: %+v", len(resp.Memories), resp.Memories)
	}
	if resp.Memories[0].WorkerID != "worker-a" || resp.Memories[0].Content == "" {
		t.Fatalf("unexpected memory: %+v", resp.Memories[0])
	}
}

func TestWorkerMemoryRequiresWorkerID(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	body, _ := json.Marshal(map[string]any{"content": "missing owner"})
	req := httptest.NewRequest(http.MethodPost, "/client/iworker/memories", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST status = %d, want 400", w.Code)
	}
}

func TestWorkerMemoryRejectsOversizedSaveBody(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	body := `{"tenant_id":"tenant-a","worker_id":"worker-a","content":"` + strings.Repeat("x", maxMemorySaveBodyBytes+1024)
	req := httptest.NewRequest(http.MethodPost, "/client/iworker/memories", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST status = %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/client/iworker/memories?tenant_id=tenant-a&worker_id=worker-a&query=x&limit=10", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Memories []MemoryDTO `json:"memories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(resp.Memories) != 0 {
		t.Fatalf("unexpected memories after oversized save: %+v", resp.Memories)
	}
}

func TestWorkerMemoryRejectsTrailingJSONWithoutSaving(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	body := `{"tenant_id":"tenant-a","worker_id":"worker-a","content":"valid-looking memory"} {"tenant_id":"tenant-a","worker_id":"worker-a","content":"extra"}`
	req := httptest.NewRequest(http.MethodPost, "/client/iworker/memories", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST status = %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/client/iworker/memories?tenant_id=tenant-a&worker_id=worker-a&query=memory&limit=10", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Memories []MemoryDTO `json:"memories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(resp.Memories) != 0 {
		t.Fatalf("unexpected memories after trailing JSON save: %+v", resp.Memories)
	}
}

func TestWorkerMemorySaveFailsWhenFlushFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")
	store, err := corememory.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove store dir: %v", err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("replace store dir with file: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	body, _ := json.Marshal(map[string]any{"tenant_id": "tenant-a", "worker_id": "worker-a", "content": "must be durable before success"})
	req := httptest.NewRequest(http.MethodPost, "/client/iworker/memories", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("POST status = %d body=%s, want flush failure", w.Code, w.Body.String())
	}
}

func postMemory(t *testing.T, mux http.Handler, payload map[string]any) MemoryDTO {
	t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/client/iworker/memories", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status = %d body=%s", w.Code, w.Body.String())
	}
	var dto MemoryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	return dto
}

func TestWorkerMemoryRecallsCompanyDepartmentAndPersonalScopes(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	postMemory(t, mux, map[string]any{
		"tenant_id": "tenant-a",
		"scope":     "company",
		"content":   "All teams use the Alpha escalation playbook.",
		"category":  "project_knowledge",
	})
	postMemory(t, mux, map[string]any{
		"tenant_id":     "tenant-a",
		"department_id": "quality",
		"scope":         "department",
		"content":       "Quality department reviews Alpha defects every Friday.",
		"category":      "project_knowledge",
	})
	postMemory(t, mux, map[string]any{
		"tenant_id": "tenant-a",
		"worker_id": "worker-a",
		"scope":     "personal",
		"content":   "Worker A prepares Alpha summaries for the morning standup.",
		"category":  "project_knowledge",
	})
	postMemory(t, mux, map[string]any{
		"tenant_id": "tenant-b",
		"scope":     "company",
		"content":   "Tenant B Alpha memory must not leak.",
		"category":  "project_knowledge",
	})

	req := httptest.NewRequest(http.MethodGet, "/client/iworker/memories?tenant_id=tenant-a&department_id=quality&worker_id=worker-a&query=Alpha&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Memories []MemoryDTO `json:"memories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(resp.Memories) != 3 {
		t.Fatalf("memories len = %d, want 3: %+v", len(resp.Memories), resp.Memories)
	}
	scopes := map[string]bool{}
	for _, memory := range resp.Memories {
		if memory.TenantID != "tenant-a" {
			t.Fatalf("unexpected tenant leak: %+v", memory)
		}
		scopes[memory.Scope] = true
	}
	for _, scope := range []string{"company", "department", "personal"} {
		if !scopes[scope] {
			t.Fatalf("missing scope %q in %+v", scope, resp.Memories)
		}
	}
}

func TestWorkerMemoryOrgUnitAliasIsVirtualDepartmentScope(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	dto := postMemory(t, mux, map[string]any{
		"tenant_id":   "tenant-a",
		"org_unit_id": "quality-domain",
		"scope":       "org_unit",
		"content":     "Quality capability domain reviews Alpha defects every Friday.",
		"category":    "project_knowledge",
	})
	if dto.OrgUnitID != "quality-domain" || dto.DepartmentID != "quality-domain" || dto.Scope != ScopeDepartment {
		t.Fatalf("unexpected virtual org-unit dto: %+v", dto)
	}

	req := httptest.NewRequest(http.MethodGet, "/client/iworker/memories?tenant_id=tenant-a&org_unit_id=quality-domain&worker_id=worker-a&query=Alpha&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Memories []MemoryDTO `json:"memories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(resp.Memories) != 1 || resp.Memories[0].OrgUnitID != "quality-domain" {
		t.Fatalf("unexpected org-unit recall: %+v", resp.Memories)
	}
}

func TestWorkerMemoryStatsCountsVisibleScopes(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	postMemory(t, mux, map[string]any{"tenant_id": "tenant-a", "scope": "company", "content": "Company memory", "category": "project_knowledge"})
	postMemory(t, mux, map[string]any{"tenant_id": "tenant-a", "department_id": "quality", "scope": "department", "content": "Department memory", "category": "instruction"})
	postMemory(t, mux, map[string]any{"tenant_id": "tenant-a", "worker_id": "worker-a", "scope": "personal", "content": "Personal memory", "category": "preference"})
	postMemory(t, mux, map[string]any{"tenant_id": "tenant-a", "worker_id": "worker-b", "scope": "personal", "content": "Other worker memory", "category": "preference"})
	postMemory(t, mux, map[string]any{"tenant_id": "tenant-b", "scope": "company", "content": "Other tenant memory", "category": "project_knowledge"})

	req := httptest.NewRequest(http.MethodGet, "/client/iworker/memory-stats?tenant_id=tenant-a&department_id=quality&worker_id=worker-a", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stats status = %d body=%s", w.Code, w.Body.String())
	}
	var stats MemoryStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if stats.Total != 3 {
		t.Fatalf("Total = %d, want 3: %+v", stats.Total, stats)
	}
	if stats.ByScope[ScopeCompany] != 1 || stats.ByScope[ScopeDepartment] != 1 || stats.ByScope[ScopePersonal] != 1 {
		t.Fatalf("ByScope = %+v, want one per visible scope", stats.ByScope)
	}
	if stats.ByCategory["preference"] != 1 {
		t.Fatalf("ByCategory = %+v, want only worker-a personal preference", stats.ByCategory)
	}
}

func TestWorkerMemoryReadsTenantFromHeader(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"worker_id": "worker-a",
		"scope":     "personal",
		"content":   "Header tenant memory",
		"category":  "project_knowledge",
	})
	postReq := httptest.NewRequest(http.MethodPost, "/client/iworker/memories", bytes.NewReader(body))
	postReq.Header.Set("X-Tenant-ID", "tenant-header")
	postRes := httptest.NewRecorder()
	mux.ServeHTTP(postRes, postReq)
	if postRes.Code != http.StatusCreated {
		t.Fatalf("POST status = %d body=%s", postRes.Code, postRes.Body.String())
	}
	var saved MemoryDTO
	if err := json.Unmarshal(postRes.Body.Bytes(), &saved); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if saved.TenantID != "tenant-header" {
		t.Fatalf("TenantID = %q, want tenant-header", saved.TenantID)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/client/iworker/memories?worker_id=worker-a&query=Header&limit=10", nil)
	getReq.Header.Set("X-Tenant-ID", "tenant-header")
	getRes := httptest.NewRecorder()
	mux.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", getRes.Code, getRes.Body.String())
	}
	var resp struct {
		Memories []MemoryDTO `json:"memories"`
	}
	if err := json.Unmarshal(getRes.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(resp.Memories) != 1 || resp.Memories[0].TenantID != "tenant-header" {
		t.Fatalf("unexpected memories: %+v", resp.Memories)
	}
}

func TestWorkerMemoryDeleteSupportsEscapedIDAndRequiresFlush(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	t.Cleanup(store.Stop)
	h := NewHandler(store)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)

	dto := postMemory(t, mux, map[string]any{
		"id":        "mem/team a",
		"tenant_id": "tenant-a",
		"worker_id": "worker-a",
		"content":   "delete escaped id",
	})
	if dto.ID != "mem/team a" {
		t.Fatalf("saved id = %q", dto.ID)
	}
	req := httptest.NewRequest(http.MethodDelete, "/client/iworker/memories/mem%2Fteam%20a?tenant_id=tenant-a&worker_id=worker-a", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/client/iworker/memories?tenant_id=tenant-a&worker_id=worker-a&query=escaped&limit=10", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Memories []MemoryDTO `json:"memories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(resp.Memories) != 0 {
		t.Fatalf("memory still visible after escaped delete: %+v", resp.Memories)
	}
}

func TestRecordCapabilityExecutionMemoryWritesPersonalAndDepartmentMemory(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	h := NewHandler(store)
	if err := h.RecordCapabilityExecutionMemory(context.Background(), "tenant-a", "worker-a", "quality", "cap-revenue", "Revenue forecast", "Monthly Ops", "success", "Forecast completed", ""); err != nil {
		t.Fatalf("record capability execution memory: %v", err)
	}

	personal := store.Search(corememory.CategoryTaskArtifact, "cap-revenue", 0)
	if len(personal) != 2 {
		t.Fatalf("memory count = %d, want 2", len(personal))
	}
	owners := map[string]bool{}
	for _, entry := range personal {
		owners[entry.OwnerID] = true
		if entry.SourceType != "iworkercenter.workflow" {
			t.Fatalf("SourceType = %q", entry.SourceType)
		}
	}
	if !owners["tenant:tenant-a:worker:worker-a"] || !owners["tenant:tenant-a:department:quality"] {
		t.Fatalf("owners = %+v", owners)
	}
}
