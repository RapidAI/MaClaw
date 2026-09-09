package agentruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RuntimeMetrics is the transport-neutral, bounded-cardinality metrics
// collector shared by GUI, MaClawSrv and TUI. It intentionally stores only
// counters and timing aggregates; request payloads, principal identifiers and
// tool arguments never enter the collector.
//
// Tenant counters are keyed by a short SHA-256 digest rather than the raw
// tenant id. This keeps Prometheus labels useful for operations while
// preventing accidental disclosure of customer identifiers. The map is
// bounded; once the limit is reached, new tenants are accounted for in the
// global counters but do not create unbounded label cardinality.
type RuntimeMetrics struct {
	global runtimeMetricCounters

	mu          sync.RWMutex
	tenantLimit int
	tenants     map[string]*runtimeMetricCounters
	tenantDrops map[string]struct{}
	tenantsDrop atomic.Uint64
}

// RuntimeMetricsSnapshot is a stable read-only representation suitable for
// transport adapters and tests. Duration values are aggregate milliseconds;
// callers can derive averages without exposing per-request timings.
type RuntimeMetricsSnapshot struct {
	TurnsAdmitted           uint64                         `json:"turns_admitted"`
	TurnsSucceeded          uint64                         `json:"turns_succeeded"`
	TurnsFailed             uint64                         `json:"turns_failed"`
	TurnsCancelled          uint64                         `json:"turns_cancelled"`
	ActiveRuns              int64                          `json:"active_runs"`
	MaxActiveRuns           uint64                         `json:"max_active_runs"`
	QueueWaitMilliseconds   uint64                         `json:"queue_wait_milliseconds"`
	QueueWaitSamples        uint64                         `json:"queue_wait_samples"`
	FirstTokenMilliseconds  uint64                         `json:"first_token_milliseconds"`
	FirstTokenSamples       uint64                         `json:"first_token_samples"`
	TokenDeltas             uint64                         `json:"token_deltas"`
	ToolCalls               uint64                         `json:"tool_calls"`
	ToolErrors              uint64                         `json:"tool_errors"`
	EventsEmitted           uint64                         `json:"events_emitted"`
	EventsDropped           uint64                         `json:"events_dropped"`
	EventsReplayed          uint64                         `json:"events_replayed"`
	QuotaRejected           uint64                         `json:"quota_rejected"`
	RateLimited             uint64                         `json:"rate_limited"`
	RateLimitBucketsEvicted uint64                         `json:"rate_limit_buckets_evicted"`
	TenantSeriesDropped     uint64                         `json:"tenant_series_dropped"`
	Tenants                 []TenantRuntimeMetricsSnapshot `json:"tenants,omitempty"`
}

// TenantRuntimeMetricsSnapshot is keyed by a deterministic non-reversible
// tenant hash. TenantHash is intentionally not the raw tenant id.
type TenantRuntimeMetricsSnapshot struct {
	TenantHash             string `json:"tenant_hash"`
	TurnsAdmitted          uint64 `json:"turns_admitted"`
	TurnsSucceeded         uint64 `json:"turns_succeeded"`
	TurnsFailed            uint64 `json:"turns_failed"`
	TurnsCancelled         uint64 `json:"turns_cancelled"`
	ActiveRuns             int64  `json:"active_runs"`
	MaxActiveRuns          uint64 `json:"max_active_runs"`
	QueueWaitMilliseconds  uint64 `json:"queue_wait_milliseconds"`
	QueueWaitSamples       uint64 `json:"queue_wait_samples"`
	FirstTokenMilliseconds uint64 `json:"first_token_milliseconds"`
	FirstTokenSamples      uint64 `json:"first_token_samples"`
	TokenDeltas            uint64 `json:"token_deltas"`
	ToolCalls              uint64 `json:"tool_calls"`
	ToolErrors             uint64 `json:"tool_errors"`
	EventsEmitted          uint64 `json:"events_emitted"`
	EventsDropped          uint64 `json:"events_dropped"`
	EventsReplayed         uint64 `json:"events_replayed"`
	QuotaRejected          uint64 `json:"quota_rejected"`
	RateLimited            uint64 `json:"rate_limited"`
}

type runtimeMetricCounters struct {
	turnsAdmitted           atomic.Uint64
	turnsSucceeded          atomic.Uint64
	turnsFailed             atomic.Uint64
	turnsCancelled          atomic.Uint64
	activeRuns              atomic.Int64
	maxActiveRuns           atomic.Uint64
	queueWaitMilliseconds   atomic.Uint64
	queueWaitSamples        atomic.Uint64
	firstTokenMilliseconds  atomic.Uint64
	firstTokenSamples       atomic.Uint64
	tokenDeltas             atomic.Uint64
	toolCalls               atomic.Uint64
	toolErrors              atomic.Uint64
	eventsEmitted           atomic.Uint64
	eventsDropped           atomic.Uint64
	eventsReplayed          atomic.Uint64
	quotaRejected           atomic.Uint64
	rateLimited             atomic.Uint64
	rateLimitBucketsEvicted atomic.Uint64
}

const defaultRuntimeMetricsTenantLimit = 1024

// NewRuntimeMetrics creates a collector with a bounded tenant series map.
// Non-positive limits use a conservative default.
func NewRuntimeMetrics(tenantLimit int) *RuntimeMetrics {
	if tenantLimit <= 0 {
		tenantLimit = defaultRuntimeMetricsTenantLimit
	}
	return &RuntimeMetrics{tenantLimit: tenantLimit, tenants: make(map[string]*runtimeMetricCounters), tenantDrops: make(map[string]struct{})}
}

func (m *RuntimeMetrics) tenantCounters(tenantID string) *runtimeMetricCounters {
	if m == nil {
		return nil
	}
	hash := TenantMetricsHash(tenantID)
	if hash == "" {
		return nil
	}
	m.mu.RLock()
	counters := m.tenants[hash]
	m.mu.RUnlock()
	if counters != nil {
		return counters
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if counters = m.tenants[hash]; counters != nil {
		return counters
	}
	if len(m.tenants) >= m.tenantLimit {
		if _, alreadyDropped := m.tenantDrops[hash]; !alreadyDropped {
			m.tenantDrops[hash] = struct{}{}
			m.tenantsDrop.Add(1)
		}
		return nil
	}
	counters = &runtimeMetricCounters{}
	m.tenants[hash] = counters
	return counters
}

func withTenant(m *RuntimeMetrics, tenantID string, fn func(*runtimeMetricCounters)) {
	if m == nil || fn == nil {
		return
	}
	fn(&m.global)
	if tenant := m.tenantCounters(tenantID); tenant != nil {
		fn(tenant)
	}
}

// TenantMetricsHash returns the stable, non-reversible label value used by
// tenant-level metrics. Empty tenant ids intentionally produce an empty hash.
func TenantMetricsHash(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(tenantID))
	return hex.EncodeToString(digest[:])[:16]
}

func (m *RuntimeMetrics) RecordTurnAdmitted(tenantID string, queueWait time.Duration) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) {
		c.turnsAdmitted.Add(1)
		active := c.activeRuns.Add(1)
		for {
			max := c.maxActiveRuns.Load()
			if active <= int64(max) || c.maxActiveRuns.CompareAndSwap(max, uint64(active)) {
				break
			}
		}
		if queueWait >= 0 {
			c.queueWaitMilliseconds.Add(uint64(queueWait / time.Millisecond))
			c.queueWaitSamples.Add(1)
		}
	})
}

// RecordTurnFinished must be called exactly once for every admitted run.
func (m *RuntimeMetrics) RecordTurnFinished(tenantID, status string) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) {
		if c.activeRuns.Load() > 0 {
			c.activeRuns.Add(-1)
		}
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "succeeded", "success", "completed":
			c.turnsSucceeded.Add(1)
		case "cancelled", "canceled":
			c.turnsCancelled.Add(1)
		default:
			c.turnsFailed.Add(1)
		}
	})
}

func (m *RuntimeMetrics) RecordToken(tenantID string, firstTokenLatency time.Duration, first bool) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) {
		c.tokenDeltas.Add(1)
		if first && firstTokenLatency >= 0 {
			c.firstTokenMilliseconds.Add(uint64(firstTokenLatency / time.Millisecond))
			c.firstTokenSamples.Add(1)
		}
	})
}

func (m *RuntimeMetrics) RecordToolCall(tenantID string, failed bool) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) {
		c.toolCalls.Add(1)
		if failed {
			c.toolErrors.Add(1)
		}
	})
}

// RecordToolError records an error for a tool invocation that was already
// counted by RecordToolCall. It exists for event streams where the error is
// only known when the tool.result event arrives.
func (m *RuntimeMetrics) RecordToolError(tenantID string) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) { c.toolErrors.Add(1) })
}

func (m *RuntimeMetrics) RecordEvent(tenantID string, emitted, replayed bool) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) {
		if emitted {
			c.eventsEmitted.Add(1)
		} else {
			c.eventsDropped.Add(1)
		}
		if replayed {
			c.eventsReplayed.Add(1)
		}
	})
}

func (m *RuntimeMetrics) RecordQuotaRejected(tenantID string) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) { c.quotaRejected.Add(1) })
}

// RecordRateLimited counts admission requests rejected by the shared
// tenant-level token bucket. It is separate from durable quota rejection so
// operators can distinguish a short-term burst from a persistent resource
// limit.
func (m *RuntimeMetrics) RecordRateLimited(tenantID string) {
	withTenant(m, tenantID, func(c *runtimeMetricCounters) { c.rateLimited.Add(1) })
}

// RecordRateLimitBucketEvicted records a bounded-cardinality eviction when a
// new tenant arrives after the local token-bucket map is full.
func (m *RuntimeMetrics) RecordRateLimitBucketEvicted() {
	if m == nil {
		return
	}
	m.global.rateLimitBucketsEvicted.Add(1)
}

func snapshotMetricCounters(c *runtimeMetricCounters) TenantRuntimeMetricsSnapshot {
	if c == nil {
		return TenantRuntimeMetricsSnapshot{}
	}
	return TenantRuntimeMetricsSnapshot{
		TurnsAdmitted: c.turnsAdmitted.Load(), TurnsSucceeded: c.turnsSucceeded.Load(),
		TurnsFailed: c.turnsFailed.Load(), TurnsCancelled: c.turnsCancelled.Load(),
		ActiveRuns: c.activeRuns.Load(), MaxActiveRuns: c.maxActiveRuns.Load(),
		QueueWaitMilliseconds: c.queueWaitMilliseconds.Load(), QueueWaitSamples: c.queueWaitSamples.Load(),
		FirstTokenMilliseconds: c.firstTokenMilliseconds.Load(), FirstTokenSamples: c.firstTokenSamples.Load(),
		TokenDeltas: c.tokenDeltas.Load(), ToolCalls: c.toolCalls.Load(), ToolErrors: c.toolErrors.Load(),
		EventsEmitted: c.eventsEmitted.Load(), EventsDropped: c.eventsDropped.Load(), EventsReplayed: c.eventsReplayed.Load(),
		QuotaRejected: c.quotaRejected.Load(), RateLimited: c.rateLimited.Load(),
	}
}

// Snapshot takes a lock only while copying the bounded tenant pointer map;
// individual counters remain lock-free and may be updated concurrently.
func (m *RuntimeMetrics) Snapshot() RuntimeMetricsSnapshot {
	if m == nil {
		return RuntimeMetricsSnapshot{}
	}
	global := snapshotMetricCounters(&m.global)
	out := RuntimeMetricsSnapshot{
		TurnsAdmitted: global.TurnsAdmitted, TurnsSucceeded: global.TurnsSucceeded,
		TurnsFailed: global.TurnsFailed, TurnsCancelled: global.TurnsCancelled,
		ActiveRuns: global.ActiveRuns, MaxActiveRuns: global.MaxActiveRuns,
		QueueWaitMilliseconds: global.QueueWaitMilliseconds, QueueWaitSamples: global.QueueWaitSamples,
		FirstTokenMilliseconds: global.FirstTokenMilliseconds, FirstTokenSamples: global.FirstTokenSamples,
		TokenDeltas: global.TokenDeltas, ToolCalls: global.ToolCalls, ToolErrors: global.ToolErrors,
		EventsEmitted: global.EventsEmitted, EventsDropped: global.EventsDropped, EventsReplayed: global.EventsReplayed,
		QuotaRejected: global.QuotaRejected, RateLimited: global.RateLimited, RateLimitBucketsEvicted: m.global.rateLimitBucketsEvicted.Load(), TenantSeriesDropped: m.tenantsDrop.Load(),
	}
	m.mu.RLock()
	out.Tenants = make([]TenantRuntimeMetricsSnapshot, 0, len(m.tenants))
	for hash, counters := range m.tenants {
		snapshot := snapshotMetricCounters(counters)
		snapshot.TenantHash = hash
		out.Tenants = append(out.Tenants, snapshot)
	}
	m.mu.RUnlock()
	sort.Slice(out.Tenants, func(i, j int) bool { return out.Tenants[i].TenantHash < out.Tenants[j].TenantHash })
	return out
}
