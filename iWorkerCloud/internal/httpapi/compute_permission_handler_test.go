package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

type failingComputeStore struct {
	computeStore
	getPermissionErr error
	getForceSyncErr  error
	clearForceErr    error
}

func (s *failingComputeStore) GetComputePermission(ctx context.Context, centerID string) (bool, error) {
	if s.getPermissionErr != nil {
		return false, s.getPermissionErr
	}
	return s.computeStore.GetComputePermission(ctx, centerID)
}

func (s *failingComputeStore) GetForceSync(ctx context.Context, centerID string) (bool, error) {
	if s.getForceSyncErr != nil {
		return false, s.getForceSyncErr
	}
	return s.computeStore.GetForceSync(ctx, centerID)
}

func (s *failingComputeStore) ClearForceSync(ctx context.Context, centerID string) error {
	if s.clearForceErr != nil {
		return s.clearForceErr
	}
	return s.computeStore.ClearForceSync(ctx, centerID)
}

func TestSetComputePermission_Grant(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/centers/{id}/compute-permission", h.SetComputePermission())

	body := `{"enabled": true}`
	req := httptest.NewRequest("PUT", "/api/admin/centers/ctr_1/compute-permission", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ComputePermission bool `json:"compute_permission"`
		ForceSync         bool `json:"force_sync"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ComputePermission {
		t.Error("expected compute_permission=true")
	}
	if resp.ForceSync {
		t.Error("expected force_sync=false when granting permission")
	}
}

func TestSetComputePermissionFailsWhenPermissionReadbackFails(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{}}
	h := NewComputeHandler(cs, mock)
	h.store = &failingComputeStore{
		computeStore:     cs,
		getPermissionErr: errors.New("permission read failed"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/centers/{id}/compute-permission", h.SetComputePermission())

	req := httptest.NewRequest(http.MethodPut, "/api/admin/centers/ctr_1/compute-permission", bytes.NewBufferString(`{"enabled":true}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "GET_PERMISSION_FAILED") {
		t.Fatalf("expected GET_PERMISSION_FAILED response: %s", w.Body.String())
	}
}

func TestSetComputePermissionFailsWhenForceSyncReadbackFails(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{}}
	h := NewComputeHandler(cs, mock)
	h.store = &failingComputeStore{
		computeStore:    cs,
		getForceSyncErr: errors.New("force sync read failed"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/centers/{id}/compute-permission", h.SetComputePermission())

	req := httptest.NewRequest(http.MethodPut, "/api/admin/centers/ctr_1/compute-permission", bytes.NewBufferString(`{"enabled":true}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "GET_FORCE_SYNC_FAILED") {
		t.Fatalf("expected GET_FORCE_SYNC_FAILED response: %s", w.Body.String())
	}
}

func TestSetComputePermission_Revoke_SetsForceSync(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()
	mock := &mockCenterAuthService{centers: map[string]*store.Center{}}
	h := NewComputeHandler(cs, mock)

	// First grant permission.
	cs.SetComputePermission(ctx, "ctr_1", true)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/centers/{id}/compute-permission", h.SetComputePermission())

	// Now revoke.
	body := `{"enabled": false}`
	req := httptest.NewRequest("PUT", "/api/admin/centers/ctr_1/compute-permission", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ComputePermission bool `json:"compute_permission"`
		ForceSync         bool `json:"force_sync"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.ComputePermission {
		t.Error("expected compute_permission=false after revoke")
	}
	if !resp.ForceSync {
		t.Error("expected force_sync=true after revoking permission")
	}
}

func TestSetComputePermission_InvalidJSON(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/centers/{id}/compute-permission", h.SetComputePermission())

	req := httptest.NewRequest("PUT", "/api/admin/centers/ctr_1/compute-permission", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCenterComputeProviders_ReflectsPermission(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()

	p := sampleTestProvider()
	cs.CreateProvider(ctx, p)

	// Grant permission to center.
	cs.SetComputePermission(ctx, "ctr_1", true)

	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("my-secret")},
	}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "my-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		ComputePermission bool `json:"compute_permission"`
		ForceSync         bool `json:"force_sync"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if !resp.ComputePermission {
		t.Error("expected compute_permission=true")
	}
	if resp.ForceSync {
		t.Error("expected force_sync=false")
	}
}

func TestCenterComputeProviders_ForceSyncClearedAfterRead(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()

	p := sampleTestProvider()
	cs.CreateProvider(ctx, p)

	// Set force_sync for center.
	cs.SetForceSync(ctx, "ctr_1", true)

	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("my-secret")},
	}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	// First request: force_sync should be true.
	req := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "my-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp1 struct {
		ForceSync bool `json:"force_sync"`
	}
	json.NewDecoder(w.Body).Decode(&resp1)
	if !resp1.ForceSync {
		t.Error("expected force_sync=true on first read")
	}

	// Second request: force_sync should be cleared.
	req2 := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers", nil)
	req2.Header.Set("X-Center-Secret", "my-secret")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	var resp2 struct {
		ForceSync bool `json:"force_sync"`
	}
	json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.ForceSync {
		t.Error("expected force_sync=false on second read (should be cleared)")
	}
}

func TestCenterComputeProvidersFailsWhenPermissionReadFails(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()
	if err := cs.CreateProvider(ctx, sampleTestProvider()); err != nil {
		t.Fatal(err)
	}

	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("my-secret")},
	}}
	h := NewComputeHandler(cs, mock)
	h.store = &failingComputeStore{
		computeStore:     cs,
		getPermissionErr: errors.New("permission read failed"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "my-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "GET_PERMISSION_FAILED") {
		t.Fatalf("expected GET_PERMISSION_FAILED response: %s", w.Body.String())
	}
}

func TestCenterComputeProvidersFailsWhenClearForceSyncFails(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()
	if err := cs.CreateProvider(ctx, sampleTestProvider()); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetForceSync(ctx, "ctr_1", true); err != nil {
		t.Fatal(err)
	}

	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("my-secret")},
	}}
	h := NewComputeHandler(cs, mock)
	h.store = &failingComputeStore{
		computeStore:  cs,
		clearForceErr: errors.New("clear failed"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "my-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CLEAR_FORCE_SYNC_FAILED") {
		t.Fatalf("expected CLEAR_FORCE_SYNC_FAILED response: %s", w.Body.String())
	}
}

func TestListCenterPermissionsUsesCenterName(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()
	if err := cs.SetComputePermission(ctx, "ctr_1", true); err != nil {
		t.Fatal(err)
	}
	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", CompanyName: "Center Service East", Status: "active"},
	}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/compute/permissions", h.ListCenterPermissions())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/compute/permissions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "\"center_name\":\"Center Service East\"") {
		t.Fatalf("expected center_name in response: %s", body)
	}
	if strings.Contains(body, "company_name") {
		t.Fatalf("compute permissions response leaked legacy company_name: %s", body)
	}
}

func TestListCenterPermissionsFailsWhenPermissionReadFails(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", CompanyName: "Center Service East", Status: "active"},
	}}
	h := NewComputeHandler(cs, mock)
	h.store = &failingComputeStore{
		computeStore:     cs,
		getPermissionErr: errors.New("permission read failed"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/compute/permissions", h.ListCenterPermissions())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/compute/permissions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "GET_PERMISSION_FAILED") {
		t.Fatalf("expected GET_PERMISSION_FAILED response: %s", w.Body.String())
	}
}
