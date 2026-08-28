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
	metricSyncBytesUp     atomic.Uint64
	metricSyncBytesDown   atomic.Uint64
	metricQuotaRejections atomic.Uint64
	metricLeaseConflicts  atomic.Uint64
)

// Metrics is GET /api/admin/cloud-workspaces/metrics (JSON, not Prometheus).
type Metrics struct {
	TenantsEnabled  int64  `json:"tenants_enabled"`
	OpenLeases      int64  `json:"open_leases"`
	SyncBytesUp     uint64 `json:"sync_bytes_up"`
	SyncBytesDown   uint64 `json:"sync_bytes_down"`
	QuotaRejections uint64 `json:"quota_rejections"`
	LeaseConflicts  uint64 `json:"lease_conflicts"`
	UsedBytes       int64  `json:"used_bytes"`
	VolumeFreeBytes int64  `json:"volume_free_bytes"`
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

func ObserveLeaseConflict() {
	metricLeaseConflicts.Add(1)
}

func resetMetricsForTest() {
	metricSyncBytesUp.Store(0)
	metricSyncBytesDown.Store(0)
	metricQuotaRejections.Store(0)
	metricLeaseConflicts.Store(0)
}

// CollectMetrics returns process counters plus live DB/volume gauges.
func (s *Service) CollectMetrics(ctx context.Context) Metrics {
	out := Metrics{
		SyncBytesUp:     metricSyncBytesUp.Load(),
		SyncBytesDown:   metricSyncBytesDown.Load(),
		QuotaRejections: metricQuotaRejections.Load(),
		LeaseConflicts:  metricLeaseConflicts.Load(),
	}
	if s == nil {
		return out
	}
	out.TenantsEnabled = s.countEnabledTenants(ctx)
	if s.Workspaces != nil {
		if n, err := s.Workspaces.CountOpenLeases(ctx, s.now()); err == nil {
			out.OpenLeases = n
		}
		if n, err := s.Workspaces.SumUsedBytes(ctx); err == nil {
			out.UsedBytes = n
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
