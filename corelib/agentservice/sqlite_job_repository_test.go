package agentservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	_ "modernc.org/sqlite"
)

func TestSQLiteJobRepositorySurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.db")
	first, err := NewSQLiteJobRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admitted, created, err := first.Admit(ctx, agentruntime.Job{ID: "job-1", Kind: "skill.run", Status: agentruntime.JobStatusRunning, TenantID: "gui", UserID: "local"})
	if err != nil || !created || admitted.Version != 1 {
		t.Fatalf("admit=%#v created=%t err=%v", admitted, created, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteJobRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	got, err := second.Get(ctx, "job-1")
	if err != nil || got.Kind != "skill.run" || got.Status != agentruntime.JobStatusRunning || got.Version != 1 {
		t.Fatalf("reopen get=%#v err=%v", got, err)
	}
}

func TestOpenGUIRuntimeJobStoresAndUpsert(t *testing.T) {
	jobs, effects, err := OpenGUIRuntimeJobStores(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = jobs.Close()
		_ = effects.Close()
	})
	ctx := context.Background()
	if err := agentruntime.UpsertJob(ctx, jobs, agentruntime.Job{ID: "run-1", Kind: "skill.run", Status: agentruntime.JobStatusRunning, TenantID: "gui", UserID: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := agentruntime.UpsertJob(ctx, jobs, agentruntime.Job{ID: "run-1", Kind: "skill.run", Status: agentruntime.JobStatusSucceeded}); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.Get(ctx, "run-1")
	if err != nil || got.Status != agentruntime.JobStatusSucceeded || got.TenantID != "gui" || got.CreatedAt.IsZero() || got.Version != 2 {
		t.Fatalf("get=%#v err=%v", got, err)
	}
}

func TestSQLiteJobRepositoryAdmissionIsUniqueAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	first, err := NewSQLiteJobRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewSQLiteJobRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	identity, err := agentruntime.NewJobIdempotencyIdentity("tenant", "user", "test.repository", "client-key", map[string]string{"value": "same"})
	if err != nil {
		t.Fatal(err)
	}
	job := func(id string) agentruntime.Job {
		return agentruntime.Job{
			ID: id, Kind: "test.repository", Status: agentruntime.JobStatusPending,
			IdempotencyDigest: identity.Digest, RequestDigest: identity.RequestDigest,
			TenantID: "tenant", UserID: "user", CreatedAt: time.Now().UTC(),
		}
	}
	const callers = 16
	start := make(chan struct{})
	results := make([]agentruntime.Job, callers)
	created := make([]bool, callers)
	errs := make([]error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			repo := agentruntime.JobRepository(first)
			if index%2 == 1 {
				repo = second
			}
			results[index], created[index], errs[index] = repo.Admit(context.Background(), job(NewID("job")))
		}(i)
	}
	close(start)
	wait.Wait()
	canonicalID := ""
	createdCount := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("admission %d: %v", i, errs[i])
		}
		if canonicalID == "" {
			canonicalID = results[i].ID
		}
		if results[i].ID != canonicalID {
			t.Fatalf("non-canonical admission %d: %#v", i, results[i])
		}
		if created[i] {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created admissions = %d, want 1", createdCount)
	}
	conflicting, err := agentruntime.NewJobIdempotencyIdentity("tenant", "user", "test.repository", "client-key", map[string]string{"value": "different"})
	if err != nil {
		t.Fatal(err)
	}
	conflictJob := job(NewID("job"))
	conflictJob.IdempotencyDigest = conflicting.Digest
	conflictJob.RequestDigest = conflicting.RequestDigest
	if _, created, err := first.Admit(context.Background(), conflictJob); created || !errors.Is(err, agentruntime.ErrJobIdempotencyConflict) {
		t.Fatalf("conflicting admission created=%v err=%v", created, err)
	}
}

func TestSQLiteJobRepositoryImportsLegacySnapshotOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	var loads int
	loader := func(context.Context) ([]agentruntime.Job, error) {
		loads++
		return []agentruntime.Job{{
			ID: "job_legacy", Kind: "legacy", Status: agentruntime.JobStatusSucceeded,
			TenantID: "tenant", UserID: "user", CreatedAt: time.Now().UTC(), Version: 1,
		}}, nil
	}
	first, err := NewSQLiteJobRepositoryWithLegacy(path, loader)
	if err != nil {
		t.Fatal(err)
	}
	items, err := first.List(context.Background())
	if err != nil || len(items) != 1 || items[0].ID != "job_legacy" || items[0].Version != 1 {
		t.Fatalf("imported jobs = %#v err=%v", items, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteJobRepositoryWithLegacy(path, loader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	items, err = second.List(context.Background())
	if err != nil || len(items) != 1 || loads != 1 {
		t.Fatalf("legacy snapshot imported more than once: items=%#v loads=%d err=%v", items, loads, err)
	}
}

func TestSQLiteJobRepositoryMigratesVersionOneLeaseColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE async_job_repository_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE async_jobs (
  job_id TEXT PRIMARY KEY,
  version INTEGER NOT NULL CHECK(version > 0),
  tenant_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  idempotency_digest TEXT,
  request_digest TEXT NOT NULL DEFAULT '',
  payload_json BLOB NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO async_job_repository_meta(key,value) VALUES('schema_version','1'),('legacy_jobs_json_imported','done');`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err := NewSQLiteJobRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	version, err := repository.MetaValue(context.Background(), "schema_version")
	if err != nil || version != SQLiteJobRepositorySchemaVersion {
		t.Fatalf("schema version = %q err=%v", version, err)
	}
	expires := time.Now().UTC().Add(time.Minute)
	job := agentruntime.Job{
		ID: "job_leased", Kind: "test.repository", Status: agentruntime.JobStatusPending,
		TenantID: "tenant", UserID: "user", CreatedAt: time.Now().UTC(),
		LeaseOwnerID: "executor_one", LeaseExpiresAt: &expires,
	}
	if _, created, err := repository.Admit(context.Background(), job); err != nil || !created {
		t.Fatalf("admit leased job after migration: created=%v err=%v", created, err)
	}
	got, err := repository.Get(context.Background(), "job_leased")
	if err != nil || got.LeaseOwnerID != "executor_one" || got.LeaseExpiresAt == nil {
		t.Fatalf("persisted lease = %#v err=%v", got, err)
	}
}

func TestSQLiteJobRepositoryImportsRuntimeJobsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(agentruntime.Job{
		ID: "gui-job", Kind: "skill.run", Status: agentruntime.JobStatusSucceeded,
		TenantID: "gui", UserID: "local", Version: 1, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE runtime_jobs (
  job_id TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  tenant_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  idempotency_digest TEXT,
  payload_json BLOB NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO runtime_jobs(job_id,version,tenant_id,user_id,kind,status,payload_json,created_at,updated_at)
VALUES('gui-job',1,'gui','local','skill.run','succeeded',?, datetime('now'), datetime('now'));`, payload)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := NewSQLiteJobRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := first.Get(context.Background(), "gui-job")
	if err != nil || got.Kind != "skill.run" || got.Status != agentruntime.JobStatusSucceeded {
		t.Fatalf("imported runtime job = %#v err=%v", got, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteJobRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	items, err := second.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("runtime_jobs imported more than once: %#v err=%v", items, err)
	}
}
