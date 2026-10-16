package cloudworkspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	SharePermissionRead  = "read"
	SharePermissionWrite = "write"
	ShareStatusActive    = "active"
	ShareStatusRevoked   = "revoked"

	shareIDPrefix            = "cwsh_"
	shareTokenBytes          = 18
	sharePublicPath          = "/hub/cloud-workspaces/shares/"
	ShareAccessTokenPrefix   = "cwss_"
	shareAccessTokenBytes    = 24
	ShareAccessMachinePrefix = "share:"
	shareCols                = `id, workspace_id, tenant_id, owner_user_id, token, default_permission, status, created_at, updated_at, password_hash, expires_at`
	sharePasswordMinRunes    = 4
	sharePasswordMaxRunes    = 64
)

var (
	ErrShareReadOnly           = errors.New("cloud workspace share is read-only")
	ErrShareSelf               = errors.New("cannot accept your own cloud workspace share")
	ErrShareRevoked            = errors.New("cloud workspace share is revoked")
	ErrShareExpired            = errors.New("cloud workspace share has expired")
	ErrSharePasswordRequired   = errors.New("cloud workspace share password required")
	ErrSharePasswordInvalid    = errors.New("cloud workspace share password is incorrect")
)

// WorkspaceShare is one cloud_workspace_shares row.
type WorkspaceShare struct {
	ID                string
	WorkspaceID       string
	TenantID          string
	OwnerUserID       string
	Token             string
	DefaultPermission string
	Status            string
	CreatedAt         string
	UpdatedAt         string
	PasswordHash      string
	ExpiresAt         string
}

// ShareWriteOptions updates password and expiry when creating or refreshing a share.
type ShareWriteOptions struct {
	Password      string
	ClearPassword bool
	TTL           string
	ExpiresAt     string
}

// WorkspaceShareRecipient is one accepted recipient.
type WorkspaceShareRecipient struct {
	WorkspaceID string `json:"workspace_id"`
	ShareID     string `json:"share_id"`
	UserID      string `json:"user_id"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	HomeHub     string `json:"home_hub,omitempty"`
	Permission  string `json:"permission"`
	AcceptedAt  string `json:"accepted_at"`
}

// WorkspaceShareView is the owner-facing share payload.
type WorkspaceShareView struct {
	WorkspaceID       string                    `json:"workspace_id"`
	ShareID           string                    `json:"share_id,omitempty"`
	ShareURL          string                    `json:"share_url,omitempty"`
	Token             string                    `json:"token,omitempty"`
	DefaultPermission string                    `json:"default_permission,omitempty"`
	Status            string                    `json:"status,omitempty"`
	PasswordSet       bool                      `json:"password_set"`
	ExpiresAt         string                    `json:"expires_at,omitempty"`
	Recipients        []WorkspaceShareRecipient `json:"recipients"`
}

// PublicShareInfo is the unauthenticated preview for a share token.
type PublicShareInfo struct {
	Active           bool   `json:"active"`
	Expired          bool   `json:"expired"`
	PasswordRequired bool   `json:"password_required"`
	ExpiresAt        string `json:"expires_at,omitempty"`
}

// EntitlementSharedWorkspace is a workspace shared with the caller.
type EntitlementSharedWorkspace struct {
	EntitlementWorkspace
	OwnerUserID     string `json:"owner_user_id"`
	OwnerEmail      string `json:"owner_email,omitempty"`
	SharePermission string `json:"share_permission"`
}

func hashSharePassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", nil
	}
	n := utf8.RuneCountInString(password)
	if n < sharePasswordMinRunes || n > sharePasswordMaxRunes {
		return "", fmt.Errorf("%w: share password must be %d-%d characters", ErrInvalidInput, sharePasswordMinRunes, sharePasswordMaxRunes)
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func checkSharePassword(hash, password string) error {
	if strings.TrimSpace(hash) == "" {
		return nil
	}
	if strings.TrimSpace(password) == "" {
		return ErrSharePasswordRequired
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return ErrSharePasswordInvalid
	}
	return nil
}

func shareExpired(share *WorkspaceShare, now time.Time) bool {
	if share == nil || strings.TrimSpace(share.ExpiresAt) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(share.ExpiresAt))
	if err != nil {
		return true
	}
	return !t.UTC().After(now.UTC())
}

func applyShareExpiry(opt ShareWriteOptions, now time.Time) (string, bool, error) {
	if raw := strings.TrimSpace(opt.ExpiresAt); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return "", false, fmt.Errorf("%w: expires_at must be RFC3339", ErrInvalidInput)
		}
		parsed = parsed.UTC()
		if !parsed.After(now.UTC()) {
			return "", false, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
		}
		return parsed.Format(time.RFC3339), true, nil
	}
	switch strings.ToLower(strings.TrimSpace(opt.TTL)) {
	case "":
		return "", false, nil
	case "never", "none", "permanent", "forever":
		return "", true, nil
	case "1d", "24h", "day":
		return now.UTC().Add(24 * time.Hour).Format(time.RFC3339), true, nil
	case "7d", "week":
		return now.UTC().AddDate(0, 0, 7).Format(time.RFC3339), true, nil
	case "30d", "1m", "month":
		return now.UTC().AddDate(0, 1, 0).Format(time.RFC3339), true, nil
	case "90d", "3m":
		return now.UTC().AddDate(0, 3, 0).Format(time.RFC3339), true, nil
	default:
		return "", false, fmt.Errorf("%w: ttl must be one of 1d, 7d, 30d, 90d, never", ErrInvalidInput)
	}
}

func assertShareJoinable(share *WorkspaceShare, password string, now time.Time) error {
	if share == nil || share.Status != ShareStatusActive {
		return ErrNotFound
	}
	if shareExpired(share, now) {
		return ErrShareExpired
	}
	return checkSharePassword(share.PasswordHash, password)
}

func normalizeSharePermission(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", SharePermissionRead, "readonly", "read-only":
		return SharePermissionRead, nil
	case SharePermissionWrite, "readwrite", "read-write", "rw":
		return SharePermissionWrite, nil
	default:
		return "", fmt.Errorf("%w: share permission must be read or write", ErrInvalidInput)
	}
}

func newShareID() string {
	return shareIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newShareToken() (string, error) {
	buf := make([]byte, shareTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// SharePublicPath is the Hub path for a share token.
func SharePublicPath(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	return sharePublicPath + token
}

func getWorkspaceByID(ctx context.Context, q queryer, id string) (*Workspace, error) {
	ws, err := scanWorkspace(q.QueryRowContext(ctx,
		`SELECT `+workspaceCols+` FROM cloud_workspaces WHERE id = ?`,
		strings.TrimSpace(id),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return ws, nil
}

func getWorkspaceByTenantID(ctx context.Context, q queryer, tenantID, id string) (*Workspace, error) {
	ws, err := getWorkspaceByID(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if store.NormalizeTenantID(ws.TenantID) != store.NormalizeTenantID(tenantID) {
		return nil, ErrNotFound
	}
	return ws, nil
}

func lookupSharePermission(ctx context.Context, q queryer, tenantID, userID, workspaceID string) (string, error) {
	var permission, status string
	_ = tenantID
	err := q.QueryRowContext(ctx, `
		SELECT r.permission, s.status
		FROM cloud_workspace_share_recipients r
		JOIN cloud_workspace_shares s ON s.id = r.share_id
		WHERE r.workspace_id = ? AND r.recipient_user_id = ?`,
		strings.TrimSpace(workspaceID), strings.TrimSpace(userID),
	).Scan(&permission, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if status != ShareStatusActive {
		return "", ErrShareRevoked
	}
	permission, err = normalizeSharePermission(permission)
	if err != nil {
		return "", err
	}
	return permission, nil
}

// requireActiveAccess allows the owner, or an accepted recipient. Write
// operations require write permission; owners always have write.
func requireActiveAccess(ctx context.Context, q queryer, tenantID, userID, id string, write bool) (*Workspace, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	id = strings.TrimSpace(id)
	if userID == "" || id == "" {
		return nil, ErrNotFound
	}
	ws, err := getOwned(ctx, q, tenantID, userID, id)
	if err == nil {
		if ws.Status != StatusActive {
			return nil, ErrNotFound
		}
		return ws, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	ws, err = getWorkspaceByID(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if ws.Status != StatusActive {
		return nil, ErrNotFound
	}
	perm, err := lookupSharePermission(ctx, q, tenantID, userID, id)
	if err != nil {
		if errors.Is(err, ErrShareRevoked) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if write && perm != SharePermissionWrite {
		return nil, ErrShareReadOnly
	}
	return ws, nil
}

// RequireAccess is the exported store form of requireActiveAccess.
func (s *Store) RequireAccess(ctx context.Context, tenantID, userID, id string, write bool) (*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	return requireActiveAccess(ctx, s.db, tenantID, userID, id, write)
}

func revokeSharesTx(ctx context.Context, q queryer, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_share_access WHERE workspace_id = ?`, workspaceID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_share_recipients WHERE workspace_id = ?`, workspaceID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_shares SET status = ?, updated_at = ? WHERE workspace_id = ? AND status = ?`,
		ShareStatusRevoked, time.Now().UTC().Format(time.RFC3339), workspaceID, ShareStatusActive); err != nil {
		return err
	}
	return nil
}

func scanShare(scanner interface{ Scan(dest ...any) error }) (*WorkspaceShare, error) {
	var row WorkspaceShare
	if err := scanner.Scan(&row.ID, &row.WorkspaceID, &row.TenantID, &row.OwnerUserID, &row.Token, &row.DefaultPermission, &row.Status, &row.CreatedAt, &row.UpdatedAt, &row.PasswordHash, &row.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) getActiveShare(ctx context.Context, q queryer, tenantID, ownerUserID, workspaceID string) (*WorkspaceShare, error) {
	row, err := scanShare(q.QueryRowContext(ctx, `
		SELECT `+shareCols+`
		FROM cloud_workspace_shares
		WHERE workspace_id = ? AND tenant_id = ? AND owner_user_id = ? AND status = ?`,
		strings.TrimSpace(workspaceID), store.NormalizeTenantID(tenantID), strings.TrimSpace(ownerUserID), ShareStatusActive))
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) LookupWorkspaceShare(ctx context.Context, token string) (*WorkspaceShare, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrNotFound
	}
	row, err := scanShare(s.db.QueryRowContext(ctx, `SELECT `+shareCols+` FROM cloud_workspace_shares WHERE token = ?`, token))
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) listShareRecipients(ctx context.Context, q queryer, shareID string) ([]WorkspaceShareRecipient, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT workspace_id, share_id, recipient_user_id, permission, accepted_at, home_hub, display_name
		FROM cloud_workspace_share_recipients WHERE share_id = ? ORDER BY accepted_at ASC`,
		strings.TrimSpace(shareID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceShareRecipient{}
	for rows.Next() {
		var rec WorkspaceShareRecipient
		if err := rows.Scan(&rec.WorkspaceID, &rec.ShareID, &rec.UserID, &rec.Permission, &rec.AcceptedAt, &rec.HomeHub, &rec.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// UpsertWorkspaceShare creates or returns the active share for an owned workspace.
func (s *Store) UpsertWorkspaceShare(ctx context.Context, tenantID, ownerUserID, workspaceID, permission string, now time.Time, opts ...ShareWriteOptions) (*WorkspaceShare, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	permission, err := normalizeSharePermission(permission)
	if err != nil {
		return nil, err
	}
	var opt ShareWriteOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	passwordHash := ""
	if opt.ClearPassword {
		passwordHash = ""
	} else if strings.TrimSpace(opt.Password) != "" {
		passwordHash, err = hashSharePassword(opt.Password)
		if err != nil {
			return nil, err
		}
	}
	expiresAt, expirySet, err := applyShareExpiry(opt, now)
	if err != nil {
		return nil, err
	}
	tenantID = store.NormalizeTenantID(tenantID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	workspaceID = strings.TrimSpace(workspaceID)
	ts := now.UTC().Format(time.RFC3339)
	var out *WorkspaceShare
	err = s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, ownerUserID, workspaceID); err != nil {
			return err
		}
		existing, err := s.getActiveShare(ctx, q, tenantID, ownerUserID, workspaceID)
		if err == nil {
			nextHash := existing.PasswordHash
			if opt.ClearPassword {
				nextHash = ""
			} else if strings.TrimSpace(opt.Password) != "" {
				nextHash = passwordHash
			}
			nextExpiry := existing.ExpiresAt
			if expirySet {
				nextExpiry = expiresAt
			}
			if existing.DefaultPermission != permission || nextHash != existing.PasswordHash || nextExpiry != existing.ExpiresAt {
				if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_shares SET default_permission = ?, password_hash = ?, expires_at = ?, updated_at = ? WHERE id = ?`, permission, nextHash, nextExpiry, ts, existing.ID); err != nil {
					return err
				}
				existing.DefaultPermission = permission
				existing.PasswordHash = nextHash
				existing.ExpiresAt = nextExpiry
				existing.UpdatedAt = ts
			}
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		token, err := newShareToken()
		if err != nil {
			return err
		}
		row := &WorkspaceShare{
			ID:                newShareID(),
			WorkspaceID:       workspaceID,
			TenantID:          tenantID,
			OwnerUserID:       ownerUserID,
			Token:             token,
			DefaultPermission: permission,
			Status:            ShareStatusActive,
			CreatedAt:         ts,
			UpdatedAt:         ts,
			PasswordHash:      passwordHash,
			ExpiresAt:         expiresAt,
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_shares (
			id, workspace_id, tenant_id, owner_user_id, token, default_permission, status, created_at, updated_at, password_hash, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.ID, row.WorkspaceID, row.TenantID, row.OwnerUserID, row.Token, row.DefaultPermission, row.Status, row.CreatedAt, row.UpdatedAt, row.PasswordHash, row.ExpiresAt); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetOwnedWorkspaceShare returns the active share plus recipients.
func (s *Store) GetOwnedWorkspaceShare(ctx context.Context, tenantID, ownerUserID, workspaceID string) (*WorkspaceShare, []WorkspaceShareRecipient, error) {
	if s == nil || s.db == nil {
		return nil, nil, ErrUnavailable
	}
	if _, err := requireActiveOwned(ctx, s.db, tenantID, ownerUserID, workspaceID); err != nil {
		return nil, nil, err
	}
	share, err := s.getActiveShare(ctx, s.db, tenantID, ownerUserID, workspaceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, []WorkspaceShareRecipient{}, nil
		}
		return nil, nil, err
	}
	recs, err := s.listShareRecipients(ctx, s.db, share.ID)
	if err != nil {
		return nil, nil, err
	}
	return share, recs, nil
}

// RevokeWorkspaceShare disables the active link and drops recipients.
func (s *Store) RevokeWorkspaceShare(ctx context.Context, tenantID, ownerUserID, workspaceID string) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	return s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, ownerUserID, workspaceID); err != nil {
			return err
		}
		return revokeSharesTx(ctx, q, workspaceID)
	})
}

// AcceptWorkspaceShare adds the caller as a recipient of the token's workspace.
func (s *Store) AcceptWorkspaceShare(ctx context.Context, tenantID, userID, token, password string, now time.Time) (*Workspace, string, error) {
	if s == nil || s.db == nil {
		return nil, "", ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	token = strings.TrimSpace(token)
	if userID == "" || token == "" {
		return nil, "", ErrNotFound
	}
	ts := now.UTC().Format(time.RFC3339)
	var ws *Workspace
	var permission string
	err := s.withImmediate(ctx, func(q queryer) error {
		share, err := scanShare(q.QueryRowContext(ctx, `
			SELECT ` + shareCols + `
			FROM cloud_workspace_shares WHERE token = ?`, token))
		if err != nil {
			return err
		}
		if err := assertShareJoinable(share, password, now); err != nil {
			return err
		}
		if share.OwnerUserID == userID {
			return ErrShareSelf
		}
		owned, err := getWorkspaceByID(ctx, q, share.WorkspaceID)
		if err != nil {
			return err
		}
		if owned.Status != StatusActive {
			return ErrNotFound
		}
		permission = share.DefaultPermission
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_share_recipients (
			workspace_id, tenant_id, share_id, recipient_user_id, permission, accepted_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, recipient_user_id) DO UPDATE SET
			share_id = excluded.share_id,
			permission = cloud_workspace_share_recipients.permission,
			accepted_at = cloud_workspace_share_recipients.accepted_at`,
			share.WorkspaceID, share.TenantID, share.ID, userID, permission, ts); err != nil {
			return err
		}
		if err := q.QueryRowContext(ctx, `SELECT permission FROM cloud_workspace_share_recipients WHERE workspace_id = ? AND recipient_user_id = ?`,
			share.WorkspaceID, userID).Scan(&permission); err != nil {
			return err
		}
		ws = owned
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return ws, permission, nil
}

// UpdateShareRecipientPermission changes one recipient's access.
func (s *Store) UpdateShareRecipientPermission(ctx context.Context, tenantID, ownerUserID, workspaceID, recipientUserID, permission string) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	permission, err := normalizeSharePermission(permission)
	if err != nil {
		return err
	}
	recipientUserID = strings.TrimSpace(recipientUserID)
	if recipientUserID == "" {
		return ErrNotFound
	}
	return s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, ownerUserID, workspaceID); err != nil {
			return err
		}
		res, err := q.ExecContext(ctx, `UPDATE cloud_workspace_share_recipients SET permission = ?
			WHERE workspace_id = ? AND recipient_user_id = ?`,
			permission, strings.TrimSpace(workspaceID), recipientUserID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_share_access SET permission = ?
			WHERE workspace_id = ? AND recipient_user_id = ?`,
			permission, strings.TrimSpace(workspaceID), recipientUserID); err != nil {
			return err
		}
		return nil
	})
}

// RemoveShareRecipient drops one recipient.
func (s *Store) RemoveShareRecipient(ctx context.Context, tenantID, ownerUserID, workspaceID, recipientUserID string) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	recipientUserID = strings.TrimSpace(recipientUserID)
	if recipientUserID == "" {
		return ErrNotFound
	}
	return s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, ownerUserID, workspaceID); err != nil {
			return err
		}
		res, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_share_recipients
			WHERE workspace_id = ? AND recipient_user_id = ?`,
			strings.TrimSpace(workspaceID), recipientUserID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_share_access
			WHERE workspace_id = ? AND recipient_user_id = ?`,
			strings.TrimSpace(workspaceID), recipientUserID); err != nil {
			return err
		}
		return nil
	})
}

// ListSharedWithUser returns active workspaces the user can open via share.
func (s *Store) ListSharedWithUser(ctx context.Context, tenantID, userID string) ([]EntitlementSharedWorkspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return []EntitlementSharedWorkspace{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id, w.tenant_id, w.user_id, w.name, w.name_norm, w.status, w.used_bytes, w.file_count, w.manifest_revision, w.created_at, w.updated_at, w.deleted_at, r.permission
		FROM cloud_workspace_share_recipients r
		JOIN cloud_workspace_shares s ON s.id = r.share_id AND s.status = ?
		JOIN cloud_workspaces w ON w.id = r.workspace_id AND w.tenant_id = r.tenant_id
		WHERE r.recipient_user_id = ? AND w.status = ?
		ORDER BY w.updated_at DESC, w.id ASC`,
		ShareStatusActive, userID, StatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EntitlementSharedWorkspace{}
	for rows.Next() {
		var (
			ws         Workspace
			deleted    sql.NullString
			permission string
		)
		if err := rows.Scan(
			&ws.ID, &ws.TenantID, &ws.UserID, &ws.Name, &ws.NameNorm, &ws.Status,
			&ws.UsedBytes, &ws.FileCount, &ws.ManifestRevision, &ws.CreatedAt, &ws.UpdatedAt, &deleted,
			&permission,
		); err != nil {
			return nil, err
		}
		if deleted.Valid {
			ws.DeletedAt = deleted.String
		}
		perm, err := normalizeSharePermission(permission)
		if err != nil {
			perm = SharePermissionRead
		}
		out = append(out, EntitlementSharedWorkspace{
			EntitlementWorkspace: EntitlementWorkspace{
				ID:             ws.ID,
				Name:           ws.Name,
				UsedBytes:      ws.UsedBytes,
				UpdatedAt:      ws.UpdatedAt,
				Status:         ws.Status,
				SyncProtocol:   "v1-sequential",
				ServerRevision: ws.ManifestRevision,
			},
			OwnerUserID:     ws.UserID,
			SharePermission: perm,
		})
	}
	return out, rows.Err()
}

// ListActiveLeasesForIDs returns unreleased leases keyed by workspace ID.
func (s *Store) ListActiveLeasesForIDs(ctx context.Context, workspaceIDs []string) (map[string]*Lease, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	out := map[string]*Lease{}
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, 0, len(workspaceIDs))
	args := make([]any, 0, len(workspaceIDs))
	seen := map[string]struct{}{}
	for _, id := range workspaceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(placeholders) == 0 {
		return out, nil
	}
	query := "SELECT " + leaseCols + " FROM cloud_workspace_leases WHERE released_at IS NULL AND workspace_id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		lease, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		out[lease.WorkspaceID] = lease
	}
	return out, rows.Err()
}

func (s *Service) decorateShareRecipients(ctx context.Context, recs []WorkspaceShareRecipient) []WorkspaceShareRecipient {
	if len(recs) == 0 {
		return []WorkspaceShareRecipient{}
	}
	if s == nil || s.Users == nil {
		return recs
	}
	for i := range recs {
		if recs[i].Email == "" && recs[i].DisplayName != "" {
			recs[i].Email = recs[i].DisplayName
		}
		user, err := s.Users.GetByID(ctx, recs[i].UserID)
		if err != nil || user == nil {
			continue
		}
		if email := strings.TrimSpace(user.Email); email != "" {
			recs[i].Email = email
		}
	}
	return recs
}

func validShareHomeHub(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if len(raw) > 256 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || strings.TrimSpace(u.Host) == "" || u.User != nil {
		return false
	}
	if u.Path != "" && u.Path != "/" {
		return false
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if strings.EqualFold(u.Scheme, "https") {
		return true
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (s *Service) shareView(ctx context.Context, share *WorkspaceShare, recs []WorkspaceShareRecipient) WorkspaceShareView {
	view := WorkspaceShareView{Recipients: []WorkspaceShareRecipient{}}
	if share == nil {
		return view
	}
	view.WorkspaceID = share.WorkspaceID
	view.ShareID = share.ID
	view.Token = share.Token
	view.ShareURL = SharePublicPath(share.Token)
	view.DefaultPermission = share.DefaultPermission
	view.Status = share.Status
	view.PasswordSet = strings.TrimSpace(share.PasswordHash) != ""
	view.ExpiresAt = strings.TrimSpace(share.ExpiresAt)
	view.Recipients = s.decorateShareRecipients(ctx, recs)
	return view
}

// CreateOrGetShare creates the active share link or returns the existing one.
func (s *Service) CreateOrGetShare(ctx context.Context, principal auth.MachinePrincipal, workspaceID, permission string, opts ...ShareWriteOptions) (WorkspaceShareView, error) {
	if s == nil || s.Workspaces == nil {
		return WorkspaceShareView{}, ErrUnavailable
	}
	share, err := s.Workspaces.UpsertWorkspaceShare(ctx, principal.TenantID, principal.UserID, workspaceID, permission, s.now(), opts...)
	if err != nil {
		return WorkspaceShareView{}, err
	}
	_, recs, err := s.Workspaces.GetOwnedWorkspaceShare(ctx, principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return WorkspaceShareView{}, err
	}
	return s.shareView(ctx, share, recs), nil
}

// GetShare returns the owner-facing share view. Missing share is an empty view.
func (s *Service) GetShare(ctx context.Context, principal auth.MachinePrincipal, workspaceID string) (WorkspaceShareView, error) {
	if s == nil || s.Workspaces == nil {
		return WorkspaceShareView{}, ErrUnavailable
	}
	share, recs, err := s.Workspaces.GetOwnedWorkspaceShare(ctx, principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return WorkspaceShareView{}, err
	}
	return s.shareView(ctx, share, recs), nil
}

// RevokeShare disables the link and removes every recipient.
func (s *Service) RevokeShare(ctx context.Context, principal auth.MachinePrincipal, workspaceID string) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	return s.Workspaces.RevokeWorkspaceShare(ctx, principal.TenantID, principal.UserID, workspaceID)
}

// AcceptShare records the caller as a recipient of token.
func (s *Service) AcceptShare(ctx context.Context, principal auth.MachinePrincipal, token, password string) (*Workspace, string, error) {
	if s == nil || s.Workspaces == nil {
		return nil, "", ErrUnavailable
	}
	return s.Workspaces.AcceptWorkspaceShare(ctx, principal.TenantID, principal.UserID, token, password, s.now())
}

// PublicShare returns unauthenticated status for a share token.
func (s *Service) PublicShare(ctx context.Context, token string) (PublicShareInfo, error) {
	if s == nil || s.Workspaces == nil {
		return PublicShareInfo{}, ErrUnavailable
	}
	share, err := s.Workspaces.LookupWorkspaceShare(ctx, token)
	if err != nil {
		return PublicShareInfo{}, err
	}
	now := s.now()
	info := PublicShareInfo{
		Active:           share.Status == ShareStatusActive && !shareExpired(share, now),
		Expired:          shareExpired(share, now),
		PasswordRequired: strings.TrimSpace(share.PasswordHash) != "",
		ExpiresAt:        strings.TrimSpace(share.ExpiresAt),
	}
	return info, nil
}

// UpdateShareRecipient changes one recipient's permission.
func (s *Service) UpdateShareRecipient(ctx context.Context, principal auth.MachinePrincipal, workspaceID, recipientUserID, permission string) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	return s.Workspaces.UpdateShareRecipientPermission(ctx, principal.TenantID, principal.UserID, workspaceID, recipientUserID, permission)
}

// RemoveShareRecipient drops one recipient.
func (s *Service) RemoveShareRecipient(ctx context.Context, principal auth.MachinePrincipal, workspaceID, recipientUserID string) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	return s.Workspaces.RemoveShareRecipient(ctx, principal.TenantID, principal.UserID, workspaceID, recipientUserID)
}

// ShareAccessRecord is a capability token bound to one shared workspace.
type ShareAccessRecord struct {
	WorkspaceID       string
	WorkspaceTenantID string
	RecipientKey      string
	Permission        string
	HomeHub           string
	DisplayName       string
}

// ShareClaim is the identity a foreign Hub/user presents when joining.
type ShareClaim struct {
	HomeHub      string `json:"home_hub"`
	HomeUserID   string `json:"home_user_id"`
	HomeTenantID string `json:"home_tenant_id"`
	DisplayName  string `json:"display_name"`
	Password     string `json:"password,omitempty"`
}

func hashShareAccessToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func federatedRecipientKey(homeHub, homeUserID string) string {
	homeHub = strings.ToLower(strings.TrimRight(strings.TrimSpace(homeHub), "/"))
	homeUserID = strings.TrimSpace(homeUserID)
	sum := sha256.Sum256([]byte(homeHub + "\x00" + homeUserID))
	return "fed_" + hex.EncodeToString(sum[:16])
}

func newShareAccessToken() (string, error) {
	buf := make([]byte, shareAccessTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return ShareAccessTokenPrefix + hex.EncodeToString(buf), nil
}

// SharePrincipal reports whether this caller authenticated with a share access token.
func SharePrincipal(principal auth.MachinePrincipal) bool {
	return strings.HasPrefix(strings.TrimSpace(principal.MachineID), ShareAccessMachinePrefix)
}

// ClaimShare lets a caller join by share token without being a user on this Hub.
func (s *Service) ClaimShare(ctx context.Context, token string, claim ShareClaim, now time.Time) (*Workspace, string, string, error) {
	if s == nil || s.Workspaces == nil {
		return nil, "", "", ErrUnavailable
	}
	if now.IsZero() {
		now = s.now()
	}
	return s.Workspaces.ClaimWorkspaceShare(ctx, token, claim, now)
}

// ResolveShareAccess authenticates a cwss_ capability token.
func (s *Service) ResolveShareAccess(ctx context.Context, accessToken string) (*ShareAccessRecord, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.ResolveShareAccess(ctx, accessToken, s.now())
}

// HasShareAccess reports whether userID is a recipient of workspaceID.
func (s *Service) HasShareAccess(ctx context.Context, userID, workspaceID string) bool {
	if s == nil || s.Workspaces == nil {
		return false
	}
	perm, err := lookupSharePermission(ctx, s.Workspaces.db, "", userID, workspaceID)
	return err == nil && perm != ""
}

func (s *Store) ClaimWorkspaceShare(ctx context.Context, token string, claim ShareClaim, now time.Time) (*Workspace, string, string, error) {
	if s == nil || s.db == nil {
		return nil, "", "", ErrUnavailable
	}
	token = strings.TrimSpace(token)
	homeUserID := strings.TrimSpace(claim.HomeUserID)
	homeHub := strings.TrimRight(strings.TrimSpace(claim.HomeHub), "/")
	if token == "" || homeUserID == "" {
		return nil, "", "", ErrNotFound
	}
	if len(homeUserID) > 128 || !validShareHomeHub(homeHub) {
		return nil, "", "", ErrInvalidInput
	}
	recipientKey := federatedRecipientKey(homeHub, homeUserID)
	display := strings.TrimSpace(claim.DisplayName)
	if display == "" {
		display = homeUserID
	}
	if runes := []rune(display); len(runes) > 128 {
		display = string(runes[:128])
	}
	accessToken, err := newShareAccessToken()
	if err != nil {
		return nil, "", "", err
	}
	tokenHash := hashShareAccessToken(accessToken)
	ts := now.UTC().Format(time.RFC3339)
	var ws *Workspace
	var permission string
	err = s.withImmediate(ctx, func(q queryer) error {
		share, err := scanShare(q.QueryRowContext(ctx, `
			SELECT ` + shareCols + `
			FROM cloud_workspace_shares WHERE token = ?`, token))
		if err != nil {
			return err
		}
		if err := assertShareJoinable(share, claim.Password, now); err != nil {
			return err
		}
		owned, err := getWorkspaceByID(ctx, q, share.WorkspaceID)
		if err != nil {
			return err
		}
		if owned.Status != StatusActive {
			return ErrNotFound
		}
		if share.OwnerUserID == homeUserID && strings.EqualFold(store.NormalizeTenantID(share.TenantID), store.NormalizeTenantID(claim.HomeTenantID)) {
			return ErrShareSelf
		}
		permission = share.DefaultPermission
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_share_recipients (
			workspace_id, tenant_id, share_id, recipient_user_id, permission, accepted_at, home_hub, home_user_id, display_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, recipient_user_id) DO UPDATE SET
			share_id = excluded.share_id,
			permission = cloud_workspace_share_recipients.permission,
			home_hub = excluded.home_hub,
			home_user_id = excluded.home_user_id,
			display_name = excluded.display_name`,
			share.WorkspaceID, owned.TenantID, share.ID, recipientKey, permission, ts, homeHub, homeUserID, display); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_share_access WHERE workspace_id = ? AND recipient_user_id = ?`, share.WorkspaceID, recipientKey); err != nil {
			return err
		}
		if err := q.QueryRowContext(ctx, `SELECT permission FROM cloud_workspace_share_recipients WHERE workspace_id = ? AND recipient_user_id = ?`,
			share.WorkspaceID, recipientKey).Scan(&permission); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_share_access (
			token_hash, workspace_id, share_id, recipient_user_id, permission, home_hub, created_at, last_used_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tokenHash, share.WorkspaceID, share.ID, recipientKey, permission, homeHub, ts, ts); err != nil {
			return err
		}
		ws = owned
		return nil
	})
	if err != nil {
		return nil, "", "", err
	}
	return ws, permission, accessToken, nil
}

func (s *Store) ResolveShareAccess(ctx context.Context, accessToken string, now time.Time) (*ShareAccessRecord, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	accessToken = strings.TrimSpace(accessToken)
	if !strings.HasPrefix(accessToken, ShareAccessTokenPrefix) {
		return nil, ErrNotFound
	}
	var rec ShareAccessRecord
	var workspaceID string
	err := s.db.QueryRowContext(ctx, `
		SELECT a.workspace_id, a.recipient_user_id, a.permission, a.home_hub, w.tenant_id
		FROM cloud_workspace_share_access a
		JOIN cloud_workspaces w ON w.id = a.workspace_id
		JOIN cloud_workspace_shares s ON s.id = a.share_id AND s.status = ?
		WHERE a.token_hash = ? AND w.status = ?`,
		ShareStatusActive, hashShareAccessToken(accessToken), StatusActive,
	).Scan(&workspaceID, &rec.RecipientKey, &rec.Permission, &rec.HomeHub, &rec.WorkspaceTenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.WorkspaceID = workspaceID
	_, _ = s.db.ExecContext(ctx, `UPDATE cloud_workspace_share_access SET last_used_at = ? WHERE token_hash = ?`,
		now.UTC().Format(time.RFC3339), hashShareAccessToken(accessToken))
	return &rec, nil
}
