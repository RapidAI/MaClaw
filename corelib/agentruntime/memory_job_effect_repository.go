package agentruntime

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryJobEffectRepository is the in-process JobEffectRepository for hosts
// that already publish Jobs through MemoryJobRepository. It preserves the
// same prepare-once and CAS update rules as the SQLite adapter.
type MemoryJobEffectRepository struct {
	mu      sync.RWMutex
	effects map[string]JobEffect // jobID\x00kind
	closed  bool
}

var _ JobEffectRepository = (*MemoryJobEffectRepository)(nil)

func NewMemoryJobEffectRepository() *MemoryJobEffectRepository {
	return &MemoryJobEffectRepository{effects: make(map[string]JobEffect)}
}

func memoryJobEffectKey(jobID, kind string) string {
	return strings.TrimSpace(jobID) + "\x00" + strings.TrimSpace(kind)
}

func (r *MemoryJobEffectRepository) ListByJob(ctx context.Context, jobID string) ([]JobEffect, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, ErrJobEffectUnavailable
	}
	jobID = strings.TrimSpace(jobID)
	out := make([]JobEffect, 0)
	for _, effect := range r.effects {
		if effect.JobID == jobID {
			out = append(out, cloneMemoryJobEffect(effect))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

func (r *MemoryJobEffectRepository) Get(ctx context.Context, jobID, kind string) (JobEffect, error) {
	if err := contextErr(ctx); err != nil {
		return JobEffect{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return JobEffect{}, ErrJobEffectUnavailable
	}
	effect, ok := r.effects[memoryJobEffectKey(jobID, kind)]
	if !ok {
		return JobEffect{}, ErrJobEffectNotFound
	}
	return cloneMemoryJobEffect(effect), nil
}

func (r *MemoryJobEffectRepository) Prepare(ctx context.Context, candidate JobEffect) (JobEffect, bool, error) {
	if err := contextErr(ctx); err != nil {
		return JobEffect{}, false, err
	}
	now := time.Now().UTC()
	candidate.Version = 1
	candidate.State = JobEffectPrepared
	candidate.ResourceID = strings.TrimSpace(candidate.ResourceID)
	candidate.ReceiptDigest = ""
	candidate.ReasonCode = ""
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = now
	}
	candidate.UpdatedAt = candidate.CreatedAt
	if err := ValidateJobEffect(candidate); err != nil {
		return JobEffect{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return JobEffect{}, false, ErrJobEffectUnavailable
	}
	key := memoryJobEffectKey(candidate.JobID, candidate.Kind)
	if existing, ok := r.effects[key]; ok {
		return cloneMemoryJobEffect(existing), false, nil
	}
	r.effects[key] = cloneMemoryJobEffect(candidate)
	return cloneMemoryJobEffect(candidate), true, nil
}

func (r *MemoryJobEffectRepository) Update(ctx context.Context, version uint64, candidate JobEffect) (JobEffect, error) {
	if err := contextErr(ctx); err != nil {
		return JobEffect{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return JobEffect{}, ErrJobEffectUnavailable
	}
	key := memoryJobEffectKey(candidate.JobID, candidate.Kind)
	existing, ok := r.effects[key]
	if !ok {
		return JobEffect{}, ErrJobEffectNotFound
	}
	if existing.Version != version {
		return JobEffect{}, ErrJobEffectConflict
	}
	candidate.JobID = existing.JobID
	candidate.Kind = existing.Kind
	candidate.TenantID = existing.TenantID
	candidate.UserID = existing.UserID
	candidate.JobKind = existing.JobKind
	candidate.CreatedAt = existing.CreatedAt
	if candidate.ResourceID == "" {
		candidate.ResourceID = existing.ResourceID
	}
	candidate.UpdatedAt = time.Now().UTC()
	candidate.Version = version + 1
	if err := ValidateJobEffect(candidate); err != nil {
		return JobEffect{}, err
	}
	r.effects[key] = cloneMemoryJobEffect(candidate)
	return cloneMemoryJobEffect(candidate), nil
}

func (r *MemoryJobEffectRepository) DeleteByJobs(ctx context.Context, jobIDs []string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrJobEffectUnavailable
	}
	wanted := make(map[string]bool, len(jobIDs))
	for _, id := range jobIDs {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	for key, effect := range r.effects {
		if wanted[effect.JobID] {
			delete(r.effects, key)
		}
	}
	return nil
}

func (r *MemoryJobEffectRepository) Probe(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return ErrJobEffectUnavailable
	}
	return nil
}

func (r *MemoryJobEffectRepository) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

func cloneMemoryJobEffect(effect JobEffect) JobEffect {
	copy := effect
	if effect.Payload != nil {
		copy.Payload = append(json.RawMessage(nil), effect.Payload...)
	}
	return copy
}
