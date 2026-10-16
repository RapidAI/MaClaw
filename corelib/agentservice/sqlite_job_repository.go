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

// SQLiteJobRepositorySchemaVersion is the production async_jobs schema.
// Version 1 lacked lease columns; version 2 adds lease_owner_id / lease_expires_at.
const SQLiteJobRepositorySchemaVersion = "2"

const (
	sqliteJobMetaSchemaVersion   = "schema_version"
	sqliteJobMetaLegacyImported  = "legacy_jobs_json_imported"
	sqliteJobMetaRuntimeImported = "runtime_jobs_imported"
)

// SQLiteJobRepository is the durable JobRepository shared by GUI and srv.
// Indexed identity/lifecycle columns support constraints and diagnostics
// while payload_json retains the shared Runtime envelope without teaching
// the repository about every future optional field.
type SQLiteJobRepository struct {
	mu     sync.RWMutex
	db     *sql.DB
	path   string
	closed bool
}

var _ agentruntime.JobRepository = (*SQLiteJobRepository)(nil)

// SQLiteJobRepositoryOptions customizes repository open. LegacyLoader is
// invoked at most once per database; an empty result still records the
// import marker so a later jobs.json cannot appear as a surprise snapshot.
type SQLiteJobRepositoryOptions struct {
	LegacyLoader func(ctx context.Context) ([]agentruntime.Job, error)
}

func NewSQLiteJobRepository(path string) (*SQLiteJobRepository, error) {
	return NewSQLiteJobRepositoryWithOptions(path, SQLiteJobRepositoryOptions{})
}

// NewSQLiteJobRepositoryWithLegacy opens the production schema and imports
// a host-owned snapshot (typically MaClawSrv jobs.json) exactly once.
func NewSQLiteJobRepositoryWithLegacy(path string, load func(context.Context) ([]agentruntime.Job, error)) (*SQLiteJobRepository, error) {
	return NewSQLiteJobRepositoryWithOptions(path, SQLiteJobRepositoryOptions{LegacyLoader: load})
}

func NewSQLiteJobRepositoryWithOptions(path string, opts SQLiteJobRepositoryOptions) (*SQLiteJobRepository, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("job repository path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// Apply connection-local durability/coordination PRAGMAs through the DSN so
	// they remain true even when database/sql opens a fresh connection.
	dsn := path + "?_pragma=busy_timeout%3d5000&_pragma=foreign_keys%3dON&_pragma=synchronous%3dFULL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection per repository handle makes connection-local PRAGMAs
	// deterministic. Keeping no idle connection also releases Windows file
	// handles between operations; embedders that forget Close therefore do not
	// pin a removable data root forever. Multiple handles still coordinate
	// through WAL and the per-connection busy timeout in the DSN.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	repository := &SQLiteJobRepository{db: db, path: path}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS async_job_repository_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS async_jobs (
  job_id TEXT PRIMARY KEY,
  version INTEGER NOT NULL CHECK(version > 0),
  tenant_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  idempotency_digest TEXT,
  request_digest TEXT NOT NULL DEFAULT '',
  lease_owner_id TEXT NOT NULL DEFAULT '',
  lease_expires_at TEXT,
  payload_json BLOB NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS async_jobs_idempotency_digest_uq
  ON async_jobs(idempotency_digest)
  WHERE idempotency_digest IS NOT NULL AND idempotency_digest <> ''`,
		`CREATE INDEX IF NOT EXISTS async_jobs_owner_created_idx
  ON async_jobs(tenant_id, user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS async_jobs_status_created_idx
  ON async_jobs(status, created_at)`,
	} {
		if err := retrySQLiteJobBusy(context.Background(), func() error {
			_, execErr := db.Exec(statement)
			return execErr
		}); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := repository.ensureSchemaVersion(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repository.importLegacySnapshotOnce(opts.LegacyLoader); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repository.importRuntimeJobsOnce(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repository, nil
}

// OpenGUIRuntimeJobStores opens durable Job and JobEffect repositories under
// dataDir. Callers that have no data directory should keep the memory stores.
func OpenGUIRuntimeJobStores(dataDir string) (agentruntime.JobRepository, agentruntime.JobEffectRepository, error) {
	dataDir = filepath.Clean(strings.TrimSpace(dataDir))
	if dataDir == "" || dataDir == "." {
		return nil, nil, fmt.Errorf("runtime job store directory is required")
	}
	jobs, err := NewSQLiteJobRepository(filepath.Join(dataDir, "jobs.db"))
	if err != nil {
		return nil, nil, err
	}
	effects, err := NewSQLiteJobEffectRepository(filepath.Join(dataDir, "job_effects.db"))
	if err != nil {
		_ = jobs.Close()
		return nil, nil, err
	}
	return jobs, effects, nil
}

func (r *SQLiteJobRepository) ensureSchemaVersion() error {
	var version string
	err := r.db.QueryRow(`SELECT value FROM async_job_repository_meta WHERE key=?`, sqliteJobMetaSchemaVersion).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = r.db.Exec(`INSERT INTO async_job_repository_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING`, sqliteJobMetaSchemaVersion, SQLiteJobRepositorySchemaVersion); err != nil {
			return err
		}
		err = r.db.QueryRow(`SELECT value FROM async_job_repository_meta WHERE key=?`, sqliteJobMetaSchemaVersion).Scan(&version)
	}
	if err != nil {
		return err
	}
	if version == "1" {
		if err := r.ensureLeaseColumns(); err != nil {
			return err
		}
		if _, err := r.db.Exec(`UPDATE async_job_repository_meta SET value=? WHERE key=? AND value='1'`, SQLiteJobRepositorySchemaVersion, sqliteJobMetaSchemaVersion); err != nil {
			return err
		}
		if err := r.db.QueryRow(`SELECT value FROM async_job_repository_meta WHERE key=?`, sqliteJobMetaSchemaVersion).Scan(&version); err != nil {
			return err
		}
	}
	if version != SQLiteJobRepositorySchemaVersion {
		return fmt.Errorf("unsupported async job repository schema version %q", version)
	}
	return nil
}

func (r *SQLiteJobRepository) ensureLeaseColumns() error {
	columns, err := r.asyncJobColumns()
	if err != nil {
		return err
	}
	for name, definition := range map[string]string{
		"lease_owner_id":   `TEXT NOT NULL DEFAULT ''`,
		"lease_expires_at": `TEXT`,
	} {
		if columns[name] {
			continue
		}
		err := retrySQLiteJobBusy(context.Background(), func() error {
			_, alterErr := r.db.Exec(`ALTER TABLE async_jobs ADD COLUMN ` + name + ` ` + definition)
			return alterErr
		})
		if err != nil {
			latest, inspectErr := r.asyncJobColumns()
			if inspectErr != nil || !latest[name] {
				return err
			}
		}
	}
	return nil
}

func (r *SQLiteJobRepository) asyncJobColumns() (map[string]bool, error) {
	rows, err := r.db.Query(`PRAGMA table_info(async_jobs)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (r *SQLiteJobRepository) importLegacySnapshotOnce(load func(context.Context) ([]agentruntime.Job, error)) error {
	var marker string
	err := r.db.QueryRow(`SELECT value FROM async_job_repository_meta WHERE key=?`, sqliteJobMetaLegacyImported).Scan(&marker)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var items []agentruntime.Job
	if load != nil {
		loaded, loadErr := load(context.Background())
		if loadErr != nil {
			return fmt.Errorf("load legacy async job snapshot: %w", loadErr)
		}
		items = loaded
	}
	return retrySQLiteJobBusy(context.Background(), func() error {
		tx, err := r.db.BeginTx(context.Background(), nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var alreadyImported string
		if err := tx.QueryRow(`SELECT value FROM async_job_repository_meta WHERE key=?`, sqliteJobMetaLegacyImported).Scan(&alreadyImported); err == nil {
			return tx.Commit()
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, item := range items {
			item.IdempotentReplay = false
			item.Version = 1
			if err := agentruntime.ValidateJobEnvelope(item); err != nil {
				return fmt.Errorf("legacy job %q: %w", item.ID, err)
			}
			if err := insertSQLiteAsyncJob(context.Background(), tx, item); err != nil {
				return fmt.Errorf("import legacy job %q: %w", item.ID, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO async_job_repository_meta(key,value) VALUES(?,?)`, sqliteJobMetaLegacyImported, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (r *SQLiteJobRepository) importRuntimeJobsOnce() error {
	exists, err := sqliteTableExists(r.db, "runtime_jobs")
	if err != nil || !exists {
		return err
	}
	var marker string
	err = r.db.QueryRow(`SELECT value FROM async_job_repository_meta WHERE key=?`, sqliteJobMetaRuntimeImported).Scan(&marker)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return retrySQLiteJobBusy(context.Background(), func() error {
		tx, err := r.db.BeginTx(context.Background(), nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var alreadyImported string
		if err := tx.QueryRow(`SELECT value FROM async_job_repository_meta WHERE key=?`, sqliteJobMetaRuntimeImported).Scan(&alreadyImported); err == nil {
			return tx.Commit()
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rows, err := tx.Query(`SELECT payload_json FROM runtime_jobs`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			job, err := decodeRuntimeJobPayload(raw)
			if err != nil {
				return err
			}
			job.IdempotentReplay = false
			if job.Version == 0 {
				job.Version = 1
			}
			if err := agentruntime.ValidateJobEnvelope(job); err != nil {
				return fmt.Errorf("runtime job %q: %w", job.ID, err)
			}
			if err := insertSQLiteAsyncJob(context.Background(), tx, job); err != nil {
				if !sqliteUniqueViolation(err) {
					return fmt.Errorf("import runtime job %q: %w", job.ID, err)
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO async_job_repository_meta(key,value) VALUES(?,?)`, sqliteJobMetaRuntimeImported, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.Commit()
	})
}

type sqliteAsyncJobSQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertSQLiteAsyncJob(ctx context.Context, executor sqliteAsyncJobSQLExecutor, job agentruntime.Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	var idempotency any
	if job.IdempotencyDigest != "" {
		idempotency = job.IdempotencyDigest
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO async_jobs(
  job_id,version,tenant_id,user_id,kind,status,idempotency_digest,request_digest,lease_owner_id,lease_expires_at,payload_json,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.Version, job.TenantID, job.UserID, job.Kind, string(job.Status), idempotency, job.RequestDigest,
		job.LeaseOwnerID, sqliteJobLeaseExpirySQL(job.LeaseExpiresAt), payload, job.CreatedAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (r *SQLiteJobRepository) List(ctx context.Context) ([]agentruntime.Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT payload_json,lease_owner_id,COALESCE(lease_expires_at,'') FROM async_jobs ORDER BY created_at, job_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]agentruntime.Job, 0)
	for rows.Next() {
		var payload []byte
		var leaseOwnerID, leaseExpiry string
		if err := rows.Scan(&payload, &leaseOwnerID, &leaseExpiry); err != nil {
			return nil, err
		}
		job, err := decodeSQLiteAsyncJob(payload, leaseOwnerID, leaseExpiry)
		if err != nil {
			return nil, err
		}
		items = append(items, job)
	}
	return items, rows.Err()
}

func (r *SQLiteJobRepository) Get(ctx context.Context, jobID string) (agentruntime.Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return agentruntime.Job{}, err
	}
	return getSQLiteAsyncJob(ctx, r.db, strings.TrimSpace(jobID))
}

type sqliteAsyncJobSQLQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getSQLiteAsyncJob(ctx context.Context, querier sqliteAsyncJobSQLQuerier, jobID string) (agentruntime.Job, error) {
	var payload []byte
	var leaseOwnerID, leaseExpiry string
	if err := querier.QueryRowContext(ctx, `SELECT payload_json,lease_owner_id,COALESCE(lease_expires_at,'') FROM async_jobs WHERE job_id=?`, jobID).Scan(&payload, &leaseOwnerID, &leaseExpiry); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agentruntime.Job{}, agentruntime.ErrJobRepositoryNotFound
		}
		return agentruntime.Job{}, err
	}
	return decodeSQLiteAsyncJob(payload, leaseOwnerID, leaseExpiry)
}

func getSQLiteAsyncJobByIdempotency(ctx context.Context, querier sqliteAsyncJobSQLQuerier, digest string) (agentruntime.Job, error) {
	var payload []byte
	var leaseOwnerID, leaseExpiry string
	if err := querier.QueryRowContext(ctx, `SELECT payload_json,lease_owner_id,COALESCE(lease_expires_at,'') FROM async_jobs WHERE idempotency_digest=?`, digest).Scan(&payload, &leaseOwnerID, &leaseExpiry); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agentruntime.Job{}, agentruntime.ErrJobRepositoryNotFound
		}
		return agentruntime.Job{}, err
	}
	return decodeSQLiteAsyncJob(payload, leaseOwnerID, leaseExpiry)
}

func decodeSQLiteAsyncJob(payload []byte, leaseOwnerID, leaseExpiry string) (agentruntime.Job, error) {
	job, err := decodeRuntimeJobPayload(payload)
	if err != nil {
		return agentruntime.Job{}, err
	}
	if job.Version == 0 {
		return agentruntime.Job{}, fmt.Errorf("job %q has no repository version", job.ID)
	}
	job.LeaseOwnerID = strings.TrimSpace(leaseOwnerID)
	expiresAt, err := parseSQLiteJobLeaseExpiry(job.ID, leaseExpiry)
	if err != nil {
		return agentruntime.Job{}, err
	}
	job.LeaseExpiresAt = expiresAt
	if err := agentruntime.ValidateJobEnvelope(job); err != nil {
		return agentruntime.Job{}, fmt.Errorf("job %q: %w", job.ID, err)
	}
	return agentruntime.CloneJob(job), nil
}

func decodeRuntimeJobPayload(raw []byte) (agentruntime.Job, error) {
	var job agentruntime.Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return agentruntime.Job{}, err
	}
	return job, nil
}

func (r *SQLiteJobRepository) Admit(ctx context.Context, candidate agentruntime.Job) (agentruntime.Job, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(candidate.ID) == "" {
		return agentruntime.Job{}, false, fmt.Errorf("job id is required")
	}
	candidate.IdempotentReplay = false
	candidate.Version = 1
	now := time.Now().UTC()
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = now
	}
	if err := agentruntime.ValidateJobEnvelope(candidate); err != nil {
		return agentruntime.Job{}, false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return agentruntime.Job{}, false, err
	}
	var result sql.Result
	err := retrySQLiteJobBusy(ctx, func() error {
		payload, marshalErr := json.Marshal(candidate)
		if marshalErr != nil {
			return marshalErr
		}
		var idempotency any
		if candidate.IdempotencyDigest != "" {
			idempotency = candidate.IdempotencyDigest
		}
		var execErr error
		result, execErr = r.db.ExecContext(ctx, `INSERT INTO async_jobs(
  job_id,version,tenant_id,user_id,kind,status,idempotency_digest,request_digest,lease_owner_id,lease_expires_at,payload_json,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
			candidate.ID, candidate.Version, candidate.TenantID, candidate.UserID, candidate.Kind, string(candidate.Status), idempotency,
			candidate.RequestDigest, candidate.LeaseOwnerID, sqliteJobLeaseExpirySQL(candidate.LeaseExpiresAt), payload, candidate.CreatedAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		return execErr
	})
	if err != nil {
		return agentruntime.Job{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return agentruntime.Job{}, false, err
	}
	if affected == 1 {
		return agentruntime.CloneJob(candidate), true, nil
	}
	if candidate.IdempotencyDigest != "" {
		canonical, lookupErr := getSQLiteAsyncJobByIdempotency(ctx, r.db, candidate.IdempotencyDigest)
		if lookupErr == nil {
			if canonical.RequestDigest != candidate.RequestDigest || canonical.TenantID != candidate.TenantID || canonical.UserID != candidate.UserID || canonical.Kind != candidate.Kind {
				return agentruntime.Job{}, false, agentruntime.ErrJobIdempotencyConflict
			}
			return canonical, false, nil
		}
		if !errors.Is(lookupErr, agentruntime.ErrJobRepositoryNotFound) {
			return agentruntime.Job{}, false, lookupErr
		}
	}
	if _, lookupErr := getSQLiteAsyncJob(ctx, r.db, candidate.ID); lookupErr == nil {
		return agentruntime.Job{}, false, agentruntime.ErrJobRepositoryVersionConflict
	} else if !errors.Is(lookupErr, agentruntime.ErrJobRepositoryNotFound) {
		return agentruntime.Job{}, false, lookupErr
	}
	return agentruntime.Job{}, false, fmt.Errorf("async job admission conflicted without a canonical record")
}

func (r *SQLiteJobRepository) Update(ctx context.Context, expectedVersion uint64, next agentruntime.Job) (agentruntime.Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if expectedVersion == 0 {
		return agentruntime.Job{}, agentruntime.ErrJobRepositoryVersionConflict
	}
	next.IdempotentReplay = false
	next.Version = expectedVersion + 1
	if err := agentruntime.ValidateJobEnvelope(next); err != nil {
		return agentruntime.Job{}, err
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return agentruntime.Job{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return agentruntime.Job{}, err
	}
	current, err := getSQLiteAsyncJob(ctx, r.db, next.ID)
	if err != nil {
		return agentruntime.Job{}, err
	}
	if current.Version != expectedVersion {
		return agentruntime.Job{}, agentruntime.ErrJobRepositoryVersionConflict
	}
	if err := agentruntime.ValidateJobImmutableFields(current, next); err != nil {
		return agentruntime.Job{}, err
	}
	var result sql.Result
	err = retrySQLiteJobBusy(ctx, func() error {
		var idempotency any
		if next.IdempotencyDigest != "" {
			idempotency = next.IdempotencyDigest
		}
		var execErr error
		result, execErr = r.db.ExecContext(ctx, `UPDATE async_jobs SET
  version=?,tenant_id=?,user_id=?,kind=?,status=?,idempotency_digest=?,request_digest=?,lease_owner_id=?,lease_expires_at=?,payload_json=?,updated_at=?
WHERE job_id=? AND version=?`, next.Version, next.TenantID, next.UserID, next.Kind, string(next.Status), idempotency,
			next.RequestDigest, next.LeaseOwnerID, sqliteJobLeaseExpirySQL(next.LeaseExpiresAt), payload, time.Now().UTC().Format(time.RFC3339Nano), next.ID, expectedVersion)
		return execErr
	})
	if err != nil {
		return agentruntime.Job{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return agentruntime.Job{}, err
	}
	if affected == 1 {
		return agentruntime.CloneJob(next), nil
	}
	if _, err := getSQLiteAsyncJob(ctx, r.db, next.ID); errors.Is(err, agentruntime.ErrJobRepositoryNotFound) {
		return agentruntime.Job{}, agentruntime.ErrJobRepositoryNotFound
	} else if err != nil {
		return agentruntime.Job{}, err
	}
	return agentruntime.Job{}, agentruntime.ErrJobRepositoryVersionConflict
}

func (r *SQLiteJobRepository) Delete(ctx context.Context, expected []agentruntime.JobVersion) error {
	if len(expected) == 0 {
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
	return retrySQLiteJobBusy(ctx, func() error {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		seen := make(map[string]struct{}, len(expected))
		for _, item := range expected {
			if strings.TrimSpace(item.ID) == "" || item.Version == 0 {
				return agentruntime.ErrJobRepositoryVersionConflict
			}
			if _, duplicate := seen[item.ID]; duplicate {
				return agentruntime.ErrJobRepositoryVersionConflict
			}
			seen[item.ID] = struct{}{}
			result, err := tx.ExecContext(ctx, `DELETE FROM async_jobs WHERE job_id=? AND version=?`, item.ID, item.Version)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 1 {
				continue
			}
			if _, err := getSQLiteAsyncJob(ctx, tx, item.ID); errors.Is(err, agentruntime.ErrJobRepositoryNotFound) {
				return agentruntime.ErrJobRepositoryNotFound
			} else if err != nil {
				return err
			}
			return agentruntime.ErrJobRepositoryVersionConflict
		}
		return tx.Commit()
	})
}

func (r *SQLiteJobRepository) Probe(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return err
	}
	var count int
	return r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM async_jobs`).Scan(&count)
}

func (r *SQLiteJobRepository) Close() error {
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

func (r *SQLiteJobRepository) ensureOpenLocked() error {
	if r == nil || r.db == nil || r.closed {
		return agentruntime.ErrJobRepositoryClosed
	}
	return nil
}

// MetaValue reads a repository metadata key. Tests use it to assert schema
// migrations without reaching into the unexported database handle.
func (r *SQLiteJobRepository) MetaValue(ctx context.Context, key string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.ensureOpenLocked(); err != nil {
		return "", err
	}
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM async_job_repository_meta WHERE key=?`, strings.TrimSpace(key)).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", agentruntime.ErrJobRepositoryNotFound
	}
	return value, err
}

func retrySQLiteJobBusy(ctx context.Context, operation func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; ; attempt++ {
		err := operation()
		if err == nil || !isAsyncJobSQLiteBusy(err) || attempt >= 6 {
			return err
		}
		delay := time.Duration(10*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isAsyncJobSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "sqlite_busy") || strings.Contains(text, "database table is locked")
}

func sqliteUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func sqliteJobLeaseExpirySQL(expiresAt *time.Time) any {
	if expiresAt == nil || expiresAt.IsZero() {
		return nil
	}
	return expiresAt.UTC().Format(time.RFC3339Nano)
}

func parseSQLiteJobLeaseExpiry(jobID, leaseExpiry string) (*time.Time, error) {
	leaseExpiry = strings.TrimSpace(leaseExpiry)
	if leaseExpiry == "" {
		return nil, nil
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, leaseExpiry)
	if err != nil {
		return nil, fmt.Errorf("job %q has invalid lease expiry: %w", jobID, err)
	}
	return &expiresAt, nil
}

func sqliteTableExists(db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found != "", nil
}
