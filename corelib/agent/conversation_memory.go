package agent

// Conversation memory: sharded in-memory session store with disk persistence
// and automatic TTL-based eviction.
//
// Migrated from gui/im_conversation_memory.go as part of the agent-unification
// plan (Phase 1, Step 2). This is the single source of truth — gui/ will
// import and alias these types.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
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
	// UnfinishedTaskSlotSourceAppExit marks a slot written during graceful app
	// shutdown for a session whose agent loop was still running. Unlike crash
	// recovery, the restored tab IS the user's resume intent, so the pipeline
	// binds these slots automatically without showing a resume banner.
	UnfinishedTaskSlotSourceAppExit UnfinishedTaskSlotSource = "app_exit"
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
	// FinishReason is host/trajectory metadata only (not sent to the LLM API).
	// Typical values: stop, tool_calls, length.
	FinishReason string `json:"finish_reason,omitempty"`
	ID           string `json:"_id,omitempty"`
	ParentID     string `json:"_parent_id,omitempty"`
	Timestamp    int64  `json:"_ts,omitempty"`
}

// ResolveAssistantFinishReason returns the LLM finish reason for trajectory/history
// metadata. When the provider omits it, defaults to tool_calls if the assistant
// issued tools, otherwise stop.
func ResolveAssistantFinishReason(finishReason string, hasToolCalls bool) string {
	fr := strings.TrimSpace(finishReason)
	if fr != "" {
		return fr
	}
	if hasToolCalls {
		return "tool_calls"
	}
	return "stop"
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
	Status           UnfinishedTaskSlotStatus `json:"status"`
	Summary          string                   `json:"summary,omitempty"`
	LastTask         string                   `json:"last_task,omitempty"`
	ResumePrompt     string                   `json:"resume_prompt,omitempty"`
	Source           UnfinishedTaskSlotSource `json:"source,omitempty"`
	EvidenceScopeKey string                   `json:"evidence_scope_key,omitempty"`
	// Recovery metadata is diagnostic evidence only. It must never be used to
	// replay an interrupted tool call automatically.
	LastCheckpointAt time.Time `json:"last_checkpoint_at,omitempty"`
	LastToolName     string    `json:"last_tool_name,omitempty"`
	SideEffectState  string    `json:"side_effect_state,omitempty"`
	RecoveryMode     string    `json:"recovery_mode,omitempty"`
	// RuntimeTaskID links UI-facing recovery state to the durable coding
	// execution ledger. It is an opaque identifier only—never a command,
	// tool payload, prompt, credential, or replay plan.
	RuntimeTaskID string    `json:"runtime_task_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	BoundAt       time.Time `json:"bound_at,omitempty"`
}

func (s UnfinishedTaskSlotSource) IsSessionExit() bool {
	return s == UnfinishedTaskSlotSourceSessionExit
}

func (s UnfinishedTaskSlotSource) IsInFlightRecovery() bool {
	return s == UnfinishedTaskSlotSourceInFlightRecovery ||
		s == UnfinishedTaskSlotSourceInFlightLeaseExpired
}

func (s UnfinishedTaskSlotSource) IsAppExit() bool {
	return s == UnfinishedTaskSlotSourceAppExit
}

// CloneUnfinishedTaskSlot returns a deep copy of the slot.
func CloneUnfinishedTaskSlot(slot *UnfinishedTaskSlot) *UnfinishedTaskSlot {
	if slot == nil {
		return nil
	}
	clone := *slot
	clone.Status = NormalizeUnfinishedTaskSlotStatus(clone.Status.String())
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
	activeBranchTipID   string
	lastAccess          time.Time
	unfinishedSlot      *UnfinishedTaskSlot
	activeSlotID        string
	inFlightTask        string // non-empty while an agent loop is executing; cleared on normal exit
	inFlightProjectPath string // project path when the in-flight task was set
	inFlightSetAt       time.Time
	inFlightRunID       string
	inFlightSequence    uint64
	inFlightLastTool    string
	inFlightSideEffect  string
}

type persistedSession struct {
	Entries             []ConversationEntry `json:"entries"`
	ActiveBranchTipID   string              `json:"active_branch_tip_id,omitempty"`
	LastAccess          time.Time           `json:"last_access"`
	UnfinishedSlot      *UnfinishedTaskSlot `json:"unfinished_slot,omitempty"`
	ActiveSlotID        string              `json:"active_slot_id,omitempty"`
	InFlightTask        string              `json:"in_flight_task,omitempty"`
	InFlightProjectPath string              `json:"in_flight_project_path,omitempty"`
	InFlightSetAt       time.Time           `json:"in_flight_set_at,omitempty"`
	InFlightRunID       string              `json:"in_flight_run_id,omitempty"`
	InFlightSequence    uint64              `json:"in_flight_sequence,omitempty"`
	InFlightLastTool    string              `json:"in_flight_last_tool,omitempty"`
	InFlightSideEffect  string              `json:"in_flight_side_effect,omitempty"`
}

// InFlightCheckpoint is evidence for a durable conversation checkpoint. It
// intentionally contains no tool arguments or result payloads.
type InFlightCheckpoint struct {
	Sequence        uint64
	LastToolName    string
	SideEffectState string
}

// ErrInFlightCheckpointRunConflict means an older loop attempted to overwrite
// recovery evidence that is now owned by a different run. Callers must stop
// automatic execution rather than guessing which task owns the session.
var ErrInFlightCheckpointRunConflict = errors.New("in-flight checkpoint run conflict")

type memorySnapshot struct {
	Sessions map[string]persistedSession `json:"sessions"`
}

type memoryShard struct {
	mu       sync.RWMutex
	sessions map[string]*conversationSession
}

// ConversationMemory is a sharded active conversation/session-state store with
// optional disk persistence and TTL-based eviction. Despite the historical
// name, it is not Maclaw long-term memory; durable user/agent memories,
// recall, audit, and surgery are owned by corelib/memory.Store.
type ConversationMemory struct {
	shards    [MemoryShardCount]*memoryShard
	Archiver  ConversationArchiver
	persistMu sync.Mutex
	// flushMu serializes the dirty-state check with the complete disk write.
	// Without it, a second FlushNow could observe dirty=false after another
	// goroutine claimed the pending write, then return before that write has
	// reached disk. Recovery checkpoints rely on FlushNow being a completion
	// barrier, not merely a request to start a flush.
	flushMu sync.Mutex
	// checkpointMu keeps a synchronous checkpoint snapshot and the following
	// same-run clear serialised. Without it a newer mutation can otherwise be
	// captured by saveToDisk while FlushNow is still reporting an older
	// checkpoint as durable.
	checkpointMu   sync.Mutex
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
	if err := cm.loadFromDisk(); err != nil {
		// Do not let the startup rewrite below replace an unreadable store with an
		// empty snapshot. The caller can still use an empty in-memory instance,
		// but the on-disk evidence remains available for diagnosis/recovery.
		log.Printf("[ConversationMemory] load persistent store failed path=%q err=%v", storePath, err)
		return cm
	}
	// loadFromDisk may have atomically promoted a crash marker to a visible
	// recovery slot. Persist that transition before returning so a second crash
	// during startup cannot re-promote the same marker.
	_ = cm.FlushNow()
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
// Exempt sessions (see conversationSessionEvictExempt) are kept: desktop app
// sessions are persistent by design, and a session holding a resumable
// unfinished slot must survive until the user decides.
func (cm *ConversationMemory) EvictExpired() {
	now := time.Now()
	type expiredEntry struct {
		userID  string
		entries []ConversationEntry
	}
	var toArchive []expiredEntry
	changed := false

	cm.checkpointLockedMutation(func() {
		for _, sh := range cm.shards {
			sh.mu.Lock()
			for uid, s := range sh.sessions {
				if now.Sub(s.lastAccess) > MemoryTTL {
					if conversationSessionEvictExempt(uid, s) {
						continue
					}
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
	})

	for _, e := range toArchive {
		if err := cm.Archiver.Archive(e.userID, e.entries); err != nil {
			fmt.Fprintf(os.Stderr, "conversation_archiver: failed to archive user %s: %v\n", e.userID, err)
		}
	}
}

// conversationSessionEvictExempt reports whether a session is immune to the
// in-memory MemoryTTL eviction. Desktop app sessions ("desktop-user" family:
// the main assistant tab and project/expert tabs) are persistent across
// restarts — project-tab sessions are already bounded by the 30-day rule in
// loadFromDisk and every session is turn-trimmed, so the 2h IM-oriented TTL
// would only cause "amnesia" after an idle gap or an overnight restart.
// Any session holding an unfinished task slot is also exempt regardless of
// platform: the slot is an explicit resume entry point and must not silently
// vanish while awaiting the user's decision.
func conversationSessionEvictExempt(userID string, s *conversationSession) bool {
	if userID == "desktop-user" || strings.HasPrefix(userID, "desktop-user:") {
		return true
	}
	if s != nil && s.unfinishedSlot != nil {
		return true
	}
	return false
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
	cm.flushMu.Lock()
	defer cm.flushMu.Unlock()
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

// Load returns a copy of the active branch entries for a user.
func (cm *ConversationMemory) Load(userID string) []ConversationEntry {
	return cm.LoadActiveBranch(userID)
}

// LoadAll returns every persisted conversation entry, including inactive
// branches. Most prompt-building code should use Load/LoadActiveBranch instead.
func (cm *ConversationMemory) LoadAll(userID string) []ConversationEntry {
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

// LoadActiveBranch returns the active path through the user's conversation tree.
func (cm *ConversationMemory) LoadActiveBranch(userID string) []ConversationEntry {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	s := sh.sessions[userID]
	if s == nil {
		return nil
	}
	return NewConversationTreeWithTip(s.entries, s.activeBranchTipID).ActiveBranch()
}

// ActiveBranchTipID returns the currently selected branch tip ID.
func (cm *ConversationMemory) ActiveBranchTipID(userID string) string {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	if s := sh.sessions[userID]; s != nil {
		return s.activeBranchTipID
	}
	return ""
}

// Save stores conversation entries for a user and treats the final entry as the
// active branch tip. Existing inactive branches are preserved when entries carry
// branch metadata.
func (cm *ConversationMemory) Save(userID string, entries []ConversationEntry) {
	cm.checkpointLockedMutation(func() {
		entries = deduplicateAdjacentAssistantEntriesForActiveBranch(entries)
		now := time.Now()
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil {
			s = &conversationSession{}
			sh.sessions[userID] = s
		}
		cm.saveEntriesLocked(s, entries, now)
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
}

// saveEntriesLocked is Save's branch-preserving mutation. The caller must
// hold the owning user shard write lock.
func (cm *ConversationMemory) saveEntriesLocked(s *conversationSession, entries []ConversationEntry, now time.Time) {
	if len(entries) == 0 {
		s.entries = nil
		s.activeBranchTipID = ""
		s.lastAccess = now
		return
	}
	hasBranchMetadata := false
	for _, entry := range entries {
		if entry.ID != "" {
			hasBranchMetadata = true
			break
		}
	}
	if !hasBranchMetadata {
		if all, tip, ok := mergeLinearEntriesIntoExistingTree(s.entries, s.activeBranchTipID, entries); ok {
			s.activeBranchTipID = tip
			s.entries = all
			s.lastAccess = now
			return
		}
		tree := NewConversationTree(entries)
		s.activeBranchTipID = tree.TipID()
		s.entries = tree.AllConversationEntries()
		s.lastAccess = now
		return
	}
	tree := NewConversationTreeWithTip(s.entries, s.activeBranchTipID)
	for _, entry := range entries {
		if entry.ID == "" {
			tree.Append(entry)
			continue
		}
		branchEntry := entryToBranchable(entry)
		if branchEntry.Timestamp == 0 {
			branchEntry.Timestamp = now.UnixMilli()
		}
		tree.entries[branchEntry.ID] = branchEntry
		if branchEntry.ParentID == "" && !containsString(tree.rootIDs, branchEntry.ID) {
			tree.rootIDs = append(tree.rootIDs, branchEntry.ID)
		}
		tree.tipID = branchEntry.ID
	}
	s.activeBranchTipID = tree.TipID()
	s.entries = tree.AllConversationEntries()
	s.lastAccess = now
}

// Append atomically appends entries to a user's conversation history.
// Unlike Load→append→Save, this is safe to call concurrently with Save
// because the read-modify-write happens under a single lock acquisition.
// Used by /btw side queries that run concurrently with the main agent loop.
func (cm *ConversationMemory) Append(userID string, entries ...ConversationEntry) {
	if len(entries) == 0 {
		return
	}
	cm.checkpointLockedMutation(func() {
		entries = deduplicateAdjacentAssistantEntriesForActiveBranch(entries)
		if len(entries) == 0 {
			return
		}
		now := time.Now()
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil {
			s = &conversationSession{}
			sh.sessions[userID] = s
		}
		tree := NewConversationTreeWithTip(s.entries, s.activeBranchTipID)
		for _, entry := range entries {
			tree.Append(entry)
		}
		s.activeBranchTipID = tree.TipID()
		s.entries = tree.AllConversationEntries()
		s.lastAccess = now
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
}

// SetActiveBranchTip rewinds or switches the visible conversation branch while
// keeping every node in the tree.
func (cm *ConversationMemory) SetActiveBranchTip(userID, tipID string) bool {
	changed := false
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		defer sh.mu.Unlock()
		s := sh.sessions[userID]
		if s == nil {
			return
		}
		tree := NewConversationTreeWithTip(s.entries, s.activeBranchTipID)
		if !tree.BranchAt(tipID) {
			return
		}
		s.activeBranchTipID = tree.TipID()
		s.lastAccess = time.Now()
		cm.markDirtyAndScheduleFlush()
		changed = true
	})
	return changed
}

// DeduplicateAdjacentAssistantEntries removes exact adjacent assistant text
// duplicates. Streaming UI reconciliation and final response persistence can
// occasionally race and hand the same completed message back twice; keeping both
// pollutes future prompts and makes the assistant repeat stale task summaries.
func DeduplicateAdjacentAssistantEntries(entries []ConversationEntry) []ConversationEntry {
	return deduplicateAdjacentAssistantEntries(entries, false)
}

func deduplicateAdjacentAssistantEntriesForActiveBranch(entries []ConversationEntry) []ConversationEntry {
	return deduplicateAdjacentAssistantEntries(entries, true)
}

func deduplicateAdjacentAssistantEntries(entries []ConversationEntry, respectBranchMetadata bool) []ConversationEntry {
	if len(entries) < 2 {
		return entries
	}
	result := make([]ConversationEntry, 0, len(entries))
	for _, entry := range entries {
		if len(result) > 0 && isDuplicateAssistantEntry(result[len(result)-1], entry, respectBranchMetadata) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func isDuplicateAssistantEntry(left, right ConversationEntry, respectBranchMetadata bool) bool {
	if left.Role != "assistant" || right.Role != "assistant" {
		return false
	}
	if respectBranchMetadata && (left.ID != "" || right.ID != "") {
		return left.ID != "" &&
			right.ID != "" &&
			left.ID == right.ID &&
			left.ParentID == right.ParentID
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

func mergeLinearEntriesIntoExistingTree(all []ConversationEntry, activeTipID string, entries []ConversationEntry) ([]ConversationEntry, string, bool) {
	if len(all) == 0 || len(entries) == 0 {
		return nil, "", false
	}
	tree := NewConversationTreeWithTip(all, activeTipID)
	active := tree.ActiveBranch()
	if len(active) == 0 {
		return nil, "", false
	}
	common := 0
	for common < len(entries) && common < len(active) && sameConversationEntryPayload(entries[common], active[common]) {
		common++
	}
	if common == 0 {
		return nil, "", false
	}
	if !tree.BranchAt(active[common-1].ID) {
		return nil, "", false
	}
	for _, entry := range entries[common:] {
		tree.Append(entry)
	}
	return tree.AllConversationEntries(), tree.TipID(), true
}

func sameConversationEntryPayload(left, right ConversationEntry) bool {
	return left.Role == right.Role &&
		reflect.DeepEqual(left.Content, right.Content) &&
		left.ReasoningContent == right.ReasoningContent &&
		reflect.DeepEqual(left.ToolCalls, right.ToolCalls) &&
		left.ToolCallID == right.ToolCallID &&
		left.ToolName == right.ToolName &&
		left.ToolOutcome == right.ToolOutcome
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// Clear removes all conversation data for a user.
func (cm *ConversationMemory) Clear(userID string) {
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		delete(sh.sessions, userID)
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
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

// UnfinishedSlots returns a stable snapshot of recovery slots for startup UI
// projection. It never binds or resumes a slot.
func (cm *ConversationMemory) UnfinishedSlots() []*UnfinishedTaskSlot {
	slots := make([]*UnfinishedTaskSlot, 0)
	for _, sh := range cm.shards {
		sh.mu.RLock()
		for _, s := range sh.sessions {
			if s != nil && s.unfinishedSlot != nil {
				slots = append(slots, CloneUnfinishedTaskSlot(s.unfinishedSlot))
			}
		}
		sh.mu.RUnlock()
	}
	return slots
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
	cm.checkpointLockedMutation(func() {
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
	})
}

// ReplaceInFlightWithUnfinishedSlot atomically records a recovery slot and
// clears the marker. It refuses to overwrite a pending user-facing slot, but
// replaces an already resolved slot with the newer interrupted-loop snapshot.
func (cm *ConversationMemory) ReplaceInFlightWithUnfinishedSlot(userID string, slot *UnfinishedTaskSlot) bool {
	if slot == nil {
		return false
	}
	clone := CloneUnfinishedTaskSlot(slot)
	if strings.TrimSpace(clone.UserID) == "" {
		clone.UserID = userID
	}
	now := time.Now()
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = now
	}
	clone.UpdatedAt = now
	replaced := false
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil {
			s = &conversationSession{}
			sh.sessions[userID] = s
		}
		if unfinishedSlotIsPendingDecision(s.unfinishedSlot) {
			sh.mu.Unlock()
			return
		}
		s.unfinishedSlot = clone
		s.activeSlotID = ""
		s.inFlightTask = ""
		s.inFlightProjectPath = ""
		s.inFlightSetAt = time.Time{}
		s.inFlightRunID = ""
		s.inFlightSequence = 0
		s.inFlightLastTool = ""
		s.inFlightSideEffect = ""
		s.lastAccess = now
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
		replaced = true
	})
	return replaced
}

func (cm *ConversationMemory) BindUnfinishedSlot(userID, slotID string) bool {
	slotID = strings.TrimSpace(slotID)
	if slotID == "" {
		return false
	}
	bound := false
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil || s.unfinishedSlot == nil || s.unfinishedSlot.SlotID != slotID {
			sh.mu.Unlock()
			return
		}
		now := time.Now()
		s.activeSlotID = slotID
		s.unfinishedSlot.Status = UnfinishedTaskSlotStatusResumed
		s.unfinishedSlot.BoundAt = now
		s.unfinishedSlot.UpdatedAt = now
		s.lastAccess = now
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
		bound = true
	})
	return bound
}

func (cm *ConversationMemory) ClearActiveSlot(userID string) {
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		if s := sh.sessions[userID]; s != nil {
			s.activeSlotID = ""
			s.lastAccess = time.Now()
		}
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
}

func (cm *ConversationMemory) DismissUnfinishedSlot(userID, slotID string) {
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		if s := sh.sessions[userID]; s != nil && s.unfinishedSlot != nil {
			if slotID == "" || s.unfinishedSlot.SlotID == strings.TrimSpace(slotID) {
				s.unfinishedSlot = nil
				s.activeSlotID = ""
				s.lastAccess = time.Now()
			}
		}
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
}

func (cm *ConversationMemory) CompleteUnfinishedSlot(userID, slotID string) {
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		if s := sh.sessions[userID]; s != nil && s.unfinishedSlot != nil {
			if slotID == "" || s.unfinishedSlot.SlotID == strings.TrimSpace(slotID) {
				now := time.Now()
				s.unfinishedSlot.Status = UnfinishedTaskSlotStatusCompleted
				s.unfinishedSlot.UpdatedAt = now
				s.activeSlotID = ""
				s.lastAccess = now
			}
		}
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
}

func (cm *ConversationMemory) ClearConversationButKeepSlot(userID string) {
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil {
			sh.mu.Unlock()
			return
		}
		s.entries = nil
		s.activeBranchTipID = ""
		s.activeSlotID = ""
		s.lastAccess = time.Now()
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
}

func (cm *ConversationMemory) ClearConversationAndDismissSlot(userID string) {
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil {
			sh.mu.Unlock()
			return
		}
		s.entries = nil
		s.activeBranchTipID = ""
		s.unfinishedSlot = nil
		s.activeSlotID = ""
		s.lastAccess = time.Now()
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
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
	cm.checkpointLockedMutation(func() {
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
		s.inFlightSequence = 0
		s.inFlightLastTool = ""
		s.inFlightSideEffect = ""
		s.lastAccess = now
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
}

// PersistInFlightCheckpoint writes the complete conversation and recovery
// marker under a single user-shard lock, then synchronously persists that
// snapshot. This prevents intentionally creating a marker-only recovery state.
func (cm *ConversationMemory) PersistInFlightCheckpoint(userID string, entries []ConversationEntry, task, projectPath, runID string, checkpoint InFlightCheckpoint) error {
	cm.checkpointMu.Lock()
	defer cm.checkpointMu.Unlock()
	return cm.persistInFlightCheckpointLocked(userID, entries, task, projectPath, runID, checkpoint)
}

func (cm *ConversationMemory) persistInFlightCheckpointLocked(userID string, entries []ConversationEntry, task, projectPath, runID string, checkpoint InFlightCheckpoint) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run ID", ErrInFlightCheckpointRunConflict)
	}
	entries = deduplicateAdjacentAssistantEntriesForActiveBranch(entries)
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	createdSession := false
	if s == nil {
		s = &conversationSession{}
		sh.sessions[userID] = s
		createdSession = true
	}
	if unfinishedSlotIsPendingDecision(s.unfinishedSlot) {
		slotID := s.unfinishedSlot.SlotID
		if createdSession {
			delete(sh.sessions, userID)
		}
		sh.mu.Unlock()
		return fmt.Errorf("%w: user=%q pending_slot=%q", ErrInFlightCheckpointRunConflict, userID, slotID)
	}
	if existingTask := strings.TrimSpace(s.inFlightTask); existingTask != "" && strings.TrimSpace(s.inFlightRunID) != runID {
		ownerRunID := strings.TrimSpace(s.inFlightRunID)
		if createdSession {
			delete(sh.sessions, userID)
		}
		sh.mu.Unlock()
		return fmt.Errorf("%w: user=%q checkpoint_run=%q owner_run=%q", ErrInFlightCheckpointRunConflict, userID, runID, ownerRunID)
	}
	before := cloneConversationSession(s)
	now := time.Now()
	cm.saveEntriesLocked(s, entries, now)
	s.inFlightTask = task
	s.inFlightProjectPath = projectPath
	s.inFlightSetAt = now
	s.inFlightRunID = runID
	s.inFlightSequence = checkpoint.Sequence
	s.inFlightLastTool = strings.TrimSpace(checkpoint.LastToolName)
	s.inFlightSideEffect = strings.TrimSpace(checkpoint.SideEffectState)
	candidate := cloneConversationSession(s)
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
	if err := cm.FlushNow(); err != nil {
		cm.restoreCheckpointMutation(userID, before, candidate, createdSession)
		return err
	}
	return nil
}

// CompleteInFlightCheckpointForRun clears the marker only when the same run
// still owns it, then durably flushes the transition. It closes the window
// where a normal completion could otherwise leave a stale marker on disk.
func (cm *ConversationMemory) CompleteInFlightCheckpointForRun(userID, runID string) error {
	cm.checkpointMu.Lock()
	defer cm.checkpointMu.Unlock()
	runID = strings.TrimSpace(runID)
	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil || (runID != "" && s.inFlightRunID != runID) {
		sh.mu.Unlock()
		return cm.FlushNow()
	}
	if s.inFlightTask == "" && s.inFlightProjectPath == "" && s.inFlightSetAt.IsZero() && s.inFlightRunID == "" {
		sh.mu.Unlock()
		return cm.FlushNow()
	}
	before := cloneConversationSession(s)
	s.inFlightTask = ""
	s.inFlightProjectPath = ""
	s.inFlightSetAt = time.Time{}
	s.inFlightRunID = ""
	s.inFlightSequence = 0
	s.inFlightLastTool = ""
	s.inFlightSideEffect = ""
	candidate := cloneConversationSession(s)
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
	if err := cm.FlushNow(); err != nil {
		cm.restoreCheckpointMutation(userID, before, candidate, false)
		return err
	}
	return nil
}

// SaveAndCompleteInFlightCheckpointForRun atomically replaces a run's
// conversation history and retires that same run's in-flight marker before
// synchronously writing the snapshot. It is for interactive pauses whose
// paired tool result becomes the durable continuation state; writing history
// and clearing the pre-tool marker as separate transitions can otherwise
// resurrect a duplicate crash-recovery card after restart.
func (cm *ConversationMemory) SaveAndCompleteInFlightCheckpointForRun(userID, runID string, entries []ConversationEntry) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run ID", ErrInFlightCheckpointRunConflict)
	}
	entries = deduplicateAdjacentAssistantEntriesForActiveBranch(entries)
	cm.checkpointMu.Lock()
	defer cm.checkpointMu.Unlock()

	sh := cm.shard(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	createdSession := false
	if s == nil {
		s = &conversationSession{}
		sh.sessions[userID] = s
		createdSession = true
	}
	if unfinishedSlotIsPendingDecision(s.unfinishedSlot) &&
		!(s.unfinishedSlot.Source == UnfinishedTaskSlotSourceInFlightLeaseExpired &&
			s.unfinishedSlot.EvidenceScopeKey == inFlightRunScopeKey(runID)) {
		slotID := s.unfinishedSlot.SlotID
		if createdSession {
			delete(sh.sessions, userID)
		}
		sh.mu.Unlock()
		return fmt.Errorf("%w: user=%q pending_slot=%q", ErrInFlightCheckpointRunConflict, userID, slotID)
	}
	if existingTask := strings.TrimSpace(s.inFlightTask); existingTask != "" && strings.TrimSpace(s.inFlightRunID) != runID {
		ownerRunID := strings.TrimSpace(s.inFlightRunID)
		if createdSession {
			delete(sh.sessions, userID)
		}
		sh.mu.Unlock()
		return fmt.Errorf("%w: user=%q completion_run=%q owner_run=%q", ErrInFlightCheckpointRunConflict, userID, runID, ownerRunID)
	}
	before := cloneConversationSession(s)
	now := time.Now()
	cm.saveEntriesLocked(s, entries, now)
	if s.inFlightRunID == runID {
		s.inFlightTask = ""
		s.inFlightProjectPath = ""
		s.inFlightSetAt = time.Time{}
		s.inFlightRunID = ""
		s.inFlightSequence = 0
		s.inFlightLastTool = ""
		s.inFlightSideEffect = ""
	}
	// A lease-expired slot for this exact run is synthetic recovery evidence;
	// the durable interactive state supersedes it. Do not touch any other slot.
	if s.unfinishedSlot != nil &&
		s.unfinishedSlot.Source == UnfinishedTaskSlotSourceInFlightLeaseExpired &&
		s.unfinishedSlot.EvidenceScopeKey == inFlightRunScopeKey(runID) {
		s.unfinishedSlot = nil
		s.activeSlotID = ""
	}
	s.lastAccess = now
	candidate := cloneConversationSession(s)
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
	if err := cm.FlushNow(); err != nil {
		cm.restoreCheckpointMutation(userID, before, candidate, createdSession)
		return err
	}
	return nil
}

// cloneConversationSession snapshots the mutable state touched by a checkpoint
// operation. It is used only while checkpointMu serializes durable mutations.
func cloneConversationSession(s *conversationSession) *conversationSession {
	if s == nil {
		return nil
	}
	copy := *s
	copy.entries = append([]ConversationEntry(nil), s.entries...)
	copy.unfinishedSlot = CloneUnfinishedTaskSlot(s.unfinishedSlot)
	return &copy
}

// restoreCheckpointMutation rolls back only the parts that still match the
// failed candidate. Other mutations are allowed while FlushNow writes; blindly
// restoring the whole session here would erase a newer user turn, slot action,
// or lease update that happened during that write.
func (cm *ConversationMemory) restoreCheckpointMutation(userID string, before, candidate *conversationSession, createdSession bool) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	current := sh.sessions[userID]
	if current == nil || candidate == nil {
		sh.mu.Unlock()
		return
	}
	if createdSession && reflect.DeepEqual(current, candidate) {
		delete(sh.sessions, userID)
		sh.mu.Unlock()
		return
	}
	if before != nil {
		if reflect.DeepEqual(current.entries, candidate.entries) && current.activeBranchTipID == candidate.activeBranchTipID {
			current.entries = append([]ConversationEntry(nil), before.entries...)
			current.activeBranchTipID = before.activeBranchTipID
		}
		if sameInFlightCheckpointState(current, candidate) {
			current.inFlightTask = before.inFlightTask
			current.inFlightProjectPath = before.inFlightProjectPath
			current.inFlightSetAt = before.inFlightSetAt
			current.inFlightRunID = before.inFlightRunID
			current.inFlightSequence = before.inFlightSequence
			current.inFlightLastTool = before.inFlightLastTool
			current.inFlightSideEffect = before.inFlightSideEffect
		}
		if reflect.DeepEqual(current.unfinishedSlot, candidate.unfinishedSlot) && current.activeSlotID == candidate.activeSlotID {
			current.unfinishedSlot = CloneUnfinishedTaskSlot(before.unfinishedSlot)
			current.activeSlotID = before.activeSlotID
		}
	}
	sh.mu.Unlock()
	// flushDirty marks the store dirty again when saveToDisk fails. Do not clear
	// that retry signal: unrelated concurrent mutations may also be pending and
	// the restored durable state still needs a later successful snapshot.
}

func sameInFlightCheckpointState(a, b *conversationSession) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.inFlightTask == b.inFlightTask &&
		a.inFlightProjectPath == b.inFlightProjectPath &&
		a.inFlightSetAt.Equal(b.inFlightSetAt) &&
		a.inFlightRunID == b.inFlightRunID &&
		a.inFlightSequence == b.inFlightSequence &&
		a.inFlightLastTool == b.inFlightLastTool &&
		a.inFlightSideEffect == b.inFlightSideEffect
}

// checkpointLockedMutation serializes a non-checkpoint session mutation with
// checkpoint snapshotting. A tool-progress checkpoint must not restore an old
// session after a concurrent normal Save/Append/slot update committed while
// its FlushNow was in progress.
func (cm *ConversationMemory) checkpointLockedMutation(mutate func()) {
	if cm == nil || mutate == nil {
		return
	}
	cm.checkpointMu.Lock()
	defer cm.checkpointMu.Unlock()
	mutate()
}

// RefreshInFlightTask extends the activity lease for an existing in-flight
// marker. It returns false when there is no active marker for userID.
func (cm *ConversationMemory) RefreshInFlightTask(userID string) bool {
	found := false
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil || strings.TrimSpace(s.inFlightTask) == "" {
			sh.mu.Unlock()
			return
		}
		found = true
		now := time.Now()
		if !s.inFlightSetAt.IsZero() && now.Sub(s.inFlightSetAt) < InFlightTaskRenewInterval {
			sh.mu.Unlock()
			return
		}
		s.inFlightSetAt = now
		s.lastAccess = now
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
	return found
}

// ClearInFlightTask removes the in-flight marker, indicating the agent
// loop completed normally.
func (cm *ConversationMemory) ClearInFlightTask(userID string) {
	changed := false
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		if s := sh.sessions[userID]; s != nil {
			if s.inFlightTask != "" || s.inFlightProjectPath != "" || !s.inFlightSetAt.IsZero() || s.inFlightRunID != "" {
				s.inFlightTask = ""
				s.inFlightProjectPath = ""
				s.inFlightSetAt = time.Time{}
				s.inFlightRunID = ""
				s.inFlightSequence = 0
				s.inFlightLastTool = ""
				s.inFlightSideEffect = ""
				changed = true
			}
		}
		sh.mu.Unlock()
		if changed {
			cm.markDirtyAndScheduleFlush()
		}
	})
}

func (cm *ConversationMemory) ClearInFlightTaskForRun(userID, runID string) {
	runID = strings.TrimSpace(runID)
	changed := false
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		if s := sh.sessions[userID]; s != nil {
			if runID == "" || s.inFlightRunID == runID {
				if s.inFlightTask != "" || s.inFlightProjectPath != "" || !s.inFlightSetAt.IsZero() || s.inFlightRunID != "" {
					s.inFlightTask = ""
					s.inFlightProjectPath = ""
					s.inFlightSetAt = time.Time{}
					s.inFlightRunID = ""
					s.inFlightSequence = 0
					s.inFlightLastTool = ""
					s.inFlightSideEffect = ""
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
	})
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
	cm.checkpointLockedMutation(func() {
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
	})
	return expired
}

func (cm *ConversationMemory) convertExpiredInFlightLocked(userID string, s *conversationSession, now time.Time) {
	task := s.inFlightTask
	projectPath := s.inFlightProjectPath
	runID := s.inFlightRunID
	// A pending slot is deliberate user state and wins. A resumed/completed
	// slot is historical state, so a newer stalled run must replace it just as
	// startup promotion and graceful shutdown snapshots do.
	if s.unfinishedSlot == nil || !unfinishedSlotIsPendingDecision(s.unfinishedSlot) {
		slotID := newRecoverySlotID("inflight-expired", now)
		s.unfinishedSlot = &UnfinishedTaskSlot{
			SlotID:           slotID,
			UserID:           userID,
			ProjectPath:      projectPath,
			Tool:             "agent",
			Status:           UnfinishedTaskSlotStatusInterrupted,
			Summary:          "Previous task stopped making progress and was moved to recovery.",
			LastTask:         task,
			ResumePrompt:     "The previous task stopped making progress. Continue from the saved conversation history and avoid repeating completed work.\n",
			Source:           UnfinishedTaskSlotSourceInFlightLeaseExpired,
			EvidenceScopeKey: inFlightRunScopeKey(runID),
			LastCheckpointAt: s.inFlightSetAt,
			LastToolName:     s.inFlightLastTool,
			SideEffectState:  s.inFlightSideEffect,
			RecoveryMode:     recoveryModeForSideEffect(s.inFlightSideEffect),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
	}
	s.inFlightTask = ""
	s.inFlightProjectPath = ""
	s.inFlightSetAt = time.Time{}
	s.inFlightRunID = ""
	s.inFlightSequence = 0
	s.inFlightLastTool = ""
	s.inFlightSideEffect = ""
	s.lastAccess = now
}

func recoveryModeForSideEffect(sideEffect string) string {
	switch strings.TrimSpace(sideEffect) {
	case "none":
		return "resume_context"
	default:
		// A local mutation may already be visible in the workspace, just as an
		// external mutation may already have reached its destination. Neither is
		// safe for blind continuation; the UI must require an explicit review.
		return "requires_review"
	}
}

// newRecoverySlotID keeps independently interrupted sessions distinguishable
// even when they are promoted or snapshotted during the same millisecond.
// The timestamp is useful in diagnostics; the random suffix prevents a UI
// action for one slot from accidentally addressing another user's slot.
func newRecoverySlotID(prefix string, now time.Time) string {
	var entropy [8]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		// crypto/rand failure is extraordinarily rare; preserve uniqueness across
		// the normal process lifetime rather than returning an empty identifier.
		return fmt.Sprintf("%s-%d-%d", prefix, now.UnixNano(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%d-%x", prefix, now.UnixMilli(), entropy[:])
}

// PromoteRecoverableCheckpoints turns markers loaded from a prior process into
// explicit recovery slots immediately. A fresh process has no active owner, so
// this intentionally does not wait for the normal in-flight lease. The slot and
// marker transition is atomic per user and idempotent on later launches.
func (cm *ConversationMemory) PromoteRecoverableCheckpoints(now time.Time) int {
	if now.IsZero() {
		now = time.Now()
	}
	promoted, changed := 0, false
	cm.checkpointLockedMutation(func() {
		for _, sh := range cm.shards {
			sh.mu.Lock()
			for userID, s := range sh.sessions {
				if s == nil || strings.TrimSpace(s.inFlightTask) == "" {
					continue
				}
				if s.unfinishedSlot == nil || !unfinishedSlotIsPendingDecision(s.unfinishedSlot) {
					s.unfinishedSlot = &UnfinishedTaskSlot{
						SlotID:           newRecoverySlotID("inflight-recovery", now),
						UserID:           userID,
						ProjectPath:      s.inFlightProjectPath,
						Tool:             "agent",
						Status:           UnfinishedTaskSlotStatusInterrupted,
						Summary:          "Previous task was interrupted after a durable tool-progress checkpoint.",
						LastTask:         s.inFlightTask,
						ResumePrompt:     "Resume from saved context. Do not repeat a previously executed tool call; review its side effects first.",
						Source:           UnfinishedTaskSlotSourceInFlightRecovery,
						EvidenceScopeKey: inFlightRunScopeKey(s.inFlightRunID),
						LastCheckpointAt: s.inFlightSetAt,
						LastToolName:     s.inFlightLastTool,
						SideEffectState:  s.inFlightSideEffect,
						RecoveryMode:     recoveryModeForSideEffect(s.inFlightSideEffect),
						CreatedAt:        now,
						UpdatedAt:        now,
					}
					promoted++
				}
				// An existing pending slot is deliberate user state. Preserve it but
				// consume the marker to prevent duplicate promotion on next launch.
				s.inFlightTask = ""
				s.inFlightProjectPath = ""
				s.inFlightSetAt = time.Time{}
				s.inFlightRunID = ""
				s.inFlightSequence = 0
				s.inFlightLastTool = ""
				s.inFlightSideEffect = ""
				s.lastAccess = now
				changed = true
			}
			sh.mu.Unlock()
		}
		if changed {
			cm.markDirtyAndScheduleFlush()
		}
	})
	return promoted
}

func unfinishedSlotIsPendingDecision(slot *UnfinishedTaskSlot) bool {
	if slot == nil {
		return false
	}
	return slot.Status != UnfinishedTaskSlotStatusResumed && slot.Status != UnfinishedTaskSlotStatusCompleted
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
	var task, projectPath string
	cm.checkpointLockedMutation(func() {
		sh := cm.shard(userID)
		sh.mu.Lock()
		s := sh.sessions[userID]
		if s == nil || s.inFlightTask == "" {
			sh.mu.Unlock()
			return
		}
		task = s.inFlightTask
		projectPath = s.inFlightProjectPath
		s.inFlightTask = ""
		s.inFlightProjectPath = ""
		s.inFlightSetAt = time.Time{}
		s.inFlightRunID = ""
		s.inFlightSequence = 0
		s.inFlightLastTool = ""
		s.inFlightSideEffect = ""
		sh.mu.Unlock()
		cm.markDirtyAndScheduleFlush()
	})
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
			entries := sanitizeConversationEntriesForPersistence(session.entries)
			snapshot.Sessions[userID] = persistedSession{
				Entries:             entries,
				ActiveBranchTipID:   session.activeBranchTipID,
				LastAccess:          session.lastAccess,
				UnfinishedSlot:      CloneUnfinishedTaskSlot(session.unfinishedSlot),
				ActiveSlotID:        session.activeSlotID,
				InFlightTask:        session.inFlightTask,
				InFlightProjectPath: session.inFlightProjectPath,
				InFlightSetAt:       session.inFlightSetAt,
				InFlightRunID:       session.inFlightRunID,
				InFlightSequence:    session.inFlightSequence,
				InFlightLastTool:    session.inFlightLastTool,
				InFlightSideEffect:  session.inFlightSideEffect,
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
	now := time.Now()
	const projectTabSessionMaxAge = 30 * 24 * time.Hour
	for userID, session := range snapshot.Sessions {
		// T18: Skip project tab sessions that haven't been accessed in 30 days.
		// Project tab sessions have userID format "desktop-user:{path}" (contains
		// a colon after the "desktop-user" prefix, distinguishing them from the
		// plain "desktop-user" local session).
		if strings.HasPrefix(userID, "desktop-user:") && len(userID) > len("desktop-user:") {
			if !session.LastAccess.IsZero() && now.Sub(session.LastAccess) > projectTabSessionMaxAge {
				log.Printf("[ConversationMemory] evicting stale project tab session user=%s (last_access=%v, age=%v)",
					userID, session.LastAccess, now.Sub(session.LastAccess))
				needsRewrite = true
				continue
			}
		}
		entries := make([]ConversationEntry, len(session.Entries))
		copy(entries, session.Entries)
		sanitized := sanitizeConversationEntriesForPersistence(entries)
		if !reflect.DeepEqual(sanitized, entries) {
			entries = sanitized
			needsRewrite = true
		}

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
			activeBranchTipID:   session.ActiveBranchTipID,
			lastAccess:          session.LastAccess,
			unfinishedSlot:      CloneUnfinishedTaskSlot(session.UnfinishedSlot),
			activeSlotID:        session.ActiveSlotID,
			inFlightTask:        session.InFlightTask,
			inFlightProjectPath: session.InFlightProjectPath,
			inFlightSetAt:       inFlightSetAt,
			inFlightRunID:       session.InFlightRunID,
			inFlightSequence:    session.InFlightSequence,
			inFlightLastTool:    session.InFlightLastTool,
			inFlightSideEffect:  session.InFlightSideEffect,
		}
		sh.mu.Unlock()
	}
	if cm.PromoteRecoverableCheckpoints(time.Now()) > 0 {
		needsRewrite = true
	} else if cm.ExpireStaleInFlightTasks(time.Now(), InFlightTaskLease) > 0 {
		needsRewrite = true
	}
	if needsRewrite {
		cm.markDirtyAndScheduleFlush()
	}
	return nil
}

func sanitizeConversationEntriesForPersistence(entries []ConversationEntry) []ConversationEntry {
	out := make([]ConversationEntry, len(entries))
	for i, entry := range entries {
		out[i] = sanitizeConversationEntryForPersistence(entry)
	}
	return out
}

func sanitizeConversationEntryForPersistence(entry ConversationEntry) ConversationEntry {
	entry.Content = sanitizeConversationPersistenceValue("content", entry.Content)
	if content, ok := entry.Content.(string); ok {
		entry.Content = stripPlainToolCallPersistenceLeak(StripRolePrefixHallucination(content))
	}
	entry.ReasoningContent = stripPlainToolCallPersistenceLeak(StripRolePrefixHallucination(security.RedactSensitiveString(entry.ReasoningContent)))
	if entry.ToolCalls != nil {
		entry.ToolCalls = sanitizeConversationPersistenceValue("tool_calls", entry.ToolCalls)
	}
	entry.ToolCallID = security.RedactSensitiveString(entry.ToolCallID)
	entry.ToolName = security.RedactSensitiveString(entry.ToolName)
	entry.ToolOutcome = security.RedactSensitiveString(entry.ToolOutcome)
	return entry
}

func stripPlainToolCallPersistenceLeak(content string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, "tool_call")
	if idx < 0 {
		return content
	}
	return strings.TrimSpace(content[:idx])
}

func sanitizeConversationPersistenceValue(key string, value interface{}) interface{} {
	sanitized := security.SanitizeSensitiveValue(key, value)
	if !reflect.DeepEqual(sanitized, value) {
		return sanitized
	}
	switch value.(type) {
	case nil, string, map[string]interface{}, map[string]string, []interface{}, []string:
		return sanitized
	}
	data, err := json.Marshal(value)
	if err != nil {
		return sanitized
	}
	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return sanitized
	}
	return security.SanitizeSensitiveValue(key, decoded)
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
