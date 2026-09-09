package cloudworkspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
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
	Revision     string          `json:"revision"`
	ManifestHash string          `json:"manifest_hash"`
	Entries      []ManifestEntry `json:"entries"`
}

// ManifestDelta is a lease-protected batch patch. It never creates a second
// source of truth: the put/delete lists are applied to manifest_entries in the
// same transaction used by ReplaceManifest.
type ManifestDelta struct {
	IfMatchRevision string          `json:"if_match_revision"`
	Puts            []ManifestEntry `json:"puts,omitempty"`
	Deletes         []string        `json:"deletes,omitempty"`
}

// manifestHash computes a stable content hash over normalized path/sha/size
// tuples. It lets a handoff validate a snapshot instead of trusting a revision
// token alone.
func manifestHash(entries []ManifestEntry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Path)
		b.WriteByte(0)
		b.WriteString(e.SHA256)
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(e.Size, 10))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
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
	seenPortable := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		p, err := ValidateManifestPath(e.Path)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[p]; ok {
			return nil, ErrInvalidPath
		}
		seen[p] = struct{}{}
		portableKey := manifestPathKey(p)
		if _, ok := seenPortable[portableKey]; ok {
			// Keep the first spelling as the canonical manifest path, but reject
			// a second spelling that would alias it on a case-insensitive or
			// Unicode-normalizing client filesystem.
			return nil, ErrInvalidPath
		}
		seenPortable[portableKey] = struct{}{}
		if !ValidSHA256Hex(e.SHA256) {
			return nil, ErrInvalidBlobKey
		}
		if e.Size < 0 || e.Size > MaxObjectBytes {
			return nil, ErrBlobTooLarge
		}
		out = append(out, ManifestEntry{Path: p, SHA256: e.SHA256, Size: e.Size})
	}
	// Canonicalize order at the boundary so manifest_hash is independent of
	// client map/JSON iteration order. Snapshots and GET /manifest therefore
	// share exactly the same normalized (path, sha256, size) sequence.
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
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
		SELECT size_bytes FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ? AND COALESCE(object_state, 'ready') = 'ready'`,
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
		UPDATE cloud_workspace_objects SET ref_count = MAX(ref_count + ?, 0) WHERE workspace_id = ? AND sha256 = ?`,
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

// GetManifest returns the current tree for an owned workspace. Reads are
// intentionally lease-free so multiple machines can observe the workspace
// while another machine is writing through v2 operations.
func (s *Store) GetManifest(ctx context.Context, tenantID, userID, workspaceID, machineID string, now time.Time) (*Manifest, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	_ = strings.TrimSpace(machineID)
	_ = now
	ws, err := requireActiveOwned(ctx, s.db, tenantID, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	entries, err := listManifestEntries(ctx, s.db, workspaceID)
	if err != nil {
		return nil, err
	}
	return &Manifest{Revision: ws.ManifestRevision, ManifestHash: manifestHash(entries), Entries: entries}, nil
}

// ReplaceManifest fully replaces the tree and updates usage in one IMMEDIATE tx.
func (s *Store) ReplaceManifest(ctx context.Context, tenantID, userID, workspaceID, machineID, ifMatch string, entries []ManifestEntry, now time.Time) (*Manifest, error) {
	return s.ReplaceManifestWithSession(ctx, tenantID, userID, workspaceID, machineID, "", 0, ifMatch, entries, now)
}

func (s *Store) ReplaceManifestWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, ifMatch string, entries []ManifestEntry, now time.Time) (*Manifest, error) {
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
		if err := assertLeaseHeldForSession(ctx, q, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
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
			out = &Manifest{Revision: ws.ManifestRevision, ManifestHash: manifestHash(old), Entries: old}
			return stageAtomicIdempotency(ctx, q, out)
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
		hash := manifestHash(entries)
		snapshotID := "cwsnap_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		// Snapshot root is committed in the same SQLite transaction as the
		// manifest and usage counters. It is immutable and can be retained for
		// recovery without making snapshots a second file-tree authority.
		if _, err := q.ExecContext(ctx, `
			INSERT INTO cloud_workspace_snapshots (snapshot_id, workspace_id, manifest_revision, manifest_hash, created_by_session, created_at, retention_class)
			VALUES (?, ?, ?, ?, ?, ?, 'standard')`,
			snapshotID, workspaceID, rev, hash, clientInstanceID, ts,
		); err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO cloud_workspace_snapshot_entries (snapshot_id, path, sha256, size_bytes)
				VALUES (?, ?, ?, ?)`, snapshotID, e.Path, e.SHA256, e.Size); err != nil {
				return err
			}
		}
		out = &Manifest{Revision: rev, ManifestHash: hash, Entries: entries}
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RestoreSnapshot creates a new manifest revision from an immutable snapshot.
// History is never edited or deleted; the restore itself is an ordinary
// manifest commit and therefore participates in ref-count/usage accounting.
func (s *Store) RestoreSnapshotWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, snapshotID, ifMatch string, now time.Time) (*Manifest, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, ErrNotFound
	}
	var out *Manifest
	err := s.withImmediate(ctx, func(q queryer) error {
		ws, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID)
		if err != nil {
			return err
		}
		if err := assertLeaseHeldForSession(ctx, q, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
			return err
		}
		if strings.TrimSpace(ifMatch) != "" && strings.TrimSpace(ifMatch) != ws.ManifestRevision {
			return ErrRevisionConflict
		}
		var snapWorkspace, snapHash string
		if err := q.QueryRowContext(ctx, `SELECT workspace_id, manifest_hash FROM cloud_workspace_snapshots WHERE snapshot_id = ?`, snapshotID).Scan(&snapWorkspace, &snapHash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if snapWorkspace != workspaceID {
			return ErrNotFound
		}
		rows, err := q.QueryContext(ctx, `SELECT path, sha256, size_bytes FROM cloud_workspace_snapshot_entries WHERE snapshot_id = ? ORDER BY path ASC`, snapshotID)
		if err != nil {
			return err
		}
		var entries []ManifestEntry
		for rows.Next() {
			var e ManifestEntry
			if err := rows.Scan(&e.Path, &e.SHA256, &e.Size); err != nil {
				rows.Close()
				return err
			}
			entries = append(entries, e)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if manifestHash(entries) != strings.TrimSpace(snapHash) {
			return ErrBlobCorrupt
		}
		old, err := listManifestEntries(ctx, q, workspaceID)
		if err != nil {
			return err
		}
		if manifestTreesEqual(old, entries) {
			out = &Manifest{Revision: ws.ManifestRevision, ManifestHash: manifestHash(old), Entries: old}
			return stageAtomicIdempotency(ctx, q, out)
		}
		var used int64
		for _, e := range entries {
			size, ok, err := objectSize(ctx, q, workspaceID, e.SHA256)
			if err != nil {
				return err
			}
			if !ok || size != e.Size {
				return ErrObjectMissing
			}
			used += e.Size
		}
		for sha, n := range countBySHA(old) {
			if err := adjustRefCount(ctx, q, workspaceID, sha, -n); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_manifest_entries WHERE workspace_id = ?`, workspaceID); err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_manifest_entries (workspace_id, path, sha256, size_bytes) VALUES (?, ?, ?, ?)`, workspaceID, e.Path, e.SHA256, e.Size); err != nil {
				return err
			}
		}
		for sha, n := range countBySHA(entries) {
			if err := adjustRefCount(ctx, q, workspaceID, sha, n); err != nil {
				return err
			}
		}
		rev := newManifestRevision()
		ts := now.UTC().Format(time.RFC3339)
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspaces SET used_bytes = ?, file_count = ?, manifest_revision = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`, used, len(entries), rev, ts, workspaceID, tenantID, userID, StatusActive); err != nil {
			return err
		}
		newSnapshotID := "cwsnap_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		hash := manifestHash(entries)
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_snapshots (snapshot_id, workspace_id, manifest_revision, manifest_hash, created_by_session, created_at, retention_class) VALUES (?, ?, ?, ?, ?, ?, 'restore')`, newSnapshotID, workspaceID, rev, hash, clientInstanceID, ts); err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_snapshot_entries (snapshot_id, path, sha256, size_bytes) VALUES (?, ?, ?, ?)`, newSnapshotID, e.Path, e.SHA256, e.Size); err != nil {
				return err
			}
		}
		out = &Manifest{Revision: rev, ManifestHash: hash, Entries: entries}
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ApplyManifestDeltaWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, delta ManifestDelta, now time.Time) (*Manifest, error) {
	puts, err := normalizeEntries(delta.Puts)
	if err != nil {
		return nil, err
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	deletes := make(map[string]struct{}, len(delta.Deletes))
	for _, raw := range delta.Deletes {
		path, err := ValidateManifestPath(raw)
		if err != nil {
			return nil, err
		}
		deletes[path] = struct{}{}
	}
	var out *Manifest
	err = s.withImmediate(ctx, func(q queryer) error {
		ws, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID)
		if err != nil {
			return err
		}
		if err := assertLeaseHeldForSession(ctx, q, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
			return err
		}
		if strings.TrimSpace(delta.IfMatchRevision) != ws.ManifestRevision {
			return ErrRevisionConflict
		}
		old, err := listManifestEntries(ctx, q, workspaceID)
		if err != nil {
			return err
		}
		entriesByPath := make(map[string]ManifestEntry, len(old)+len(puts))
		for _, e := range old {
			entriesByPath[e.Path] = e
		}
		for p := range deletes {
			delete(entriesByPath, p)
		}
		for _, e := range puts {
			entriesByPath[e.Path] = e
		}
		if len(entriesByPath) > MaxManifestEntries {
			return ErrTooManyEntries
		}
		entries := make([]ManifestEntry, 0, len(entriesByPath))
		for _, e := range entriesByPath {
			entries = append(entries, e)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		if manifestTreesEqual(old, entries) {
			out = &Manifest{Revision: ws.ManifestRevision, ManifestHash: manifestHash(old), Entries: old}
			return stageAtomicIdempotency(ctx, q, out)
		}
		var used int64
		for _, e := range entries {
			size, ok, err := objectSize(ctx, q, workspaceID, e.SHA256)
			if err != nil {
				return err
			}
			if !ok || size != e.Size {
				return ErrObjectMissing
			}
			used += e.Size
		}
		for sha, n := range countBySHA(old) {
			if err := adjustRefCount(ctx, q, workspaceID, sha, -n); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_manifest_entries WHERE workspace_id = ?`, workspaceID); err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_manifest_entries (workspace_id, path, sha256, size_bytes) VALUES (?, ?, ?, ?)`, workspaceID, e.Path, e.SHA256, e.Size); err != nil {
				return err
			}
		}
		for sha, n := range countBySHA(entries) {
			if err := adjustRefCount(ctx, q, workspaceID, sha, n); err != nil {
				return err
			}
		}
		rev := newManifestRevision()
		ts := now.UTC().Format(time.RFC3339)
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspaces SET used_bytes = ?, file_count = ?, manifest_revision = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`, used, len(entries), rev, ts, workspaceID, tenantID, userID, StatusActive); err != nil {
			return err
		}
		snapshotID := "cwsnap_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		hash := manifestHash(entries)
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_snapshots (snapshot_id, workspace_id, manifest_revision, manifest_hash, created_by_session, created_at, retention_class) VALUES (?, ?, ?, ?, ?, ?, 'standard')`, snapshotID, workspaceID, rev, hash, clientInstanceID, ts); err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_snapshot_entries (snapshot_id, path, sha256, size_bytes) VALUES (?, ?, ?, ?)`, snapshotID, e.Path, e.SHA256, e.Size); err != nil {
				return err
			}
		}
		out = &Manifest{Revision: rev, ManifestHash: hash, Entries: entries}
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
