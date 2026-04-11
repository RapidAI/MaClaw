package compute

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newTestHandler creates a Handler backed by in-memory/temp components.
func newTestHandler(t *testing.T) (*Handler, *SyncManager, *SourceManager, *LocalStore) {
	t.Helper()
	dir := t.TempDir()
	localPath := filepath.Join(dir, "providers.json")

	syncMgr := NewSyncManager("", "", "")
	sourceMgr := NewSourceManager(syncMgr)
	localStore := NewLocalStore(localPath)
	h := NewHandler(syncMgr, sourceMgr, localStore)
	return h, syncMgr, sourceMgr, localStore
}

func setupMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	h.RegisterAdminRoutes(mux)
	return mux
}

func TestGetSource_Default(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	mux := setupMux(h)

	req := httptest.NewRequest(http.MethodGet, "/admin/compute/source", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["source"] != "cloud" {
		t.Errorf("source = %v, want cloud", body["source"])
	}
}

func TestSetSource_InvalidValue(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	mux := setupMux(h)

	payload := `{"source":"invalid"}`
	req := httptest.NewRequest(http.MethodPut, "/admin/compute/source", bytes.NewBufferString(payload))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestSetSource_LocalWithoutPermission(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	mux := setupMux(h)

	payload := `{"source":"local"}`
	req := httptest.NewRequest(http.MethodPut, "/admin/compute/source", bytes.NewBufferString(payload))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestGetProviders_EmptyCloud(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	mux := setupMux(h)

	req := httptest.NewRequest(http.MethodGet, "/admin/compute/providers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	providers, ok := body["providers"].([]any)
	if !ok {
		t.Fatal("providers not an array")
	}
	if len(providers) != 0 {
		t.Errorf("len(providers) = %d, want 0", len(providers))
	}
	if body["source"] != "cloud" {
		t.Errorf("source = %v, want cloud", body["source"])
	}
}

func TestSyncStatus_Pending(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	mux := setupMux(h)

	req := httptest.NewRequest(http.MethodGet, "/admin/compute/sync-status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "pending" {
		t.Errorf("status = %v, want pending", body["status"])
	}
}

func TestLocalProviders_CRUD(t *testing.T) {
	h, syncMgr, sourceMgr, _ := newTestHandler(t)
	mux := setupMux(h)

	// Grant permission and switch to local mode
	syncMgr.mu.Lock()
	syncMgr.computePermission = true
	syncMgr.mu.Unlock()
	if err := sourceMgr.SetSource("local"); err != nil {
		t.Fatalf("SetSource: %v", err)
	}

	// Create a local provider
	createPayload := `{"name":"test-provider","base_url":"https://api.example.com","protocol":"openai","model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/compute/local-providers", bytes.NewBufferString(createPayload))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body: %s", w.Code, w.Body.String())
	}

	// List local providers
	req = httptest.NewRequest(http.MethodGet, "/admin/compute/local-providers", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", w.Code)
	}
	var listBody struct {
		Providers []ComputeProvider `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listBody.Providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(listBody.Providers))
	}
	providerID := listBody.Providers[0].ID
	if listBody.Providers[0].Name != "test-provider" {
		t.Errorf("name = %q, want test-provider", listBody.Providers[0].Name)
	}

	// Update the provider
	updatePayload := `{"name":"updated-provider","base_url":"https://api2.example.com","protocol":"anthropic","model":"claude-3"}`
	req = httptest.NewRequest(http.MethodPut, "/admin/compute/local-providers/"+providerID, bytes.NewBufferString(updatePayload))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	// Delete the provider
	req = httptest.NewRequest(http.MethodDelete, "/admin/compute/local-providers/"+providerID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", w.Code)
	}

	// Verify empty after delete
	req = httptest.NewRequest(http.MethodGet, "/admin/compute/local-providers", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var afterDelete struct {
		Providers []ComputeProvider `json:"providers"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &afterDelete)
	if len(afterDelete.Providers) != 0 {
		t.Errorf("after delete: len(providers) = %d, want 0", len(afterDelete.Providers))
	}
}

func TestLocalProviders_ForbiddenInCloudMode(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	mux := setupMux(h)

	// Try to create in cloud mode (default)
	payload := `{"name":"test","base_url":"https://api.example.com","protocol":"openai"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/compute/local-providers", bytes.NewBufferString(payload))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	mux := setupMux(h)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodDelete, "/admin/compute/source"},
		{http.MethodPut, "/admin/compute/providers"},
		{http.MethodGet, "/admin/compute/sync"},
		{http.MethodPost, "/admin/compute/sync-status"},
		{http.MethodGet, "/admin/compute/test"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d, want 405", tt.method, tt.path, w.Code)
		}
	}
}

// Ensure temp dir cleanup works (no leaked files).
func TestLocalStore_TempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_providers.json")
	ls := NewLocalStore(path)
	_ = ls.SaveProvider(ComputeProvider{Name: "x", Protocol: "openai"})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
