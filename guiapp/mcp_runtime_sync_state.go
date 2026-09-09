package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// MCPRuntimeSyncState is the durable, non-secret readiness state for a
// marketplace MCP runtime. It is deliberately separate from AppConfig:
// configuration remains the committed business result while this state keeps
// execution/inventory fail-closed until initialize + tools/list succeeds.
type MCPRuntimeSyncState struct {
	ServerID    string `json:"server_id"`
	Transport   string `json:"transport,omitempty"`
	Status      string `json:"status"` // pending | ready | needs_review
	Attempts    int    `json:"attempts"`
	NextRetryAt string `json:"next_retry_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

const mcpRuntimeSyncMaxAttempts = 3

var mcpRuntimeSyncStateMu sync.Mutex

// mcpRuntimeSyncPersistenceFailures closes the window where a runtime probe
// fails but the durable state update cannot be written (for example a full or
// temporarily unavailable filesystem). Without this process-local fence, a
// stale on-disk "ready" marker could continue admitting execution until the
// next successful write. Keys include the state-file path so isolated tests or
// multiple configured data roots cannot inherit another instance's blocker.
var mcpRuntimeSyncPersistenceFailures = map[string]bool{}

func mcpRuntimeSyncPersistenceKey(serverID string) string {
	return mcpRuntimeSyncStatePath() + "|" + strings.TrimSpace(serverID)
}

func mcpRuntimeSyncStatePath() string {
	return filepath.Join(corelib.MaclawBaseDir(), "skill_evolution", "mcp_runtime_sync.json")
}

func readMCPRuntimeSyncStates() (map[string]MCPRuntimeSyncState, error) {
	path := mcpRuntimeSyncStatePath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]MCPRuntimeSyncState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var states map[string]MCPRuntimeSyncState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, fmt.Errorf("decode MCP runtime sync state: %w", err)
	}
	if states == nil {
		states = map[string]MCPRuntimeSyncState{}
	}
	for id, state := range states {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(state.ServerID) == "" || state.ServerID != id {
			return nil, fmt.Errorf("invalid MCP runtime sync state identity")
		}
		if state.Status != "pending" && state.Status != "ready" && state.Status != "needs_review" {
			return nil, fmt.Errorf("unsupported MCP runtime sync status %q", state.Status)
		}
		if state.Attempts < 0 || state.Attempts > mcpRuntimeSyncMaxAttempts {
			return nil, fmt.Errorf("invalid MCP runtime sync attempts for %s", id)
		}
		if strings.TrimSpace(state.NextRetryAt) != "" {
			if _, err := time.Parse(time.RFC3339, state.NextRetryAt); err != nil {
				return nil, fmt.Errorf("invalid MCP runtime sync retry time for %s", id)
			}
		}
		if strings.TrimSpace(state.UpdatedAt) != "" {
			if _, err := time.Parse(time.RFC3339, state.UpdatedAt); err != nil {
				return nil, fmt.Errorf("invalid MCP runtime sync update time for %s", id)
			}
		}
	}
	return states, nil
}

func writeMCPRuntimeSyncStates(states map[string]MCPRuntimeSyncState) error {
	path := mcpRuntimeSyncStatePath()
	if len(states) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return configfile.AtomicWriteJSON(path, states)
}

func (a *App) mcpRuntimeSyncPending(serverID string) bool {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return true
	}
	mcpRuntimeSyncStateMu.Lock()
	defer mcpRuntimeSyncStateMu.Unlock()
	if mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(serverID)] {
		return true
	}
	states, err := readMCPRuntimeSyncStates()
	if err != nil {
		return true
	}
	state, ok := states[serverID]
	if !ok {
		// A managed MCP config may predate the durable state file, or the
		// process may have crashed immediately after committing config and
		// before recording the first probe. Treat that window as pending rather
		// than allowing execution based on configuration alone.
		return a.mcpRuntimeSyncRequired(serverID)
	}
	if state.Status == "ready" {
		return false
	}
	if state.Status == "needs_review" {
		return true
	}
	if state.NextRetryAt != "" {
		if next, err := time.Parse(time.RFC3339, state.NextRetryAt); err == nil && time.Now().Before(next) {
			return true
		}
	}
	return true
}

// mcpRuntimeSyncShouldRetry reports whether a previously failed runtime probe
// is due for another bounded attempt. needs_review is terminal until an
// operator changes the configuration; it must not be retried by an install
// no-op or a background marketplace poll.
func (a *App) mcpRuntimeSyncShouldRetry(serverID string) bool {
	mcpRuntimeSyncStateMu.Lock()
	persistenceFailed := mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(serverID)]
	mcpRuntimeSyncStateMu.Unlock()
	if persistenceFailed {
		return false
	}
	state, ok := loadMCPRuntimeSyncState(serverID)
	if !ok {
		// Missing state for a managed deployment is a recoverable first-probe
		// condition; allow the bounded install reconciliation to establish it.
		return a.mcpRuntimeSyncRequired(serverID)
	}
	if state.Status != "pending" {
		return false
	}
	if strings.TrimSpace(state.NextRetryAt) == "" {
		return true
	}
	next, err := time.Parse(time.RFC3339, state.NextRetryAt)
	return err != nil || !time.Now().Before(next)
}

// mcpRuntimeSyncAllowsAutomaticStart gates startup/background process
// creation for marketplace-managed local MCP servers. A durable pending state
// with a future retry time and a terminal needs_review state must not be
// bypassed by a generic AutoStart sync; only a new configuration revision or
// an explicit operator action may re-arm those states. Missing state and a
// ready marker remain startable so a normal application restart can recreate a
// previously-proven process.
func (a *App) mcpRuntimeSyncAllowsAutomaticStart(entry corelib.LocalMCPServerEntry) bool {
	if a == nil || entry.Source != corelib.MCPSourceMarket || entry.Capability == nil {
		return true
	}
	mcpRuntimeSyncStateMu.Lock()
	persistenceFailed := mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(entry.ID)]
	mcpRuntimeSyncStateMu.Unlock()
	if persistenceFailed {
		return false
	}
	state, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || state.Status == "ready" {
		return true
	}
	if state.Status == "needs_review" {
		return false
	}
	if state.Status == "pending" && strings.TrimSpace(state.NextRetryAt) != "" {
		next, err := time.Parse(time.RFC3339, state.NextRetryAt)
		if err == nil && time.Now().Before(next) {
			return false
		}
	}
	return true
}

// mcpRuntimeSyncAllowsAutomaticStartForManager keeps LocalMCPManager's
// low-level sync independent from App ownership while still applying the
// durable startup gate whenever a registry is attached to an App.
func mcpRuntimeSyncAllowsAutomaticStartForManager(m *LocalMCPManager, entry corelib.LocalMCPServerEntry) bool {
	if m == nil || m.registry == nil || m.registry.app == nil {
		return true
	}
	return m.registry.app.mcpRuntimeSyncAllowsAutomaticStart(entry)
}

func (a *App) mcpRuntimeSyncRequired(serverID string) bool {
	if a == nil || strings.TrimSpace(serverID) == "" {
		return false
	}
	if a.mcpRegistry == nil {
		return false
	}
	// Read configuration directly instead of acquiring MCPRegistry's mutex.
	// This helper is called while ListServers/Unregister may already hold an
	// RLock; nested RWMutex reads can deadlock when a writer is queued.
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	for _, entry := range cfg.MCPServers {
		if entry.ID == serverID {
			return entry.Source == corelib.MCPSourceMarket && entry.Capability != nil
		}
	}
	for _, entry := range cfg.LocalMCPServers {
		if entry.ID == serverID {
			return entry.Source == corelib.MCPSourceMarket && entry.Capability != nil
		}
	}
	return false
}

func loadMCPRuntimeSyncState(serverID string) (MCPRuntimeSyncState, bool) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return MCPRuntimeSyncState{}, false
	}
	mcpRuntimeSyncStateMu.Lock()
	defer mcpRuntimeSyncStateMu.Unlock()
	states, err := readMCPRuntimeSyncStates()
	if err != nil {
		// Do not expose filesystem paths or parser details through the Wails
		// view. The admission decision remains fail-closed, while the durable
		// state file itself is the operator-facing diagnostic artifact.
		return MCPRuntimeSyncState{ServerID: serverID, Status: "needs_review", LastError: "runtime synchronization state unavailable"}, true
	}
	state, ok := states[serverID]
	return state, ok
}

func (a *App) recordMCPRuntimeSyncFailure(serverID, transport, requestID string, cause error) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return fmt.Errorf("MCP runtime sync state requires server_id")
	}
	mcpRuntimeSyncStateMu.Lock()
	defer mcpRuntimeSyncStateMu.Unlock()
	states, err := readMCPRuntimeSyncStates()
	if err != nil {
		return err
	}
	state := states[serverID]
	state.ServerID = serverID
	if state.Status == "needs_review" && state.Attempts >= mcpRuntimeSyncMaxAttempts {
		// Terminal review state is idempotent. Repeated background probes must
		// not keep incrementing attempts or turn a valid terminal record into an
		// invalid state that cannot be persisted.
		return nil
	}
	state.Transport = strings.TrimSpace(transport)
	state.RequestID = strings.TrimSpace(requestID)
	state.Status = "pending"
	state.Attempts++
	if cause != nil {
		state.LastError = summarizeMCPRuntimeSyncError(cause)
	}
	if state.Attempts >= mcpRuntimeSyncMaxAttempts {
		state.Status = "needs_review"
		state.NextRetryAt = ""
	} else {
		state.NextRetryAt = time.Now().Add(time.Duration(state.Attempts) * time.Minute).UTC().Format(time.RFC3339)
	}
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	states[serverID] = state
	if err := writeMCPRuntimeSyncStates(states); err != nil {
		mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(serverID)] = true
		return err
	}
	delete(mcpRuntimeSyncPersistenceFailures, mcpRuntimeSyncPersistenceKey(serverID))
	return nil
}

// resetMCPRuntimeSyncPending starts a fresh checked-readiness cycle for a new
// configuration revision. It intentionally preserves a blocker instead of
// deleting the old state: callers must not execute against a newly committed
// config until initialize/process start and tool discovery have succeeded.
func (a *App) resetMCPRuntimeSyncPending(serverID, transport string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return fmt.Errorf("MCP runtime sync state requires server_id")
	}
	mcpRuntimeSyncStateMu.Lock()
	defer mcpRuntimeSyncStateMu.Unlock()
	states, err := readMCPRuntimeSyncStates()
	if err != nil {
		return err
	}
	state, exists := states[serverID]
	if !exists {
		state = MCPRuntimeSyncState{ServerID: serverID}
	}
	state.ServerID = serverID
	state.Transport = strings.TrimSpace(transport)
	state.Status = "pending"
	state.Attempts = 0
	state.NextRetryAt = ""
	state.LastError = ""
	// A configuration revision starts a new readiness attempt. Retaining the
	// previous probe's request ID would make subsequent audit/runtime events
	// appear correlated with a stale configuration and complicate recovery
	// diagnostics.
	state.RequestID = ""
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	states[serverID] = state
	if err := writeMCPRuntimeSyncStates(states); err != nil {
		mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(serverID)] = true
		return err
	}
	delete(mcpRuntimeSyncPersistenceFailures, mcpRuntimeSyncPersistenceKey(serverID))
	return nil
}

// summarizeMCPRuntimeSyncError intentionally discards raw transport text.
// HTTP errors can contain endpoint fragments or provider response bodies, and
// those values must never become durable operator/UI state. Keep only a small
// stable classification useful for retry diagnosis.
func summarizeMCPRuntimeSyncError(err error) string {
	if err == nil {
		return "runtime synchronization failed"
	}
	if errors.Is(err, context.Canceled) {
		return "runtime synchronization canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "runtime synchronization timed out"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "initialize"):
		return "runtime initialize failed"
	case strings.Contains(text, "tools/list"), strings.Contains(text, "discover tools"):
		return "runtime tool discovery failed"
	case strings.Contains(text, "start local mcp"), strings.Contains(text, "process"):
		return "local MCP process startup failed"
	case strings.Contains(text, "http 401"), strings.Contains(text, "http 403"), strings.Contains(text, "unauthorized"), strings.Contains(text, "forbidden"):
		return "runtime authorization failed"
	case strings.Contains(text, "http "):
		return "runtime HTTP request failed"
	default:
		return "runtime synchronization failed"
	}
}

func (a *App) markMCPRuntimeSyncReady(serverID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}
	mcpRuntimeSyncStateMu.Lock()
	defer mcpRuntimeSyncStateMu.Unlock()
	states, err := readMCPRuntimeSyncStates()
	if err != nil {
		return err
	}
	if _, exists := states[serverID]; !exists {
		// A managed server can be discovered successfully on its first startup
		// after this state file was introduced. Persist a non-secret ready
		// marker so the missing-state fail-closed rule does not become a
		// permanent blocker. Unmanaged entries keep the zero-side-effect path.
		if !a.mcpRuntimeSyncRequired(serverID) {
			delete(mcpRuntimeSyncPersistenceFailures, mcpRuntimeSyncPersistenceKey(serverID))
			return nil
		}
		states[serverID] = MCPRuntimeSyncState{
			ServerID:  serverID,
			Status:    "ready",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := writeMCPRuntimeSyncStates(states); err != nil {
			mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(serverID)] = true
			return err
		}
		delete(mcpRuntimeSyncPersistenceFailures, mcpRuntimeSyncPersistenceKey(serverID))
		return nil
	}
	if a.mcpRuntimeSyncRequired(serverID) {
		// Keep an explicit ready marker for managed entries. Missing state is
		// intentionally fail-closed, so deleting a successful observation would
		// make every subsequent call look unproven after restart.
		state := states[serverID]
		state.ServerID = serverID
		state.Status = "ready"
		state.Attempts = 0
		state.NextRetryAt = ""
		state.LastError = ""
		state.RequestID = ""
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		states[serverID] = state
	} else {
		delete(states, serverID)
	}
	if err := writeMCPRuntimeSyncStates(states); err != nil {
		mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(serverID)] = true
		return err
	}
	delete(mcpRuntimeSyncPersistenceFailures, mcpRuntimeSyncPersistenceKey(serverID))
	return nil
}

// clearMCPRuntimeSyncState removes state for a deleted server without
// consulting the registry. Callers may hold the registry lock while deleting
// the config entry, so this helper must remain independent of registry locks.
func clearMCPRuntimeSyncState(serverID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}
	mcpRuntimeSyncStateMu.Lock()
	defer mcpRuntimeSyncStateMu.Unlock()
	states, err := readMCPRuntimeSyncStates()
	if err != nil {
		return err
	}
	if _, exists := states[serverID]; !exists {
		// The durable file may already have been removed by an earlier cleanup
		// attempt. Clear the process-local fence as part of the same explicit
		// unregister/managed-to-manual transition so a reused ID is not blocked
		// forever by stale in-memory state.
		delete(mcpRuntimeSyncPersistenceFailures, mcpRuntimeSyncPersistenceKey(serverID))
		return nil
	}
	delete(states, serverID)
	if err := writeMCPRuntimeSyncStates(states); err != nil {
		mcpRuntimeSyncPersistenceFailures[mcpRuntimeSyncPersistenceKey(serverID)] = true
		return err
	}
	delete(mcpRuntimeSyncPersistenceFailures, mcpRuntimeSyncPersistenceKey(serverID))
	return nil
}

// reconcileManagedMCPRuntimeSyncOnStartup retries due managed runtimes after
// a process restart. Configuration and runtime readiness are intentionally
// separate state axes: a committed marketplace MCP must remain blocked until
// initialize/tools-list (or local process startup/discovery) succeeds again.
// Each probe is isolated so one unavailable server cannot delay or suppress
// reconciliation of its siblings.
func (a *App) reconcileManagedMCPRuntimeSyncOnStartup() {
	if a == nil {
		return
	}
	a.ensureInteractionInfra()
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	type target struct {
		id        string
		transport string
		local     bool
	}
	var targets []target
	var localIDs []string
	for _, entry := range cfg.MCPServers {
		if entry.Source == corelib.MCPSourceMarket && entry.Capability != nil && a.mcpRuntimeSyncShouldRetry(entry.ID) {
			targets = append(targets, target{id: entry.ID, transport: "http"})
		}
	}
	for _, entry := range cfg.LocalMCPServers {
		if entry.Source == corelib.MCPSourceMarket && entry.Capability != nil && !entry.Disabled && entry.AutoStart && a.mcpRuntimeSyncShouldRetry(entry.ID) {
			localIDs = append(localIDs, entry.ID)
		}
	}
	// One checked local sync reconciles the complete desired set; launching one
	// sync per server would redundantly restart/discover every sibling and race
	// their retry counters.
	if len(localIDs) > 0 {
		targets = append(targets, target{transport: "stdio", local: true})
	}
	for _, item := range targets {
		go func(item target) {
			ctx, cancel := context.WithTimeout(context.Background(), mcpBackgroundProbeTimeout)
			defer cancel()
			// LocalMCPManager records per-entry outcomes internally. Keep a
			// pre-probe snapshot so that, if the manager exits before touching
			// an entry (for example cancellation during startup), the fallback
			// below can distinguish that from an already-persisted result.
			var localBefore map[string]MCPRuntimeSyncState
			if item.local {
				localBefore = make(map[string]MCPRuntimeSyncState, len(localIDs))
				for _, id := range localIDs {
					if state, ok := loadMCPRuntimeSyncState(id); ok {
						localBefore[id] = state
					}
				}
			}
			var probeErr error
			if item.local {
				a.ensureLocalMCPManager()
				if a.localMCPManager == nil {
					probeErr = fmt.Errorf("local MCP manager unavailable")
				} else {
					probeErr = a.localMCPManager.SyncFromConfigCheckedContext(ctx)
				}
			} else {
				a.ensureInteractionInfra()
				if a.mcpRegistry == nil {
					probeErr = fmt.Errorf("MCP registry unavailable")
				} else {
					probeErr = a.mcpRegistry.HealthCheckStrictContext(ctx, item.id)
				}
			}
			ids := []string{item.id}
			if item.local {
				ids = localIDs
			}
			if probeErr != nil {
				// LocalMCPManager.syncFromConfig records readiness/failure per
				// entry while it starts and discovers each process. Do not
				// blindly record the same error for the whole batch here: doing
				// so would double-increment a failed entry and incorrectly consume
				// the retry budget of healthy siblings. Only fill in a state for
				// entries that were not reached (for example cancellation before
				// the manager began) or when the manager itself was unavailable.
				for _, id := range ids {
					if item.local {
						if after, exists := loadMCPRuntimeSyncState(id); exists {
							if before, hadBefore := localBefore[id]; !hadBefore || after != before {
								// Manager persisted this entry's outcome; avoid a
								// duplicate attempt increment in the coordinator.
								continue
							}
						}
					}
					if stateErr := a.recordMCPRuntimeSyncFailure(id, item.transport, "", probeErr); stateErr != nil {
						log.Printf("[MCPRuntime] startup reconciliation failed for %s: %v (state: %v)", id, probeErr, stateErr)
					}
				}
				return
			}
			if item.local {
				a.finalizeLocalMCPRuntimeSync(a.localMCPManager, ids)
				return
			}
			for _, id := range ids {
				if stateErr := a.markMCPRuntimeSyncReady(id); stateErr != nil {
					log.Printf("[MCPRuntime] startup readiness state update failed for %s: %v", id, stateErr)
				}
			}
		}(item)
	}
}

// finalizeLocalMCPRuntimeSync converts a successful checked local sync into
// durable readiness only for entries that are actually running. The manager
// may legitimately skip an entry when a newer blocker was written after the
// startup target list was built; blindly marking every target ready would
// erase that blocker and re-open execution against an unverified process.
func (a *App) finalizeLocalMCPRuntimeSync(manager *LocalMCPManager, ids []string) {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if state, exists := loadMCPRuntimeSyncState(id); exists && (state.Status == "needs_review" || state.Status == "ready") {
			continue
		}
		if manager == nil || !manager.IsRunning(id) {
			// Do not consume a retry budget that is currently in backoff or
			// terminal review. A due pending entry with no running process is
			// a real failed probe and gets one bounded attempt recorded.
			if !a.mcpRuntimeSyncShouldRetry(id) {
				continue
			}
			if err := a.recordMCPRuntimeSyncFailure(id, "stdio", "", fmt.Errorf("local MCP runtime %s is not running after checked sync", id)); err != nil {
				log.Printf("[MCPRuntime] startup readiness failure for %s: %v", id, err)
			}
			continue
		}
		if stateErr := a.markMCPRuntimeSyncReady(id); stateErr != nil {
			log.Printf("[MCPRuntime] startup readiness state update failed for %s: %v", id, stateErr)
		}
	}
}
