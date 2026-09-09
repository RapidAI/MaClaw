package cloudworkspace

import (
	"context"
	"os"
	"strings"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var (
	metricSyncBytesUp         atomic.Uint64
	metricSyncBytesDown       atomic.Uint64
	metricQuotaRejections     atomic.Uint64
	metricLeaseConflicts      atomic.Uint64
	metricGCFailures          atomic.Uint64
	metricBandwidthRejections atomic.Uint64
)

// Metrics is GET /api/admin/cloud-workspaces/metrics (JSON, not Prometheus).
type Metrics struct {
	TenantsEnabled      int64  `json:"tenants_enabled"`
	OpenLeases          int64  `json:"open_leases"`
	SyncBytesUp         uint64 `json:"sync_bytes_up"`
	SyncBytesDown       uint64 `json:"sync_bytes_down"`
	QuotaRejections     uint64 `json:"quota_rejections"`
	LeaseConflicts      uint64 `json:"lease_conflicts"`
	UsedBytes           int64  `json:"used_bytes"`
	LogicalBytes        int64  `json:"logical_bytes"`
	RetainedBytes       int64  `json:"retained_bytes"`
	SnapshotBytes       int64  `json:"snapshot_retained_bytes"`
	StagingBytes        int64  `json:"staging_bytes"`
	UnreferencedBytes   int64  `json:"unreferenced_retained_bytes"`
	VolumeFreeBytes     int64  `json:"volume_free_bytes"`
	AuditEvents         int64  `json:"audit_events"`
	GCFailures          uint64 `json:"gc_failures"`
	// BandwidthRejections counts process-lifetime 429 admissions; the window
	// gauges are the durable current-window bytes for the metrics scope.
	BandwidthRejections     uint64 `json:"bandwidth_rejections"`
	BandwidthWindowBytesUp  int64  `json:"bandwidth_window_bytes_up"`
	BandwidthWindowBytesDown int64 `json:"bandwidth_window_bytes_down"`
}

func ObserveSyncBytesUp(n int64) {
	if n > 0 {
		metricSyncBytesUp.Add(uint64(n))
	}
}

func ObserveSyncBytesDown(n int64) {
	if n > 0 {
		metricSyncBytesDown.Add(uint64(n))
	}
}

func ObserveQuotaRejection() {
	metricQuotaRejections.Add(1)
}

// ObserveBandwidthRejection counts hourly-quota 429 admissions separately
// from storage quota rejections so operators can alert on each independently.
func ObserveBandwidthRejection() {
	metricBandwidthRejections.Add(1)
}

func ObserveLeaseConflict() {
	metricLeaseConflicts.Add(1)
}

func resetMetricsForTest() {
	metricSyncBytesUp.Store(0)
	metricSyncBytesDown.Store(0)
	metricQuotaRejections.Store(0)
	metricLeaseConflicts.Store(0)
	metricGCFailures.Store(0)
	metricBandwidthRejections.Store(0)
}

// CollectMetrics returns process counters plus live DB/volume gauges.
func (s *Service) CollectMetrics(ctx context.Context) Metrics {
	out := Metrics{
		SyncBytesUp:         metricSyncBytesUp.Load(),
		SyncBytesDown:       metricSyncBytesDown.Load(),
		QuotaRejections:     metricQuotaRejections.Load(),
		LeaseConflicts:      metricLeaseConflicts.Load(),
		GCFailures:          metricGCFailures.Load(),
		BandwidthRejections: metricBandwidthRejections.Load(),
	}
	if s == nil {
		return out
	}
	out.TenantsEnabled = s.countEnabledTenants(ctx)
	if s.Workspaces != nil {
		if up, down, err := s.Workspaces.BandwidthWindowUsage(ctx, "", s.now()); err == nil {
			out.BandwidthWindowBytesUp = up
			out.BandwidthWindowBytesDown = down
		}
		if n, err := s.Workspaces.CountOpenLeases(ctx, s.now()); err == nil {
			out.OpenLeases = n
		}
		if n, err := s.Workspaces.SumUsedBytes(ctx); err == nil {
			out.UsedBytes = n
		}
		if usage, err := s.Workspaces.TotalRetainedUsage(ctx); err == nil {
			out.LogicalBytes = usage.LogicalBytes
			out.RetainedBytes = usage.RetainedBytes
			out.SnapshotBytes = usage.SnapshotRetainedBytes
			out.StagingBytes = usage.StagingBytes
			out.UnreferencedBytes = usage.UnreferencedRetainedBytes
		}
		if n, err := s.Workspaces.CountAuditRowsAll(ctx); err == nil {
			out.AuditEvents = n
		}
	}
	if s.Blobs != nil {
		path := strings.TrimSpace(s.Blobs.Root)
		if path == "" {
			path = os.TempDir()
		}
		if n, err := archiveutil.AvailableBytes(path); err == nil {
			out.VolumeFreeBytes = n
		}
	}
	return out
}

// CollectMetricsForTenant is the tenant-scoped projection used by admin HTTP
// handlers. Process counters and physical free space are installation-wide;
// all workspace, lease, audit and purge gauges are filtered by tenant.
func (s *Service) CollectMetricsForTenant(ctx context.Context, tenantID string) Metrics {
	out := Metrics{
		SyncBytesUp:         metricSyncBytesUp.Load(),
		SyncBytesDown:       metricSyncBytesDown.Load(),
		QuotaRejections:     metricQuotaRejections.Load(),
		LeaseConflicts:      metricLeaseConflicts.Load(),
		GCFailures:          metricGCFailures.Load(),
		BandwidthRejections: metricBandwidthRejections.Load(),
	}
	if s == nil {
		return out
	}
	if strings.TrimSpace(tenantID) == "" {
		// Never let an omitted tenant silently normalize to the default tenant.
		// Callers that need installation-wide numbers must use CollectMetrics.
		return out
	}
	tenantID = store.NormalizeTenantID(tenantID)
	settings := s.LoadTenantSettings(ctx, tenantID)
	if settings.Mode == ModeAllUsers || settings.Mode == ModeDepartments {
		out.TenantsEnabled = 1
	}
	if s.Workspaces != nil {
		if up, down, err := s.Workspaces.BandwidthWindowUsage(ctx, tenantID, s.now()); err == nil {
			out.BandwidthWindowBytesUp = up
			out.BandwidthWindowBytesDown = down
		}
		if n, err := s.Workspaces.CountOpenLeasesForTenant(ctx, tenantID, s.now()); err == nil {
			out.OpenLeases = n
		}
		if n, err := s.Workspaces.SumUsedBytesForTenant(ctx, tenantID); err == nil {
			out.UsedBytes = n
		}
		if usage, err := s.Workspaces.TenantRetainedUsage(ctx, tenantID); err == nil {
			out.LogicalBytes = usage.LogicalBytes
			out.RetainedBytes = usage.RetainedBytes
			out.SnapshotBytes = usage.SnapshotRetainedBytes
			out.StagingBytes = usage.StagingBytes
			out.UnreferencedBytes = usage.UnreferencedRetainedBytes
		}
		if n, err := s.Workspaces.CountAuditRows(ctx, tenantID); err == nil {
			out.AuditEvents = n
		}
	}
	if s.Blobs != nil {
		path := strings.TrimSpace(s.Blobs.Root)
		if path == "" {
			path = os.TempDir()
		}
		if n, err := archiveutil.AvailableBytes(path); err == nil {
			out.VolumeFreeBytes = n
		}
	}
	return out
}

func (s *Service) countEnabledTenants(ctx context.Context) int64 {
	ids := map[string]struct{}{store.DefaultTenantID: {}}
	if s.Workspaces != nil {
		if got, err := s.Workspaces.ListSettingTenantIDs(ctx); err == nil {
			for _, id := range got {
				ids[id] = struct{}{}
			}
		}
		if got, err := s.Workspaces.ListDistinctTenantIDs(ctx); err == nil {
			for _, id := range got {
				ids[id] = struct{}{}
			}
		}
	}
	var n int64
	for id := range ids {
		settings := s.LoadTenantSettings(ctx, id)
		if settings.Mode == ModeAllUsers || settings.Mode == ModeDepartments {
			n++
		}
	}
	return n
}
