package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/compute"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"

	_ "modernc.org/sqlite"
)

// mockCenterAuthService implements CenterAuthService for testing.
type mockCenterAuthService struct {
	centers map[string]*store.Center
}

func (m *mockCenterAuthService) Get(_ context.Context, id string) (*store.Center, error) {
	c, ok := m.centers[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return c, nil
}

func (m *mockCenterAuthService) List(_ context.Context) ([]*store.Center, error) {
	result := make([]*store.Center, 0, len(m.centers))
	for _, c := range m.centers {
		result = append(result, c)
	}
	return result, nil
}

func (m *mockCenterAuthService) AuthenticateCenter(_ context.Context, id, secret string) (*store.Center, error) {
	c, ok := m.centers[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	h := sha256.Sum256([]byte(secret))
	if c.SecretHash != hex.EncodeToString(h[:]) {
		return nil, fmt.Errorf("unauthorized")
	}
	return c, nil
}

func hashTestSecret(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func newTestComputeStore(t *testing.T) *compute.ProviderStore {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := compute.NewProviderStore(db, key)
	if err := s.CreateTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func sampleTestProvider() *compute.ComputeProvider {
	return &compute.ComputeProvider{
		Name:                 "Test Provider",
		BaseURL:              "https://api.openai.com/v1",
		APIKey:               "sk-test-key-12345",
		Protocol:             "openai",
		UserAgent:            "openclaw",
		ComputeType:          "general",
		Model:                "gpt-4",
		Enabled:              true,
		Priority:             10,
		Description:          "Test",
		InputPricePerMToken:  2.5,
		OutputPricePerMToken: 10.0,
	}
}

func TestCenterComputeProviders_MissingSecret(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "AUTH_FAILED" {
		t.Errorf("expected AUTH_FAILED, got %q", resp["error"])
	}
}

func TestCenterComputeProviders_QuerySecretRejected(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("my-secret")},
	}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers?secret=my-secret", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for query secret, got %d", w.Code)
	}
}
func TestCenterComputeProviders_InvalidSecret(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("correct-secret")},
	}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "wrong-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCenterComputeProviders_DisabledCenter(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "disabled", SecretHash: hashTestSecret("my-secret")},
	}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "my-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "CENTER_DISABLED" {
		t.Errorf("expected CENTER_DISABLED, got %q", resp["error"])
	}
}

func TestCenterComputeProviders_Success_NoAssignments(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()

	// Create two enabled providers.
	p1 := sampleTestProvider()
	p1.Name = "Provider A"
	cs.CreateProvider(ctx, p1)

	p2 := sampleTestProvider()
	p2.Name = "Provider B"
	cs.CreateProvider(ctx, p2)

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
		Providers         []compute.ComputeProvider `json:"providers"`
		ComputePermission bool                      `json:"compute_permission"`
		ForceSync         bool                      `json:"force_sync"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	// No assignments: all enabled providers returned.
	if len(resp.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(resp.Providers))
	}

	// Full api_key should be present (not masked).
	for _, p := range resp.Providers {
		if p.APIKey == "" {
			t.Errorf("expected full api_key for provider %q, got empty", p.Name)
		}
	}

	if resp.ComputePermission != false {
		t.Error("expected compute_permission=false")
	}
	if resp.ForceSync != false {
		t.Error("expected force_sync=false")
	}
}

func TestCenterComputeProviders_WithAssignments(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()

	p1 := sampleTestProvider()
	p1.Name = "Assigned"
	cs.CreateProvider(ctx, p1)

	p2 := sampleTestProvider()
	p2.Name = "Not Assigned"
	cs.CreateProvider(ctx, p2)

	// Assign only p1 to ctr_1.
	cs.AssignProvider(ctx, "ctr_1", p1.ID)

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
		Providers []compute.ComputeProvider `json:"providers"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Providers) != 1 {
		t.Fatalf("expected 1 assigned provider, got %d", len(resp.Providers))
	}
	if resp.Providers[0].Name != "Assigned" {
		t.Errorf("expected 'Assigned', got %q", resp.Providers[0].Name)
	}
}

func TestCenterComputeProviders_SecretFromHeader(t *testing.T) {
	cs := newTestComputeStore(t)
	ctx := context.Background()

	p := sampleTestProvider()
	cs.CreateProvider(ctx, p)

	mock := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("header-secret")},
	}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest("GET", "/api/centers/ctr_1/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "header-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCenterComputeProviders_CenterNotFound(t *testing.T) {
	cs := newTestComputeStore(t)
	mock := &mockCenterAuthService{centers: map[string]*store.Center{}}
	h := NewComputeHandler(cs, mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/compute-providers", h.CenterComputeProviders())

	req := httptest.NewRequest("GET", "/api/centers/nonexistent/compute-providers", nil)
	req.Header.Set("X-Center-Secret", "any")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
