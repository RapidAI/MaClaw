package agent

// Conversation memory: sharded in-memory session store with disk persistence
// and automatic TTL-based eviction.
//
// Migrated from gui/im_conversation_memory.go as part of the agent-unification
// plan (Phase 1, Step 2). This is the single source of truth — gui/ will
// import and alias these types.

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
	MaxConversationTurns      = 40
	MaxMemoryTokenEstimate    = 60_000
	MemoryTTL                 = 2 * time.Hour
	MemoryCleanupInterval     = 10 * time.Minute
	MemoryPersistDebounce     = 150 * time.Millisecond
	MemoryPersistSignalBuffer = 1
	MemoryShardCount          = 16
)

// ConversationEntry represents a single message in a conversation.
type ConversationEntry struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        interface{} `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
}

// ToMessage converts a ConversationEntry to a map suitable for the LLM API.
func (e ConversationEntry) ToMessage() interface{} {
	m := map[string]interface{}{"role": e.Role, "content": e.Content}
	if e.ReasoningContent != "" {
		m["reasoning_content"] = e.ReasoningContent
	} else if e.ToolCalls != nil {
		// DeepSeek thinking mode: the reasoning_content field MUST exist on
		// assistant messages that have tool_calls. A missing field causes
		// HTTP 400. An empty string is accepted.
		// See: https://api-docs.deepseek.com/guides/thinking_mode
		m["reasoning_content"] = ""
	}
	if e.ToolCalls != nil {
		m["tool_calls"] = e.ToolCalls
	}
	if e.ToolCallID != "" {
		m["tool_call_id"] = e.ToolCallID
	}
	return m
}

// UnfinishedTaskSlot tracks an incomplete task that can be resumed later.
type UnfinishedTaskSlot struct {
	SlotID           string    `json:"slot_id"`
	UserID           string    `json:"user_id"`
	ProjectPath      string    `json:"project_path,omitempty"`
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

// CloneUnfinishedTaskSlot returns a deep copy of the slot.
func CloneUnfinishedTaskSlot(slot *UnfinishedTaskSlot) *UnfinishedTaskSlot {
	if slot == nil {
		return nil
	}
	clone := *slot
	return &clone
}

// ConversationArchiver is an optional interface for archiving expired
// conversations to long-term memory. When nil, expired conversations
// are simply discarded.
type ConversationArchiver interface {
	Archive(userID string, entries []ConversationEntry) error
}

// --- Internal types ---

type conversationSession struct {
	entries        []ConversationEntry
	lastAccess     time.Time
	unfinishedSlot *UnfinishedTaskSlot
	activeSlotID   string
	inFlightTask   string // non-empty while an agent loop is executing; cleared on normal exit
}

type persistedSession struct {
	Entries        []ConversationEntry `json:"entries"`
	LastAccess     time.Time           `json:"last_access"`
	UnfinishedSlot *UnfinishedTaskSlot `json:"unfinished_slot,omitempty"`
	ActiveSlotID   string              `json:"active_slot_id,omitempty"`
	InFlightTask   string              `json:"in_flight_task,omitempty"`
}

type memorySnapshot struct {
	Sessions map[string]persistedSession `json:"sessions"`
}

type memoryShard struct {
	mu       sync.RWMutex
	sessions map[string]*conversationSession
}

// ConversationMemory is a sharded in-memory conversation store with
// optional disk persistence and TTL-based eviction.
type ConversationMemory struct {
	shards         [MemoryShardCount]*memoryShard
	Archiver       ConversationArchiver
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

// NewConversationMemory creates an in-memory conversation store.
func NewConversationMemory() *ConversationMemory {
	cm := &ConversationMemory{
		evictionStopCh: make(chan struct{}),
		persistStopCh:  make(chan struct{}),
		persistCh:      make(chan struct{}, MemoryPersistSignalBuffer),
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

// NewPersistentConversationMemory creates a conversation store that
// persists to disk at the given path.
func NewPersistentConversationMemory(storePath string) *ConversationMemory {
	cm := NewConversationMemory()
	cm.storePath = storePath
	_ = cm.loadFromDisk()
	return cm
}

func (cm *ConversationMemory) shard(userID string) *memoryShard {
	h := uint32(2166136261)
	for i := 0; i < len(userID); i++ {
		h ^= uint32(userID[i])
		h *= 16777619
	}
	return cm.shards[h&(MemoryShardCount-1)]
}

func (cm *ConversationMemory) evictionLoop() {
	defer cm.evictionWG.Done()
	ticker := time.NewTicker(MemoryCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cm.EvictExpired()
		case <-cm.evictionStopCh:
			return
		}
	}
}

// EvictExpired removes conversations that haven't been accessed within MemoryTTL.
func (cm *ConversationMemory) EvictExpired() {
	now := time.Now()
	type expiredEntry struct {
		userID  string
		entries []ConversationEntry
	}
	var toArchive []expiredEntry
	changed := false

	for _, sh := range cm.shards {
		sh.mu.Lock()
		for uid, s := range sh.sessions {
			if now.Sub(s.lastAccess) > MemoryTTL {
				if cm.Archiver != nil {
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

	for _, e := range toArchive {
		if err := cm.Archiver.Archive(e.userID, e.entries); err != nil {
			fmt.Fprintf(os.Stderr, "conversation_archiver: failed to archive user %s: %v\n", e.userID, err)
		}
	}
}

func (cm *ConversationMemory) persistLoop() {
	defer cm.persistWG.Done()
	var timer *time.Timer
	var timerCh <-chan time.Time
	for {
		select {
		case <-cm.persistCh:
			if timer == nil {
				timer = time.NewTimer(MemoryPersistDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(MemoryPersistDebounce)
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

func (cm *ConversationMemory) markDirtyAndScheduleFlush() {
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

func (cm *ConversationMemory) flushDirty() error {
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

// FlushNow synchronously persists any dirty state to disk, bypassing the
// debounce timer. Call this before process exit to avoid data loss when the
// process may be killed before the async persist loop runs.
func (cm *ConversationMemory) FlushNow() error {
	return cm.flushDirty()
}

// Stop gracefully shuts down eviction and persistence goroutines.
func (cm *ConversationMemory) Stop() {
	cm.stopOnce.Do(func() {
		close(cm.evictionStopCh)
		cm.evictionWG.Wait()
		close(cm.persistStopCh)
		cm.persistWG.Wait()
	})
}

// Load returns a copy of the conversation entries for a user.
func (cm *ConversationMemory) Load(userID string) []ConversationEntry {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	s := sh.sessions[userID]
	if s == nil {
		return nil
	}
	out := make([]ConversationEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Save stores conversation entries for a user.
func (cm *ConversationMemory) Save(userID string, entries []ConversationEntry) {
	copied := make([]ConversationEntry, len(entries))
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

// Append atomically appends entries to a user's conversation history.
// Unlike Load→append→Save, this is safe to call concurrently with Save
// because the read-modify-write happens under a single lock acquisition.
// Used by /btw side queries that run concurrently with the main agent loop.
func (cm *ConversationMemory) Append(userID string, entries ...ConversationEntry) {
	if len(entries) == 0 {
		return
	}
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil {
		s = &conversationSession{}
		sh.sessions[userID] = s
	}
	s.entries = append(s.entries, entries...)
	s.lastAccess = time.Now()
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// Clear removes all conversation data for a user.
func (cm *ConversationMemory) Clear(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	delete(sh.sessions, userID)
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// LastAccessTime returns the last access time for a user's session.
func (cm *ConversationMemory) LastAccessTime(userID string) time.Time {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	if s, ok := sh.sessions[userID]; ok {
		return s.lastAccess
	}
	return time.Time{}
}

// --- Unfinished task slot methods ---

func (cm *ConversationMemory) GetUnfinishedSlot(userID string) *UnfinishedTaskSlot {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	s := sh.sessions[userID]
	if s == nil {
		return nil
	}
	return CloneUnfinishedTaskSlot(s.unfinishedSlot)
}

func (cm *ConversationMemory) ActiveUnfinishedSlot(userID string) *UnfinishedTaskSlot {
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
	return CloneUnfinishedTaskSlot(s.unfinishedSlot)
}

func (cm *ConversationMemory) UpsertUnfinishedSlot(userID string, slot *UnfinishedTaskSlot) {
	if slot == nil {
		return
	}
	clone := CloneUnfinishedTaskSlot(slot)
	if strings.TrimSpace(clone.UserID) == "" {
		clone.UserID = userID
	}
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
	if s.lastAccess.IsZero() {
		s.lastAccess = time.Now()
	}
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

func (cm *ConversationMemory) BindUnfinishedSlot(userID, slotID string) bool {
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

func (cm *ConversationMemory) ClearActiveSlot(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if s := sh.sessions[userID]; s != nil {
		s.activeSlotID = ""
		s.lastAccess = time.Now()
	}
	cm.markDirtyAndScheduleFlush()
}

func (cm *ConversationMemory) DismissUnfinishedSlot(userID, slotID string) {
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

func (cm *ConversationMemory) CompleteUnfinishedSlot(userID, slotID string) {
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

func (cm *ConversationMemory) ClearConversationButKeepSlot(userID string) {
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

func (cm *ConversationMemory) ClearConversationAndDismissSlot(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s := sh.sessions[userID]
	if s == nil {
		return
	}
	s.entries = nil
	s.unfinishedSlot = nil
	s.activeSlotID = ""
	s.lastAccess = time.Now()
	cm.markDirtyAndScheduleFlush()
}

// --- In-flight task marker ---
//
// The in-flight task marker tracks whether an agent loop is currently
// executing. It is set at the START of the agent loop and cleared at the
// END (normal exit, cancel, max-rounds, etc.). If the process is killed
// while the agent loop is running (e.g., by an updater), the marker
// remains on disk. On the next app startup, the marker's presence tells
// the system that the previous task was interrupted abnormally, and the
// conversation history should be treated as an incomplete task — regardless
// of what the user's next message says.

// SetInFlightTask marks that an agent loop is currently executing for this
// user. The task string is a brief description (e.g., the user's original
// request, truncated). This must be followed by a synchronous FlushNow()
// to ensure the marker survives a process kill.
func (cm *ConversationMemory) SetInFlightTask(userID, task string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil {
		s = &conversationSession{}
		sh.sessions[userID] = s
	}
	s.inFlightTask = task
	s.lastAccess = time.Now()
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// ClearInFlightTask removes the in-flight marker, indicating the agent
// loop completed normally.
func (cm *ConversationMemory) ClearInFlightTask(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	if s := sh.sessions[userID]; s != nil {
		s.inFlightTask = ""
	}
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// ConsumeInFlightTask atomically reads and clears the in-flight marker.
// Returns the task description if the marker was set (meaning the previous
// agent loop was interrupted), or empty string if no interruption occurred.
// This is a one-shot operation — calling it twice returns empty on the
// second call.
func (cm *ConversationMemory) ConsumeInFlightTask(userID string) string {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s := sh.sessions[userID]
	if s == nil || s.inFlightTask == "" {
		return ""
	}
	task := s.inFlightTask
	s.inFlightTask = ""
	cm.markDirtyAndScheduleFlush()
	return task
}

// --- Disk persistence ---

func (cm *ConversationMemory) saveToDisk() error {
	if cm.storePath == "" {
		return nil
	}
	cm.persistMu.Lock()
	defer cm.persistMu.Unlock()

	snapshot := memorySnapshot{Sessions: make(map[string]persistedSession)}
	for _, sh := range cm.shards {
		sh.mu.RLock()
		for userID, session := range sh.sessions {
			if session == nil {
				continue
			}
			entries := make([]ConversationEntry, len(session.entries))
			copy(entries, session.entries)
			snapshot.Sessions[userID] = persistedSession{
				Entries:        entries,
				LastAccess:     session.lastAccess,
				UnfinishedSlot: CloneUnfinishedTaskSlot(session.unfinishedSlot),
				ActiveSlotID:   session.activeSlotID,
				InFlightTask:   session.inFlightTask,
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

func (cm *ConversationMemory) loadFromDisk() error {
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
	// Detect legacy fields (session_id, tool) that should be dropped on rewrite.
	legacyNeedsRewrite := strings.Contains(string(data), "\"session_id\"") || strings.Contains(string(data), "\"tool\"")
	var snapshot memorySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	for userID, session := range snapshot.Sessions {
		entries := make([]ConversationEntry, len(session.Entries))
		copy(entries, session.Entries)
		sh := cm.shard(userID)
		sh.mu.Lock()
		sh.sessions[userID] = &conversationSession{
			entries:        entries,
			lastAccess:     session.LastAccess,
			unfinishedSlot: CloneUnfinishedTaskSlot(session.UnfinishedSlot),
			activeSlotID:   session.ActiveSlotID,
			inFlightTask:   session.InFlightTask,
		}
		sh.mu.Unlock()
	}
	if legacyNeedsRewrite {
		cm.persistStateMu.Lock()
		cm.dirty = true
		cm.persistStateMu.Unlock()
	}
	return nil
}
