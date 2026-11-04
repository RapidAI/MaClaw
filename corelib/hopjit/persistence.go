package hopjit

import (
	"context"
	"encoding/json"
	"sync"
)

type PersistenceProvider interface {
	SaveSnapshot(context.Context, EngineSnapshot) error
	LoadSnapshot(context.Context, string) (EngineSnapshot, error)
	Exists(context.Context, string) (bool, error)
}

func CloneSnapshot(snapshot EngineSnapshot) (EngineSnapshot, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return EngineSnapshot{}, err
	}
	var clone EngineSnapshot
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return EngineSnapshot{}, err
	}
	return clone, nil
}

type MemoryPersistence struct {
	mu        sync.Mutex
	snapshots map[string]EngineSnapshot
}

func NewMemoryPersistence() *MemoryPersistence {
	return &MemoryPersistence{snapshots: map[string]EngineSnapshot{}}
}

func (s *MemoryPersistence) SaveSnapshot(ctx context.Context, snapshot EngineSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clone, err := CloneSnapshot(snapshot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.snapshots[snapshot.RunID]; ok && snapshot.StateSeq <= current.StateSeq {
		return ErrStaleSnapshot
	}
	s.snapshots[snapshot.RunID] = clone
	return nil
}

func (s *MemoryPersistence) LoadSnapshot(ctx context.Context, runID string) (EngineSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return EngineSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[runID]
	if !ok {
		return EngineSnapshot{}, ErrSnapshotNotFound
	}
	return CloneSnapshot(snapshot)
}

func (s *MemoryPersistence) Exists(ctx context.Context, runID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.snapshots[runID]
	return ok, nil
}
