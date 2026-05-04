package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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
