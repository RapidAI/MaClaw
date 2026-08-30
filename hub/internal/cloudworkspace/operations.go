package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

var ErrOperationConflict = errors.New("cloud workspace operation conflict")

type Operation struct {
	OpID             string `json:"op_id"`
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	BaseFileRevision string `json:"base_file_revision,omitempty"`
	ObjectSHA256     string `json:"object_sha256,omitempty"`
	PlainSize        int64  `json:"plain_size,omitempty"`
	ClientInstanceID string `json:"client_instance_id"`
}

type OperationResult struct {
	Accepted     bool   `json:"accepted"`
	WorkspaceSeq int64  `json:"workspace_seq"`
	FileRevision string `json:"file_revision"`
	Merge        string `json:"merge"`
	ConflictSeq  int64  `json:"conflict_seq,omitempty"`
}

// Event is the append-only change-log record consumed by multi-machine
// clients.  Keeping this typed avoids silently changing JSON field types when
// the storage implementation evolves.
type Event struct {
	Seq              int64  `json:"seq"`
	OpID             string `json:"op_id"`
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	BaseFileRevision string `json:"base_file_revision,omitempty"`
	NewFileRevision  string `json:"new_file_revision"`
	ObjectSHA256     string `json:"object_sha256,omitempty"`
	ClientInstanceID string `json:"client_instance_id"`
	ConflictOfSeq    int64  `json:"conflict_of_seq,omitempty"`
	CreatedAt        string `json:"created_at"`
}

func newFileRevision() string { return "fr_" + strings.ReplaceAll(uuid.NewString(), "-", "") }

// bootstrapFilesFromManifest lazily initializes the v2 per-file state for
// workspaces created by the legacy manifest API. It is idempotent and runs in
// the same transaction as the first operation, so a concurrent writer cannot
// observe an empty v2 tree and accidentally treat an existing file as new.
func bootstrapFilesFromManifest(ctx context.Context, q queryer, workspaceID string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO cloud_workspace_files
			(workspace_id, path, file_revision, object_sha256, plain_size_bytes, tombstone, updated_seq)
		SELECT m.workspace_id, m.path,
			'legacy_' || lower(hex(randomblob(16))), m.sha256, m.size_bytes, 0, 0
		FROM cloud_workspace_manifest_entries m
		WHERE m.workspace_id = ?
		  AND NOT EXISTS (
			SELECT 1 FROM cloud_workspace_files f
			WHERE f.workspace_id = m.workspace_id AND f.path = m.path
		  )`, workspaceID)
	return err
}

// ApplyOperation atomically applies one file operation. It intentionally does
// not require the legacy exclusive lease: v2 sessions are multi-writer.
func (s *Store) ApplyOperation(ctx context.Context, tenantID, userID, workspaceID string, op Operation, now time.Time) (*OperationResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID, workspaceID, op.OpID, op.Path, op.Kind = strings.TrimSpace(userID), strings.TrimSpace(workspaceID), strings.TrimSpace(op.OpID), strings.TrimSpace(op.Path), strings.ToLower(strings.TrimSpace(op.Kind))
	if op.OpID == "" || op.Path == "" || op.ClientInstanceID == "" || (op.Kind != "put" && op.Kind != "delete") {
		return nil, ErrInvalidPath
	}
	if op.PlainSize < 0 || op.PlainSize > MaxObjectBytes {
		return nil, ErrBlobTooLarge
	}
	clean, err := ValidateManifestPath(op.Path)
	if err != nil {
		return nil, err
	}
	op.Path = clean
	if op.Kind == "put" && !ValidSHA256Hex(op.ObjectSHA256) {
		return nil, ErrInvalidBlobKey
	}
	if op.Kind == "delete" {
		op.ObjectSHA256 = ""
		op.PlainSize = 0
	}
	var out *OperationResult
	err = s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID); err != nil {
			return err
		}
		if err := bootstrapFilesFromManifest(ctx, q, workspaceID); err != nil {
			return err
		}
		if op.Kind == "put" {
			var objectSize, plainSize int64
			if err := q.QueryRowContext(ctx, `SELECT size_bytes, COALESCE(plain_size_bytes, size_bytes) FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, op.ObjectSHA256).Scan(&objectSize, &plainSize); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrObjectMissing
				}
				return err
			}
			if plainSize <= 0 {
				plainSize = objectSize
			}
			if op.PlainSize != 0 && op.PlainSize != plainSize {
				return ErrBlobHashMismatch
			}
			op.PlainSize = plainSize
		}
		var seq int64
		var rev, base string
		var tomb int
		var currentSeq64 int64
		var conflictSeq sql.NullInt64
		err := q.QueryRowContext(ctx, `SELECT seq, new_file_revision, conflict_of_seq FROM cloud_workspace_events WHERE workspace_id = ? AND op_id = ?`, workspaceID, op.OpID).Scan(&seq, &rev, &conflictSeq)
		if err == nil {
			if conflictSeq.Valid {
				out = &OperationResult{Accepted: false, WorkspaceSeq: seq, FileRevision: rev, Merge: "conflict", ConflictSeq: conflictSeq.Int64}
			} else {
				out = &OperationResult{Accepted: true, WorkspaceSeq: seq, FileRevision: rev, Merge: "duplicate"}
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rowErr := q.QueryRowContext(ctx, `SELECT file_revision, tombstone, updated_seq FROM cloud_workspace_files WHERE workspace_id = ? AND path = ?`, workspaceID, op.Path).Scan(&base, &tomb, &currentSeq64)
		if errors.Is(rowErr, sql.ErrNoRows) {
			base = ""
		} else if rowErr != nil {
			return rowErr
		}
		currentSeq := int(currentSeq64)
		merge := "none"
		if strings.TrimSpace(op.BaseFileRevision) != strings.TrimSpace(base) {
			merge = "conflict"
		}
		newRev := newFileRevision()
		if merge == "conflict" {
			// A rejected operation does not create a new file revision. Keeping
			// the winning revision in the event makes idempotent retries stable.
			newRev = base
		}
		res, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_events (workspace_id, op_id, path, kind, base_file_revision, new_file_revision, object_sha256, client_instance_id, conflict_of_seq, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, workspaceID, op.OpID, op.Path, op.Kind, op.BaseFileRevision, newRev, op.ObjectSHA256, op.ClientInstanceID, nullableSeq(currentSeq, merge == "conflict"), now.UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
		seq, _ = res.LastInsertId()
		if merge == "conflict" {
			// Keep the canonical file untouched. The event points at the
			// revision that won the race so clients can fetch it and perform a
			// three-way merge. ConflictSeq must never point at the conflict event
			// itself (that made retries impossible to reason about).
			out = &OperationResult{Accepted: false, WorkspaceSeq: seq, FileRevision: base, Merge: merge, ConflictSeq: currentSeq64}
			return nil
		}
		tombstone := 0
		if op.Kind == "delete" {
			tombstone = 1
		}
		_, err = q.ExecContext(ctx, `INSERT INTO cloud_workspace_files (workspace_id, path, file_revision, object_sha256, plain_size_bytes, tombstone, updated_seq) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(workspace_id,path) DO UPDATE SET file_revision=excluded.file_revision, object_sha256=excluded.object_sha256, plain_size_bytes=excluded.plain_size_bytes, tombstone=excluded.tombstone, updated_seq=excluded.updated_seq`, workspaceID, op.Path, newRev, op.ObjectSHA256, op.PlainSize, tombstone, seq)
		if err != nil {
			return err
		}
		out = &OperationResult{Accepted: true, WorkspaceSeq: seq, FileRevision: newRev, Merge: merge}
		return nil
	})
	return out, err
}

func nullableSeq(seq int, present bool) any {
	if !present || seq <= 0 {
		return nil
	}
	return seq
}

func (s *Store) ListEvents(ctx context.Context, workspaceID string, after, limit int64) ([]Event, error) {
	if after < 0 {
		after = 0
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, op_id, path, kind, base_file_revision, new_file_revision, object_sha256, client_instance_id, conflict_of_seq, created_at FROM cloud_workspace_events WHERE workspace_id = ? AND seq > ? ORDER BY seq LIMIT ?`, workspaceID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var seq int64
		var ev Event
		var conflict sql.NullInt64
		var base, objectSHA sql.NullString
		if err := rows.Scan(&seq, &ev.OpID, &ev.Path, &ev.Kind, &base, &ev.NewFileRevision, &objectSHA, &ev.ClientInstanceID, &conflict, &ev.CreatedAt); err != nil {
			return nil, err
		}
		ev.Seq = seq
		if base.Valid {
			ev.BaseFileRevision = base.String
		}
		if objectSHA.Valid {
			ev.ObjectSHA256 = objectSHA.String
		}
		if conflict.Valid {
			ev.ConflictOfSeq = conflict.Int64
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
