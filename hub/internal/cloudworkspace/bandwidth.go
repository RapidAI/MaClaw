package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// ErrBandwidthLimit reports that the tenant-configured hourly transfer quota
// would be exceeded. The concrete *BandwidthLimitError carries the retry
// window so HTTP and client layers can surface a Retry-After contract.
var ErrBandwidthLimit = errors.New("cloud workspace bandwidth limit exceeded")

const (
	// bandwidthWindow is the fixed tumbling quota window. Usage rows are keyed
	// by the UTC hour start, so every device and every Hub process sharing one
	// database converges on the same counters without a background roller.
	bandwidthWindow = time.Hour
	// bandwidthRetain bounds how long past windows are kept for diagnostics.
	// Pruning runs lazily inside admission transactions, never as a sweeper.
	bandwidthRetain = 48 * time.Hour
)

// BandwidthLimitError is a fail-closed admission rejection. Scope is "user" or
// "tenant"; Direction is "up" or "down".
type BandwidthLimitError struct {
	Scope             string
	Direction         string
	Limit             int64
	Used              int64
	Delta             int64
	RetryAfterSeconds int64
}

func (e *BandwidthLimitError) Error() string {
	return fmt.Sprintf("%s: %s %s limit %d bytes/hour (used %d, requested %d, retry after %ds)",
		ErrBandwidthLimit, e.Scope, e.Direction, e.Limit, e.Used, e.Delta, e.RetryAfterSeconds)
}

// Is lets errors.Is(err, ErrBandwidthLimit) match the wrapped detail.
func (e *BandwidthLimitError) Is(target error) bool { return target == ErrBandwidthLimit }

func bandwidthWindowStart(now time.Time) time.Time {
	return now.UTC().Truncate(bandwidthWindow)
}

func bandwidthRetryAfterSeconds(now time.Time) int64 {
	remain := bandwidthWindowStart(now).Add(bandwidthWindow).Sub(now.UTC())
	secs := int64(remain / time.Second)
	if secs < 1 {
		secs = 1
	}
	return secs
}

// admitBandwidthDelta must be called inside a BEGIN IMMEDIATE write
// transaction so the read-check-increment sequence is serialized across every
// Hub process sharing this database. A zero limit disables that level; a
// zero/negative delta stays admissible. Failed or aborted uploads are not
// refunded: the bytes crossed the wire, and the conservative direction keeps
// retry loops from amplifying transfer.
func admitBandwidthDelta(ctx context.Context, q queryer, tenantID, userID string, up, down int64, userLimit, tenantLimit int64, now time.Time) error {
	if up <= 0 && down <= 0 {
		return nil
	}
	if userLimit <= 0 && tenantLimit <= 0 {
		return nil
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrNotFound
	}
	windowStart := bandwidthWindowStart(now).Format(time.RFC3339)

	var usedUp, usedDown int64
	err := q.QueryRowContext(ctx, `
		SELECT bytes_up, bytes_down FROM cloud_workspace_bandwidth_usage
		 WHERE tenant_id = ? AND user_id = ? AND window_start = ?`,
		tenantID, userID, windowStart).Scan(&usedUp, &usedDown)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if userLimit > 0 {
		if up > 0 && usedUp+up > userLimit {
			return &BandwidthLimitError{Scope: "user", Direction: "up", Limit: userLimit, Used: usedUp, Delta: up, RetryAfterSeconds: bandwidthRetryAfterSeconds(now)}
		}
		if down > 0 && usedDown+down > userLimit {
			return &BandwidthLimitError{Scope: "user", Direction: "down", Limit: userLimit, Used: usedDown, Delta: down, RetryAfterSeconds: bandwidthRetryAfterSeconds(now)}
		}
	}
	if tenantLimit > 0 {
		var tenantUp, tenantDown int64
		if err := q.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(bytes_up), 0), COALESCE(SUM(bytes_down), 0)
			  FROM cloud_workspace_bandwidth_usage
			 WHERE tenant_id = ? AND window_start = ?`,
			tenantID, windowStart).Scan(&tenantUp, &tenantDown); err != nil {
			return err
		}
		if up > 0 && tenantUp+up > tenantLimit {
			return &BandwidthLimitError{Scope: "tenant", Direction: "up", Limit: tenantLimit, Used: tenantUp, Delta: up, RetryAfterSeconds: bandwidthRetryAfterSeconds(now)}
		}
		if down > 0 && tenantDown+down > tenantLimit {
			return &BandwidthLimitError{Scope: "tenant", Direction: "down", Limit: tenantLimit, Used: tenantDown, Delta: down, RetryAfterSeconds: bandwidthRetryAfterSeconds(now)}
		}
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO cloud_workspace_bandwidth_usage (tenant_id, user_id, window_start, bytes_up, bytes_down)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, user_id, window_start) DO UPDATE SET
			bytes_up = bytes_up + excluded.bytes_up,
			bytes_down = bytes_down + excluded.bytes_down`,
		tenantID, userID, windowStart, up, down); err != nil {
		return err
	}
	// window_start is RFC3339 UTC, so lexical order matches chronological order.
	cutoff := now.UTC().Add(-bandwidthRetain).Truncate(bandwidthWindow).Format(time.RFC3339)
	_, err = q.ExecContext(ctx, `DELETE FROM cloud_workspace_bandwidth_usage WHERE window_start < ?`, cutoff)
	return err
}

// AdmitBandwidth durably charges one transfer against the tenant-configured
// hourly windows. Hub processes sharing the same SQLite database serialize on
// BEGIN IMMEDIATE; separate databases do not share counters.
func (s *Store) AdmitBandwidth(ctx context.Context, tenantID, userID string, up, down, userLimit, tenantLimit int64, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	return s.withImmediate(ctx, func(q queryer) error {
		return admitBandwidthDelta(ctx, q, tenantID, userID, up, down, userLimit, tenantLimit, now)
	})
}

// BandwidthWindowUsage sums the current quota window for one tenant. An empty
// tenantID reports the installation-wide total for global metrics; callers
// must never pass an unauthenticated or defaulted tenant here.
func (s *Store) BandwidthWindowUsage(ctx context.Context, tenantID string, now time.Time) (up, down int64, err error) {
	if s == nil || s.db == nil {
		return 0, 0, ErrUnavailable
	}
	windowStart := bandwidthWindowStart(now).Format(time.RFC3339)
	if strings.TrimSpace(tenantID) == "" {
		err = s.db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(bytes_up), 0), COALESCE(SUM(bytes_down), 0)
			  FROM cloud_workspace_bandwidth_usage WHERE window_start = ?`, windowStart).Scan(&up, &down)
		return up, down, err
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(bytes_up), 0), COALESCE(SUM(bytes_down), 0)
		  FROM cloud_workspace_bandwidth_usage WHERE tenant_id = ? AND window_start = ?`,
		store.NormalizeTenantID(tenantID), windowStart).Scan(&up, &down)
	return up, down, err
}

// bandwidthLimited reports whether the tenant configured any hourly transfer
// quota. When disabled (the default), callers skip pre-flight metadata reads.
func (s *Service) bandwidthLimited(ctx context.Context, tenantID string) bool {
	settings := s.LoadTenantSettings(ctx, tenantID)
	return settings.BandwidthUserBytesPerHour > 0 || settings.BandwidthTenantBytesPerHour > 0
}

// admitBandwidth charges a transfer against the tenant's configured hourly
// quota. Both limits disabled short-circuit without touching the database so
// default deployments keep the pre-quota hot path.
func (s *Service) admitBandwidth(ctx context.Context, principal auth.MachinePrincipal, up, down int64) error {
	if up <= 0 && down <= 0 {
		return nil
	}
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	settings := s.LoadTenantSettings(ctx, principal.TenantID)
	userLimit := settings.BandwidthUserBytesPerHour
	tenantLimit := settings.BandwidthTenantBytesPerHour
	if userLimit <= 0 && tenantLimit <= 0 {
		return nil
	}
	return s.Workspaces.AdmitBandwidth(ctx, principal.TenantID, principal.UserID, up, down, userLimit, tenantLimit, s.now())
}
