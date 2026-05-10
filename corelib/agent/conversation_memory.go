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
	InFlightTaskLease         = 30 * time.Minute
	InFlightTaskRenewInterval = 2 * time.Minute
)

type UnfinishedTaskSlotSource string

const (
	UnfinishedTaskSlotSourceSessionExit          UnfinishedTaskSlotSource = "session_exit"
	UnfinishedTaskSlotSourceInFlightRecovery     UnfinishedTaskSlotSource = "in_flight_recovery"
	UnfinishedTaskSlotSourceInFlightLeaseExpired UnfinishedTaskSlotSource = "in_flight_lease_expired"
	UnfinishedTaskSlotSourceMaxRounds            UnfinishedTaskSlotSource = "max_rounds"
)

// ConversationEntry represents a single message in a conversation.
type ConversationEntry struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        interface{} `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
	ToolName         string      `json:"tool_name,omitempty"`
	ToolOutcome      string      `json:"tool_outcome,omitempty"`
}

// ToMessage converts a ConversationEntry to a map suitable for the LLM API.
func (e ConversationEntry) ToMessage() interface{} {
	m := map[string]interface{}{"role": e.Role, "content": e.Content}
	if e.ReasoningContent != "" {
		m["reasoning_content"] = e.ReasoningContent
	} else if e.Role == "assistant" {
		// DeepSeek V4+ thinking mode: when tools are present in the request,
		// the API requires reasoning_content on ALL assistant messages — not
		// just those with tool_calls. Since MacLaw always sends tools in the
		// agent loop, we unconditionally include reasoning_content on every
		// assistant message. An empty string is accepted by the API.
		//
		// For non-DeepSeek providers, the field is simply ignored.
		//
		// See: https://api-docs.deepseek.com/guides/thinking_mode
		//   "With tools: drop_thinking is automatically disabled.
		//    All turns retain their reasoning."
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
	SlotID           string                   `json:"slot_id"`
	UserID           string                   `json:"user_id"`
	ProjectPath      string                   `json:"project_path,omitempty"`
	Tool             string                   `json:"tool,omitempty"`
	Status           string                   `json:"status"`
	Summary          string                   `json:"summary,omitempty"`
	LastTask         string                   `json:"last_task,omitempty"`
	ResumePrompt     string                   `json:"resume_prompt,omitempty"`
	Source           UnfinishedTaskSlotSource `json:"source,omitempty"`
	EvidenceScopeKey string                   `json:"evidence_scope_key,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
	BoundAt          time.Time                `json:"bound_at,omitempty"`
}

func (s UnfinishedTaskSlotSource) IsSessionExit() bool {
	return s == UnfinishedTaskSlotSourceSessionExit
}

func (s UnfinishedTaskSlotSource) IsInFlightRecovery() bool {
	return s == UnfinishedTaskSlotSourceInFlightRecovery ||
		s == UnfinishedTaskSlotSourceInFlightLeaseExpired
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
	entries             []ConversationEntry
	lastAccess          time.Time
	unfinishedSlot      *UnfinishedTaskSlot
	activeSlotID        string
	inFlightTask        string // non-empty while an agent loop is executing; cleared on normal exit
	inFlightProjectPath string // project path when the in-flight task was set
	inFlightSetAt       time.Time
	inFlightRunID       string
}

type persistedSession struct {
	Entries             []ConversationEntry `json:"entries"`
	LastAccess          time.Time           `json:"last_access"`
	UnfinishedSlot      *UnfinishedTaskSlot `json:"unfinished_slot,omitempty"`
	ActiveSlotID        string              `json:"active_slot_id,omitempty"`
	InFlightTask        string              `json:"in_flight_task,omitempty"`
	InFlightProjectPath string              `json:"in_flight_project_path,omitempty"`
	InFlightSetAt       time.Time           `json:"in_flight_set_at,omitempty"`
	InFlightRunID       string              `json:"in_flight_run_id,omitempty"`
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
			cm.ExpireStaleInFlightTasks(time.Now(), InFlightTaskLease)
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
	entries = DeduplicateAdjacentAssistantEntries(entries)
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
	entries = DeduplicateAdjacentAssistantEntries(entries)
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
	s.entries = DeduplicateAdjacentAssistantEntries(append(s.entries, entries...))
	s.lastAccess = time.Now()
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// DeduplicateAdjacentAssistantEntries removes exact adjacent assistant text
// duplicates. Streaming UI reconciliation and final response persistence can
// occasionally race and hand the same completed message back twice; keeping both
// pollutes future prompts and makes the assistant repeat stale task summaries.
func DeduplicateAdjacentAssistantEntries(entries []ConversationEntry) []ConversationEntry {
	if len(entries) < 2 {
		return entries
	}
	result := make([]ConversationEntry, 0, len(entries))
	for _, entry := range entries {
		if len(result) > 0 && isDuplicateAssistantEntry(result[len(result)-1], entry) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func isDuplicateAssistantEntry(left, right ConversationEntry) bool {
	if left.Role != "assistant" || right.Role != "assistant" {
		return false
	}
	if left.ToolCalls != nil || right.ToolCalls != nil || left.ToolCallID != "" || right.ToolCallID != "" {
		return false
	}
	leftText, leftOK := left.Content.(string)
	rightText, rightOK := right.Content.(string)
	if !leftOK || !rightOK {
		return false
	}
	return strings.TrimSpace(leftText) != "" && strings.TrimSpace(leftText) == strings.TrimSpace(rightText)
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
	s.unfinishedSlot.Status = UnfinishedTaskSlotStatusResumed.String()
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
			s.unfinishedSlot.Status = UnfinishedTaskSlotStatusCompleted.String()
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
// The in-flight task marker tracks an agent loop that has produced
// recoverable intermediate state. It is set lazily after useful tool work is
// committed to history, then cleared at the END (normal exit, cancel,
// max-rounds, etc.). If the process is killed after that point (e.g., by an
// updater), the marker remains on disk. On the next app startup, the marker's
// presence tells the system that the previous task was interrupted
// abnormally, and the conversation history should be treated as an incomplete
// task regardless of what the user's next message says.

// SetInFlightTask marks recoverable in-flight work for this user. The task
// string is a brief description (e.g., the user's original request,
// truncated). Callers that need crash recovery should follow this with a
// synchronous FlushNow().
//
// Optional args are kept for older call sites: projectPath[0] records the
// project path and projectPath[1], when supplied, records the loop run ID. New
// code should prefer SetInFlightTaskForRun for clarity.
func (cm *ConversationMemory) SetInFlightTask(userID, task string, projectPath ...string) {
	runID := ""
	if len(projectPath) > 1 {
		runID = projectPath[1]
	}
	path := ""
	if len(projectPath) > 0 {
		path = projectPath[0]
	}
	cm.SetInFlightTaskForRun(userID, task, path, runID)
}

func (cm *ConversationMemory) SetInFlightTaskForRun(userID, task, projectPath, runID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil {
		s = &conversationSession{}
		sh.sessions[userID] = s
	}
	now := time.Now()
	s.inFlightTask = task
	s.inFlightSetAt = now
	s.inFlightProjectPath = projectPath
	s.inFlightRunID = strings.TrimSpace(runID)
	s.lastAccess = now
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// RefreshInFlightTask extends the activity lease for an existing in-flight
// marker. It returns false when there is no active marker for userID.
func (cm *ConversationMemory) RefreshInFlightTask(userID string) bool {
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil || strings.TrimSpace(s.inFlightTask) == "" {
		sh.mu.Unlock()
		return false
	}
	now := time.Now()
	if !s.inFlightSetAt.IsZero() && now.Sub(s.inFlightSetAt) < InFlightTaskRenewInterval {
		sh.mu.Unlock()
		return true
	}
	s.inFlightSetAt = now
	s.lastAccess = now
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
	return true
}

// ClearInFlightTask removes the in-flight marker, indicating the agent
// loop completed normally.
func (cm *ConversationMemory) ClearInFlightTask(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	changed := false
	if s := sh.sessions[userID]; s != nil {
		if s.inFlightTask != "" || s.inFlightProjectPath != "" || !s.inFlightSetAt.IsZero() || s.inFlightRunID != "" {
			s.inFlightTask = ""
			s.inFlightProjectPath = ""
			s.inFlightSetAt = time.Time{}
			s.inFlightRunID = ""
			changed = true
		}
	}
	sh.mu.Unlock()
	if changed {
		cm.markDirtyAndScheduleFlush()
	}
}

func (cm *ConversationMemory) ClearInFlightTaskForRun(userID, runID string) {
	runID = strings.TrimSpace(runID)
	sh := cm.shard(userID)
	sh.mu.Lock()
	changed := false
	if s := sh.sessions[userID]; s != nil {
		if runID == "" || s.inFlightRunID == runID {
			if s.inFlightTask != "" || s.inFlightProjectPath != "" || !s.inFlightSetAt.IsZero() || s.inFlightRunID != "" {
				s.inFlightTask = ""
				s.inFlightProjectPath = ""
				s.inFlightSetAt = time.Time{}
				s.inFlightRunID = ""
				changed = true
			}
		}
		if s.unfinishedSlot != nil &&
			s.unfinishedSlot.Source == UnfinishedTaskSlotSourceInFlightLeaseExpired &&
			s.unfinishedSlot.EvidenceScopeKey == inFlightRunScopeKey(runID) {
			s.unfinishedSlot = nil
			s.activeSlotID = ""
			changed = true
		}
	}
	sh.mu.Unlock()
	if changed {
		cm.markDirtyAndScheduleFlush()
	}
}

// ExpireStaleInFlightTasks converts stale in-flight markers into unfinished
// slots. This keeps the recoverability invariant while preventing a durable
// busy flag from making the UI spin forever after a stuck or interrupted loop.
func (cm *ConversationMemory) ExpireStaleInFlightTasks(now time.Time, lease time.Duration) int {
	if now.IsZero() {
		now = time.Now()
	}
	if lease <= 0 {
		lease = InFlightTaskLease
	}
	expired := 0
	for _, sh := range cm.shards {
		sh.mu.Lock()
		for userID, s := range sh.sessions {
			if s == nil || strings.TrimSpace(s.inFlightTask) == "" {
				continue
			}
			setAt := s.inFlightSetAt
			if setAt.IsZero() {
				setAt = s.lastAccess
			}
			if setAt.IsZero() || now.Sub(setAt) <= lease {
				continue
			}
			cm.convertExpiredInFlightLocked(userID, s, now)
			expired++
		}
		sh.mu.Unlock()
	}
	if expired > 0 {
		cm.markDirtyAndScheduleFlush()
	}
	return expired
}

func (cm *ConversationMemory) convertExpiredInFlightLocked(userID string, s *conversationSession, now time.Time) {
	task := s.inFlightTask
	projectPath := s.inFlightProjectPath
	runID := s.inFlightRunID
	if s.unfinishedSlot == nil {
		slotID := fmt.Sprintf("inflight-expired-%d", now.UnixMilli())
		s.unfinishedSlot = &UnfinishedTaskSlot{
			SlotID:           slotID,
			UserID:           userID,
			ProjectPath:      projectPath,
			Tool:             "agent",
			Status:           "interrupted",
			Summary:          "Previous task stopped making progress and was moved to recovery.",
			LastTask:         task,
			ResumePrompt:     "The previous task stopped making progress. Continue from the saved conversation history and avoid repeating completed work.\n",
			Source:           UnfinishedTaskSlotSourceInFlightLeaseExpired,
			EvidenceScopeKey: inFlightRunScopeKey(runID),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
	}
	s.inFlightTask = ""
	s.inFlightProjectPath = ""
	s.inFlightSetAt = time.Time{}
	s.inFlightRunID = ""
	s.lastAccess = now
}

func inFlightRunScopeKey(runID string) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ""
	}
	return "in_flight_run:" + runID
}

// ConsumeInFlightTask atomically reads and clears the in-flight marker.
// Returns the task description and project path if the marker was set
// (meaning the previous agent loop was interrupted), or empty strings
// if no interruption occurred.
// This is a one-shot operation — calling it twice returns empty on the
// second call.
func (cm *ConversationMemory) ConsumeInFlightTask(userID string) (string, string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s := sh.sessions[userID]
	if s == nil || s.inFlightTask == "" {
		return "", ""
	}
	task := s.inFlightTask
	projectPath := s.inFlightProjectPath
	s.inFlightTask = ""
	s.inFlightProjectPath = ""
	s.inFlightSetAt = time.Time{}
	s.inFlightRunID = ""
	cm.markDirtyAndScheduleFlush()
	return task, projectPath
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
				Entries:             entries,
				LastAccess:          session.lastAccess,
				UnfinishedSlot:      CloneUnfinishedTaskSlot(session.unfinishedSlot),
				ActiveSlotID:        session.activeSlotID,
				InFlightTask:        session.inFlightTask,
				InFlightProjectPath: session.inFlightProjectPath,
				InFlightSetAt:       session.inFlightSetAt,
				InFlightRunID:       session.inFlightRunID,
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
	needsRewrite := legacyNeedsRewrite
	for userID, session := range snapshot.Sessions {
		entries := make([]ConversationEntry, len(session.Entries))
		copy(entries, session.Entries)

		// Enforce structural invariant: no orphaned tool messages.
		// A tool entry is orphaned if no preceding assistant entry declares
		// its tool_call_id. This can happen if a previous version of
		// applyHistoryCompression or trimHistoryWithSummary split a tool-call group.
		// Repair on load so no downstream code ever sees corrupted data.
		repaired := repairOrphanedToolEntries(entries)
		if len(repaired) != len(entries) {
			log.Printf("[ConversationMemory] repaired %d orphaned tool entries for user=%s on load",
				len(entries)-len(repaired), userID)
			entries = repaired
			needsRewrite = true
		}
		deduped := DeduplicateAdjacentAssistantEntries(entries)
		if len(deduped) != len(entries) {
			log.Printf("[ConversationMemory] removed %d duplicate assistant entries for user=%s on load",
				len(entries)-len(deduped), userID)
			entries = deduped
			needsRewrite = true
		}
		inFlightSetAt := session.InFlightSetAt
		if strings.TrimSpace(session.InFlightTask) != "" && inFlightSetAt.IsZero() {
			inFlightSetAt = session.LastAccess
			needsRewrite = true
		}

		sh := cm.shard(userID)
		sh.mu.Lock()
		sh.sessions[userID] = &conversationSession{
			entries:             entries,
			lastAccess:          session.LastAccess,
			unfinishedSlot:      CloneUnfinishedTaskSlot(session.UnfinishedSlot),
			activeSlotID:        session.ActiveSlotID,
			inFlightTask:        session.InFlightTask,
			inFlightProjectPath: session.InFlightProjectPath,
			inFlightSetAt:       inFlightSetAt,
			inFlightRunID:       session.InFlightRunID,
		}
		sh.mu.Unlock()
	}
	if cm.ExpireStaleInFlightTasks(time.Now(), InFlightTaskLease) > 0 {
		needsRewrite = true
	}
	if needsRewrite {
		cm.persistStateMu.Lock()
		cm.dirty = true
		cm.persistStateMu.Unlock()
	}
	return nil
}

// repairOrphanedToolEntries removes tool entries whose tool_call_id has no
// matching assistant(tool_calls) declaration. Returns the input slice
// unchanged if no orphans are found (zero allocation fast path).
func repairOrphanedToolEntries(entries []ConversationEntry) []ConversationEntry {
	// Fast path: check if any tool entries exist at all.
	hasToolEntries := false
	for _, e := range entries {
		if e.Role == "tool" {
			hasToolEntries = true
			break
		}
	}
	if !hasToolEntries {
		return entries
	}

	// Build set of declared tool_call IDs from assistant entries.
	declaredIDs := make(map[string]bool)
	for _, e := range entries {
		if e.Role != "assistant" || e.ToolCalls == nil {
			continue
		}
		for _, id := range extractToolCallIDs(e.ToolCalls) {
			declaredIDs[id] = true
		}
	}

	// Check if all tool entries are valid.
	allValid := true
	for _, e := range entries {
		if e.Role == "tool" && (e.ToolCallID == "" || !declaredIDs[e.ToolCallID]) {
			allValid = false
			break
		}
	}
	if allValid {
		return entries // zero allocation — no orphans found
	}

	// Remove orphaned tool entries.
	result := make([]ConversationEntry, 0, len(entries))
	for _, e := range entries {
		if e.Role == "tool" && (e.ToolCallID == "" || !declaredIDs[e.ToolCallID]) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// extractToolCallIDs extracts tool_call IDs from a ToolCalls field.
// Handles both []interface{} (from JSON round-trip) and typed slices
// (from in-process construction) via json.Marshal fallback.
func extractToolCallIDs(toolCalls interface{}) []string {
	// Fast path: after JSON round-trip, ToolCalls is []interface{}.
	if arr, ok := toolCalls.([]interface{}); ok {
		ids := make([]string, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				if id, ok := m["id"].(string); ok && id != "" {
					ids = append(ids, id)
				}
			}
		}
		return ids
	}
	// Fallback: typed slice — marshal/unmarshal to extract IDs.
	data, err := json.Marshal(toolCalls)
	if err != nil {
		return nil
	}
	var calls []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &calls) != nil {
		return nil
	}
	ids := make([]string, 0, len(calls))
	for _, c := range calls {
		if c.ID != "" {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
