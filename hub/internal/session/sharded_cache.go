package session

import (
	"hash/fnv"
	"sync"
)

const numShards = 16

// ShardedCache splits session entries across multiple shards to reduce lock
// contention under high concurrency. Each shard has its own RWMutex.
type ShardedCache struct {
	shards [numShards]cacheShard
}

type cacheShard struct {
	mu       sync.RWMutex
	sessions map[string]*SessionCacheEntry
}

func NewShardedCache() *ShardedCache {
	sc := &ShardedCache{}
	for i := range sc.shards {
		sc.shards[i].sessions = make(map[string]*SessionCacheEntry)
	}
	return sc
}

func (sc *ShardedCache) shard(sessionID string) *cacheShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sessionID))
	return &sc.shards[h.Sum32()%numShards]
}

func (sc *ShardedCache) Get(sessionID string) (*SessionCacheEntry, bool) {
	s := sc.shard(sessionID)
	s.mu.RLock()
	entry, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	return entry, ok
}

func (sc *ShardedCache) Set(sessionID string, entry *SessionCacheEntry) {
	s := sc.shard(sessionID)
	s.mu.Lock()
	s.sessions[sessionID] = entry
	s.mu.Unlock()
}

func (sc *ShardedCache) Delete(sessionID string) {
	s := sc.shard(sessionID)
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

// Modify atomically retrieves (or creates) an entry and applies a mutation
// function while holding the shard write lock. This prevents concurrent
// modifications from losing updates (e.g. two preview deltas appending lines
// simultaneously). The init function is called only if the entry doesn't exist.
func (sc *ShardedCache) Modify(sessionID string, init func() *SessionCacheEntry, mutate func(entry *SessionCacheEntry)) {
	s := sc.shard(sessionID)
	s.mu.Lock()
	entry, ok := s.sessions[sessionID]
	if !ok {
		entry = init()
		s.sessions[sessionID] = entry
	}
	mutate(entry)
	s.mu.Unlock()
}

// Range iterates over all entries across all shards. The callback receives
// a snapshot; mutations inside the callback do not affect the cache.
// Each shard is locked independently for the duration of its iteration.
func (sc *ShardedCache) Range(fn func(sessionID string, entry *SessionCacheEntry) bool) {
	for i := range sc.shards {
		s := &sc.shards[i]
		s.mu.RLock()
		for id, entry := range s.sessions {
			if !fn(id, entry) {
				s.mu.RUnlock()
				return
			}
		}
		s.mu.RUnlock()
	}
}

// RangeWithDelete iterates all shards and deletes entries for which the
// callback returns true. Used by the reaper.
func (sc *ShardedCache) RangeWithDelete(shouldDelete func(sessionID string, entry *SessionCacheEntry) bool) {
	for i := range sc.shards {
		s := &sc.shards[i]
		s.mu.Lock()
		for id, entry := range s.sessions {
			if shouldDelete(id, entry) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

// Len returns the total number of entries across all shards.
func (sc *ShardedCache) Len() int {
	total := 0
	for i := range sc.shards {
		s := &sc.shards[i]
		s.mu.RLock()
		total += len(s.sessions)
		s.mu.RUnlock()
	}
	return total
}
