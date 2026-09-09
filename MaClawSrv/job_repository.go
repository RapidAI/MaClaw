package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

const asyncJobRepositorySchemaVersion = agentservice.SQLiteJobRepositorySchemaVersion

// newSQLiteAsyncJobRepository opens the shared production JobRepository and
// imports jobs.json exactly once. Schema, leases, busy retry and CAS live in
// agentservice so GUI and srv persist the same envelope.
func newSQLiteAsyncJobRepository(path, legacyPath string) (*agentservice.SQLiteJobRepository, error) {
	legacyPath = strings.TrimSpace(legacyPath)
	var loader func(context.Context) ([]agentruntime.Job, error)
	if legacyPath != "" {
		loader = func(ctx context.Context) ([]agentruntime.Job, error) {
			return (&fileAsyncJobStore{path: legacyPath}).Load(ctx)
		}
	}
	return agentservice.NewSQLiteJobRepositoryWithLegacy(path, loader)
}

func cloneAsyncJobEnvelope(job agentruntime.Job) agentruntime.Job {
	return agentruntime.CloneJob(job)
}

func validateAsyncJobEnvelope(job agentruntime.Job) error {
	return agentruntime.ValidateJobEnvelope(job)
}

func validateAsyncJobImmutableFields(current, next agentruntime.Job) error {
	return agentruntime.ValidateJobImmutableFields(current, next)
}

// jobStoreRepositoryAdapter preserves the old atomic-snapshot JobStore for
// tests and single-writer deployments. It intentionally provides no
// cross-process guarantee; MaClawSrv production composition uses SQLite.
type jobStoreRepositoryAdapter struct {
	mu     sync.Mutex
	store  agentruntime.JobStore
	closed bool
}

var _ agentruntime.JobRepository = (*jobStoreRepositoryAdapter)(nil)

func newJobStoreRepositoryAdapter(store agentruntime.JobStore) *jobStoreRepositoryAdapter {
	return &jobStoreRepositoryAdapter{store: store}
}

func (s *jobStoreRepositoryAdapter) loadLocked(ctx context.Context) ([]agentruntime.Job, error) {
	if s == nil || s.store == nil || s.closed {
		return nil, errors.New("async job repository is closed")
	}
	items, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].IdempotentReplay = false
		if items[i].Version == 0 {
			items[i].Version = 1
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (s *jobStoreRepositoryAdapter) List(ctx context.Context) ([]agentruntime.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = cloneAsyncJobEnvelope(items[i])
	}
	return items, nil
}

func (s *jobStoreRepositoryAdapter) Get(ctx context.Context, jobID string) (agentruntime.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked(ctx)
	if err != nil {
		return agentruntime.Job{}, err
	}
	for _, item := range items {
		if item.ID == jobID {
			return cloneAsyncJobEnvelope(item), nil
		}
	}
	return agentruntime.Job{}, agentruntime.ErrJobRepositoryNotFound
}

func (s *jobStoreRepositoryAdapter) Admit(ctx context.Context, candidate agentruntime.Job) (agentruntime.Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked(ctx)
	if err != nil {
		return agentruntime.Job{}, false, err
	}
	for _, item := range items {
		if candidate.IdempotencyDigest != "" && item.IdempotencyDigest == candidate.IdempotencyDigest {
			if item.RequestDigest != candidate.RequestDigest || item.TenantID != candidate.TenantID || item.UserID != candidate.UserID || item.Kind != candidate.Kind {
				return agentruntime.Job{}, false, agentruntime.ErrJobIdempotencyConflict
			}
			return cloneAsyncJobEnvelope(item), false, nil
		}
		if item.ID == candidate.ID {
			return agentruntime.Job{}, false, agentruntime.ErrJobRepositoryVersionConflict
		}
	}
	candidate.IdempotentReplay = false
	candidate.Version = 1
	if err := validateAsyncJobEnvelope(candidate); err != nil {
		return agentruntime.Job{}, false, err
	}
	items = append(items, cloneAsyncJobEnvelope(candidate))
	if err := s.store.Replace(ctx, items); err != nil {
		return agentruntime.Job{}, false, err
	}
	return cloneAsyncJobEnvelope(candidate), true, nil
}

func (s *jobStoreRepositoryAdapter) Update(ctx context.Context, expectedVersion uint64, next agentruntime.Job) (agentruntime.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked(ctx)
	if err != nil {
		return agentruntime.Job{}, err
	}
	found := false
	for i := range items {
		if items[i].ID != next.ID {
			continue
		}
		found = true
		if items[i].Version != expectedVersion {
			return agentruntime.Job{}, agentruntime.ErrJobRepositoryVersionConflict
		}
		if err := validateAsyncJobImmutableFields(items[i], next); err != nil {
			return agentruntime.Job{}, err
		}
		next.Version = expectedVersion + 1
		next.IdempotentReplay = false
		if err := validateAsyncJobEnvelope(next); err != nil {
			return agentruntime.Job{}, err
		}
		items[i] = cloneAsyncJobEnvelope(next)
		break
	}
	if !found {
		return agentruntime.Job{}, agentruntime.ErrJobRepositoryNotFound
	}
	if err := s.store.Replace(ctx, items); err != nil {
		return agentruntime.Job{}, err
	}
	return cloneAsyncJobEnvelope(next), nil
}

func (s *jobStoreRepositoryAdapter) Delete(ctx context.Context, expected []agentruntime.JobVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked(ctx)
	if err != nil {
		return err
	}
	wanted := make(map[string]uint64, len(expected))
	for _, item := range expected {
		if _, duplicate := wanted[item.ID]; duplicate {
			return agentruntime.ErrJobRepositoryVersionConflict
		}
		wanted[item.ID] = item.Version
	}
	kept := make([]agentruntime.Job, 0, len(items))
	for _, item := range items {
		expectedVersion, remove := wanted[item.ID]
		if !remove {
			kept = append(kept, item)
			continue
		}
		if item.Version != expectedVersion {
			return agentruntime.ErrJobRepositoryVersionConflict
		}
		delete(wanted, item.ID)
	}
	if len(wanted) > 0 {
		return agentruntime.ErrJobRepositoryNotFound
	}
	return s.store.Replace(ctx, kept)
}

func (s *jobStoreRepositoryAdapter) Probe(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.loadLocked(ctx)
	return err
}

func (s *jobStoreRepositoryAdapter) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}
