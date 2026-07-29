package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// stickyCodingUserMu serializes read-modify-write of per-user sticky coding memory
// so parallel plan steps do not clobber each other's fields (step status, route, …).
var stickyCodingUserMu sync.Map // userID -> *sync.Mutex

func withStickyCodingUserLock(userID string, fn func()) {
	userID = strings.TrimSpace(userID)
	if userID == "" || fn == nil {
		if fn != nil {
			fn()
		}
		return
	}
	v, _ := stickyCodingUserMu.LoadOrStore(userID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	fn()
}

// stickyCodingWorkbenchMemory holds multi-turn state for pure coding
// environments (create-task local/remote). Survives each SubAgent completion
// via rearmSticky* and is also persisted under the task workspace (or
// ~/.maclaw/data/coding_workbench) so reopen after restart can continue.
type stickyCodingWorkbenchMemory struct {
	Kind         string `json:"kind"` // "local" | "remote"
	ProjectPath  string `json:"project_path,omitempty"`
	LastUserText string `json:"last_user_text,omitempty"`
	LastSummary  string `json:"last_summary,omitempty"`
	// SessionPlan is the durable overall goal for multi-turn coding (usually
	// the first non-empty user request). Kept separate from LastUserText so
	// follow-ups do not overwrite the original objective.
	SessionPlan string `json:"session_plan,omitempty"`
	// ExecutionPlan is the latest auto-generated multi-step plan for a complex
	// pure-coding request (markdown T1/T2…). Used for continuity and UI context.
	ExecutionPlan string   `json:"execution_plan,omitempty"`
	FilesModified []string `json:"files_modified,omitempty"`
	FilesCreated  []string `json:"files_created,omitempty"`
	TurnCount     int      `json:"turn_count,omitempty"`
	UpdatedAtUnix int64    `json:"updated_at_unix,omitempty"`
	// SessionFullAccess is session-scoped path full-access for pure coding
	// workbenches (create-task). It does not write subagent_full_access to
	// config; high-risk bash still follows SessionHighRiskAccess / global grant.
	SessionFullAccess bool `json:"session_full_access,omitempty"`
	// SessionHighRiskAccess allows high-risk bash without prompts for this
	// coding session only (separate from path trust).
	SessionHighRiskAccess bool `json:"session_high_risk_access,omitempty"`
	// SessionPermissionMode is the user's explicit choice from the input-box
	// permission menu: "request" | "workspace" | "full". Empty means legacy
	// default (workspace-style path trust for pure coding create/ensure).
	// Prevents Ensure from re-granting SessionFullAccess after the user
	// deliberately selected "请求授权".
	SessionPermissionMode string `json:"session_permission_mode,omitempty"`
	// ApprovedDirs carries allow_dir grants across multi-turn re-arms.
	ApprovedDirs []string `json:"approved_dirs,omitempty"`
	// PlanMode: "auto" | "approve" | "off" — multi-step plan policy.
	PlanMode string `json:"plan_mode,omitempty"`
	// Pending plan awaiting user approve (JSON of codingWorkbenchPendingPlan).
	PendingPlanJSON     string `json:"pending_plan_json,omitempty"`
	PendingPlanMarkdown string `json:"pending_plan_markdown,omitempty"`
	PendingPlanUserText string `json:"pending_plan_user_text,omitempty"`
	// ApprovedPlanJSON is a one-shot plan ready to execute after /plan approve.
	ApprovedPlanJSON string `json:"approved_plan_json,omitempty"`
	// SkipNextPlan forces the next resolve to single-task (from /plan skip).
	SkipNextPlan bool `json:"skip_next_plan,omitempty"`
	// StepStatuses is live Todo status for the active multi-step plan.
	StepStatuses []codingWorkbenchStepStatus `json:"step_statuses,omitempty"`
	// ProjectInstructions is cached AGENTS.md / CLAUDE.md content.
	ProjectInstructions       string   `json:"project_instructions,omitempty"`
	ProjectInstructionSources []string `json:"project_instruction_sources,omitempty"`
	// CheckpointJSON is the last pure-coding session checkpoint.
	CheckpointJSON   string `json:"checkpoint_json,omitempty"`
	CheckpointLabel  string `json:"checkpoint_label,omitempty"`
	CheckpointAtUnix int64  `json:"checkpoint_at_unix,omitempty"`
	// CheckpointHistoryJSON is a ring of prior checkpoints (bodies stripped;
	// file content lives in sidecars when present). JSON array of codingWorkbenchCheckpoint.
	CheckpointHistoryJSON string `json:"checkpoint_history_json,omitempty"`
	// Session-level token/cost observability (sum of pure-coding turns).
	SessionInputTokens   int     `json:"session_input_tokens,omitempty"`
	SessionOutputTokens  int     `json:"session_output_tokens,omitempty"`
	SessionEstCostRMB    float64 `json:"session_est_cost_rmb,omitempty"`
	LastTurnInputTokens  int     `json:"last_turn_input_tokens,omitempty"`
	LastTurnOutputTokens int     `json:"last_turn_output_tokens,omitempty"`
	LastTurnEstCostRMB   float64 `json:"last_turn_est_cost_rmb,omitempty"`
	// Last model route (observability for pure-coding turns).
	LastRouteModel  string `json:"last_route_model,omitempty"`
	LastRouteSource string `json:"last_route_source,omitempty"`
	LastRouteTask   string `json:"last_route_task,omitempty"`
	LastRouteReason string `json:"last_route_reason,omitempty"`
	// RoutePref: auto | primary | reasoning | vision — pure-coding model preference.
	RoutePref string `json:"route_pref,omitempty"`
	// BackgroundVerifySummary is the last async /bg test (or verify) outcome.
	BackgroundVerifySummary string `json:"background_verify_summary,omitempty"`
	BackgroundVerifyAtUnix  int64  `json:"background_verify_at_unix,omitempty"`
	// WorktreeMode: auto | always | off — git worktree isolation for write steps.
	WorktreeMode string `json:"worktree_mode,omitempty"`
	// WorktreeNotes recent worktree create/merge lines for /worktree status.
	WorktreeNotes []string `json:"worktree_notes,omitempty"`
	// WorktreeConflicts kept isolation trees after failed merges (adopt/discard).
	WorktreeConflicts []codingWorkbenchConflict `json:"worktree_conflicts,omitempty"`
	// Conflict UI memory (last active conflict / multi-select / focused file).
	ConflictActiveID  string   `json:"conflict_active_id,omitempty"`
	ConflictSelected  []string `json:"conflict_selected,omitempty"`
	ConflictFocusFile string   `json:"conflict_focus_file,omitempty"`
	// ConflictLog is a short audit trail of adopt/keep/base/discard actions.
	ConflictLog []string `json:"conflict_log,omitempty"`
	// Remote SSH continuity (reopen if session still alive). Password is never stored.
	RemoteSessionID  string `json:"remote_session_id,omitempty"`
	RemoteWorkDir    string `json:"remote_work_dir,omitempty"`
	RemoteProjectDir string `json:"remote_project_dir,omitempty"`
	RemoteHost       string `json:"remote_host,omitempty"`
	RemoteUser       string `json:"remote_user,omitempty"`
	RemotePort       int    `json:"remote_port,omitempty"`
}

const stickyCodingMemoryPrevOutputsMax = 8
const stickyCodingMemoryFileName = ".coding_workbench.json"
const stickyCodingMemoryMaxAge = 30 * 24 * time.Hour

func (m stickyCodingWorkbenchMemory) prevOutputs() []string {
	var out []string
	if m.TurnCount > 0 {
		out = append(out, fmt.Sprintf("Session continuity: this is turn %d in the same full coding workbench session.", m.TurnCount+1))
	}
	if s := strings.TrimSpace(m.SessionPlan); s != "" {
		out = append(out, "Session plan / overall goal:\n"+truncateRunesForSubAgent(s, 800))
	}
	if s := strings.TrimSpace(m.ExecutionPlan); s != "" {
		out = append(out, "Active multi-step execution plan:\n"+truncateRunesForSubAgent(s, 1200))
	}
	// Project AGENTS.md / CLAUDE.md — high priority for agent behavior.
	if s := strings.TrimSpace(m.ProjectInstructions); s != "" {
		out = append(out, formatProjectInstructionsForContext(s, m.ProjectInstructionSources))
	}
	if s := strings.TrimSpace(m.LastUserText); s != "" {
		out = append(out, "Previous user request:\n"+truncateRunesForSubAgent(s, 400))
	}
	if s := strings.TrimSpace(m.LastSummary); s != "" {
		out = append(out, "Previous turn result summary:\n"+truncateRunesForSubAgent(s, 1200))
	}
	if len(m.FilesModified) > 0 {
		files := uniqueSortedSubAgentStrings(m.FilesModified)
		if len(files) > 20 {
			files = files[:20]
		}
		out = append(out, "Files modified earlier in this session: "+strings.Join(files, ", "))
	}
	if len(m.FilesCreated) > 0 {
		files := uniqueSortedSubAgentStrings(m.FilesCreated)
		if len(files) > 20 {
			files = files[:20]
		}
		out = append(out, "Files created earlier in this session: "+strings.Join(files, ", "))
	}
	// Prefer instructions + plan over older file lists when over cap.
	if len(out) > stickyCodingMemoryPrevOutputsMax {
		out = out[:stickyCodingMemoryPrevOutputsMax]
	}
	return out
}

func stickyCodingMemoryFilePath(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	if projectPath := projectPathFromSessionOwnerID(userID); projectPath != "" {
		return filepath.Join(projectPath, stickyCodingMemoryFileName)
	}
	sum := sha1.Sum([]byte(userID))
	return filepath.Join(corelib.MaclawBaseDir(), "data", "coding_workbench", hex.EncodeToString(sum[:])+".json")
}

func loadStickyCodingWorkbenchMemoryFromDisk(userID string) (stickyCodingWorkbenchMemory, bool) {
	path := stickyCodingMemoryFilePath(userID)
	if path == "" {
		return stickyCodingWorkbenchMemory{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return stickyCodingWorkbenchMemory{}, false
	}
	var mem stickyCodingWorkbenchMemory
	if err := json.Unmarshal(data, &mem); err != nil {
		log.Printf("[coding-env] load sticky memory failed path=%s err=%v", path, err)
		return stickyCodingWorkbenchMemory{}, false
	}
	if mem.UpdatedAtUnix > 0 {
		age := time.Since(time.Unix(mem.UpdatedAtUnix, 0))
		if age > stickyCodingMemoryMaxAge {
			_ = os.Remove(path)
			return stickyCodingWorkbenchMemory{}, false
		}
	}
	return mem, true
}

func persistStickyCodingWorkbenchMemoryToDisk(userID string, mem stickyCodingWorkbenchMemory) {
	path := stickyCodingMemoryFilePath(userID)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[coding-env] mkdir sticky memory failed path=%s err=%v", path, err)
		return
	}
	// Compact JSON: sticky is rewritten often during multi-step runs; indent was pure cost.
	data, err := json.Marshal(mem)
	if err != nil {
		return
	}
	if err := atomicWriteFile(path, data); err != nil {
		log.Printf("[coding-env] persist sticky memory failed path=%s err=%v", path, err)
	}
}

func deleteStickyCodingWorkbenchMemoryFromDisk(userID string) {
	path := stickyCodingMemoryFilePath(userID)
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

func (h *IMMessageHandler) getStickyCodingWorkbenchMemory(userID string) stickyCodingWorkbenchMemory {
	if h == nil {
		return stickyCodingWorkbenchMemory{}
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return stickyCodingWorkbenchMemory{}
	}
	raw, ok := h.stickyCodingWorkbenchMemory.Load(userID)
	if ok {
		mem, _ := raw.(stickyCodingWorkbenchMemory)
		return mem
	}
	// Cold load after restart / process restart.
	// LoadOrStore: a concurrent update must not be overwritten by a slower disk load.
	if mem, loaded := loadStickyCodingWorkbenchMemoryFromDisk(userID); loaded {
		actual, _ := h.stickyCodingWorkbenchMemory.LoadOrStore(userID, mem)
		if got, ok := actual.(stickyCodingWorkbenchMemory); ok {
			return got
		}
		return mem
	}
	return stickyCodingWorkbenchMemory{}
}

func (h *IMMessageHandler) storeStickyCodingWorkbenchMemory(userID string, mem stickyCodingWorkbenchMemory) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Immediate durable write: cancel any pending debounce so a later timer
	// cannot overwrite this snapshot with a stale one.
	withStickyCodingUserLock(userID, func() {
		mem.UpdatedAtUnix = time.Now().Unix()
		h.stickyCodingWorkbenchMemory.Store(userID, mem)
		cancelStickyCodingDebouncedPersist(userID)
		persistStickyCodingWorkbenchMemoryToDisk(userID, mem)
	})
}

// updateStickyCodingWorkbenchMemory applies fn under the per-user lock and
// persists to disk.
func (h *IMMessageHandler) updateStickyCodingWorkbenchMemory(userID string, fn func(*stickyCodingWorkbenchMemory)) {
	h.updateStickyCodingWorkbenchMemoryOpts(userID, true, fn)
}

// updateStickyCodingWorkbenchMemoryOpts is the core RMW helper.
// persist=false updates memory immediately and schedules a debounced disk flush
// (coalesced); callers that need hard durability should use persist=true
// (e.g. recordStickyLocalCodingTurn, conflicts, plan approve).
func (h *IMMessageHandler) updateStickyCodingWorkbenchMemoryOpts(userID string, persist bool, fn func(*stickyCodingWorkbenchMemory)) {
	if h == nil || fn == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	withStickyCodingUserLock(userID, func() {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		fn(&mem)
		mem.UpdatedAtUnix = time.Now().Unix()
		h.stickyCodingWorkbenchMemory.Store(userID, mem)
		if persist {
			cancelStickyCodingDebouncedPersist(userID)
			persistStickyCodingWorkbenchMemoryToDisk(userID, mem)
		} else {
			scheduleStickyCodingDebouncedPersist(userID, mem)
		}
	})
}

// stickyCodingDebounce holds pending flush timers (userID → *time.Timer).
// stickyCodingPendingFlush holds the latest snapshot to write when the timer fires.
var (
	stickyCodingDebounce     sync.Map
	stickyCodingPendingFlush sync.Map
)

const stickyCodingDebounceDelay = 450 * time.Millisecond

func cancelStickyCodingDebouncedPersist(userID string) {
	if v, ok := stickyCodingDebounce.Load(userID); ok {
		if t, ok := v.(*time.Timer); ok {
			t.Stop()
		}
		stickyCodingDebounce.Delete(userID)
	}
	stickyCodingPendingFlush.Delete(userID)
}

func scheduleStickyCodingDebouncedPersist(userID string, mem stickyCodingWorkbenchMemory) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Always keep the newest snapshot so a delayed flush never writes stale data.
	stickyCodingPendingFlush.Store(userID, mem)
	if v, ok := stickyCodingDebounce.Load(userID); ok {
		if t, ok := v.(*time.Timer); ok {
			t.Stop()
		}
	}
	t := time.AfterFunc(stickyCodingDebounceDelay, func() {
		// Serialize with update/store/clear so we never interleave disk writes.
		withStickyCodingUserLock(userID, func() {
			stickyCodingDebounce.Delete(userID)
			raw, ok := stickyCodingPendingFlush.LoadAndDelete(userID)
			if !ok {
				return
			}
			snap, _ := raw.(stickyCodingWorkbenchMemory)
			persistStickyCodingWorkbenchMemoryToDisk(userID, snap)
		})
	})
	stickyCodingDebounce.Store(userID, t)
}

// flushStickyCodingDebouncedPersistNow writes any pending debounced snapshot
// for userID immediately (used before clear / shutdown).
// Caller should hold the user lock when concurrent with RMW updates; public
// FlushStickyCodingWorkbenchMemory already serializes. This helper is also used
// from AfterFunc under lock.
func flushStickyCodingDebouncedPersistNow(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Best-effort stop timer outside lock (AfterFunc may already be running).
	if v, ok := stickyCodingDebounce.Load(userID); ok {
		if t, ok := v.(*time.Timer); ok {
			t.Stop()
		}
	}
	withStickyCodingUserLock(userID, func() {
		stickyCodingDebounce.Delete(userID)
		raw, ok := stickyCodingPendingFlush.LoadAndDelete(userID)
		if !ok {
			return
		}
		snap, _ := raw.(stickyCodingWorkbenchMemory)
		persistStickyCodingWorkbenchMemoryToDisk(userID, snap)
	})
}

// flushAllStickyCodingDebouncedPersist flushes every pending debounced sticky
// write. Call from App.shutdown so multi-step step-status updates are not lost.
func flushAllStickyCodingDebouncedPersist() {
	stickyCodingPendingFlush.Range(func(key, value interface{}) bool {
		userID, _ := key.(string)
		if userID == "" {
			return true
		}
		flushStickyCodingDebouncedPersistNow(userID)
		return true
	})
	// Also stop any orphan timers with no pending payload.
	stickyCodingDebounce.Range(func(key, value interface{}) bool {
		if t, ok := value.(*time.Timer); ok {
			t.Stop()
		}
		stickyCodingDebounce.Delete(key)
		return true
	})
}

func (h *IMMessageHandler) clearStickyCodingWorkbenchMemory(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Best-effort: drop kept isolation trees before wiping sticky (prevents
	// orphan worktrees under %TEMP%/maclaw-coding-worktrees after /new).
	// Must run outside the delete lock so discard helpers can update sticky.
	h.cleanupStickyCodingIsolationArtifacts(userID)
	// Drop checkpoint sidecar files for this user (no sticky left to reference them).
	if n, _ := pruneCodingCheckpointSidecars(userID, ""); n > 0 {
		log.Printf("[coding-env] cleared checkpoint sidecars user=%s removed=%d", userID, n)
	}

	withStickyCodingUserLock(userID, func() {
		// Drop pending flush so a timer cannot re-create the file after delete.
		cancelStickyCodingDebouncedPersist(userID)
		h.stickyCodingWorkbenchMemory.Delete(userID)
		deleteStickyCodingWorkbenchMemoryFromDisk(userID)
	})
}

// cleanupStickyCodingIsolationArtifacts discards local worktrees / remote
// isolates recorded in WorktreeConflicts and prunes coding worktrees for the
// sticky project path. Safe if nothing is recorded.
func (h *IMMessageHandler) cleanupStickyCodingIsolationArtifacts(userID string) {
	if h == nil {
		return
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	// Copy conflict list — discard mutates sticky.
	conflicts := append([]codingWorkbenchConflict(nil), mem.WorktreeConflicts...)
	for _, c := range conflicts {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			id = c.Path
		}
		if _, err := h.discardCodingWorkbenchConflict(userID, id); err != nil {
			log.Printf("[coding-env] cleanup isolation artifact user=%s id=%s: %v", userID, id, err)
		}
	}
	// Prune any leftover maclaw/coding-* worktrees under the project git root.
	if p := strings.TrimSpace(mem.ProjectPath); p != "" {
		if msg, err := pruneCodingWorkbenchWorktrees(p); err != nil {
			log.Printf("[coding-env] prune worktrees project=%s: %v", p, err)
		} else if msg != "" && !strings.Contains(msg, "No maclaw") {
			log.Printf("[coding-env] prune worktrees project=%s: %s", p, truncateRunesForSubAgent(msg, 200))
		}
	}
}

// FlushStickyCodingWorkbenchMemory persists one user's sticky state now
// (cancels debounce). Prefer live memory over the pending debounce snapshot so
// concurrent mem-only updates are not lost. Used by shutdown / tests — not by
// hot status polls (those read RAM only).
func (h *IMMessageHandler) FlushStickyCodingWorkbenchMemory(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Stop timer first so AfterFunc does not race LoadAndDelete of pending.
	if v, ok := stickyCodingDebounce.Load(userID); ok {
		if t, ok := v.(*time.Timer); ok {
			t.Stop()
		}
	}
	withStickyCodingUserLock(userID, func() {
		stickyCodingDebounce.Delete(userID)
		stickyCodingPendingFlush.Delete(userID)
		if raw, ok := h.stickyCodingWorkbenchMemory.Load(userID); ok {
			if mem, ok := raw.(stickyCodingWorkbenchMemory); ok {
				persistStickyCodingWorkbenchMemoryToDisk(userID, mem)
			}
		}
	})
}

// FlushAllStickyCodingWorkbenchMemory persists all in-memory sticky coding
// sessions and any debounced pending writes. Wired into App.shutdown.
func (h *IMMessageHandler) FlushAllStickyCodingWorkbenchMemory() {
	if h == nil {
		// Still flush global debounce map (pending snapshots).
		flushAllStickyCodingDebouncedPersist()
		return
	}
	// First flush debounce queue (may have newer snapshots than handler map
	// if a timer already coalesced).
	flushAllStickyCodingDebouncedPersist()
	h.stickyCodingWorkbenchMemory.Range(func(key, value interface{}) bool {
		userID, _ := key.(string)
		mem, _ := value.(stickyCodingWorkbenchMemory)
		if userID == "" {
			return true
		}
		withStickyCodingUserLock(userID, func() {
			// Re-load in case concurrent update advanced state.
			if raw, ok := h.stickyCodingWorkbenchMemory.Load(userID); ok {
				if m, ok := raw.(stickyCodingWorkbenchMemory); ok {
					mem = m
				}
			}
			persistStickyCodingWorkbenchMemoryToDisk(userID, mem)
		})
		return true
	})
}

func stickyRememberSessionPlan(mem *stickyCodingWorkbenchMemory, userText string) {
	if mem == nil {
		return
	}
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return
	}
	// Keep the original overall goal; follow-up turns only refresh LastUserText.
	if strings.TrimSpace(mem.SessionPlan) == "" {
		mem.SessionPlan = truncateRunesForSubAgent(userText, 800)
	}
}

func (h *IMMessageHandler) recordStickyLocalCodingTurn(userID, projectPath, userText string, result *CodingSubAgentResult) {
	if h == nil || result == nil {
		return
	}
	// Persist: flushes mem-only step status updates from the turn.
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.Kind = "local"
		mem.ProjectPath = strings.TrimSpace(projectPath)
		mem.LastUserText = strings.TrimSpace(userText)
		stickyRememberSessionPlan(mem, userText)
		mem.TurnCount++
		summary := strings.TrimSpace(result.Summary)
		if summary == "" {
			summary = strings.TrimSpace(result.Error)
		}
		mem.LastSummary = summary
		mem.FilesModified = uniqueSortedSubAgentStrings(append(mem.FilesModified, result.FilesModified...))
		mem.FilesCreated = uniqueSortedSubAgentStrings(append(mem.FilesCreated, result.FilesCreated...))
		if len(mem.FilesModified) > 40 {
			mem.FilesModified = mem.FilesModified[len(mem.FilesModified)-40:]
		}
		if len(mem.FilesCreated) > 40 {
			mem.FilesCreated = mem.FilesCreated[len(mem.FilesCreated)-40:]
		}
	})
}

func (h *IMMessageHandler) recordStickyRemoteCodingTurn(userID, userText string, result *RemoteCodingSubAgentResult) {
	if h == nil || result == nil {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.Kind = "remote"
		mem.LastUserText = strings.TrimSpace(userText)
		stickyRememberSessionPlan(mem, userText)
		mem.TurnCount++
		summary := strings.TrimSpace(result.Summary)
		if summary == "" {
			summary = strings.TrimSpace(result.Error)
		}
		if summary == "" {
			summary = fmt.Sprintf("status=%s iterations=%d", result.Status, result.Iterations)
		}
		mem.LastSummary = summary
		if len(result.FilesModified) > 0 {
			mem.FilesModified = uniqueSortedSubAgentStrings(append(mem.FilesModified, result.FilesModified...))
		}
		if len(result.FilesCreated) > 0 {
			mem.FilesCreated = uniqueSortedSubAgentStrings(append(mem.FilesCreated, result.FilesCreated...))
		}
		if len(mem.FilesModified) > 40 {
			mem.FilesModified = mem.FilesModified[len(mem.FilesModified)-40:]
		}
		if len(mem.FilesCreated) > 40 {
			mem.FilesCreated = mem.FilesCreated[len(mem.FilesCreated)-40:]
		}
	})
}

// markStickyCodingSessionFullAccess enables session-scoped path full-access for
// a pure coding workbench (create-task local/remote). Safe to call repeatedly.
// Does not enable high-risk bash auto-allow.
// Dialog grants escalate out of explicit "request" so multi-turn applySticky
// honors the user's allow-full choice (request menu alone is not sticky trust).
func (h *IMMessageHandler) markStickyCodingSessionFullAccess(userID, kind, projectPath string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if kind != "" {
			mem.Kind = kind
		}
		if p := strings.TrimSpace(projectPath); p != "" {
			mem.ProjectPath = p
		}
		mem.SessionFullAccess = true
		// Path trust from a dialog is stronger than menu "请求授权".
		mode := strings.ToLower(strings.TrimSpace(mem.SessionPermissionMode))
		if stickyCodingPermissionIsRequest(mode) || mode == "" {
			if mem.SessionHighRiskAccess {
				mem.SessionPermissionMode = "full"
			} else {
				mem.SessionPermissionMode = "workspace"
			}
		} else if mem.SessionHighRiskAccess && mode != "full" {
			mem.SessionPermissionMode = "full"
		}
	})
}

// markStickyCodingSessionHighRiskAccess enables session-scoped high-risk bash
// auto-allow (create-task pure coding). Independent of path full-access.
// When path trust is already present, escalates SessionPermissionMode to full
// (including from explicit request after a prior path allow-full dialog).
func (h *IMMessageHandler) markStickyCodingSessionHighRiskAccess(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SessionHighRiskAccess = true
		if mem.SessionFullAccess {
			mode := strings.ToLower(strings.TrimSpace(mem.SessionPermissionMode))
			if mode != "full" {
				mem.SessionPermissionMode = "full"
			}
		}
	})
}

// setStickyCodingSessionPermissionMode records the user's input-box choice and
// applies sticky path/high-risk flags in one disk write.
// mode: "request" | "workspace" | "full"
func (h *IMMessageHandler) setStickyCodingSessionPermissionMode(userID, mode, kind, projectPath string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "request", "ask":
		mode = "request"
	case "workspace":
		mode = "workspace"
	case "full":
		mode = "full"
	default:
		return
	}
	wantPath := mode == "full" || mode == "workspace"
	wantRisk := mode == "full"
	nextProject := strings.TrimSpace(projectPath)
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		// Empty projectPath means "keep sticky ProjectPath" (permission UI must
		// not replace the execution workspace path with the task folder path).
		kindSame := kind == "" || mem.Kind == kind
		projectSame := nextProject == "" || strings.TrimSpace(mem.ProjectPath) == nextProject
		if mem.SessionPermissionMode == mode &&
			mem.SessionFullAccess == wantPath &&
			mem.SessionHighRiskAccess == wantRisk &&
			kindSame && projectSame {
			return
		}
		if kind != "" {
			mem.Kind = kind
		}
		if nextProject != "" {
			mem.ProjectPath = nextProject
		}
		mem.SessionPermissionMode = mode
		mem.SessionFullAccess = wantPath
		mem.SessionHighRiskAccess = wantRisk
	})
}

// clearStickyCodingSessionFullAccess drops session path full-access and
// session high-risk auto-allow (request-authorization posture).
func (h *IMMessageHandler) clearStickyCodingSessionFullAccess(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if !mem.SessionFullAccess && !mem.SessionHighRiskAccess && mem.SessionPermissionMode == "" {
			return
		}
		mem.SessionFullAccess = false
		mem.SessionHighRiskAccess = false
		mem.SessionPermissionMode = "request"
		// Keep ApprovedDirs so explicit allow_dir grants remain.
	})
}

// setStickyCodingSessionPlan updates the durable multi-turn session plan/goal.
func (h *IMMessageHandler) setStickyCodingSessionPlan(userID, plan string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SessionPlan = truncateRunesForSubAgent(strings.TrimSpace(plan), 800)
	})
}

// rememberStickyApprovedDir stores an allow_dir grant so multi-turn re-arms keep it.
func (h *IMMessageHandler) rememberStickyApprovedDir(userID, dir string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	dir = strings.TrimSpace(dir)
	if userID == "" || dir == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ApprovedDirs = uniqueSortedSubAgentStrings(append(mem.ApprovedDirs, dir))
		if len(mem.ApprovedDirs) > 32 {
			mem.ApprovedDirs = mem.ApprovedDirs[len(mem.ApprovedDirs)-32:]
		}
	})
}

// applyStickyCodingPermissions overlays session path trust, high-risk trust,
// and prior allow_dir grants onto a CodingSubAgent scope state.
func (h *IMMessageHandler) applyStickyCodingPermissions(userID string, sa *CodingSubAgent) {
	if h == nil || sa == nil || sa.scopeApproval == nil {
		return
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	// Explicit "请求授权" ignores path-full sticky flags (menu posture).
	// High-risk dialog grants still stick so the user is not re-prompted for the
	// same class of bash after an allow-full high-risk decision.
	if stickyCodingPermissionIsRequest(mem.SessionPermissionMode) {
		for _, dir := range mem.ApprovedDirs {
			if strings.TrimSpace(dir) != "" {
				sa.scopeApproval.approveDir(dir)
			}
		}
		if mem.SessionHighRiskAccess {
			sa.scopeApproval.grantHighRiskFullAccess()
		}
		return
	}
	// Path trust only — does not auto-allow high-risk bash.
	if mem.SessionFullAccess {
		sa.scopeApproval.grantFullAccess()
	}
	if mem.SessionHighRiskAccess {
		sa.scopeApproval.grantHighRiskFullAccess()
	}
	for _, dir := range mem.ApprovedDirs {
		if strings.TrimSpace(dir) != "" {
			sa.scopeApproval.approveDir(dir)
		}
	}
}

// bindStickyRemoteCodingContext stores remote SSH coordinates for reopen/re-arm.
// host/user/port are optional non-secret reconnect metadata (password never stored).
func (h *IMMessageHandler) bindStickyRemoteCodingContext(userID string, remoteCtx remoteCodingTemplateContext, host, user string, port int) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	sessionID := strings.TrimSpace(remoteCtx.SessionID)
	if userID == "" || sessionID == "" {
		return
	}
	workDir := strings.TrimSpace(remoteCtx.WorkDir)
	projectDir := strings.TrimSpace(remoteCtx.ProjectDir)
	if projectDir == "" {
		projectDir = workDir
	}
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	if port <= 0 || port >= 65536 {
		port = 0
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		// Skip mutation when coordinates are already sticky (caller still writes once).
		if mem.Kind == "remote" &&
			mem.RemoteSessionID == sessionID &&
			mem.RemoteWorkDir == workDir &&
			mem.RemoteProjectDir == projectDir &&
			(host == "" || mem.RemoteHost == host) &&
			(user == "" || mem.RemoteUser == user) &&
			(port == 0 || mem.RemotePort == port) {
			return
		}
		mem.Kind = "remote"
		mem.RemoteSessionID = sessionID
		mem.RemoteWorkDir = workDir
		mem.RemoteProjectDir = projectDir
		if host != "" {
			mem.RemoteHost = host
		}
		if user != "" {
			mem.RemoteUser = user
		}
		if port > 0 {
			mem.RemotePort = port
		}
	})
}

// codingWorkbenchPermissionMode returns the effective pure-coding permission tier.
// "full" = global full access and/or session path+high-risk trust;
// "workspace" = session path trust only (high-risk still prompts);
// "request" = seed project+parent only / interactive.
func (h *IMMessageHandler) codingWorkbenchPermissionMode(userID string, globalFullAccess bool) string {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	// Explicit "request" wins over global so the user can force prompts on a pure
	// coding tab even when global full-access is enabled elsewhere.
	mode := strings.ToLower(strings.TrimSpace(mem.SessionPermissionMode))
	if mode == "request" || mode == "ask" {
		return "request"
	}
	if globalFullAccess || mode == "full" {
		return "full"
	}
	if mode == "workspace" {
		return "workspace"
	}
	// Legacy sticky flags without explicit mode.
	if mem.SessionHighRiskAccess && mem.SessionFullAccess {
		return "full"
	}
	if mem.SessionFullAccess {
		return "workspace"
	}
	return "request"
}

// stickyCodingEffectiveFullAccess is true when pure-coding should skip all
// path and high-risk prompts (input-box "完全控制" / full mode).
func (h *IMMessageHandler) stickyCodingEffectiveFullAccess(userID string, globalFullAccess bool) bool {
	return h.codingWorkbenchPermissionMode(userID, globalFullAccess) == "full"
}

func stickyCodingPermissionIsRequest(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode == "request" || mode == "ask"
}

// applyStickyRemoteCodingPermissions overlays sticky path trust, high-risk
// trust, and allow_dir grants onto a remote approval state.
func (h *IMMessageHandler) applyStickyRemoteCodingPermissions(userID string, state *remoteHighRiskApprovalState) {
	if h == nil || state == nil {
		return
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if stickyCodingPermissionIsRequest(mem.SessionPermissionMode) {
		for _, dir := range mem.ApprovedDirs {
			if strings.TrimSpace(dir) != "" {
				state.approveDir(dir)
			}
		}
		if mem.SessionHighRiskAccess {
			state.grantHighRiskFullAccess()
		}
		return
	}
	if mem.SessionFullAccess {
		state.grantPathFullAccess()
	}
	if mem.SessionHighRiskAccess {
		state.grantHighRiskFullAccess()
	}
	for _, dir := range mem.ApprovedDirs {
		if strings.TrimSpace(dir) != "" {
			state.approveDir(dir)
		}
	}
}

// maybeUpgradeStickyPermissionModeToFull promotes SessionPermissionMode to
// "full" when both path and high-risk session grants are present (e.g. after
// sequential allow-full dialogs). Dialog grants escalate even from explicit
// "request" so the input-box mode matches multi-turn sticky enforcement.
func (h *IMMessageHandler) maybeUpgradeStickyPermissionModeToFull(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if !mem.SessionFullAccess || !mem.SessionHighRiskAccess {
			return
		}
		if strings.ToLower(strings.TrimSpace(mem.SessionPermissionMode)) == "full" {
			return
		}
		mem.SessionPermissionMode = "full"
	})
}

// syncStickyCodingFullAccessFromGlobal copies global full-access into sticky
// path+high-risk flags so pure coding runtimes and UI stay aligned.
// Does not override an explicit session "request" choice.
func (h *IMMessageHandler) syncStickyCodingFullAccessFromGlobal(userID, kind, projectPath string, globalFullAccess bool) {
	if h == nil || !globalFullAccess {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	mode := strings.ToLower(strings.TrimSpace(mem.SessionPermissionMode))
	if mode == "request" || mode == "ask" {
		return
	}
	// Skip disk write when sticky already reflects full control.
	needPath := !mem.SessionFullAccess
	needRisk := !mem.SessionHighRiskAccess
	needMode := mode != "full"
	needKind := kind != "" && mem.Kind != kind
	needProject := projectPath != "" && strings.TrimSpace(mem.ProjectPath) == ""
	if !needPath && !needRisk && !needMode && !needKind && !needProject {
		return
	}
	h.setStickyCodingSessionPermissionMode(userID, "full", kind, projectPath)
}

// ensureLoopCtxUserID binds the pure-coding session owner onto loopCtx so
// sticky permission lookups (full/workspace/request) resolve correctly.
func ensureLoopCtxUserID(loopCtx *LoopContext, userID string) {
	if loopCtx == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	cur := strings.TrimSpace(loopCtx.UserID)
	if cur == "" {
		loopCtx.UserID = userID
		return
	}
	// Upgrade bare desktop-user → desktop-user:<taskPath> so code-preview
	// routing (projectPathFromSessionOwnerID) can resolve the managed tab path.
	// Never downgrade or overwrite a different project owner.
	if cur == desktopUserID && userID != desktopUserID && strings.HasPrefix(userID, desktopUserID+":") {
		loopCtx.UserID = userID
	}
}

// isPureCodingWorkbenchSession reports whether this owner has an armed or
// sticky pure coding (local/remote) workbench session.
func (h *IMMessageHandler) isPureCodingWorkbenchSession(userID string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	if h.hasPendingTemplateSubAgentExecution(userID) {
		return true
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	kind := strings.TrimSpace(mem.Kind)
	return kind == "local" || kind == "remote"
}

// syncCodingWorkbenchSessionPlanFromGoal mirrors a /goal objective into the
// pure-coding SessionPlan so the multi-turn banner stays aligned.
func (h *IMMessageHandler) syncCodingWorkbenchSessionPlanFromGoal(userID, objective string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	objective = strings.TrimSpace(objective)
	if userID == "" || objective == "" {
		return
	}
	if !h.isPureCodingWorkbenchSession(userID) {
		return
	}
	h.setStickyCodingSessionPlan(userID, objective)
}

// ensurePureCodingArmedForGoalContinuation re-arms sticky local/remote pending
// routing when sticky memory still classifies the session as pure coding but
// in-memory pending flags were dropped (restart, cold path, race). Remote with
// a dead SSH session is left unarmed (NeedsReconnect path still applies).
func (h *IMMessageHandler) ensurePureCodingArmedForGoalContinuation(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || h.hasPendingTemplateSubAgentExecution(userID) {
		return
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	switch strings.TrimSpace(mem.Kind) {
	case "local":
		execDir := strings.TrimSpace(mem.ProjectPath)
		if execDir == "" {
			if p := projectPathFromSessionOwnerID(userID); p != "" {
				execDir = p
			}
		}
		if execDir != "" {
			h.rearmStickyLocalCodingEnvironment(userID, execDir)
		}
	case "remote":
		sessionID := strings.TrimSpace(mem.RemoteSessionID)
		workDir := strings.TrimSpace(mem.RemoteWorkDir)
		projectDir := strings.TrimSpace(mem.RemoteProjectDir)
		if workDir == "" {
			workDir = projectDir
		}
		if projectDir == "" {
			projectDir = workDir
		}
		if sessionID == "" || workDir == "" {
			return
		}
		// Only re-arm when SSH session is still alive.
		if h.ensureSSHManager() != nil && !h.sshSessionAlive(sessionID) {
			return
		}
		h.rearmStickyRemoteCodingEnvironment(userID, remoteCodingTemplateContext{
			SessionID:  sessionID,
			WorkDir:    workDir,
			ProjectDir: projectDir,
		})
	}
}
