package agentservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	_ "modernc.org/sqlite"
)

// SQLiteJobEffectRepository is the shared protected effect/replay repository
// used by non-UI Runtime hosts. It intentionally lives in agentservice rather
// than MaClawSrv so GUI and srv compositions can use the same implementation.
type SQLiteJobEffectRepository struct {
	mu     sync.RWMutex
	db     *sql.DB
	closed bool
}

var _ agentruntime.JobEffectRepository = (*SQLiteJobEffectRepository)(nil)

func NewSQLiteJobEffectRepository(path string) (*SQLiteJobEffectRepository, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("job effect repository path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=busy_timeout%3d5000&_pragma=foreign_keys%3dON&_pragma=synchronous%3dFULL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	repository := &SQLiteJobEffectRepository{db: db}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS async_job_effects (
  job_id TEXT NOT NULL,
  effect_kind TEXT NOT NULL,
  version INTEGER NOT NULL CHECK(version > 0),
  tenant_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  job_kind TEXT NOT NULL,
  resource_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  receipt_digest TEXT NOT NULL DEFAULT '',
  reason_code TEXT NOT NULL DEFAULT '',
  payload_json BLOB,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(job_id, effect_kind)
)`,
		`CREATE INDEX IF NOT EXISTS async_job_effects_scope_idx ON async_job_effects(tenant_id, user_id, job_kind, updated_at)`,
		`CREATE INDEX IF NOT EXISTS async_job_effects_state_idx ON async_job_effects(state, updated_at)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return repository, nil
}

func (r *SQLiteJobEffectRepository) ListByJob(ctx context.Context, jobID string) ([]agentruntime.JobEffect, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT job_id,effect_kind,version,tenant_id,user_id,job_kind,resource_id,state,receipt_digest,reason_code,payload_json,created_at,updated_at FROM async_job_effects WHERE job_id=? ORDER BY effect_kind`, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var effects []agentruntime.JobEffect
	for rows.Next() {
		effect, err := scanJobEffect(rows)
		if err != nil {
			return nil, err
		}
		effects = append(effects, effect)
	}
	return effects, rows.Err()
}

func (r *SQLiteJobEffectRepository) Get(ctx context.Context, jobID, kind string) (agentruntime.JobEffect, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return agentruntime.JobEffect{}, err
	}
	return getSQLiteJobEffect(ctx, r.db, jobID, kind)
}

func (r *SQLiteJobEffectRepository) Prepare(ctx context.Context, candidate agentruntime.JobEffect) (agentruntime.JobEffect, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	candidate.Version = 1
	candidate.State = agentruntime.JobEffectPrepared
	candidate.ResourceID = strings.TrimSpace(candidate.ResourceID)
	candidate.ReceiptDigest = ""
	candidate.ReasonCode = ""
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = now
	}
	candidate.UpdatedAt = candidate.CreatedAt
	if err := agentruntime.ValidateJobEffect(candidate); err != nil {
		return agentruntime.JobEffect{}, false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return agentruntime.JobEffect{}, false, err
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO async_job_effects(job_id,effect_kind,version,tenant_id,user_id,job_kind,resource_id,state,receipt_digest,reason_code,payload_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(job_id,effect_kind) DO NOTHING`,
		candidate.JobID, candidate.Kind, candidate.Version, candidate.TenantID, candidate.UserID, candidate.JobKind, candidate.ResourceID, candidate.State, candidate.ReceiptDigest, candidate.ReasonCode, nullableJobEffectPayload(candidate.Payload), jobEffectTime(candidate.CreatedAt), jobEffectTime(candidate.UpdatedAt))
	if err != nil {
		return agentruntime.JobEffect{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return agentruntime.JobEffect{}, false, err
	}
	if affected == 1 {
		return cloneJobEffect(candidate), true, nil
	}
	current, err := getSQLiteJobEffect(ctx, r.db, candidate.JobID, candidate.Kind)
	if err != nil {
		return agentruntime.JobEffect{}, false, err
	}
	if !sameJobEffectIdentity(current, candidate) || current.ResourceID != candidate.ResourceID || !jsonBytesEqual(current.Payload, candidate.Payload) {
		return agentruntime.JobEffect{}, false, agentruntime.ErrJobEffectConflict
	}
	return current, false, nil
}

func (r *SQLiteJobEffectRepository) Update(ctx context.Context, expectedVersion uint64, next agentruntime.JobEffect) (agentruntime.JobEffect, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if expectedVersion == 0 {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return agentruntime.JobEffect{}, err
	}
	current, err := getSQLiteJobEffect(ctx, r.db, next.JobID, next.Kind)
	if err != nil {
		return agentruntime.JobEffect{}, err
	}
	if current.Version != expectedVersion || !sameJobEffectIdentity(current, next) || !current.CreatedAt.Equal(next.CreatedAt) {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	if current.ResourceID != "" && current.ResourceID != next.ResourceID {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	if !validJobEffectTransition(current.State, next.State) {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	if current.State == agentruntime.JobEffectCommitted || current.State == agentruntime.JobEffectFailed {
		if current.ResourceID != next.ResourceID || current.ReceiptDigest != next.ReceiptDigest || current.ReasonCode != next.ReasonCode || !jsonBytesEqual(current.Payload, next.Payload) {
			return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
		}
		return current, nil
	}
	next.Version = expectedVersion + 1
	next.ResourceID = strings.TrimSpace(next.ResourceID)
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = time.Now().UTC()
	if err := agentruntime.ValidateJobEffect(next); err != nil {
		return agentruntime.JobEffect{}, err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE async_job_effects SET version=?,resource_id=?,state=?,receipt_digest=?,reason_code=?,payload_json=?,updated_at=? WHERE job_id=? AND effect_kind=? AND version=?`,
		next.Version, next.ResourceID, next.State, next.ReceiptDigest, next.ReasonCode, nullableJobEffectPayload(next.Payload), jobEffectTime(next.UpdatedAt), next.JobID, next.Kind, expectedVersion)
	if err != nil {
		return agentruntime.JobEffect{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return agentruntime.JobEffect{}, err
	}
	if affected != 1 {
		if _, getErr := getSQLiteJobEffect(ctx, r.db, next.JobID, next.Kind); errors.Is(getErr, agentruntime.ErrJobEffectNotFound) {
			return agentruntime.JobEffect{}, getErr
		}
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	return cloneJobEffect(next), nil
}

func (r *SQLiteJobEffectRepository) DeleteByJobs(ctx context.Context, jobIDs []string) error {
	if len(jobIDs) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	seen := make(map[string]struct{}, len(jobIDs))
	for _, jobID := range jobIDs {
		jobID = strings.TrimSpace(jobID)
		if jobID == "" {
			return agentruntime.ErrInvalidJobEffect
		}
		if _, duplicate := seen[jobID]; duplicate {
			continue
		}
		seen[jobID] = struct{}{}
		if _, err := tx.ExecContext(ctx, `DELETE FROM async_job_effects WHERE job_id=?`, jobID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLiteJobEffectRepository) Probe(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return err
	}
	var count int
	return r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_job_effects`).Scan(&count)
}

func (r *SQLiteJobEffectRepository) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *SQLiteJobEffectRepository) ensureOpenLocked() error {
	if r == nil || r.db == nil || r.closed {
		return errors.New("job effect repository is closed")
	}
	return nil
}

type jobEffectScanner interface {
	Scan(...any) error
}

func scanJobEffect(scanner jobEffectScanner) (agentruntime.JobEffect, error) {
	var effect agentruntime.JobEffect
	var state, createdAt, updatedAt string
	var payload []byte
	if err := scanner.Scan(&effect.JobID, &effect.Kind, &effect.Version, &effect.TenantID, &effect.UserID, &effect.JobKind, &effect.ResourceID, &state, &effect.ReceiptDigest, &effect.ReasonCode, &payload, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agentruntime.JobEffect{}, agentruntime.ErrJobEffectNotFound
		}
		return agentruntime.JobEffect{}, err
	}
	effect.State = agentruntime.JobEffectState(state)
	effect.Payload = append(json.RawMessage(nil), payload...)
	effect.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	effect.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if err := agentruntime.ValidateJobEffect(effect); err != nil {
		return agentruntime.JobEffect{}, err
	}
	return cloneJobEffect(effect), nil
}

func getSQLiteJobEffect(ctx context.Context, db *sql.DB, jobID, kind string) (agentruntime.JobEffect, error) {
	row := db.QueryRowContext(ctx, `SELECT job_id,effect_kind,version,tenant_id,user_id,job_kind,resource_id,state,receipt_digest,reason_code,payload_json,created_at,updated_at FROM async_job_effects WHERE job_id=? AND effect_kind=?`, strings.TrimSpace(jobID), strings.TrimSpace(kind))
	return scanJobEffect(row)
}

func sameJobEffectIdentity(a, b agentruntime.JobEffect) bool {
	return a.JobID == b.JobID && a.TenantID == b.TenantID && a.UserID == b.UserID && a.JobKind == b.JobKind && a.Kind == b.Kind
}

func validJobEffectTransition(current, next agentruntime.JobEffectState) bool {
	switch current {
	case agentruntime.JobEffectPrepared:
		return next == agentruntime.JobEffectPrepared || next == agentruntime.JobEffectCommitted || next == agentruntime.JobEffectFailed || next == agentruntime.JobEffectUnknown
	case agentruntime.JobEffectUnknown:
		return next == agentruntime.JobEffectUnknown || next == agentruntime.JobEffectCommitted || next == agentruntime.JobEffectFailed
	case agentruntime.JobEffectCommitted, agentruntime.JobEffectFailed:
		return next == current
	default:
		return false
	}
}

func nullableJobEffectPayload(payload json.RawMessage) any {
	if len(payload) == 0 {
		return nil
	}
	return []byte(payload)
}

func jsonBytesEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return string(a) == string(b)
}

func cloneJobEffect(effect agentruntime.JobEffect) agentruntime.JobEffect {
	effect.Payload = append(json.RawMessage(nil), effect.Payload...)
	return effect
}

func jobEffectTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
