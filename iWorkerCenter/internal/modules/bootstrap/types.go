package bootstrap

import "time"

type WatcherPolicy struct {
	Enabled            bool `json:"enabled"`
	SingleFlight       bool `json:"single_flight"`
	MaxRunSeconds      int  `json:"max_run_seconds"`
	ScaleByWorkerCount bool `json:"scale_by_worker_count"`
}

type Plan struct {
	TenantID                      string        `json:"tenant_id"`
	CompanyName                   string        `json:"company_name"`
	BusinessSummary               string        `json:"business_summary"`
	Priority                      string        `json:"priority"`
	VirtualDepartments            []string      `json:"virtual_departments"`
	InitialIWorkers               []string      `json:"initial_iworkers"`
	MemoryScopes                  []string      `json:"memory_scopes"`
	RecurringTasks                []string      `json:"recurring_tasks"`
	RequiresExecutiveConfirmation []string      `json:"requires_executive_confirmation"`
	WatcherPolicy                 WatcherPolicy `json:"watcher_policy"`
	Status                        string        `json:"status"`
	UpdatedAt                     time.Time     `json:"updated_at"`
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Level   string `json:"level"`
}

type FirstWaveTask struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	OwnerIWorker        string `json:"owner_iworker"`
	ExpectedOutput      string `json:"expected_output"`
	MemoryScope         string `json:"memory_scope"`
	EscalationThreshold string `json:"escalation_threshold"`
	RequiresPeerReview  bool   `json:"requires_peer_review"`
	RecommendedTrigger  string `json:"recommended_trigger"`
}

type AppliedAsset struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Run struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
	Status        string          `json:"status"`
	Plan          Plan            `json:"plan"`
	Tasks         []FirstWaveTask `json:"tasks"`
	AppliedAssets []AppliedAsset  `json:"applied_assets"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type Status struct {
	TenantID           string            `json:"tenant_id"`
	HasPlan            bool              `json:"has_plan"`
	ReadyToStart       bool              `json:"ready_to_start"`
	Plan               *Plan             `json:"plan,omitempty"`
	ValidationIssues   []ValidationIssue `json:"validation_issues"`
	LastRun            *Run              `json:"last_run,omitempty"`
	SuggestedFirstWave []FirstWaveTask   `json:"suggested_first_wave"`
	AppliedAssets      []AppliedAsset    `json:"applied_assets"`
}
