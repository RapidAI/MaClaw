package llmservice

import (
	"context"
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "time/tzdata"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// ---------------------------------------------------------------------------
// Usage Statistics
// ---------------------------------------------------------------------------

// TenantUsageRecord is the per-request usage record stored for billing/stats.
type TenantUsageRecord struct {
	ID                int64     `json:"id,omitempty"`
	HubID             string    `json:"hub_id"`
	TenantID          string    `json:"tenant_id"`
	RequestID         string    `json:"request_id,omitempty"`
	Model             string    `json:"model"`
	ProviderID        string    `json:"provider_id"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CachedInputTokens int64     `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64     `json:"cache_write_tokens,omitempty"`
	CacheUsageSource  string    `json:"cache_usage_source,omitempty"`
	UsageAnomaly      string    `json:"usage_anomaly,omitempty"`
	PricingSource     string    `json:"pricing_source,omitempty"`
	Credits           float64   `json:"credits"`
	CacheHit          bool      `json:"cache_hit"`
	AuthID            string    `json:"auth_id,omitempty"` // which authorization was charged
	SyncID            string    `json:"sync_id,omitempty"` // stable cross-node identity for HA replication dedup
	ServiceGroupID    string    `json:"service_group_id,omitempty"`
	WorkloadClass     string    `json:"workload_class,omitempty"`
	ClassSource       string    `json:"class_source,omitempty"`
	Preview           string    `json:"preview,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// TenantUsageSummary is an aggregated usage report.
type TenantUsageSummary struct {
	HubID             string  `json:"hub_id"`
	TenantID          string  `json:"tenant_id"`
	ServiceGroupID    string  `json:"service_group_id,omitempty"`
	Model             string  `json:"model,omitempty"`
	Period            string  `json:"period"`       // "daily" / "weekly" / "monthly"
	PeriodStart       string  `json:"period_start"` // "2026-01-01"
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	CachedInputTokens int64   `json:"cached_input_tokens"`
	CacheWriteTokens  int64   `json:"cache_write_tokens"`
	TotalCredits      float64 `json:"total_credits"`
	TotalRequests     int64   `json:"total_requests"`
	CacheHits         int64   `json:"cache_hits"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
}

// UsageReconciliationReport is the upstream fact a Hub uses to reconcile one
// tenant/day.  It is deliberately scoped by the authenticated Hub and tenant;
// provider-wide admin cards aggregate every Hub and are not a ledger source.
// ServiceGroups is the same day split by the catalog group HubCenter actually
// billed, so a Hub can line its official-route usage up against one group
// instead of only the tenant total.
type UsageReconciliationReport struct {
	HubID         string               `json:"hub_id"`
	TenantID      string               `json:"tenant_id"`
	Date          string               `json:"date"`
	Timezone      string               `json:"timezone"`
	Upstream      TenantUsageSummary   `json:"upstream"`
	ServiceGroups []TenantUsageSummary `json:"service_groups,omitempty"`
}

// TokenTraffic is input/output/total token volume for one window.
type TokenTraffic struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// ProviderPeriodTraffic is current day/week/month volume for one provider.
type ProviderPeriodTraffic struct {
	Day   TokenTraffic `json:"day"`
	Week  TokenTraffic `json:"week"`
	Month TokenTraffic `json:"month"`
}

// ProviderTrafficReport is the admin card payload for provider traffic.
type ProviderTrafficReport struct {
	Timezone string                           `json:"timezone"`
	Traffic  map[string]ProviderPeriodTraffic `json:"traffic"`
}

// UsageRepository persists usage records and provides aggregation queries.
type UsageRepository interface {
	Insert(ctx context.Context, record *TenantUsageRecord) error
	QuerySummary(ctx context.Context, filter UsageFilter) ([]TenantUsageSummary, error)
	QueryRecent(ctx context.Context, hubID, tenantID string, limit int) ([]*TenantUsageRecord, error)
	QueryProviderTraffic(ctx context.Context, dayStart, weekStart, monthStart time.Time) (map[string]ProviderPeriodTraffic, error)
	QueryServiceGroupTraffic(ctx context.Context, dayStart, weekStart, monthStart time.Time) (map[string]ProviderPeriodTraffic, error)
	QueryClassTraffic(ctx context.Context, serviceGroupID string, since time.Time) ([]ClassTrafficRow, map[string]int64, []ClassTrafficSample, error)
}

// UsageBatchRepository applies HA-replicated usage batches idempotently.
// applied reports whether this call actually inserted the records (false when
// the batch ID was already applied on this node).
type UsageBatchRepository interface {
	InsertBatch(ctx context.Context, batchID, sourceNodeID string, records []*TenantUsageRecord) (applied bool, err error)
}

type ClassTrafficRow struct {
	Class        string `json:"class"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

type ClassTrafficSample struct {
	At      time.Time `json:"at"`
	Class   string    `json:"class"`
	Source  string    `json:"source"`
	Preview string    `json:"preview,omitempty"`
}

// ServiceGroupTrafficReport contains compact day/week/month token totals for
// list cards. Detailed class sources and request previews stay in the traffic
// dialog endpoint.
type ServiceGroupTrafficReport struct {
	Timezone string                           `json:"timezone"`
	Traffic  map[string]ProviderPeriodTraffic `json:"traffic"`
}

// UsageFilter for querying aggregated stats.
type UsageFilter struct {
	HubID               string `json:"hub_id,omitempty"`
	TenantID            string `json:"tenant_id,omitempty"`
	Model               string `json:"model,omitempty"`
	ServiceGroupID      string `json:"service_group_id,omitempty"`
	WorkloadClass       string `json:"workload_class,omitempty"`
	Period              string `json:"period,omitempty"`     // "daily" / "weekly" / "monthly"
	StartDate           string `json:"start_date,omitempty"` // "2026-01-01"
	EndDate             string `json:"end_date,omitempty"`
	Timezone            string `json:"timezone,omitempty"`
	Limit               int    `json:"limit,omitempty"`
	GroupByServiceGroup bool   `json:"group_by_service_group,omitempty"`
}

// UsageRecorderImpl implements llmpool.UsageRecorder for HubCenter.
type UsageRecorderImpl struct {
	repo  UsageRepository
	hubID string // can be set per-request via context, or default

	sinkMu sync.RWMutex
	sink   func(*TenantUsageRecord)
}

// NewUsageRecorder creates a usage recorder backed by the given repository.
func NewUsageRecorder(repo UsageRepository) *UsageRecorderImpl {
	return &UsageRecorderImpl{repo: repo}
}

// SetSyncSink registers a hook invoked with every successfully persisted
// usage record. HubCenter uses it to feed the HA usage-batch replicator.
func (u *UsageRecorderImpl) SetSyncSink(sink func(*TenantUsageRecord)) {
	u.sinkMu.Lock()
	defer u.sinkMu.Unlock()
	u.sink = sink
}

// RecordUsage implements llmpool.UsageRecorder.
func (u *UsageRecorderImpl) RecordUsage(ctx context.Context, record *llmpool.UsageRecord) error {
	if u.repo == nil || record == nil {
		return nil
	}
	// Extract hub/tenant from context (set by proxy handler)
	hubID, tenantID := usageContextValues(ctx)
	stored := &TenantUsageRecord{
		HubID:             hubID,
		TenantID:          tenantID,
		RequestID:         record.RequestID,
		Model:             record.Model,
		ProviderID:        record.ProviderID,
		ServiceGroupID:    record.ServiceGroupID,
		WorkloadClass:     record.WorkloadClass,
		ClassSource:       record.ClassSource,
		Preview:           record.Preview,
		InputTokens:       record.InputTokens,
		OutputTokens:      record.OutputTokens,
		CachedInputTokens: record.CachedInputTokens,
		CacheWriteTokens:  record.CacheWriteTokens,
		CacheUsageSource:  record.CacheUsageSource,
		UsageAnomaly:      record.UsageAnomaly,
		PricingSource:     record.PricingSource,
		Credits:           record.Credits,
		CacheHit:          record.CacheHit,
		AuthID:            record.AuthID,
		CreatedAt:         record.Timestamp,
	}
	// Normalize the timestamp before persisting so the row and the copy handed
	// to the sync sink carry the same value; otherwise peers would stamp the
	// replicated row with their own apply time.
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now()
	}
	// The sync ID lets peers deduplicate at record level: a row republished by
	// a later seed pass (e.g. one that was still in the live buffer when this
	// node restarted) is ignored instead of counted twice.
	if stored.SyncID == "" {
		stored.SyncID = newUsageSyncID()
	}
	if err := u.repo.Insert(ctx, stored); err != nil {
		return err
	}
	u.sinkMu.RLock()
	sink := u.sink
	u.sinkMu.RUnlock()
	if sink != nil {
		sink(stored)
	}
	return nil
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

func newUsageSyncID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("t_%d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("u_%x", buf[:])
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

// QueryUsageReconciliation returns the exact calendar-day usage that this
// HubCenter persisted for one authenticated Hub tenant.
func (s *StatsService) QueryUsageReconciliation(ctx context.Context, hubID, tenantID, date, timezone string) (*UsageReconciliationReport, error) {
	loc := TrafficLocation(timezone)
	date = strings.TrimSpace(date)
	if _, err := time.ParseInLocation("2006-01-02", date, loc); err != nil {
		date = time.Now().In(loc).Format("2006-01-02")
	}
	report := &UsageReconciliationReport{
		HubID:    strings.TrimSpace(hubID),
		TenantID: strings.TrimSpace(tenantID),
		Date:     date,
		Timezone: loc.String(),
		Upstream: TenantUsageSummary{
			HubID:       strings.TrimSpace(hubID),
			TenantID:    strings.TrimSpace(tenantID),
			Period:      "daily",
			PeriodStart: date,
		},
	}
	if s == nil || s.repo == nil || report.HubID == "" || report.TenantID == "" {
		return report, nil
	}
	groups, err := s.repo.QuerySummary(ctx, UsageFilter{
		HubID: report.HubID, TenantID: report.TenantID,
		Period: "daily", StartDate: date, EndDate: date, Timezone: loc.String(),
		GroupByServiceGroup: true,
	})
	if err != nil {
		return nil, err
	}
	report.ServiceGroups = make([]TenantUsageSummary, 0, len(groups))
	index := make(map[string]int, len(groups))
	for _, row := range groups {
		if !strings.EqualFold(strings.TrimSpace(row.HubID), report.HubID) || !strings.EqualFold(strings.TrimSpace(row.TenantID), report.TenantID) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(row.ServiceGroupID))
		if i, ok := index[key]; ok {
			addTenantUsageSummary(&report.ServiceGroups[i], row)
			addTenantUsageSummary(&report.Upstream, row)
			continue
		}
		report.ServiceGroups = append(report.ServiceGroups, row)
		index[key] = len(report.ServiceGroups) - 1
		addTenantUsageSummary(&report.Upstream, row)
	}
	if len(report.ServiceGroups) == 0 {
		report.ServiceGroups = nil
	} else {
		sort.Slice(report.ServiceGroups, func(i, j int) bool {
			return report.ServiceGroups[i].ServiceGroupID < report.ServiceGroups[j].ServiceGroupID
		})
	}
	return report, nil
}

func addTenantUsageSummary(dst *TenantUsageSummary, src TenantUsageSummary) {
	if dst == nil {
		return
	}
	if dst.Period == "" {
		dst.Period = src.Period
	}
	if dst.PeriodStart == "" {
		dst.PeriodStart = src.PeriodStart
	}
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CachedInputTokens += src.CachedInputTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.TotalCredits += src.TotalCredits
	dst.TotalRequests += src.TotalRequests
	dst.CacheHits += src.CacheHits
	if dst.TotalRequests > 0 {
		dst.CacheHitRate = float64(dst.CacheHits) / float64(dst.TotalRequests)
	}
}

// QueryRecentRecords returns recent usage records for a hub+tenant.
func (s *StatsService) QueryRecentRecords(ctx context.Context, hubID, tenantID string, limit int) ([]*TenantUsageRecord, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.QueryRecent(ctx, hubID, tenantID, limit)
}

// QueryProviderTraffic returns current day/week/month token volume per provider.
func (s *StatsService) QueryProviderTraffic(ctx context.Context, timezone string, now time.Time) (*ProviderTrafficReport, error) {
	loc := TrafficLocation(timezone)
	if now.IsZero() {
		now = time.Now()
	}
	dayStart, weekStart, monthStart := ProviderTrafficBounds(now, loc)
	report := &ProviderTrafficReport{
		Timezone: loc.String(),
		Traffic:  map[string]ProviderPeriodTraffic{},
	}
	if s.repo == nil {
		return report, nil
	}
	traffic, err := s.repo.QueryProviderTraffic(ctx, dayStart, weekStart, monthStart)
	if err != nil {
		return nil, err
	}
	if traffic != nil {
		report.Traffic = traffic
	}
	return report, nil
}

// QueryServiceGroupTraffic returns current day/week/month token totals for all
// service groups in one repository pass.
func (s *StatsService) QueryServiceGroupTraffic(ctx context.Context, timezone string, now time.Time) (*ServiceGroupTrafficReport, error) {
	loc := TrafficLocation(timezone)
	if now.IsZero() {
		now = time.Now()
	}
	dayStart, weekStart, monthStart := ProviderTrafficBounds(now, loc)
	report := &ServiceGroupTrafficReport{
		Timezone: loc.String(),
		Traffic:  map[string]ProviderPeriodTraffic{},
	}
	if s.repo == nil {
		return report, nil
	}
	traffic, err := s.repo.QueryServiceGroupTraffic(ctx, dayStart, weekStart, monthStart)
	if err != nil {
		return nil, err
	}
	if traffic != nil {
		report.Traffic = traffic
	}
	return report, nil
}

func (s *StatsService) QueryClassTraffic(ctx context.Context, serviceGroupID string, since time.Time) ([]ClassTrafficRow, map[string]int64, []ClassTrafficSample, error) {
	if s.repo == nil {
		return NormalizeClassTrafficRows(nil), map[string]int64{}, nil, nil
	}
	rows, sources, samples, err := s.repo.QueryClassTraffic(ctx, serviceGroupID, since)
	if err != nil {
		return nil, nil, nil, err
	}
	if sources == nil {
		sources = map[string]int64{}
	}
	if samples == nil {
		samples = []ClassTrafficSample{}
	}
	return NormalizeClassTrafficRows(rows), sources, samples, nil
}

func NormalizeClassTrafficRows(rows []ClassTrafficRow) []ClassTrafficRow {
	byClass := map[string]ClassTrafficRow{}
	for _, row := range rows {
		class := strings.TrimSpace(row.Class)
		if class == "" {
			class = "unclassified"
		}
		curr := byClass[class]
		curr.Class = class
		curr.Requests += row.Requests
		curr.InputTokens += row.InputTokens
		curr.OutputTokens += row.OutputTokens
		curr.TotalTokens += row.TotalTokens
		byClass[class] = curr
	}
	out := make([]ClassTrafficRow, 0, len(llmpool.FrozenWorkloadClasses)+3)
	total := ClassTrafficRow{Class: "total"}
	appendRow := func(class string) {
		row := byClass[class]
		row.Class = class
		out = append(out, row)
		total.Requests += row.Requests
		total.InputTokens += row.InputTokens
		total.OutputTokens += row.OutputTokens
		total.TotalTokens += row.TotalTokens
	}
	for _, class := range llmpool.FrozenWorkloadClasses {
		appendRow(class)
	}
	appendRow(llmpool.WorkloadFallbackBalanced)
	appendRow(llmpool.WorkloadUnclassified)
	out = append(out, total)
	return out
}

// TrafficLocation resolves an IANA timezone for token-traffic windows.
// Empty or unknown names fall back to Asia/Shanghai, matching the admin UI.
func TrafficLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Asia/Shanghai"
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Shanghai", 8*3600)
}

// ClassTrafficSince maps the admin window switch onto the same local
// day/week/month starts as the service-group traffic cards. Legacy 24h/7d/30d
// query values are aliases so old URLs keep the same calendar windows.
func ClassTrafficSince(window string, loc *time.Location, now time.Time) (time.Time, string) {
	if loc == nil {
		loc = TrafficLocation("")
	}
	if now.IsZero() {
		now = time.Now()
	}
	dayStart, weekStart, monthStart := ProviderTrafficBounds(now, loc)
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "week", "7d":
		return weekStart, "week"
	case "month", "30d":
		return monthStart, "month"
	default: // "day", "24h", empty, or unknown
		return dayStart, "day"
	}
}

// ProviderTrafficBounds returns local day/week/month starts. Weeks start on Monday.
func ProviderTrafficBounds(now time.Time, loc *time.Location) (dayStart, weekStart, monthStart time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	dayStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekStart = dayStart.AddDate(0, 0, -(weekday - 1))
	monthStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	return
}
