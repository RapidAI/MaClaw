package llmservice

import (
	"context"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// ---------------------------------------------------------------------------
// Usage Statistics
// ---------------------------------------------------------------------------

// TenantUsageRecord is the per-request usage record stored for billing/stats.
type TenantUsageRecord struct {
	ID           int64     `json:"id,omitempty"`
	HubID        string    `json:"hub_id"`
	TenantID     string    `json:"tenant_id"`
	Model        string    `json:"model"`
	ProviderID   string    `json:"provider_id"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	Credits      float64   `json:"credits"`
	CacheHit     bool      `json:"cache_hit"`
	AuthID       string    `json:"auth_id,omitempty"` // which authorization was charged
	CreatedAt    time.Time `json:"created_at"`
}

// TenantUsageSummary is an aggregated usage report.
type TenantUsageSummary struct {
	HubID         string  `json:"hub_id"`
	TenantID      string  `json:"tenant_id"`
	Model         string  `json:"model,omitempty"`
	Period        string  `json:"period"`       // "daily" / "weekly" / "monthly"
	PeriodStart   string  `json:"period_start"` // "2026-01-01"
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	TotalCredits  float64 `json:"total_credits"`
	TotalRequests int64   `json:"total_requests"`
	CacheHits     int64   `json:"cache_hits"`
	CacheHitRate  float64 `json:"cache_hit_rate"`
}

// UsageRepository persists usage records and provides aggregation queries.
type UsageRepository interface {
	Insert(ctx context.Context, record *TenantUsageRecord) error
	QuerySummary(ctx context.Context, filter UsageFilter) ([]TenantUsageSummary, error)
	QueryRecent(ctx context.Context, hubID, tenantID string, limit int) ([]*TenantUsageRecord, error)
}

// UsageFilter for querying aggregated stats.
type UsageFilter struct {
	HubID     string `json:"hub_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	Model     string `json:"model,omitempty"`
	Period    string `json:"period,omitempty"`     // "daily" / "weekly" / "monthly"
	StartDate string `json:"start_date,omitempty"` // "2026-01-01"
	EndDate   string `json:"end_date,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// UsageRecorderImpl implements llmpool.UsageRecorder for HubCenter.
type UsageRecorderImpl struct {
	repo  UsageRepository
	hubID string // can be set per-request via context, or default
}

// NewUsageRecorder creates a usage recorder backed by the given repository.
func NewUsageRecorder(repo UsageRepository) *UsageRecorderImpl {
	return &UsageRecorderImpl{repo: repo}
}

// RecordUsage implements llmpool.UsageRecorder.
func (u *UsageRecorderImpl) RecordUsage(ctx context.Context, record *llmpool.UsageRecord) error {
	if u.repo == nil || record == nil {
		return nil
	}
	// Extract hub/tenant from context (set by proxy handler)
	hubID, tenantID := usageContextValues(ctx)
	return u.repo.Insert(ctx, &TenantUsageRecord{
		HubID:        hubID,
		TenantID:     tenantID,
		Model:        record.Model,
		ProviderID:   record.ProviderID,
		InputTokens:  record.InputTokens,
		OutputTokens: record.OutputTokens,
		Credits:      record.Credits,
		CacheHit:     record.CacheHit,
		AuthID:       record.AuthID,
		CreatedAt:    record.Timestamp,
	})
}

// ---------------------------------------------------------------------------
// Context keys for passing hub/tenant through the request pipeline
// ---------------------------------------------------------------------------

type usageContextKey struct{}

type usageContextData struct {
	HubID    string
	TenantID string
}

// WithUsageContext attaches hub/tenant info to context for usage recording.
func WithUsageContext(ctx context.Context, hubID, tenantID string) context.Context {
	return context.WithValue(ctx, usageContextKey{}, &usageContextData{
		HubID:    hubID,
		TenantID: tenantID,
	})
}

func usageContextValues(ctx context.Context) (string, string) {
	v, _ := ctx.Value(usageContextKey{}).(*usageContextData)
	if v == nil {
		return "", ""
	}
	return v.HubID, v.TenantID
}

// StatsService wraps a UsageRepository for the admin API layer.
type StatsService struct {
	repo UsageRepository
}

// NewStatsService creates a stats service.
func NewStatsService(repo UsageRepository) *StatsService {
	return &StatsService{repo: repo}
}

// QueryUsageSummary returns aggregated usage matching the filter.
func (s *StatsService) QueryUsageSummary(ctx context.Context, filter UsageFilter) ([]TenantUsageSummary, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.QuerySummary(ctx, filter)
}

// QueryRecentRecords returns recent usage records for a hub+tenant.
func (s *StatsService) QueryRecentRecords(ctx context.Context, hubID, tenantID string, limit int) ([]*TenantUsageRecord, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.QueryRecent(ctx, hubID, tenantID, limit)
}
