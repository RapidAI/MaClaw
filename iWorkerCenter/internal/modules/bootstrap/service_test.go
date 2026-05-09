package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizePlanProvidesSafeDefaults(t *testing.T) {
	plan := NormalizePlan("tenant-a", Plan{})
	if plan.TenantID != "tenant-a" {
		t.Fatalf("tenant = %q", plan.TenantID)
	}
	if len(plan.VirtualDepartments) == 0 || len(plan.InitialIWorkers) == 0 || len(plan.RecurringTasks) == 0 {
		t.Fatalf("expected defaults, got %+v", plan)
	}
	if !plan.WatcherPolicy.Enabled || !plan.WatcherPolicy.SingleFlight || !plan.WatcherPolicy.ScaleByWorkerCount {
		t.Fatalf("watcher policy should be safe by default: %+v", plan.WatcherPolicy)
	}
}

func TestValidatePlanRequiresParallelWorkersAndMemoryScopes(t *testing.T) {
	plan := NormalizePlan("tenant-a", Plan{
		CompanyName:        "Acme",
		VirtualDepartments: []string{"Ops", "Sales", "Quality"},
		InitialIWorkers:    []string{"Only iWorker"},
		MemoryScopes:       []string{"company"},
	})
	issues := ValidatePlan(plan)
	if len(issues) < 2 {
		t.Fatalf("expected validation issues, got %+v", issues)
	}
	if noBlockingIssues(issues) {
		t.Fatal("expected blocking validation issues")
	}
}

func TestServiceDraftApplyAndStartFirstWave(t *testing.T) {
	svc := NewService(t.TempDir() + "/bootstrap.json")
	plan, issues, err := svc.DraftPlan("tenant-a", Plan{CompanyName: "Acme"})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if !noBlockingIssues(issues) {
		t.Fatalf("draft defaults should be startable, issues=%+v", issues)
	}
	if plan.Status != "draft" {
		t.Fatalf("status = %q", plan.Status)
	}
	applied, issues, _, err := svc.ApplyPlan("tenant-a", plan)
	if err != nil {
		t.Fatalf("apply: %v issues=%+v", err, issues)
	}
	if applied.Status != "applied" {
		t.Fatalf("applied status = %q", applied.Status)
	}
	run, err := svc.StartFirstWave("tenant-a")
	if err != nil {
		t.Fatalf("start first wave: %v", err)
	}
	if len(run.Tasks) == 0 || run.Status != "first_wave_started" {
		t.Fatalf("unexpected run: %+v", run)
	}
	status := svc.Status("tenant-a")
	if !status.HasPlan || status.LastRun == nil || !status.ReadyToStart {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestServiceUsesRequestTenantOverPlanTenant(t *testing.T) {
	svc := NewService(t.TempDir() + "/bootstrap.json")
	plan, _, err := svc.DraftPlan("tenant-a", Plan{
		TenantID:    "tenant-b",
		CompanyName: "Acme",
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if plan.TenantID != "tenant-a" {
		t.Fatalf("plan tenant = %q", plan.TenantID)
	}
	statusA := svc.Status("tenant-a")
	if !statusA.HasPlan || statusA.Plan == nil || statusA.Plan.TenantID != "tenant-a" {
		t.Fatalf("expected tenant-a plan, got %+v", statusA)
	}
	statusB := svc.Status("tenant-b")
	if statusB.HasPlan {
		t.Fatalf("tenant-b should not inherit tenant-a bootstrap plan: %+v", statusB)
	}
}

func TestDraftPlanRejectsInvalidJSONWithoutSavingPlan(t *testing.T) {
	svc := NewService(t.TempDir() + "/bootstrap.json")
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap/draft-plan", strings.NewReader(`{"company_name":`))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if status := svc.Status("tenant-a"); status.HasPlan {
		t.Fatalf("unexpected saved plan after invalid JSON: %+v", status)
	}
}

func TestApplyPlanRejectsOversizedJSONWithoutSavingPlan(t *testing.T) {
	svc := NewService(t.TempDir() + "/bootstrap.json")
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	body := `{"company_name":"Acme","company_address":"` + strings.Repeat("x", maxBootstrapPlanBodyBytes+1024)
	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap/apply-plan", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if status := svc.Status("tenant-a"); status.HasPlan {
		t.Fatalf("unexpected saved plan after oversized JSON: %+v", status)
	}
}

func TestDraftPlanRejectsTrailingJSONWithoutSavingPlan(t *testing.T) {
	svc := NewService(t.TempDir() + "/bootstrap.json")
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)

	body := `{"company_name":"Acme","legal_person":"Alice","company_address":"Shanghai","contact_email":"admin@example.com"} {"company_name":"Extra"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap/draft-plan", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if status := svc.Status("tenant-a"); status.HasPlan {
		t.Fatalf("unexpected saved plan after trailing JSON: %+v", status)
	}
}

func TestNormalizePlanKeepsEnterpriseIdentityFields(t *testing.T) {
	plan := NormalizePlan("tenant-a", Plan{
		CompanyName:    "  Acme  ",
		LegalPerson:    "  Alice  ",
		CompanyAddress: "  Shanghai  ",
		ContactEmail:   "  admin@example.com  ",
	})
	if plan.CompanyName != "Acme" || plan.LegalPerson != "Alice" || plan.CompanyAddress != "Shanghai" || plan.ContactEmail != "admin@example.com" {
		t.Fatalf("enterprise fields not normalized: %+v", plan)
	}
}

func TestValidatePlanRejectsInvalidContactEmail(t *testing.T) {
	plan := NormalizePlan("tenant-a", Plan{
		CompanyName:        "Acme",
		LegalPerson:        "Alice",
		CompanyAddress:     "Shanghai",
		ContactEmail:       "not-an-email",
		VirtualDepartments: []string{"Ops", "Sales", "Quality"},
		InitialIWorkers:    []string{"Ops iWorker", "Quality iWorker"},
		MemoryScopes:       []string{"company", "department", "personal"},
	})
	issues := ValidatePlan(plan)
	var found bool
	for _, issue := range issues {
		if issue.Field == "contact_email" && issue.Level == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected contact_email validation error, got %+v", issues)
	}
}
