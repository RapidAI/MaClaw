package cloudworkspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// RecoveryReport is the independently recomputed state of a restored cloud
// workspace generation. It is embedded in Hub backup manifests and compared
// again before a restore is allowed to replace the target data directory.
type RecoveryReport struct {
	Workspaces      int                  `json:"workspaces"`
	Objects         int                  `json:"objects"`
	VerifiedObjects int                  `json:"verified_objects"`
	Snapshots       int                  `json:"snapshots"`
	Sidecars        int                  `json:"sidecars"`
	Usage           RetainedUsage        `json:"usage"`
	MasterKey       *MasterKeyDescriptor `json:"master_key,omitempty"`
}

// VerifyRecovery checks the SQLite roots against the encrypted immutable file
// tree. Staging and pending files are deliberately not recovery roots: they
// may be left by a request that had not reached its atomic metadata commit.
func VerifyRecovery(ctx context.Context, db *sql.DB, blobRoot, keyDir string) (*RecoveryReport, error) {
	return verifyRecoveryWithProvider(ctx, db, blobRoot, keyDir, nil)
}

// VerifyRecoveryWithProvider is the pluggable-provider variant used by
// deployments that keep Cloud Workspace keys in an external KMS/secret
// manager. keyDir is retained only as a compatibility argument for the
// default file/environment provider and is not read when provider is set.
func VerifyRecoveryWithProvider(ctx context.Context, db *sql.DB, blobRoot string, provider KeyProvider) (*RecoveryReport, error) {
	return verifyRecoveryWithProvider(ctx, db, blobRoot, "", provider)
}

func verifyRecoveryWithProvider(ctx context.Context, db *sql.DB, blobRoot, keyDir string, provider KeyProvider) (*RecoveryReport, error) {
	if db == nil {
		return nil, ErrUnavailable
	}
	present, err := recoveryTableExists(ctx, db, "cloud_workspaces")
	if err != nil {
		return nil, err
	}
	if !present {
		return &RecoveryReport{}, nil
	}

	report := &RecoveryReport{}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspaces`).Scan(&report.Workspaces); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_objects`).Scan(&report.Objects); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_snapshots`).Scan(&report.Snapshots); err != nil {
		return nil, err
	}
	if err := verifyManifestRoots(ctx, db); err != nil {
		return nil, err
	}

	if provider == nil {
		provider = defaultKeyProvider(keyDir)
	}
	blobs := &BlobStore{Root: blobRoot, KeyDir: keyDir, Keys: provider, DB: db}
	if err := ensureRecoveryRootsReady(ctx, db); err != nil {
		return nil, err
	}
	refs, err := readyRecoveryObjects(ctx, db)
	if err != nil {
		return nil, err
	}
	sidecars, err := committedRecoverySidecars(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 || len(sidecars) > 0 {
		descriptor, keyErr := provider.Descriptor(ctx)
		if keyErr != nil {
			return nil, fmt.Errorf("cloud workspace recovery key unavailable: %w", keyErr)
		}
		report.MasterKey = &descriptor
	}
	for _, ref := range refs {
		plain, getErr := blobs.Get(ctx, ref.TenantID, ref.UserID, ref.WorkspaceID, ref.SHA256)
		if getErr != nil {
			return nil, fmt.Errorf("verify cloud workspace object %s/%s: %w", ref.WorkspaceID, ref.SHA256, getErr)
		}
		if int64(len(plain)) != ref.Size {
			return nil, fmt.Errorf("cloud workspace object %s/%s size=%d want=%d", ref.WorkspaceID, ref.SHA256, len(plain), ref.Size)
		}
		report.VerifiedObjects++
	}
	for _, item := range sidecars {
		plain, getErr := blobs.GetSidecar(ctx, item.TenantID, item.UserID, item.WorkspaceID, item.Name)
		if getErr != nil {
			return nil, fmt.Errorf("verify cloud workspace sidecar %s/%s: %w", item.WorkspaceID, item.Name, getErr)
		}
		sum := sha256.Sum256(plain)
		if hex.EncodeToString(sum[:]) != item.Revision {
			return nil, fmt.Errorf("cloud workspace sidecar %s/%s revision mismatch", item.WorkspaceID, item.Name)
		}
		report.Sidecars++
	}
	usage, err := NewStore(db).TotalRetainedUsage(ctx)
	if err != nil {
		return nil, err
	}
	report.Usage = usage
	return report, nil
}

type recoveryObject struct {
	TenantID    string
	UserID      string
	WorkspaceID string
	SHA256      string
	Size        int64
}

func ensureRecoveryRootsReady(ctx context.Context, db *sql.DB) error {
	var missing int
	err := db.QueryRowContext(ctx, `
		WITH roots(workspace_id, sha256) AS (
			SELECT workspace_id, sha256 FROM cloud_workspace_manifest_entries
			UNION
			SELECT s.workspace_id, e.sha256
			FROM cloud_workspace_snapshot_entries e
			JOIN cloud_workspace_snapshots s ON s.snapshot_id = e.snapshot_id
		)
		SELECT COUNT(*) FROM roots r
		LEFT JOIN cloud_workspace_objects o ON o.workspace_id = r.workspace_id AND o.sha256 = r.sha256
		WHERE o.sha256 IS NULL OR LOWER(COALESCE(NULLIF(TRIM(o.object_state), ''), 'ready')) != 'ready'`).Scan(&missing)
	if err != nil {
		return err
	}
	if missing != 0 {
		return fmt.Errorf("cloud workspace recovery roots have %d missing or non-ready objects", missing)
	}
	return nil
}

func readyRecoveryObjects(ctx context.Context, db *sql.DB) ([]recoveryObject, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT w.tenant_id, w.user_id, o.workspace_id, o.sha256,
			CASE WHEN COALESCE(o.plain_size_bytes, 0) > 0 THEN o.plain_size_bytes ELSE o.size_bytes END
		FROM cloud_workspace_objects o
		JOIN cloud_workspaces w ON w.id = o.workspace_id
		WHERE LOWER(COALESCE(NULLIF(TRIM(o.object_state), ''), 'ready')) = 'ready'
		ORDER BY o.workspace_id, o.sha256`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recoveryObject
	for rows.Next() {
		var item recoveryObject
		if err := rows.Scan(&item.TenantID, &item.UserID, &item.WorkspaceID, &item.SHA256, &item.Size); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type recoverySidecar struct {
	TenantID    string
	UserID      string
	WorkspaceID string
	Name        string
	Revision    string
}

func committedRecoverySidecars(ctx context.Context, db *sql.DB) ([]recoverySidecar, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT w.tenant_id, w.user_id, c.workspace_id, c.name, c.revision
		FROM cloud_workspace_sidecars c
		JOIN cloud_workspaces w ON w.id = c.workspace_id
		WHERE TRIM(c.revision) != ''
		ORDER BY c.workspace_id, c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recoverySidecar
	for rows.Next() {
		var item recoverySidecar
		if err := rows.Scan(&item.TenantID, &item.UserID, &item.WorkspaceID, &item.Name, &item.Revision); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func verifyManifestRoots(ctx context.Context, db *sql.DB) error {
	var refDrift int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cloud_workspace_objects o
		WHERE o.ref_count != (
			SELECT COUNT(*) FROM cloud_workspace_manifest_entries e
			WHERE e.workspace_id = o.workspace_id AND e.sha256 = o.sha256
		)`).Scan(&refDrift); err != nil {
		return err
	}
	if refDrift != 0 {
		return fmt.Errorf("cloud workspace object ref_count drift on %d rows", refDrift)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, used_bytes, file_count FROM cloud_workspaces ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var workspaceID string
		var storedBytes int64
		var storedCount int
		if err := rows.Scan(&workspaceID, &storedBytes, &storedCount); err != nil {
			return err
		}
		entries, err := recoveryManifestEntries(ctx, db, `SELECT path, sha256, size_bytes FROM cloud_workspace_manifest_entries WHERE workspace_id = ? ORDER BY path`, workspaceID)
		if err != nil {
			return err
		}
		var logical int64
		for _, entry := range entries {
			logical += entry.Size
		}
		if logical != storedBytes || len(entries) != storedCount {
			return fmt.Errorf("cloud workspace %s logical counters drift: bytes=%d/%d files=%d/%d", workspaceID, storedBytes, logical, storedCount, len(entries))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	snapshots, err := db.QueryContext(ctx, `SELECT snapshot_id, manifest_hash FROM cloud_workspace_snapshots ORDER BY snapshot_id`)
	if err != nil {
		return err
	}
	defer snapshots.Close()
	for snapshots.Next() {
		var snapshotID, storedHash string
		if err := snapshots.Scan(&snapshotID, &storedHash); err != nil {
			return err
		}
		entries, err := recoveryManifestEntries(ctx, db, `SELECT path, sha256, size_bytes FROM cloud_workspace_snapshot_entries WHERE snapshot_id = ? ORDER BY path`, snapshotID)
		if err != nil {
			return err
		}
		if manifestHash(entries) != strings.ToLower(strings.TrimSpace(storedHash)) {
			return fmt.Errorf("cloud workspace snapshot %s manifest hash mismatch", snapshotID)
		}
	}
	return snapshots.Err()
}

func recoveryManifestEntries(ctx context.Context, db *sql.DB, query, id string) ([]ManifestEntry, error) {
	rows, err := db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ManifestEntry
	for rows.Next() {
		var entry ManifestEntry
		if err := rows.Scan(&entry.Path, &entry.SHA256, &entry.Size); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func recoveryTableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
