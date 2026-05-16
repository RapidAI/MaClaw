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
	if s.backend == nil {
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

	merged := 0
	added := 0
	deleted := 0

	// Process modified entries (new or updated).
	for _, remote := range modified {
		if remote.Version <= s.sync.lastVersion {
			continue // defensive: should not happen with correct Since() impl
		}

		// Update high-water mark.
		if remote.Version > s.sync.lastVersion {
			s.sync.lastVersion = remote.Version
		}

		// Check if this entry already exists locally.
		localIdx := s.findEntryIndexByIDLocked(remote.ID)
		if localIdx >= 0 {
			// Existing entry: update if remote is newer.
			local := &s.entries[localIdx]
			if remote.UpdatedAt.After(local.UpdatedAt) || remote.Version > local.Version {
				*local = remote
				s.updateIndicesForEntryLocked(remote)
				merged++
			}
		} else {
			// New entry from another instance: append.
			s.entries = append(s.entries, remote)
			s.addToIndicesLocked(remote)
			added++
		}
	}

	// Process deletions.
	for _, id := range deletedIDs {
		if s.removeFromEntriesAndIndicesLocked(id) {
			deleted++
		}
	}

	s.mu.Unlock()

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

// --- Index manipulation helpers (must be called under s.mu.Lock) ---

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

// addToIndicesLocked adds a single entry to all in-memory indices.
// Must be called under s.mu.Lock.
func (s *Store) addToIndicesLocked(e Entry) {
	s.bm25.addEntry(e)
	if len(e.Embedding) > 0 {
		s.vecIndex.add(e.ID, e.Embedding)
	}
	s.autoLink(e)
	if s.entityIndex != nil {
		s.entityIndex.IndexEntry(&e)
	}
	if s.projIndex != nil {
		s.projIndex.IndexEntry(&e)
	}
	if s.semanticGraph != nil {
		s.semanticGraph.IndexEntry(&e)
	}
}

// updateIndicesForEntryLocked updates indices for a modified entry.
// Must be called under s.mu.Lock.
func (s *Store) updateIndicesForEntryLocked(e Entry) {
	s.bm25.updateEntry(e)
	if len(e.Embedding) > 0 {
		s.vecIndex.add(e.ID, e.Embedding) // add is upsert
	}
	if s.entityIndex != nil {
		s.entityIndex.IndexEntry(&e)
	}
	if s.semanticGraph != nil {
		s.semanticGraph.IndexEntry(&e)
	}
}

// removeFromEntriesAndIndicesLocked removes an entry by ID from the entries
// slice and all indices. Returns true if the entry was found and removed.
// Must be called under s.mu.Lock.
func (s *Store) removeFromEntriesAndIndicesLocked(id string) bool {
	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			s.bm25.removeEntry(id)
			s.vecIndex.remove(id)
			s.graph.remove(id)
			if s.entityIndex != nil {
				s.entityIndex.RemoveEntry(id)
			}
			if s.semanticGraph != nil {
				s.semanticGraph.RemoveEntry(id)
			}
			return true
		}
	}
	return false
}
