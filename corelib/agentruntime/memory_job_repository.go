package agentruntime

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// MemoryJobRepository is a concurrency-safe JobRepository implementation for
// hosts that do not yet have a durable backend. It preserves the same CAS and
// idempotency semantics as the SQLite adapter, making migration incremental.
type MemoryJobRepository struct {
	mu     sync.RWMutex
	jobs   map[string]Job
	closed bool
}

var _ JobRepository = (*MemoryJobRepository)(nil)

func NewMemoryJobRepository() *MemoryJobRepository {
	return &MemoryJobRepository{jobs: make(map[string]Job)}
}

func (r *MemoryJobRepository) List(ctx context.Context) ([]Job, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, ErrJobRepositoryClosed
	}
	ids := make([]string, 0, len(r.jobs))
	for id := range r.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Job, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneMemoryJob(r.jobs[id]))
	}
	return out, nil
}

func (r *MemoryJobRepository) Get(ctx context.Context, id string) (Job, error) {
	if err := contextErr(ctx); err != nil {
		return Job{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return Job{}, ErrJobRepositoryClosed
	}
	job, ok := r.jobs[id]
	if !ok {
		return Job{}, ErrJobRepositoryNotFound
	}
	return cloneMemoryJob(job), nil
}

func (r *MemoryJobRepository) Admit(ctx context.Context, candidate Job) (Job, bool, error) {
	if err := contextErr(ctx); err != nil {
		return Job{}, false, err
	}
	if candidate.ID == "" {
		return Job{}, false, fmt.Errorf("job id is required")
	}
	if err := ValidateJobIdempotencyState(candidate.IdempotencyDigest, candidate.RequestDigest, false); err != nil {
		return Job{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Job{}, false, ErrJobRepositoryClosed
	}
	for _, existing := range r.jobs {
		if candidate.IdempotencyDigest != "" && existing.IdempotencyDigest == candidate.IdempotencyDigest {
			if existing.RequestDigest != candidate.RequestDigest {
				return Job{}, false, ErrJobIdempotencyConflict
			}
			return cloneMemoryJob(existing), false, nil
		}
	}
	if _, exists := r.jobs[candidate.ID]; exists {
		return Job{}, false, fmt.Errorf("job id already exists")
	}
	candidate.Version = 1
	r.jobs[candidate.ID] = cloneMemoryJob(candidate)
	return cloneMemoryJob(candidate), true, nil
}

func (r *MemoryJobRepository) Update(ctx context.Context, version uint64, candidate Job) (Job, error) {
	if err := contextErr(ctx); err != nil {
		return Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Job{}, ErrJobRepositoryClosed
	}
	existing, ok := r.jobs[candidate.ID]
	if !ok {
		return Job{}, ErrJobRepositoryNotFound
	}
	if existing.Version != version {
		return Job{}, ErrJobRepositoryVersionConflict
	}
	candidate.Version = version + 1
	r.jobs[candidate.ID] = cloneMemoryJob(candidate)
	return cloneMemoryJob(candidate), nil
}

func (r *MemoryJobRepository) Delete(ctx context.Context, versions []JobVersion) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrJobRepositoryClosed
	}
	for _, item := range versions {
		job, ok := r.jobs[item.ID]
		if !ok {
			return ErrJobRepositoryNotFound
		}
		if job.Version != item.Version {
			return ErrJobRepositoryVersionConflict
		}
	}
	for _, item := range versions {
		delete(r.jobs, item.ID)
	}
	return nil
}

func (r *MemoryJobRepository) Probe(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return ErrJobRepositoryClosed
	}
	return nil
}

func (r *MemoryJobRepository) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func cloneMemoryJob(job Job) Job {
	return CloneJob(job)
}
