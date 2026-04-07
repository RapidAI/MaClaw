package main

// Conversation memory: sharded in-memory session store with disk persistence
// and automatic TTL-based eviction.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxConversationTurns      = 40
	maxMemoryTokenEstimate    = 60_000        // lowered: tools+system prompt consume ~15-20K
	memoryTTL                 = 2 * time.Hour // 对话记忆过期时间
	memoryCleanupInterval     = 10 * time.Minute
	memoryPersistDebounce     = 150 * time.Millisecond
	memoryPersistSignalBuffer = 1
)

type conversationEntry struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        interface{} `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
}

type unfinishedTaskSlot struct {
	SlotID           string    `json:"slot_id"`
	UserID           string    `json:"user_id"`
	ProjectPath      string    `json:"project_path,omitempty"`
	SessionID        string    `json:"session_id,omitempty"`
	Tool             string    `json:"tool,omitempty"`
	Status           string    `json:"status"`
	Summary          string    `json:"summary,omitempty"`
	LastTask         string    `json:"last_task,omitempty"`
	ResumePrompt     string    `json:"resume_prompt,omitempty"`
	Source           string    `json:"source,omitempty"`
	EvidenceScopeKey string    `json:"evidence_scope_key,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	BoundAt          time.Time `json:"bound_at,omitempty"`
}

func cloneUnfinishedTaskSlot(slot *unfinishedTaskSlot) *unfinishedTaskSlot {
	if slot == nil {
		return nil
	}
	clone := *slot
	return &clone
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
	entries        []conversationEntry
	lastAccess     time.Time
	unfinishedSlot *unfinishedTaskSlot
	activeSlotID   string
}

type persistedConversationSession struct {
	Entries        []conversationEntry `json:"entries"`
	LastAccess     time.Time           `json:"last_access"`
	UnfinishedSlot *unfinishedTaskSlot `json:"unfinished_slot,omitempty"`
	ActiveSlotID   string              `json:"active_slot_id,omitempty"`
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
	shards         [memoryShardCount]*memoryShard
	archiver       *ConversationArchiver
	persistMu      sync.Mutex
	storePath      string
	evictionStopCh chan struct{}
	persistStopCh  chan struct{}
	persistCh      chan struct{}
	persistStateMu sync.Mutex
	dirty          bool
	stopOnce       sync.Once
	evictionWG     sync.WaitGroup
	persistWG      sync.WaitGroup
}

func newConversationMemory() *conversationMemory {
	cm := &conversationMemory{
		evictionStopCh: make(chan struct{}),
		persistStopCh:  make(chan struct{}),
		persistCh:      make(chan struct{}, memoryPersistSignalBuffer),
	}
	for i := range cm.shards {
		cm.shards[i] = &memoryShard{
			sessions: make(map[string]*conversationSession),
		}
	}
	cm.evictionWG.Add(1)
	go cm.evictionLoop()
	cm.persistWG.Add(1)
	go cm.persistLoop()
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
	defer cm.evictionWG.Done()
	ticker := time.NewTicker(memoryCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cm.evictExpired()
		case <-cm.evictionStopCh:
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
		cm.markDirtyAndScheduleFlush()
	}

	// Archive outside any lock so slow I/O doesn't block other users.
	for _, e := range toArchive {
		if err := cm.archiver.Archive(e.userID, e.entries); err != nil {
			fmt.Fprintf(os.Stderr, "conversation_archiver: failed to archive user %s: %v\n", e.userID, err)
		}
	}
}

func (cm *conversationMemory) persistLoop() {
	defer cm.persistWG.Done()
	var timer *time.Timer
	var timerCh <-chan time.Time

	for {
		select {
		case <-cm.persistCh:
			if timer == nil {
				timer = time.NewTimer(memoryPersistDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(memoryPersistDebounce)
			}
			timerCh = timer.C
		case <-timerCh:
			timerCh = nil
			if err := cm.flushDirty(); err != nil {
				log.Printf("[conversation_memory] persist failed: %v", err)
			}
		case <-cm.persistStopCh:
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			if err := cm.flushDirty(); err != nil {
				log.Printf("[conversation_memory] final persist failed: %v", err)
			}
			return
		}
	}
}

func (cm *conversationMemory) markDirtyAndScheduleFlush() {
	if cm.storePath == "" {
		return
	}
	cm.persistStateMu.Lock()
	cm.dirty = true
	cm.persistStateMu.Unlock()
	select {
	case cm.persistCh <- struct{}{}:
	default:
	}
}

func (cm *conversationMemory) flushDirty() error {
	if cm.storePath == "" {
		return nil
	}
	cm.persistStateMu.Lock()
	if !cm.dirty {
		cm.persistStateMu.Unlock()
		return nil
	}
	cm.dirty = false
	cm.persistStateMu.Unlock()

	if err := cm.saveToDisk(); err != nil {
		cm.persistStateMu.Lock()
		cm.dirty = true
		cm.persistStateMu.Unlock()
		return err
	}
	return nil
}

func (cm *conversationMemory) stop() {
	cm.stopOnce.Do(func() {
		close(cm.evictionStopCh)
		cm.evictionWG.Wait()
		close(cm.persistStopCh)
		cm.persistWG.Wait()
	})
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

func (cm *conversationMemory) getUnfinishedSlot(userID string) *unfinishedTaskSlot {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	s := sh.sessions[userID]
	if s == nil {
		return nil
	}
	return cloneUnfinishedTaskSlot(s.unfinishedSlot)
}

func (cm *conversationMemory) activeUnfinishedSlot(userID string) *unfinishedTaskSlot {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	s := sh.sessions[userID]
	if s == nil || s.unfinishedSlot == nil || strings.TrimSpace(s.activeSlotID) == "" {
		return nil
	}
	if s.unfinishedSlot.SlotID != s.activeSlotID {
		return nil
	}
	return cloneUnfinishedTaskSlot(s.unfinishedSlot)
}

func (cm *conversationMemory) upsertUnfinishedSlot(userID string, slot *unfinishedTaskSlot) {
	if slot == nil {
		return
	}
	clone := cloneUnfinishedTaskSlot(slot)
	clone.UserID = firstNonEmptyTraceText(strings.TrimSpace(clone.UserID), userID)
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = time.Now()
	}
	clone.UpdatedAt = time.Now()
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil {
		s = &conversationSession{}
		sh.sessions[userID] = s
	}
	s.unfinishedSlot = clone
	if strings.TrimSpace(s.activeSlotID) == "" {
		s.activeSlotID = ""
	}
	if s.lastAccess.IsZero() {
		s.lastAccess = time.Now()
	}
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

func (cm *conversationMemory) bindUnfinishedSlot(userID, slotID string) bool {
	slotID = strings.TrimSpace(slotID)
	if slotID == "" {
		return false
	}
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s := sh.sessions[userID]
	if s == nil || s.unfinishedSlot == nil || s.unfinishedSlot.SlotID != slotID {
		return false
	}
	s.activeSlotID = slotID
	s.unfinishedSlot.Status = "resumed"
	s.unfinishedSlot.BoundAt = time.Now()
	s.unfinishedSlot.UpdatedAt = time.Now()
	s.lastAccess = time.Now()
	cm.markDirtyAndScheduleFlush()
	return true
}

func (cm *conversationMemory) clearActiveSlot(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if s := sh.sessions[userID]; s != nil {
		s.activeSlotID = ""
		s.lastAccess = time.Now()
	}
	cm.markDirtyAndScheduleFlush()
}

func (cm *conversationMemory) dismissUnfinishedSlot(userID, slotID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if s := sh.sessions[userID]; s != nil && s.unfinishedSlot != nil {
		if slotID == "" || s.unfinishedSlot.SlotID == strings.TrimSpace(slotID) {
			s.unfinishedSlot = nil
			s.activeSlotID = ""
			s.lastAccess = time.Now()
		}
	}
	cm.markDirtyAndScheduleFlush()
}

func (cm *conversationMemory) completeUnfinishedSlot(userID, slotID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if s := sh.sessions[userID]; s != nil && s.unfinishedSlot != nil {
		if slotID == "" || s.unfinishedSlot.SlotID == strings.TrimSpace(slotID) {
			s.unfinishedSlot.Status = "completed"
			s.unfinishedSlot.UpdatedAt = time.Now()
			s.activeSlotID = ""
			s.lastAccess = time.Now()
		}
	}
	cm.markDirtyAndScheduleFlush()
}

func (cm *conversationMemory) clearConversationButKeepSlot(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s := sh.sessions[userID]
	if s == nil {
		return
	}
	s.entries = nil
	s.activeSlotID = ""
	s.lastAccess = time.Now()
	cm.markDirtyAndScheduleFlush()
}

func (cm *conversationMemory) save(userID string, entries []conversationEntry) {
	copied := make([]conversationEntry, len(entries))
	copy(copied, entries)
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil {
		s = &conversationSession{}
		sh.sessions[userID] = s
	}
	s.entries = copied
	s.lastAccess = time.Now()
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

func (cm *conversationMemory) clear(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	delete(sh.sessions, userID)
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
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
				Entries:        entries,
				LastAccess:     session.lastAccess,
				UnfinishedSlot: cloneUnfinishedTaskSlot(session.unfinishedSlot),
				ActiveSlotID:   session.activeSlotID,
			}
		}
		sh.mu.RUnlock()
	}

	if err := os.MkdirAll(filepath.Dir(cm.storePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
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
			entries:        entries,
			lastAccess:     session.LastAccess,
			unfinishedSlot: cloneUnfinishedTaskSlot(session.UnfinishedSlot),
			activeSlotID:   session.ActiveSlotID,
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
