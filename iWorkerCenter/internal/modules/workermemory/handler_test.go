package workermemory

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
