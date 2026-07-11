package capability

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// MarketSubmission represents a skill uploaded from an enterprise Hub to
// HubCenter's global skill market, awaiting approval.
type MarketSubmission struct {
	TenantID              string `json:"tenant_id,omitempty"`
	ID                    string `json:"id"`
	CapabilityRef         string `json:"capability_ref"`
	CapabilityName        string `json:"capability_name"`
	HubCenterSubmissionID string `json:"hubcenter_submission_id"`
	Status                string `json:"status"` // uploading, pending, processing, published, failed
	RejectReason          string `json:"reject_reason,omitempty"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

// NewID generates a new unique ID with the given prefix. Exported for use
// by handler code.
func NewID(prefix string) string {
	return newID(prefix)
}

// ErrMarketSubmissionInFlight is returned when a capability already has an
// uploading/pending/processing market submission row.
var ErrMarketSubmissionInFlight = errors.New("capability already has an in-flight market submission")

// CreateMarketSubmission records a new submission to HubCenter's skill market.
// Prefer ClaimMarketUpload for upload-to-market so concurrent claims are atomic.
func (s *Service) CreateMarketSubmission(ctx context.Context, sub *MarketSubmission) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	tenantID := tenantIDFromContext(ctx)
	sub.TenantID = tenantID
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO capability_market_submissions (tenant_id, id, capability_ref, capability_name, hubcenter_submission_id, status, reject_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, sub.ID, sub.CapabilityRef, sub.CapabilityName, sub.HubCenterSubmissionID, sub.Status, sub.RejectReason, sub.CreatedAt, sub.UpdatedAt,
	)
	return err
}

// ClaimMarketUpload atomically reserves an in-flight market submission for a
// capability: if another uploading/pending/processing row exists, returns
// ErrMarketSubmissionInFlight.
//
// Uses SQLite BEGIN IMMEDIATE (via a dedicated connection) so concurrent
// claims cannot both observe count=0 under DEFERRED transaction semantics.
func (s *Service) ClaimMarketUpload(ctx context.Context, sub *MarketSubmission) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	if sub == nil {
		return errors.New("market submission is nil")
	}
	tenantID := tenantIDFromContext(ctx)
	sub.TenantID = tenantID
	capRef := strings.TrimSpace(sub.CapabilityRef)
	if capRef == "" {
		return errors.New("capability_ref is required")
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		// Only fall back when IMMEDIATE is unsupported (non-SQLite). Real lock /
		// I/O failures must surface — falling back to DEFERRED would re-open races.
		if !isBeginImmediateUnsupported(err) {
			return err
		}
		return s.claimMarketUploadTx(ctx, sub, tenantID, capRef)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM capability_market_submissions WHERE tenant_id = ? AND capability_ref = ? AND status IN (`+inFlightMarketSubmissionSQL+`)`,
		tenantID, capRef,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrMarketSubmissionInFlight
	}

	if _, err := conn.ExecContext(ctx,
		`INSERT INTO capability_market_submissions (tenant_id, id, capability_ref, capability_name, hubcenter_submission_id, status, reject_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, sub.ID, capRef, sub.CapabilityName, sub.HubCenterSubmissionID, sub.Status, sub.RejectReason, sub.CreatedAt, sub.UpdatedAt,
	); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

// isBeginImmediateUnsupported reports dialect errors where BEGIN IMMEDIATE is
// not a valid statement (e.g. Postgres). Lock contention is NOT unsupported.
func isBeginImmediateUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "syntax") ||
		strings.Contains(msg, "unrecognized") ||
		strings.Contains(msg, "near \"immediate\"") ||
		strings.Contains(msg, "unexpected")
}

func (s *Service) claimMarketUploadTx(ctx context.Context, sub *MarketSubmission, tenantID, capRef string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM capability_market_submissions WHERE tenant_id = ? AND capability_ref = ? AND status IN (`+inFlightMarketSubmissionSQL+`)`,
		tenantID, capRef,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrMarketSubmissionInFlight
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO capability_market_submissions (tenant_id, id, capability_ref, capability_name, hubcenter_submission_id, status, reject_reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, sub.ID, capRef, sub.CapabilityName, sub.HubCenterSubmissionID, sub.Status, sub.RejectReason, sub.CreatedAt, sub.UpdatedAt,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMarketSubmissions returns all market submissions for the current tenant,
// ordered by creation time (newest first).
func (s *Service) ListMarketSubmissions(ctx context.Context) ([]MarketSubmission, error) {
	if s == nil || s.db == nil {
		return []MarketSubmission{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, id, capability_ref, capability_name, hubcenter_submission_id, status, reject_reason, created_at, updated_at FROM capability_market_submissions WHERE tenant_id = ? ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MarketSubmission
	for rows.Next() {
		var item MarketSubmission
		if err := rows.Scan(&item.TenantID, &item.ID, &item.CapabilityRef, &item.CapabilityName, &item.HubCenterSubmissionID, &item.Status, &item.RejectReason, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if items == nil {
		items = []MarketSubmission{}
	}
	return items, rows.Err()
}

// UpdateMarketSubmissionStatus updates the status (and optional reject reason)
// of an existing market submission.
func (s *Service) UpdateMarketSubmissionStatus(ctx context.Context, id, status, rejectReason string) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE capability_market_submissions SET status = ?, reject_reason = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`,
		strings.TrimSpace(status), strings.TrimSpace(rejectReason), now, tenantID, strings.TrimSpace(id),
	)
	return err
}

// AttachMarketSubmissionRemote binds the HubCenter submission id after the
// network upload succeeds and moves the local row out of "uploading".
// rejectReason is stored as-is (empty clears a previous failure reason).
func (s *Service) AttachMarketSubmissionRemote(ctx context.Context, id, hubCenterSubmissionID, status, rejectReason string) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE capability_market_submissions SET hubcenter_submission_id = ?, status = ?, reject_reason = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`,
		strings.TrimSpace(hubCenterSubmissionID), strings.TrimSpace(status), strings.TrimSpace(rejectReason), now, tenantID, strings.TrimSpace(id),
	)
	return err
}

// DeleteCapability performs a transactional soft-delete: marks the capability
// as "deleted" and disables all associated managed deployments and
// recommendations in a single atomic operation.
func (s *Service) DeleteCapability(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Soft-delete the capability.
	res, err := tx.ExecContext(ctx,
		`UPDATE capabilities SET status = 'deleted', updated_at = ? WHERE tenant_id = ? AND id = ?`,
		now, tenantID, strings.TrimSpace(id),
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	// Disable associated managed deployments.
	_, _ = tx.ExecContext(ctx,
		`UPDATE managed_capability_deployments SET enabled = 0, updated_at = ? WHERE tenant_id = ? AND capability_ref = ?`,
		now, tenantID, strings.TrimSpace(id),
	)

	// Disable associated recommendations.
	_, _ = tx.ExecContext(ctx,
		`UPDATE recommended_capabilities SET enabled = 0, updated_at = ? WHERE tenant_id = ? AND capability_ref = ?`,
		now, tenantID, strings.TrimSpace(id),
	)

	return tx.Commit()
}

// inFlightMarketSubmissionStatuses are local statuses that still block a new
// upload-to-market. Terminal outcomes (published/failed/rejected) must not block
// version upgrades or retries after a remote failure.
const inFlightMarketSubmissionSQL = `'uploading', 'pending', 'processing'`

// HasActiveSubmission checks if a capability already has an in-flight submission
// to HubCenter (idempotency guard for concurrent upload-to-market).
func (s *Service) HasActiveSubmission(ctx context.Context, capabilityRef string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	tenantID := tenantIDFromContext(ctx)
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM capability_market_submissions WHERE tenant_id = ? AND capability_ref = ? AND status IN (`+inFlightMarketSubmissionSQL+`)`,
		tenantID, strings.TrimSpace(capabilityRef),
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ActiveMarketSubmissions returns in-flight submission records that currently
// prevent another upload of the same capability. Callers can reconcile these
// with HubCenter before deciding whether the idempotency guard should apply.
func (s *Service) ActiveMarketSubmissions(ctx context.Context, capabilityRef string) ([]MarketSubmission, error) {
	if s == nil || s.db == nil {
		return []MarketSubmission{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, id, capability_ref, capability_name, hubcenter_submission_id, status, reject_reason, created_at, updated_at
		 FROM capability_market_submissions
		 WHERE tenant_id = ? AND capability_ref = ? AND status IN (`+inFlightMarketSubmissionSQL+`)
		 ORDER BY created_at DESC`,
		tenantID, strings.TrimSpace(capabilityRef))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]MarketSubmission, 0)
	for rows.Next() {
		var item MarketSubmission
		if err := rows.Scan(&item.TenantID, &item.ID, &item.CapabilityRef, &item.CapabilityName, &item.HubCenterSubmissionID, &item.Status, &item.RejectReason, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// DisableDeploymentsForCapability disables all managed deployments referencing
// the given capability. Kept for backward compatibility; prefer DeleteCapability
// for transactional delete.
func (s *Service) DisableDeploymentsForCapability(ctx context.Context, capabilityRef string) error {
	if s == nil || s.db == nil {
		return nil
	}
	tenantID := tenantIDFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`UPDATE managed_capability_deployments SET enabled = 0, updated_at = ? WHERE tenant_id = ? AND capability_ref = ?`,
		time.Now().UTC().Format(time.RFC3339), tenantID, capabilityRef,
	)
	return err
}

// DisableRecommendationsForCapability disables all recommendations referencing
// the given capability. Kept for backward compatibility; prefer DeleteCapability
// for transactional delete.
func (s *Service) DisableRecommendationsForCapability(ctx context.Context, capabilityRef string) error {
	if s == nil || s.db == nil {
		return nil
	}
	tenantID := tenantIDFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`UPDATE recommended_capabilities SET enabled = 0, updated_at = ? WHERE tenant_id = ? AND capability_ref = ?`,
		time.Now().UTC().Format(time.RFC3339), tenantID, capabilityRef,
	)
	return err
}
