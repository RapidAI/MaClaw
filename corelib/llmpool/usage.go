package llmpool

import "context"

// UsageRecorder is the interface for recording LLM token usage.
// Hub implements this for per-user billing; HubCenter implements it
// for per-tenant (hub→tenant) billing.
type UsageRecorder interface {
	RecordUsage(ctx context.Context, record *UsageRecord) error
}

// UsageQuerier is the interface for querying aggregated usage statistics.
type UsageQuerier interface {
	// QueryUsage returns usage summaries matching the given filter.
	QueryUsage(ctx context.Context, filter UsageFilter) ([]UsageSummary, error)
}

// UsageFilter specifies query parameters for usage statistics.
type UsageFilter struct {
	HubID          string `json:"hub_id,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	Model          string `json:"model,omitempty"`
	ProviderID     string `json:"provider_id,omitempty"`
	ServiceGroupID string `json:"service_group_id,omitempty"`
	WorkloadClass  string `json:"workload_class,omitempty"`
	Period         string `json:"period,omitempty"`     // "daily" / "weekly" / "monthly"
	StartDate      string `json:"start_date,omitempty"` // "2026-01-01"
	EndDate        string `json:"end_date,omitempty"`   // "2026-01-31"
	Limit          int    `json:"limit,omitempty"`
}

// UsageSummary represents aggregated usage for a time period.
type UsageSummary struct {
	HubID          string  `json:"hub_id,omitempty"`
	TenantID       string  `json:"tenant_id,omitempty"`
	Model          string  `json:"model,omitempty"`
	ProviderID     string  `json:"provider_id,omitempty"`
	ServiceGroupID string  `json:"service_group_id,omitempty"`
	WorkloadClass  string  `json:"workload_class,omitempty"`
	Period         string  `json:"period"`
	PeriodStart    string  `json:"period_start"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	TotalCredits   float64 `json:"total_credits"`
	TotalRequests  int64   `json:"total_requests"`
	CacheHits      int64   `json:"cache_hits"`
	CacheHitRate   float64 `json:"cache_hit_rate"`
}
