package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/centers"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store/sqlite"
)

func newApprovalTestServices(t *testing.T) (*sqlite.Provider, *centers.Service, *license.Service) {
	t.Helper()
	provider, err := sqlite.NewProvider(":memory:")
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	t.Cleanup(provider.Close)
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	stores := sqlite.NewStore(provider)
	licenseSvc := license.NewService(stores.Licenses, key)
	centerSvc := centers.NewService(stores.Centers, licenseSvc)
	return provider, centerSvc, licenseSvc
}

func insertApprovalCenter(t *testing.T, provider *sqlite.Provider, id string) {
	t.Helper()
	stores := sqlite.NewStore(provider)
	now := time.Now().UTC().Truncate(time.Second)
	if err := stores.Centers.Create(context.Background(), &store.Center{
		ID:          id,
		CompanyName: "Approval Inc",
		AdminEmail:  "admin@example.com",
		Status:      "pending",
		SecretHash:  "hash",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create center: %v", err)
	}
}

func TestConfirmCenterTrialHandlerActivatesAndIssuesLicense(t *testing.T) {
	provider, centerSvc, licenseSvc := newApprovalTestServices(t)
	insertApprovalCenter(t, provider, "ctr_trial")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm-trial", ConfirmCenterTrialHandler(centerSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_trial/confirm-trial", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	stores := sqlite.NewStore(provider)
	center, err := stores.Centers.GetByID(context.Background(), "ctr_trial")
	if err != nil {
		t.Fatalf("get center: %v", err)
	}
	if center.Status != "active" {
		t.Fatalf("center status = %q, want active", center.Status)
	}
	lic, err := licenseSvc.GetActive(context.Background(), "ctr_trial")
	if err != nil {
		t.Fatalf("active license missing: %v", err)
	}
	if lic.Type != "trial" || lic.CenterID != "ctr_trial" || lic.IsLongTerm {
		t.Fatalf("unexpected trial license: %+v", lic)
	}
}

func TestConfirmCenterManualHandlerActivatesAndIssuesPermanentLicense(t *testing.T) {
	provider, centerSvc, licenseSvc := newApprovalTestServices(t)
	insertApprovalCenter(t, provider, "ctr_manual")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm", ConfirmCenterManualHandler(centerSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_manual/confirm", strings.NewReader(`{"modules":["compute","skill_market"],"days":0}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	stores := sqlite.NewStore(provider)
	center, err := stores.Centers.GetByID(context.Background(), "ctr_manual")
	if err != nil {
		t.Fatalf("get center: %v", err)
	}
	if center.Status != "active" {
		t.Fatalf("center status = %q, want active", center.Status)
	}
	lic, err := licenseSvc.GetActive(context.Background(), "ctr_manual")
	if err != nil {
		t.Fatalf("active license missing: %v", err)
	}
	if lic.Type != "manual" || !lic.IsLongTerm || !strings.Contains(lic.Modules, "skill_market") {
		t.Fatalf("unexpected manual license: %+v", lic)
	}
}

func TestConfirmCenterManualHandlerRejectsInvalidJSONWithoutLicense(t *testing.T) {
	provider, centerSvc, licenseSvc := newApprovalTestServices(t)
	insertApprovalCenter(t, provider, "ctr_manual_bad_json")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm", ConfirmCenterManualHandler(centerSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_manual_bad_json/confirm", strings.NewReader(`{"modules":`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	stores := sqlite.NewStore(provider)
	center, err := stores.Centers.GetByID(context.Background(), "ctr_manual_bad_json")
	if err != nil {
		t.Fatalf("get center: %v", err)
	}
	if center.Status != "pending" {
		t.Fatalf("center status = %q, want pending", center.Status)
	}
	if lic, err := licenseSvc.GetActive(context.Background(), "ctr_manual_bad_json"); err == nil || lic != nil {
		t.Fatalf("license should not be issued after invalid JSON: lic=%+v err=%v", lic, err)
	}
}

func TestConfirmCenterManualHandlerRejectsNegativeDurationWithoutActivating(t *testing.T) {
	provider, centerSvc, licenseSvc := newApprovalTestServices(t)
	insertApprovalCenter(t, provider, "ctr_manual_negative_days")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm", ConfirmCenterManualHandler(centerSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_manual_negative_days/confirm", strings.NewReader(`{"modules":["compute"],"days":-1}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	stores := sqlite.NewStore(provider)
	center, err := stores.Centers.GetByID(context.Background(), "ctr_manual_negative_days")
	if err != nil {
		t.Fatalf("get center: %v", err)
	}
	if center.Status != "pending" {
		t.Fatalf("center status = %q, want pending", center.Status)
	}
	if lic, err := licenseSvc.GetActive(context.Background(), "ctr_manual_negative_days"); err == nil || lic != nil {
		t.Fatalf("license should not be issued after negative duration: lic=%+v err=%v", lic, err)
	}
}

func TestConfirmCenterManualHandlerDoesNotActivateWhenLicenseSigningFails(t *testing.T) {
	provider, _, _ := newApprovalTestServices(t)
	stores := sqlite.NewStore(provider)
	centerSvc := centers.NewService(stores.Centers, license.NewService(stores.Licenses, nil))
	insertApprovalCenter(t, provider, "ctr_manual_signing_failed")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm", ConfirmCenterManualHandler(centerSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_manual_signing_failed/confirm", strings.NewReader(`{"modules":["compute"],"days":365}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	center, err := stores.Centers.GetByID(context.Background(), "ctr_manual_signing_failed")
	if err != nil {
		t.Fatalf("get center: %v", err)
	}
	if center.Status != "pending" {
		t.Fatalf("center status = %q, want pending after license signing failure", center.Status)
	}
	if lic, err := stores.Licenses.GetActiveByCenterID(context.Background(), "ctr_manual_signing_failed"); err == nil || lic != nil {
		t.Fatalf("license should not be issued after signing failure: lic=%+v err=%v", lic, err)
	}
}

func TestConfirmCenterManualHandlerDoesNotPanicWithoutLicenseService(t *testing.T) {
	provider, _, _ := newApprovalTestServices(t)
	stores := sqlite.NewStore(provider)
	centerSvc := centers.NewService(stores.Centers, nil)
	insertApprovalCenter(t, provider, "ctr_manual_no_license_service")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm", ConfirmCenterManualHandler(centerSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_manual_no_license_service/confirm", strings.NewReader(`{"modules":["compute"],"days":365}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	center, err := stores.Centers.GetByID(context.Background(), "ctr_manual_no_license_service")
	if err != nil {
		t.Fatalf("get center: %v", err)
	}
	if center.Status != "pending" {
		t.Fatalf("center status = %q, want pending when license service is unavailable", center.Status)
	}
}

func TestConfirmManualLicenseCanBeFetchedByRegisteredCenter(t *testing.T) {
	provider, centerSvc, licenseSvc := newApprovalTestServices(t)
	stores := sqlite.NewStore(provider)
	now := time.Now().UTC().Truncate(time.Second)
	if err := stores.Centers.Create(context.Background(), &store.Center{
		ID:          "ctr_fetch_license",
		CompanyName: "Fetch License Inc",
		AdminEmail:  "admin@example.com",
		Status:      "pending",
		SecretHash:  hashTestSecret("center-secret"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create center: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/centers/{id}/confirm", ConfirmCenterManualHandler(centerSvc))
	mux.HandleFunc("GET /api/centers/{id}/license", GetActiveLicenseHandler(licenseSvc, centerSvc))

	confirmReq := httptest.NewRequest(http.MethodPost, "/api/admin/centers/ctr_fetch_license/confirm", strings.NewReader(`{"modules":["compute","skill_market"],"days":365}`))
	confirmReq.Header.Set("Content-Type", "application/json")
	confirmRes := httptest.NewRecorder()
	mux.ServeHTTP(confirmRes, confirmReq)
	if confirmRes.Code != http.StatusOK {
		t.Fatalf("confirm status = %d body=%s", confirmRes.Code, confirmRes.Body.String())
	}

	licenseReq := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_fetch_license/license", nil)
	licenseReq.Header.Set("X-Center-Secret", "center-secret")
	licenseRes := httptest.NewRecorder()
	mux.ServeHTTP(licenseRes, licenseReq)
	if licenseRes.Code != http.StatusOK {
		t.Fatalf("license status = %d body=%s", licenseRes.Code, licenseRes.Body.String())
	}
	var lic store.License
	if err := json.NewDecoder(licenseRes.Body).Decode(&lic); err != nil {
		t.Fatalf("decode license: %v", err)
	}
	if lic.CenterID != "ctr_fetch_license" || lic.Type != "manual" || lic.IsLongTerm || !strings.Contains(lic.Modules, "skill_market") {
		t.Fatalf("license = %+v", lic)
	}
}

func TestDeleteCenterHandlerCleansDependentRows(t *testing.T) {
	provider, centerSvc, _ := newApprovalTestServices(t)
	stores := sqlite.NewStore(provider)
	now := time.Now().UTC().Truncate(time.Second)
	if err := stores.Centers.Create(context.Background(), &store.Center{
		ID:          "ctr_delete_http",
		CompanyName: "Delete HTTP Inc",
		AdminEmail:  "admin@example.com",
		Status:      "active",
		SecretHash:  hashTestSecret("center-secret"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create center: %v", err)
	}
	if _, err := provider.Write.ExecContext(context.Background(), `INSERT INTO licenses (id, center_id, modules, type, expires_at, is_long_term, certificate, created_at) VALUES (?, ?, ?, ?, ?, 0, ?, ?)`, "lic_delete_http", "ctr_delete_http", `["compute"]`, "manual", now.AddDate(0, 1, 0).Format(time.RFC3339), "cert", now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert license: %v", err)
	}
	if _, err := provider.Write.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS center_provider_assignments (center_id TEXT NOT NULL, provider_id TEXT NOT NULL, PRIMARY KEY(center_id, provider_id))`); err != nil {
		t.Fatalf("create assignment table: %v", err)
	}
	if _, err := provider.Write.ExecContext(context.Background(), `INSERT INTO center_provider_assignments (center_id, provider_id) VALUES (?, ?)`, "ctr_delete_http", "provider-http"); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := provider.Write.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS token_usage_records (center_id TEXT NOT NULL, model TEXT NOT NULL DEFAULT '', prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0, total_tokens INTEGER NOT NULL DEFAULT 0, cost REAL NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatalf("create usage table: %v", err)
	}
	if _, err := provider.Write.ExecContext(context.Background(), `INSERT INTO token_usage_records (center_id, model, prompt_tokens, completion_tokens, total_tokens, cost, created_at) VALUES (?, ?, 1, 2, 3, 0.12, ?)`, "ctr_delete_http", "gpt-test", now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert usage: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/admin/centers/{id}", DeleteCenterHandler(centerSvc))
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/centers/ctr_delete_http", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", res.Code, res.Body.String())
	}

	for _, check := range []struct {
		name  string
		query string
	}{
		{"center", `SELECT COUNT(*) FROM centers WHERE id="ctr_delete_http"`},
		{"license", `SELECT COUNT(*) FROM licenses WHERE center_id="ctr_delete_http"`},
		{"assignment", `SELECT COUNT(*) FROM center_provider_assignments WHERE center_id="ctr_delete_http"`},
		{"usage", `SELECT COUNT(*) FROM token_usage_records WHERE center_id="ctr_delete_http"`},
	} {
		var count int
		if err := provider.Read.QueryRowContext(context.Background(), check.query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if count != 0 {
			t.Fatalf("%s rows left after delete: %d", check.name, count)
		}
	}
}

func TestDeleteCenterHandlerReturnsNotFoundForMissingCenter(t *testing.T) {
	_, centerSvc, _ := newApprovalTestServices(t)
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/admin/centers/{id}", DeleteCenterHandler(centerSvc))

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/centers/missing-center", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("delete status = %d body=%s, want 404", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "CENTER_NOT_FOUND") {
		t.Fatalf("body = %s, want CENTER_NOT_FOUND", res.Body.String())
	}
}
