package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/compute"
	_ "modernc.org/sqlite"
)

func setupComputeTest(t *testing.T) (*ComputeHandler, *compute.ProviderStore) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i)
	}
	store := compute.NewProviderStore(db, encKey)
	if err := store.CreateTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewComputeHandler(store, nil), store
}

func createTestProvider(t *testing.T, store *compute.ProviderStore) *compute.ComputeProvider {
	t.Helper()
	p := &compute.ComputeProvider{
		Name:        "test-provider",
		BaseURL:     "https://api.example.com",
		APIKey:      "sk-secret-key-123",
		Protocol:    "openai",
		UserAgent:   "openclaw",
		ComputeType: "general",
		Model:       "gpt-4",
		Enabled:     true,
		Priority:    10,
		Description: "test provider",
	}
	if err := store.CreateProvider(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreateProvider(t *testing.T) {
	h, _ := setupComputeTest(t)

	body := `{"name":"new-provider","base_url":"https://api.openai.com","api_key":"sk-123","protocol":"openai","user_agent":"openclaw","compute_type":"general","model":"gpt-4","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compute/providers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.CreateProvider().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp compute.ComputeProvider
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.APIKey != "" {
		t.Error("api_key should be masked in response")
	}
	if !resp.HasAPIKey {
		t.Error("has_api_key should be true")
	}
	if resp.ID == "" {
		t.Error("id should be set")
	}
	if resp.Name != "new-provider" {
		t.Errorf("expected name new-provider, got %s", resp.Name)
	}
}

func TestCreateProviderValidationError(t *testing.T) {
	h, _ := setupComputeTest(t)

	// Invalid base_url (not HTTPS)
	body := `{"name":"bad","base_url":"http://api.example.com","protocol":"openai"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compute/providers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.CreateProvider().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListProviders(t *testing.T) {
	h, store := setupComputeTest(t)
	createTestProvider(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/compute/providers", nil)
	w := httptest.NewRecorder()
	h.ListProviders().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var providers []compute.ComputeProvider
	if err := json.NewDecoder(w.Body).Decode(&providers); err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].APIKey != "" {
		t.Error("api_key should be masked")
	}
	if !providers[0].HasAPIKey {
		t.Error("has_api_key should be true")
	}
}

func TestListProvidersEmpty(t *testing.T) {
	h, _ := setupComputeTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/compute/providers", nil)
	w := httptest.NewRecorder()
	h.ListProviders().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var providers []compute.ComputeProvider
	if err := json.NewDecoder(w.Body).Decode(&providers); err != nil {
		t.Fatal(err)
	}
	if providers == nil {
		t.Error("providers should be empty array, not null")
	}
}

func TestGetProvider(t *testing.T) {
	h, store := setupComputeTest(t)
	p := createTestProvider(t, store)

	// Use a mux to properly route path parameters
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/compute/providers/{id}", h.GetProvider())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/compute/providers/"+p.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp compute.ComputeProvider
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.APIKey != "" {
		t.Error("api_key should be masked")
	}
	if !resp.HasAPIKey {
		t.Error("has_api_key should be true")
	}
	if resp.Name != "test-provider" {
		t.Errorf("expected name test-provider, got %s", resp.Name)
	}
}

func TestGetProviderNotFound(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/compute/providers/{id}", h.GetProvider())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/compute/providers/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateProvider(t *testing.T) {
	h, store := setupComputeTest(t)
	p := createTestProvider(t, store)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/compute/providers/{id}", h.UpdateProvider())

	body := `{"name":"updated-name","base_url":"https://api.updated.com","protocol":"anthropic","user_agent":"claude-code/2.0.0","compute_type":"coding","model":"claude-3","enabled":false,"priority":20}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/compute/providers/"+p.ID, bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp compute.ComputeProvider
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Name != "updated-name" {
		t.Errorf("expected name updated-name, got %s", resp.Name)
	}
	if resp.APIKey != "" {
		t.Error("api_key should be masked")
	}
	// HasAPIKey should still be true since we didn't clear the key
	if !resp.HasAPIKey {
		t.Error("has_api_key should be true (key preserved)")
	}
}

func TestUpdateProviderNotFound(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/compute/providers/{id}", h.UpdateProvider())

	body := `{"name":"x","base_url":"https://api.example.com","protocol":"openai"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/compute/providers/nonexistent", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteProvider(t *testing.T) {
	h, store := setupComputeTest(t)
	p := createTestProvider(t, store)

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/admin/compute/providers/{id}", h.DeleteProvider())

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/compute/providers/"+p.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's gone
	got, err := store.GetProvider(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("provider should be deleted")
	}
}

func TestDeleteProviderNotFound(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/admin/compute/providers/{id}", h.DeleteProvider())

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/compute/providers/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestToggleProvider(t *testing.T) {
	h, store := setupComputeTest(t)
	p := createTestProvider(t, store)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/compute/providers/{id}/toggle", h.ToggleProvider())

	// Provider starts enabled=true, toggle should make it false
	req := httptest.NewRequest(http.MethodPost, "/api/admin/compute/providers/"+p.ID+"/toggle", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp compute.ComputeProvider
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Enabled {
		t.Error("expected enabled=false after toggle")
	}
	if resp.APIKey != "" {
		t.Error("api_key should be masked")
	}
}

func TestToggleProviderNotFound(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/compute/providers/{id}/toggle", h.ToggleProvider())

	req := httptest.NewRequest(http.MethodPost, "/api/admin/compute/providers/nonexistent/toggle", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAssignProviderToCenter(t *testing.T) {
	h, store := setupComputeTest(t)
	p := createTestProvider(t, store)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/compute-providers", h.AssignProviderToCenter())

	body := `{"provider_id":"` + p.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/center-1/compute-providers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify assignment exists in store.
	ids, err := store.ListAssignments(context.Background(), "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != p.ID {
		t.Errorf("expected assignment [%s], got %v", p.ID, ids)
	}
}

func TestAssignProviderToCenterMissingProviderID(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/compute-providers", h.AssignProviderToCenter())

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/center-1/compute-providers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAssignProviderToCenterProviderNotFound(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/compute-providers", h.AssignProviderToCenter())

	body := `{"provider_id":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/center-1/compute-providers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAssignProviderToCenterIdempotent(t *testing.T) {
	h, store := setupComputeTest(t)
	p := createTestProvider(t, store)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/compute-providers", h.AssignProviderToCenter())

	body := `{"provider_id":"` + p.ID + `"}`

	// Assign twice — should not error (INSERT OR IGNORE).
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/center-1/compute-providers", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	ids, err := store.ListAssignments(context.Background(), "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Errorf("expected 1 assignment after duplicate assign, got %d", len(ids))
	}
}

func TestUnassignProviderFromCenter(t *testing.T) {
	h, store := setupComputeTest(t)
	p := createTestProvider(t, store)

	// Pre-assign.
	if err := store.AssignProvider(context.Background(), "center-1", p.ID); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/admin/centers/{id}/compute-providers/{provider_id}", h.UnassignProviderFromCenter())

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/centers/center-1/compute-providers/"+p.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify assignment removed.
	ids, err := store.ListAssignments(context.Background(), "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 assignments after unassign, got %d", len(ids))
	}
}

func TestUnassignProviderFromCenterNonexistent(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/admin/centers/{id}/compute-providers/{provider_id}", h.UnassignProviderFromCenter())

	// Unassigning a non-existent assignment should still return 200 (DELETE is idempotent).
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/centers/center-1/compute-providers/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListCenterAssignments(t *testing.T) {
	h, store := setupComputeTest(t)
	p1 := createTestProvider(t, store)

	// Create a second provider.
	p2 := &compute.ComputeProvider{
		Name:        "provider-2",
		BaseURL:     "https://api2.example.com",
		APIKey:      "sk-key-2",
		Protocol:    "anthropic",
		UserAgent:   "openclaw",
		ComputeType: "coding",
		Model:       "claude-3",
		Enabled:     true,
	}
	if err := store.CreateProvider(context.Background(), p2); err != nil {
		t.Fatal(err)
	}

	// Assign both to center-1.
	if err := store.AssignProvider(context.Background(), "center-1", p1.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AssignProvider(context.Background(), "center-1", p2.ID); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/centers/{id}/compute-providers", h.ListCenterAssignments())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/centers/center-1/compute-providers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Assignments []string `json:"assignments"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(resp.Assignments))
	}
}

func TestListCenterAssignmentsEmpty(t *testing.T) {
	h, _ := setupComputeTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/centers/{id}/compute-providers", h.ListCenterAssignments())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/centers/center-1/compute-providers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Assignments []string `json:"assignments"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Assignments == nil {
		t.Error("assignments should be empty array, not null")
	}
	if len(resp.Assignments) != 0 {
		t.Errorf("expected 0 assignments, got %d", len(resp.Assignments))
	}
}
