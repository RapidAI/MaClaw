package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const MaxManifestEntries = 20000

// ManifestEntry is one file in the workspace tree. Protocol has no mtime.
type ManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest is GET/PUT /manifest.
type Manifest struct {
	Revision string          `json:"revision"`
	Entries  []ManifestEntry `json:"entries"`
}

func newManifestRevision() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func manifestTreesEqual(a, b []ManifestEntry) bool {
	if a == nil {
		a = []ManifestEntry{}
	}
	if b == nil {
		b = []ManifestEntry{}
	}
	if len(a) != len(b) {
		return false
	}
	type key struct {
		path string
		sha  string
		size int64
	}
	have := make(map[key]struct{}, len(a))
	for _, e := range a {
		have[key{path: e.Path, sha: e.SHA256, size: e.Size}] = struct{}{}
	}
	for _, e := range b {
		if _, ok := have[key{path: e.Path, sha: e.SHA256, size: e.Size}]; !ok {
			return false
		}
	}
	return true
}

func normalizeEntries(entries []ManifestEntry) ([]ManifestEntry, error) {
	if len(entries) > MaxManifestEntries {
		return nil, ErrTooManyEntries
	}
	if entries == nil {
		entries = []ManifestEntry{}
	}
	out := make([]ManifestEntry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		p, err := ValidateManifestPath(e.Path)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[p]; ok {
			return nil, ErrInvalidPath
		}
		seen[p] = struct{}{}
		if !ValidSHA256Hex(e.SHA256) {
			return nil, ErrInvalidBlobKey
		}
		if e.Size < 0 || e.Size > MaxObjectBytes {
			return nil, ErrBlobTooLarge
		}
		out = append(out, ManifestEntry{Path: p, SHA256: e.SHA256, Size: e.Size})
	}
	return out, nil
}

func listManifestEntries(ctx context.Context, q queryer, workspaceID string) ([]ManifestEntry, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT path, sha256, size_bytes FROM cloud_workspace_manifest_entries
		WHERE workspace_id = ? ORDER BY path ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManifestEntry{}
	for rows.Next() {
		var e ManifestEntry
		if err := rows.Scan(&e.Path, &e.SHA256, &e.Size); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func objectSize(ctx context.Context, q queryer, workspaceID, sha256hex string) (int64, bool, error) {
	var n int64
	err := q.QueryRowContext(ctx, `
		SELECT size_bytes FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`,
		workspaceID, sha256hex,
	).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

func adjustRefCount(ctx context.Context, q queryer, workspaceID, sha256hex string, delta int) error {
	if delta == 0 {
		return nil
	}
	_, err := q.ExecContext(ctx, `
		UPDATE cloud_workspace_objects SET ref_count = ref_count + ? WHERE workspace_id = ? AND sha256 = ?`,
		delta, workspaceID, sha256hex)
	return err
}

func countBySHA(entries []ManifestEntry) map[string]int {
	out := make(map[string]int, len(entries))
	for _, e := range entries {
		out[e.SHA256]++
	}
	return out
}

// GetManifest returns the current tree for an owned workspace held by this machine.
func (s *Store) GetManifest(ctx context.Context, tenantID, userID, workspaceID, machineID string, now time.Time) (*Manifest, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	machineID = strings.TrimSpace(machineID)
	ws, err := requireActiveOwned(ctx, s.db, tenantID, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	if err := assertLeaseHeld(ctx, s.db, workspaceID, machineID, now); err != nil {
		return nil, err
	}
	entries, err := listManifestEntries(ctx, s.db, workspaceID)
	if err != nil {
		return nil, err
	}
	return &Manifest{Revision: ws.ManifestRevision, Entries: entries}, nil
}

// ReplaceManifest fully replaces the tree and updates usage in one IMMEDIATE tx.
func (s *Store) ReplaceManifest(ctx context.Context, tenantID, userID, workspaceID, machineID, ifMatch string, entries []ManifestEntry, now time.Time) (*Manifest, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	entries, err := normalizeEntries(entries)
	if err != nil {
		return nil, err
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	machineID = strings.TrimSpace(machineID)
	ts := now.UTC().Format(time.RFC3339)
	var out *Manifest
	err = s.withImmediate(ctx, func(q queryer) error {
		ws, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID)
		if err != nil {
			return err
		}
		if err := assertLeaseHeld(ctx, q, workspaceID, machineID, now); err != nil {
			return err
		}
		if strings.TrimSpace(ifMatch) != ws.ManifestRevision {
			return ErrRevisionConflict
		}
		old, err := listManifestEntries(ctx, q, workspaceID)
		if err != nil {
			return err
		}
		if manifestTreesEqual(old, entries) {
			out = &Manifest{Revision: ws.ManifestRevision, Entries: old}
			return nil
		}
		var used int64
		for _, e := range entries {
			size, ok, err := objectSize(ctx, q, workspaceID, e.SHA256)
			if err != nil {
				return err
			}
			if !ok {
				return ErrObjectMissing
			}
			if size != e.Size {
				return ErrBlobHashMismatch
			}
			used += e.Size
		}
		oldCounts := countBySHA(old)
		newCounts := countBySHA(entries)
		for sha, n := range oldCounts {
			if err := adjustRefCount(ctx, q, workspaceID, sha, -n); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_manifest_entries WHERE workspace_id = ?`, workspaceID); err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO cloud_workspace_manifest_entries (workspace_id, path, sha256, size_bytes)
				VALUES (?, ?, ?, ?)`, workspaceID, e.Path, e.SHA256, e.Size); err != nil {
				return err
			}
		}
		for sha, n := range newCounts {
			if err := adjustRefCount(ctx, q, workspaceID, sha, n); err != nil {
				return err
			}
		}
		rev := newManifestRevision()
		if _, err := q.ExecContext(ctx, `
			UPDATE cloud_workspaces SET used_bytes = ?, file_count = ?, manifest_revision = ?, updated_at = ?
			WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`,
			used, len(entries), rev, ts, workspaceID, tenantID, userID, StatusActive,
		); err != nil {
			return err
		}
		out = &Manifest{Revision: rev, Entries: entries}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
