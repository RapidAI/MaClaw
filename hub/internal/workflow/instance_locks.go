package workflow

import "sync"

// instanceLocks provides per-instance serialization for the approval
// read-modify-write-persist cycle in ResumeInstance.
//
// Concurrent approver decisions on the SAME instance (countersign / any-N-of-M)
// otherwise interleave a read of InstanceData, an in-place mutation of the
// per-node approval state, and a full UpdateInstanceData overwrite — so one
// decision can clobber another's persisted vote (Finding 1.6 / Requirement 2.6).
//
// Serializing the cycle per instance makes each decision apply atomically: the
// second decision observes the first's persisted state before it reads, so no
// vote is lost. This is a wiring-level mechanism — it changes only HOW the
// approval state is persisted (by serializing concurrent writers on the same
// instance); the per-mode decision logic, the InstanceStore interface, and the
// conditional UpdateStatus contract are all unchanged.
//
// Locks are reference-counted so the map does not grow unbounded: an entry is
// removed once no goroutine holds or waits on it. Decisions on DIFFERENT
// instances never contend (each instance has its own mutex).
type instanceLocks struct {
	mu      sync.Mutex
	entries map[string]*instanceLockEntry
}

// instanceLockEntry is the per-instance mutex plus a reference count of the
// goroutines currently holding or waiting on it.
type instanceLockEntry struct {
	mu      sync.Mutex
	waiters int
}

// newInstanceLocks creates an empty per-instance lock registry.
func newInstanceLocks() *instanceLocks {
	return &instanceLocks{entries: make(map[string]*instanceLockEntry)}
}

// acquire blocks until the calling goroutine holds the per-instance lock for id
// and returns a release function. The release function must be called exactly
// once (typically via defer) to unlock and drop the reference.
func (l *instanceLocks) acquire(id string) func() {
	l.mu.Lock()
	entry, ok := l.entries[id]
	if !ok {
		entry = &instanceLockEntry{}
		l.entries[id] = entry
	}
	entry.waiters++
	l.mu.Unlock()

	entry.mu.Lock()

	return func() {
		entry.mu.Unlock()

		l.mu.Lock()
		entry.waiters--
		if entry.waiters == 0 {
			delete(l.entries, id)
		}
		l.mu.Unlock()
	}
}
