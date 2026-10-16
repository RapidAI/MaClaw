package cloudworkspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	CloudWorkspaceProtocol = "v1-sequential"
	InstanceSessionTTL     = 15 * time.Minute

	instanceSessionIDPrefix = "cwses_"
	clientInstanceIDPrefix  = "cwi_"
	instanceTokenPrefix     = "cwst_"
	instanceSessionCols     = `id, token_hash, tenant_id, user_id, machine_id, client_instance_id, protocol, created_at, last_seen_at, expires_at, revoked_at`
)

// InstanceSession is a Hub-issued process identity. Token is returned only
// from IssueInstanceSession and is never persisted in plaintext.
type InstanceSession struct {
	ID               string `json:"session_id"`
	Token            string `json:"session_token,omitempty"`
	TenantID         string `json:"-"`
	UserID           string `json:"-"`
	MachineID        string `json:"-"`
	ClientInstanceID string `json:"client_instance_id"`
	Protocol         string `json:"protocol"`
	CreatedAt        string `json:"created_at"`
	LastSeenAt       string `json:"last_seen_at"`
	ExpiresAt        string `json:"expires_at"`
	RevokedAt        string `json:"revoked_at,omitempty"`
}

func newInstanceSessionID() string {
	return instanceSessionIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newClientInstanceID() string {
	return clientInstanceIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func newInstanceSessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return instanceTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func instanceSessionTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func scanInstanceSession(scanner interface{ Scan(dest ...any) error }) (*InstanceSession, error) {
	var (
		out     InstanceSession
		token   string
		revoked sql.NullString
	)
	if err := scanner.Scan(
		&out.ID, &token, &out.TenantID, &out.UserID, &out.MachineID,
		&out.ClientInstanceID, &out.Protocol, &out.CreatedAt, &out.LastSeenAt,
		&out.ExpiresAt, &revoked,
	); err != nil {
		return nil, err
	}
	if revoked.Valid {
		out.RevokedAt = revoked.String
	}
	return &out, nil
}

// IssueInstanceSession creates a new non-forgeable process identity. The
// client does not choose ClientInstanceID; only the Hub-generated value is
// later allowed to own a writer lease.
func (s *Store) IssueInstanceSession(ctx context.Context, principal auth.MachinePrincipal, protocol string, now time.Time) (*InstanceSession, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID := store.NormalizeTenantID(principal.TenantID)
	userID := strings.TrimSpace(principal.UserID)
	machineID := strings.TrimSpace(principal.MachineID)
	protocol = strings.TrimSpace(protocol)
	if userID == "" || machineID == "" {
		return nil, ErrInstanceSessionInvalid
	}
	if protocol != CloudWorkspaceProtocol {
		return nil, ErrProtocolMismatch
	}
	token, err := newInstanceSessionToken()
	if err != nil {
		return nil, err
	}
	ts := now.UTC()
	out := &InstanceSession{
		ID:               newInstanceSessionID(),
		Token:            token,
		TenantID:         tenantID,
		UserID:           userID,
		MachineID:        machineID,
		ClientInstanceID: newClientInstanceID(),
		Protocol:         protocol,
		CreatedAt:        ts.Format(time.RFC3339),
		LastSeenAt:       ts.Format(time.RFC3339),
		ExpiresAt:        ts.Add(InstanceSessionTTL).Format(time.RFC3339),
	}
	err = s.withImmediate(ctx, func(q queryer) error {
		// Expired and revoked rows have no authority. Bound their retention so
		// abandoned desktop processes cannot grow the table without limit.
		_, _ = q.ExecContext(ctx, `DELETE FROM cloud_workspace_instance_sessions WHERE expires_at <= ? OR (revoked_at IS NOT NULL AND revoked_at <= ?)`, ts.Add(-24*time.Hour).Format(time.RFC3339), ts.Add(-24*time.Hour).Format(time.RFC3339))
		_, insertErr := q.ExecContext(ctx, `INSERT INTO cloud_workspace_instance_sessions (
			id, token_hash, tenant_id, user_id, machine_id, client_instance_id,
			protocol, created_at, last_seen_at, expires_at, revoked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
			out.ID, instanceSessionTokenHash(token), out.TenantID, out.UserID,
			out.MachineID, out.ClientInstanceID, out.Protocol, out.CreatedAt,
			out.LastSeenAt, out.ExpiresAt,
		)
		return insertErr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AuthenticateInstanceSession validates an opaque token against the machine
// credential and extends its short idle TTL. The UPDATE predicate prevents a
// concurrent revoke from being overwritten by a late request.
func (s *Store) AuthenticateInstanceSession(ctx context.Context, principal auth.MachinePrincipal, rawToken, protocol string, now time.Time) (*InstanceSession, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrInstanceSessionRequired
	}
	protocol = strings.TrimSpace(protocol)
	if protocol != CloudWorkspaceProtocol {
		return nil, ErrProtocolMismatch
	}
	tenantID := store.NormalizeTenantID(principal.TenantID)
	userID := strings.TrimSpace(principal.UserID)
	machineID := strings.TrimSpace(principal.MachineID)
	if userID == "" || machineID == "" {
		return nil, ErrInstanceSessionInvalid
	}
	now = now.UTC()
	tokenHash := instanceSessionTokenHash(rawToken)
	row, err := scanInstanceSession(s.db.QueryRowContext(ctx,
		`SELECT `+instanceSessionCols+` FROM cloud_workspace_instance_sessions WHERE token_hash = ?`, tokenHash,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInstanceSessionInvalid
	}
	if err != nil {
		return nil, err
	}
	if row.TenantID != tenantID || row.UserID != userID || row.MachineID != machineID || row.Protocol != protocol || row.RevokedAt != "" {
		return nil, ErrInstanceSessionInvalid
	}
	expiresAt, parseErr := time.Parse(time.RFC3339, row.ExpiresAt)
	if parseErr != nil || !expiresAt.UTC().After(now) {
		return nil, ErrInstanceSessionInvalid
	}
	// Avoid turning every manifest/object request into a SQLite writer. Active
	// clients refresh only in the latter half of the idle window; heartbeat
	// traffic still keeps the session alive without contending on every chunk.
	if expiresAt.UTC().Sub(now) > InstanceSessionTTL/2 {
		return row, nil
	}
	lastSeen := now.Format(time.RFC3339)
	expires := now.Add(InstanceSessionTTL).Format(time.RFC3339)
	err = s.withImmediate(ctx, func(q queryer) error {
		res, updateErr := q.ExecContext(ctx, `UPDATE cloud_workspace_instance_sessions
			SET last_seen_at = ?, expires_at = ?
			WHERE id = ? AND token_hash = ? AND tenant_id = ? AND user_id = ? AND machine_id = ? AND protocol = ? AND revoked_at IS NULL AND expires_at > ?`,
			lastSeen, expires, row.ID, tokenHash, tenantID, userID, machineID, protocol, now.Format(time.RFC3339),
		)
		if updateErr != nil {
			return updateErr
		}
		n, rowsErr := res.RowsAffected()
		if rowsErr != nil || n != 1 {
			return ErrInstanceSessionInvalid
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	row.LastSeenAt = lastSeen
	row.ExpiresAt = expires
	return row, nil
}

// RevokeInstanceSession invalidates exactly one issued process identity.
func (s *Store) RevokeInstanceSession(ctx context.Context, principal auth.MachinePrincipal, sessionID, rawToken, protocol string, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	rawToken = strings.TrimSpace(rawToken)
	protocol = strings.TrimSpace(protocol)
	if sessionID == "" || rawToken == "" {
		return ErrInstanceSessionRequired
	}
	if protocol != CloudWorkspaceProtocol {
		return ErrProtocolMismatch
	}
	tenantID := store.NormalizeTenantID(principal.TenantID)
	userID := strings.TrimSpace(principal.UserID)
	machineID := strings.TrimSpace(principal.MachineID)
	if userID == "" || machineID == "" {
		return ErrInstanceSessionInvalid
	}
	ts := now.UTC().Format(time.RFC3339)
	return s.withImmediate(ctx, func(q queryer) error {
		res, err := q.ExecContext(ctx, `UPDATE cloud_workspace_instance_sessions SET revoked_at = ?
			WHERE id = ? AND token_hash = ? AND tenant_id = ? AND user_id = ? AND machine_id = ? AND protocol = ? AND revoked_at IS NULL`,
			ts, sessionID, instanceSessionTokenHash(rawToken), tenantID, userID, machineID, protocol,
		)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrInstanceSessionInvalid
		}
		return nil
	})
}

func (s *Service) IssueInstanceSession(ctx context.Context, principal auth.MachinePrincipal, protocol string) (*InstanceSession, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.IssueInstanceSession(ctx, principal, protocol, s.now())
}

func (s *Service) AuthenticateInstanceSession(ctx context.Context, principal auth.MachinePrincipal, rawToken, protocol string) (*InstanceSession, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.AuthenticateInstanceSession(ctx, principal, rawToken, protocol, s.now())
}

func (s *Service) RevokeInstanceSession(ctx context.Context, principal auth.MachinePrincipal, sessionID, rawToken, protocol string) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	return s.Workspaces.RevokeInstanceSession(ctx, principal, sessionID, rawToken, protocol, s.now())
}
