package main

// Conversation memory: sharded in-memory session store with disk persistence
// and automatic TTL-based eviction.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxConversationTurns   = 40
	maxMemoryTokenEstimate = 60_000        // lowered: tools+system prompt consume ~15-20K
	memoryTTL              = 2 * time.Hour // 对话记忆过期时间
	memoryCleanupInterval  = 10 * time.Minute
)

type conversationEntry struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        interface{} `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
}

// toMessage converts a conversationEntry to a map suitable for the LLM API.
func (e conversationEntry) toMessage() interface{} {
	m := map[string]interface{}{"role": e.Role, "content": e.Content}
	if e.ReasoningContent != "" {
		m["reasoning_content"] = e.ReasoningContent
	}
	if e.ToolCalls != nil {
		m["tool_calls"] = e.ToolCalls
	}
	if e.ToolCallID != "" {
		m["tool_call_id"] = e.ToolCallID
	}
	return m
}

type conversationSession struct {
	entries    []conversationEntry
	lastAccess time.Time
}

type persistedConversationSession struct {
	Entries    []conversationEntry `json:"entries"`
	LastAccess time.Time           `json:"last_access"`
}

type conversationMemorySnapshot struct {
	Sessions map[string]persistedConversationSession `json:"sessions"`
}

// memoryShardCount is the number of shards for conversation memory.
// Must be a power of 2 for fast modulo via bitwise AND.
const memoryShardCount = 16

// memoryShard holds a subset of conversation sessions, protected by its
// own lock to reduce contention when multiple users chat concurrently.
type memoryShard struct {
	mu       sync.RWMutex
	sessions map[string]*conversationSession
}

type conversationMemory struct {
	shards    [memoryShardCount]*memoryShard
	stopCh    chan struct{}
	archiver  *ConversationArchiver
	persistMu sync.Mutex
	storePath string
}

func newConversationMemory() *conversationMemory {
	cm := &conversationMemory{
		stopCh: make(chan struct{}),
	}
	for i := range cm.shards {
		cm.shards[i] = &memoryShard{
			sessions: make(map[string]*conversationSession),
		}
	}
	go cm.evictionLoop()
	return cm
}

func newPersistentConversationMemory(storePath string) *conversationMemory {
	cm := newConversationMemory()
	cm.storePath = storePath
	_ = cm.loadFromDisk()
	return cm
}

// shard returns the shard for a given userID using FNV-1a hash.
func (cm *conversationMemory) shard(userID string) *memoryShard {
	h := uint32(2166136261) // FNV offset basis
	for i := 0; i < len(userID); i++ {
		h ^= uint32(userID[i])
		h *= 16777619 // FNV prime
	}
	return cm.shards[h&(memoryShardCount-1)]
}

// evictionLoop 定期清理过期的对话记忆，防止内存无限增长
func (cm *conversationMemory) evictionLoop() {
	ticker := time.NewTicker(memoryCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cm.evictExpired()
		case <-cm.stopCh:
			return
		}
	}
}

func (cm *conversationMemory) evictExpired() {
	now := time.Now()
	// Collect expired sessions outside the lock to avoid holding it during
	// archival (which may perform network I/O).
	type expiredEntry struct {
		userID  string
		entries []conversationEntry
	}
	var toArchive []expiredEntry
	changed := false

	for _, sh := range cm.shards {
		sh.mu.Lock()
		for uid, s := range sh.sessions {
			if now.Sub(s.lastAccess) > memoryTTL {
				if cm.archiver != nil {
					toArchive = append(toArchive, expiredEntry{uid, s.entries})
				}
				delete(sh.sessions, uid)
				changed = true
			}
		}
		sh.mu.Unlock()
	}

	if changed {
		_ = cm.saveToDisk()
	}

	// Archive outside any lock so slow I/O doesn't block other users.
	for _, e := range toArchive {
		if err := cm.archiver.Archive(e.userID, e.entries); err != nil {
			fmt.Fprintf(os.Stderr, "conversation_archiver: failed to archive user %s: %v\n", e.userID, err)
		}
	}
}

func (cm *conversationMemory) stop() {
	select {
	case cm.stopCh <- struct{}{}:
	default:
	}
	_ = cm.saveToDisk()
}

func (cm *conversationMemory) load(userID string) []conversationEntry {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	s := sh.sessions[userID]
	if s == nil {
		return nil
	}
	out := make([]conversationEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

func (cm *conversationMemory) save(userID string, entries []conversationEntry) {
	copied := make([]conversationEntry, len(entries))
	copy(copied, entries)
	sh := cm.shard(userID)
	sh.mu.Lock()
	sh.sessions[userID] = &conversationSession{
		entries:    copied,
		lastAccess: time.Now(),
	}
	sh.mu.Unlock()
	_ = cm.saveToDisk()
}

func (cm *conversationMemory) clear(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	delete(sh.sessions, userID)
	sh.mu.Unlock()
	_ = cm.saveToDisk()
}

func (cm *conversationMemory) saveToDisk() error {
	if cm.storePath == "" {
		return nil
	}
	cm.persistMu.Lock()
	defer cm.persistMu.Unlock()

	snapshot := conversationMemorySnapshot{Sessions: make(map[string]persistedConversationSession)}
	for _, sh := range cm.shards {
		sh.mu.RLock()
		for userID, session := range sh.sessions {
			if session == nil {
				continue
			}
			entries := make([]conversationEntry, len(session.entries))
			copy(entries, session.entries)
			snapshot.Sessions[userID] = persistedConversationSession{
				Entries:    entries,
				LastAccess: session.lastAccess,
			}
		}
		sh.mu.RUnlock()
	}

	if err := os.MkdirAll(filepath.Dir(cm.storePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := cm.storePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, cm.storePath)
}

func (cm *conversationMemory) loadFromDisk() error {
	if cm.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(cm.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snapshot conversationMemorySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	for userID, session := range snapshot.Sessions {
		entries := make([]conversationEntry, len(session.Entries))
		copy(entries, session.Entries)
		sh := cm.shard(userID)
		sh.mu.Lock()
		sh.sessions[userID] = &conversationSession{
			entries:    entries,
			lastAccess: session.LastAccess,
		}
		sh.mu.Unlock()
	}
	return nil
}

// lastAccessTime returns the last access time for a user's conversation session.
// Returns zero time if no session exists.
func (cm *conversationMemory) lastAccessTime(userID string) time.Time {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	if s, ok := sh.sessions[userID]; ok {
		return s.lastAccess
	}
	return time.Time{}
}
