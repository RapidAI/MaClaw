package bootstrap

import "testing"

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
