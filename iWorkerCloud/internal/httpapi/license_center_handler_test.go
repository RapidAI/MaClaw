package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

type memoryLicenseRepo struct {
	licenses map[string]*store.License
}

func (m *memoryLicenseRepo) Create(context.Context, *store.License) error { return nil }
func (m *memoryLicenseRepo) GetByID(context.Context, string) (*store.License, error) {
	return nil, fmt.Errorf("not found")
}
func (m *memoryLicenseRepo) GetByCenterID(context.Context, string) ([]*store.License, error) {
	return nil, nil
}
func (m *memoryLicenseRepo) GetActiveByCenterID(_ context.Context, centerID string) (*store.License, error) {
	if lic, ok := m.licenses[centerID]; ok {
		return lic, nil
	}
	return nil, fmt.Errorf("not found")
}
func (m *memoryLicenseRepo) Revoke(context.Context, string) error           { return nil }
func (m *memoryLicenseRepo) List(context.Context) ([]*store.License, error) { return nil, nil }

func TestGetActiveLicenseRequiresCenterSecret(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	licSvc := license.NewService(&memoryLicenseRepo{licenses: map[string]*store.License{
		"ctr_1": {ID: "lic_1", CenterID: "ctr_1", Certificate: "signed-cert"},
	}}, priv)
	centerAuth := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("center-secret")},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/license", GetActiveLicenseHandler(licSvc, centerAuth))

	missing := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/license", nil)
	missingRes := httptest.NewRecorder()
	mux.ServeHTTP(missingRes, missing)
	if missingRes.Code != http.StatusUnauthorized {
		t.Fatalf("missing secret status = %d, want 401", missingRes.Code)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/license", nil)
	invalid.Header.Set("X-Center-Secret", "wrong")
	invalidRes := httptest.NewRecorder()
	mux.ServeHTTP(invalidRes, invalid)
	if invalidRes.Code != http.StatusUnauthorized {
		t.Fatalf("invalid secret status = %d, want 401", invalidRes.Code)
	}

	valid := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/license", nil)
	valid.Header.Set("X-Center-Secret", "center-secret")
	validRes := httptest.NewRecorder()
	mux.ServeHTTP(validRes, valid)
	if validRes.Code != http.StatusOK {
		t.Fatalf("valid secret status = %d body=%s", validRes.Code, validRes.Body.String())
	}
	var body store.License
	if err := json.NewDecoder(validRes.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ID != "lic_1" || body.CenterID != "ctr_1" {
		t.Fatalf("unexpected license: %+v", body)
	}
}

func TestGetActiveLicenseRejectsDisabledCenter(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	licSvc := license.NewService(&memoryLicenseRepo{licenses: map[string]*store.License{}}, priv)
	centerAuth := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "disabled", SecretHash: hashTestSecret("center-secret")},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/centers/{id}/license", GetActiveLicenseHandler(licSvc, centerAuth))

	req := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/license", nil)
	req.Header.Set("X-Center-Secret", "center-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", res.Code)
	}
}
