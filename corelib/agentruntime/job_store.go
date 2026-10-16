package agentruntime

import (
	"context"
	"errors"
	"strings"
)

var (
	// ErrJobRepositoryClosed means the repository no longer accepts reads or
	// writes. It is transport-neutral so hosts can expose a stable readiness
	// failure without importing a service package.
	ErrJobRepositoryClosed = errors.New("job repository is closed")
	// ErrJobRepositoryNotFound means the requested durable job no longer
	// exists. It is distinct from an unavailable repository.
	ErrJobRepositoryNotFound = errors.New("job repository record not found")
	// ErrJobRepositoryVersionConflict means a compare-and-swap mutation lost
	// a race to another host/process. Callers must reload the canonical Job and
	// must not retry the stale mutation blindly.
	ErrJobRepositoryVersionConflict = errors.New("job repository version conflict")
)

// IsJobRepositoryConcurrencyError reports a lost CAS or a vanished record.
// Hosts must reload the canonical Job and must not treat it as a persist
// failure that fails the worker.
func IsJobRepositoryConcurrencyError(err error) bool {
	return errors.Is(err, ErrJobRepositoryVersionConflict) || errors.Is(err, ErrJobRepositoryNotFound)
}

// JobStore is the transport-neutral persistence boundary for asynchronous
// work. Replace must atomically publish the complete snapshot: after success a
// subsequent Load must return that snapshot, while failure must leave the
// previously committed snapshot visible. Implementations must not retain or
// mutate caller-owned Job values.
//
// The interface intentionally does not prescribe retries or replay. A host
// must reconcile unknown external effects before replacing their state.
type JobStore interface {
	Load(context.Context) ([]Job, error)
	Replace(context.Context, []Job) error
}

// JobVersion identifies the exact durable revision to delete. Repositories
// must validate every member before deleting any member so Delete remains
// atomic for bulk API and retention operations.
type JobVersion struct {
	ID      string
	Version uint64
}

// JobRepository is the multi-writer persistence boundary for asynchronous
// Runtime work. Admission and the idempotency lookup are one atomic operation;
// Update/Delete use optimistic concurrency so one process cannot overwrite a
// newer transition committed by another process.
//
// Implementations must enforce a database-level unique constraint for every
// non-empty IdempotencyDigest. Admit returns the canonical existing Job with
// created=false for a same-request replay and ErrJobIdempotencyConflict for a
// same identity used with a different request. Returned Jobs never set the
// response-only IdempotentReplay field.
type JobRepository interface {
	List(context.Context) ([]Job, error)
	Get(context.Context, string) (Job, error)
	Admit(context.Context, Job) (canonical Job, created bool, err error)
	Update(context.Context, uint64, Job) (Job, error)
	Delete(context.Context, []JobVersion) error
	Probe(context.Context) error
	Close() error
}

// UpsertJob admits a new Job or CAS-updates the existing one. GUI skill
// runs, orchestration plans and user-data migration use this so a DTO
// mirror cannot invent a second lifecycle write path. Version conflicts
// retry against the canonical record.
func UpsertJob(ctx context.Context, repo JobRepository, candidate Job) error {
	if repo == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(candidate.ID) == "" {
		return errors.New("job id is required")
	}
	for attempt := 0; attempt < 8; attempt++ {
		current, err := repo.Get(ctx, candidate.ID)
		if errors.Is(err, ErrJobRepositoryNotFound) {
			_, _, err = repo.Admit(ctx, candidate)
			if errors.Is(err, ErrJobRepositoryVersionConflict) || errors.Is(err, ErrJobIdempotencyConflict) {
				continue
			}
			return err
		}
		if err != nil {
			return err
		}
		next := candidate
		next.ID = current.ID
		next.TenantID = current.TenantID
		next.UserID = current.UserID
		next.Kind = current.Kind
		next.IdempotencyDigest = current.IdempotencyDigest
		next.RequestDigest = current.RequestDigest
		next.LeaseOwnerID = current.LeaseOwnerID
		next.CreatedAt = current.CreatedAt
		_, err = repo.Update(ctx, current.Version, next)
		if errors.Is(err, ErrJobRepositoryVersionConflict) {
			continue
		}
		return err
	}
	return ErrJobRepositoryVersionConflict
}
