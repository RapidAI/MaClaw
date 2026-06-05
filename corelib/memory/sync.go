package memory

import (
	"log"
	"time"
)

const (
	// DefaultSyncInterval is the polling interval for cross-instance sync.
	// 3 seconds provides a good balance between latency and overhead.
	DefaultSyncInterval = 3 * time.Second
)

// SyncConfig controls the sync loop behavior.
type SyncConfig struct {
	// Enabled controls whether the sync loop runs. Set to true for maclawsrv
	// multi-instance deployments. False for GUI/TUI single-instance.
	Enabled bool

	// Interval is the polling interval. Defaults to DefaultSyncInterval (3s).
	Interval time.Duration

	// InstanceID uniquely identifies this process instance. Used to skip
	// entries written by this instance (already in memory). If empty, all
	// entries from Since() are processed (safe but slightly redundant).
	InstanceID string
}

// syncState holds the runtime state for the sync loop.
type syncState struct {
	lastVersion int64
	instanceID  string
	interval    time.Duration
	stopCh      chan struct{}
}

// startSyncLoop launches the background sync goroutine if the backend supports
// sync and sync is enabled. Called from NewStore after initial load.
func (s *Store) startSyncLoop(cfg SyncConfig) {
	if !cfg.Enabled {
		return
	}
	// Check backend supports sync.
	if s.backend == nil || !s.backend.SupportsSync() {
		return
	}

	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultSyncInterval
	}

	// Initialize lastVersion from the backend's current max.
	maxV, err := s.backend.MaxVersion()
	if err != nil {
		log.Printf("[memory_sync] WARNING: failed to get max version: %v", err)
		maxV = 0
	}

	s.sync = &syncState{
		lastVersion: maxV,
		instanceID:  cfg.InstanceID,
		interval:    interval,
		stopCh:      make(chan struct{}),
	}

	go s.syncLoop()
}

// syncLoop polls the backend for changes from other instances.
func (s *Store) syncLoop() {
	if s.sync == nil {
		return
	}
	ticker := time.NewTicker(s.sync.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-s.sync.stopCh:
			return
		case <-ticker.C:
			s.syncOnce()
		}
	}
}

// syncOnce performs a single sync cycle: poll Since(lastVersion), merge changes.
func (s *Store) syncOnce() {
	if s.backend == nil || s.sync == nil {
		return
	}

	// Check stop signal before DB access — backend may be closing.
	select {
	case <-s.stopCh:
		return
	default:
	}

	modified, deletedIDs, err := s.backend.Since(s.sync.lastVersion)
	if err != nil {
		log.Printf("[memory_sync] poll error: %v", err)
		return
	}

	if len(modified) == 0 && len(deletedIDs) == 0 {
		return // no changes
	}

	s.mu.Lock()
	merged, added, deleted := s.applyRemoteSyncBatchLocked(modified, deletedIDs)
	// Capture the rebuild-done channel before releasing the lock so we can
	// wait for the async index rebuild below (outside s.mu).
	rebuildDone := s.lastRebuildDone
	s.mu.Unlock()

	// Wait for the background index rebuild to finish before returning.
	// syncOnce runs in its own goroutine (syncLoop ticker); blocking here is
	// harmless and ensures that callers (e.g. tests, WaitRebuild) that check
	// indexes immediately after a sync see a consistent state.
	if rebuildDone != nil {
		select {
		case <-rebuildDone:
		case <-s.stopCh:
			return
		}
	}

	// Update watermark from the backend's max version (covers deletions).
	// Done outside the lock since MaxVersion() is a DB read that may block.
	// Check stop signal first — backend may have been closed during shutdown.
	select {
	case <-s.stopCh:
		return
	default:
	}
	if maxV, err := s.backend.MaxVersion(); err == nil && maxV > s.sync.lastVersion {
		s.sync.lastVersion = maxV
	}

	if merged > 0 || added > 0 || deleted > 0 {
		log.Printf("[memory_sync] synced: %d merged, %d added, %d deleted (version=%d)",
			merged, added, deleted, s.sync.lastVersion)
	}
}

// --- Remote sync reconciliation helpers (must be called under s.mu.Lock) ---

// applyRemoteSyncBatchLocked reconciles backend changes from another instance
// into this store's in-memory view. It must not write back to the backend:
// SQLite sync rows are already authoritative. Derived indexes are rebuilt once
// after the remote window is applied so graph/project/entity state changes as a
// single local snapshot.
func (s *Store) applyRemoteSyncBatchLocked(modified []Entry, deletedIDs []string) (merged, added, deleted int) {
	entries := append([]Entry(nil), s.entries...)
	indexByID := make(map[string]int, len(entries)+len(modified))
	for i := range entries {
		indexByID[entries[i].ID] = i
	}

	// Process modified entries (new or updated).
	for _, remote := range modified {
		if remote.Version <= s.sync.lastVersion {
			continue // defensive: should not happen with correct Since() impl
		}

		// Update high-water mark.
		if remote.Version > s.sync.lastVersion {
			s.sync.lastVersion = remote.Version
		}

		localIdx, exists := indexByID[remote.ID]
		if exists {
			local := &entries[localIdx]
			if remote.UpdatedAt.After(local.UpdatedAt) || remote.Version > local.Version {
				entries[localIdx] = remote
				merged++
			}
			continue
		}
		indexByID[remote.ID] = len(entries)
		entries = append(entries, remote)
		added++
	}

	if len(deletedIDs) > 0 {
		deleteSet := make(map[string]struct{}, len(deletedIDs))
		for _, id := range deletedIDs {
			deleteSet[id] = struct{}{}
		}
		kept := make([]Entry, 0, len(entries))
		for _, entry := range entries {
			if _, ok := deleteSet[entry.ID]; ok {
				deleted++
				continue
			}
			kept = append(kept, entry)
		}
		entries = kept
	}

	if merged > 0 || added > 0 || deleted > 0 {
		s.replaceEntriesAndRebuildAsync(entries, true)
	}
	return merged, added, deleted
}

// findEntryIndexByIDLocked returns the index of the entry with the given ID,
// or -1 if not found. Must be called under s.mu.Lock or s.mu.RLock.
func (s *Store) findEntryIndexByIDLocked(id string) int {
	for i := range s.entries {
		if s.entries[i].ID == id {
			return i
		}
	}
	return -1
}
