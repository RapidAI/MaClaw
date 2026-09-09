package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// LocalMCPManager manages the lifecycle of all local (stdio) MCP server
// processes. It starts/stops clients based on the config and provides
// tool discovery and invocation for the agent pipeline.
type LocalMCPManager struct {
	registry     *MCPRegistry
	syncMu       sync.Mutex
	mu           sync.RWMutex
	clients      map[string]*LocalMCPClient            // shared clients keyed by server ID
	ownerClients map[string]map[string]*LocalMCPClient // server ID -> owner ID -> dedicated client
	startLocks   map[string]*sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewLocalMCPManager creates a new manager.
func NewLocalMCPManager(registry *MCPRegistry) *LocalMCPManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &LocalMCPManager{
		registry:     registry,
		clients:      make(map[string]*LocalMCPClient),
		ownerClients: make(map[string]map[string]*LocalMCPClient),
		startLocks:   make(map[string]*sync.Mutex),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// SyncFromConfig reads the local MCP server config and starts/stops
// clients as needed. Enabled servers are started whenever a sync happens.
// App startup decides whether to trigger the initial sync based on AutoStart.
func (m *LocalMCPManager) SyncFromConfig() {
	if err := m.syncFromConfig(context.Background()); err != nil {
		log.Printf("[LocalMCP] config sync completed with errors: %v", err)
	}
}

// SyncFromConfigChecked is the observable synchronization boundary used by
// transactional callers. The historical SyncFromConfig API remains
// best-effort for startup/background callers, while managed capability
// installation can now distinguish a committed config from a runtime that
// failed to start or discover its tools.
func (m *LocalMCPManager) SyncFromConfigChecked() error {
	if m == nil || m.registry == nil {
		return fmt.Errorf("local MCP manager is not initialized")
	}
	return m.syncFromConfig(context.Background())
}

// SyncFromConfigCheckedContext is the cancellation-aware checked boundary
// used by a foreground managed-capability transaction. It does not change
// the manager lifetime context: cancellation stops this synchronization and
// any process it is currently starting, while a later sync may retry normally.
func (m *LocalMCPManager) SyncFromConfigCheckedContext(ctx context.Context) error {
	if m == nil || m.registry == nil {
		return fmt.Errorf("local MCP manager is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return m.syncFromConfig(ctx)
}

func (m *LocalMCPManager) syncFromConfig(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	runCtx, cancel := context.WithCancel(m.ctx)
	defer cancel()
	stopParentWatch := make(chan struct{})
	defer close(stopParentWatch)
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-stopParentWatch:
		case <-runCtx.Done():
		}
	}()
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	var syncErrors []error

	// Don't start new processes if the manager is shutting down.
	select {
	case <-runCtx.Done():
		return runCtx.Err()
	default:
	}

	entries := m.registry.ListLocalServers()

	// Build a set of desired server IDs
	desired := make(map[string]corelib.LocalMCPServerEntry, len(entries))
	blocked := make(map[string]bool)
	for _, e := range entries {
		if !e.Disabled {
			desired[e.ID] = e
			// A generic sync must honor the same durable runtime gate as
			// startup AutoStart. Keep the config entry in the desired set so
			// it is not removed, but stop/avoid starting its process while a
			// managed blocker is in backoff or needs_review.
			if !mcpRuntimeSyncAllowsAutomaticStartForManager(m, e) {
				blocked[e.ID] = true
			}
		}
	}

	type clientToStop struct {
		id     string
		client *LocalMCPClient
	}
	var toStop []clientToStop
	var toStart []corelib.LocalMCPServerEntry

	m.mu.Lock()

	// Stop clients that are no longer in config or are disabled
	for id, client := range m.clients {
		entry, ok := desired[id]
		// A managed update can retain the same ID while changing command,
		// arguments, environment, or capability metadata. Reuse is unsafe in
		// that case: the old process would continue serving the new config.
		if !ok || blocked[id] || !reflect.DeepEqual(client.entry, entry) {
			delete(m.clients, id)
			toStop = append(toStop, clientToStop{id: id, client: client})
		}
	}
	for id, byOwner := range m.ownerClients {
		entry, ok := desired[id]
		if ok && !blocked[id] {
			for owner, ownerClient := range byOwner {
				if reflect.DeepEqual(ownerClient.entry, entry) {
					continue
				}
				delete(byOwner, owner)
				toStop = append(toStop, clientToStop{id: id + ":" + owner, client: ownerClient})
			}
			if len(byOwner) == 0 {
				delete(m.ownerClients, id)
			}
			continue
		}
		for owner, ownerClient := range byOwner {
			delete(byOwner, owner)
			toStop = append(toStop, clientToStop{id: id + ":" + owner, client: ownerClient})
		}
		delete(m.ownerClients, id)
	}

	// Also remove clients whose processes have crashed
	for id, client := range m.clients {
		if !client.IsRunning() {
			delete(m.clients, id)
			toStop = append(toStop, clientToStop{id: id, client: client})
		}
	}
	for id, byOwner := range m.ownerClients {
		for owner, client := range byOwner {
			if !client.IsRunning() {
				delete(byOwner, owner)
				toStop = append(toStop, clientToStop{id: id + ":" + owner, client: client})
			}
		}
		if len(byOwner) == 0 {
			delete(m.ownerClients, id)
		}
	}

	// Plan new clients (or restarted crashed ones) while holding the map lock,
	// but do slow process startup and tool discovery after releasing it so
	// active agent tool calls are not blocked by config sync.
	for id, entry := range desired {
		if _, exists := m.clients[id]; exists {
			continue
		}
		if blocked[id] {
			continue
		}
		toStart = append(toStart, entry)
	}
	m.mu.Unlock()

	for _, item := range toStop {
		log.Printf("[LocalMCP] stopping removed/disabled/crashed server %s", item.id)
		item.client.Stop()
	}

	for _, entry := range toStart {
		select {
		case <-runCtx.Done():
			syncErrors = append(syncErrors, runCtx.Err())
			return errors.Join(syncErrors...)
		default:
		}
		client := NewLocalMCPClient(entry)
		if err := client.Start(runCtx); err != nil {
			log.Printf("[LocalMCP] failed to start %s (%s): %v", entry.Name, entry.Command, err)
			startErr := fmt.Errorf("start local MCP %s: %w", entry.ID, err)
			syncErrors = append(syncErrors, startErr)
			if stateErr := m.recordRuntimeSyncFailure(entry.ID, "stdio", startErr); stateErr != nil {
				syncErrors = append(syncErrors, fmt.Errorf("persist runtime sync failure for %s: %w", entry.ID, stateErr))
			}
			continue
		}
		// Discover tools with retry — some servers need a moment after
		// the handshake before tools/list is ready.
		var tools []MCPToolView
		var discoverErr error
		for attempt := 1; attempt <= 3; attempt++ {
			tools, discoverErr = client.DiscoverToolsContext(runCtx)
			if discoverErr == nil {
				break
			}
			log.Printf("[LocalMCP] discover tools for %s attempt %d/3 failed: %v", entry.Name, attempt, discoverErr)
			if attempt < 3 {
				select {
				case <-runCtx.Done():
					discoverErr = runCtx.Err()
				case <-time.After(time.Duration(attempt) * time.Second):
				}
			}
		}
		if discoverErr != nil {
			log.Printf("[LocalMCP] giving up tool discovery for %s: %v", entry.Name, discoverErr)
			client.Stop()
			discoverFailure := fmt.Errorf("discover tools for local MCP %s: %w", entry.ID, discoverErr)
			syncErrors = append(syncErrors, discoverFailure)
			if stateErr := m.recordRuntimeSyncFailure(entry.ID, "stdio", discoverFailure); stateErr != nil {
				syncErrors = append(syncErrors, fmt.Errorf("persist runtime sync failure for %s: %w", entry.ID, stateErr))
			}
			continue
		}
		log.Printf("[LocalMCP] started %s with %d tools", entry.Name, len(tools))
		select {
		case <-runCtx.Done():
			client.Stop()
			syncErrors = append(syncErrors, runCtx.Err())
			return errors.Join(syncErrors...)
		default:
		}
		m.mu.Lock()
		if existing, exists := m.clients[entry.ID]; exists && existing.IsRunning() {
			m.mu.Unlock()
			client.Stop()
			continue
		}
		m.clients[entry.ID] = client
		m.mu.Unlock()
		if stateErr := m.recordRuntimeSyncReady(entry.ID); stateErr != nil {
			syncErrors = append(syncErrors, fmt.Errorf("persist runtime readiness for %s: %w", entry.ID, stateErr))
		}
	}
	return errors.Join(syncErrors...)
}

// Runtime readiness is durable because local MCP processes are ephemeral. A
// background startup sync therefore records both failure and successful
// checked discovery, so a restart cannot silently erase an unavailable
// managed runtime or leave a stale blocker after recovery.
func (m *LocalMCPManager) recordRuntimeSyncFailure(serverID, transport string, cause error) error {
	if m == nil || m.registry == nil || m.registry.app == nil {
		return nil
	}
	entry := m.registry.findLocalServer(serverID)
	if entry == nil || entry.Source != corelib.MCPSourceMarket || entry.Capability == nil {
		// Durable runtime blockers are reserved for marketplace-managed
		// deployments. Manual local servers retain their historical best-effort
		// startup behavior and must not create global state-file side effects.
		return nil
	}
	if err := m.registry.app.recordMCPRuntimeSyncFailure(serverID, transport, "", cause); err != nil {
		log.Printf("[LocalMCP] persist runtime sync failure for %s: %v", serverID, err)
		return err
	}
	return nil
}

func (m *LocalMCPManager) recordRuntimeSyncReady(serverID string) error {
	if m == nil || m.registry == nil || m.registry.app == nil {
		return nil
	}
	entry := m.registry.findLocalServer(serverID)
	if entry == nil || entry.Source != corelib.MCPSourceMarket || entry.Capability == nil {
		return nil
	}
	if err := m.registry.app.markMCPRuntimeSyncReady(serverID); err != nil {
		log.Printf("[LocalMCP] clear runtime sync state for %s: %v", serverID, err)
		return err
	}
	return nil
}

func (m *LocalMCPManager) startLockForKey(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startLocks == nil {
		m.startLocks = make(map[string]*sync.Mutex)
	}
	lock := m.startLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		m.startLocks[key] = lock
	}
	return lock
}

// GetAllTools returns tool definitions from all running local MCP servers,
// formatted for the ToolDefinitionGenerator.
func (m *LocalMCPManager) GetAllTools() []LocalMCPToolSet {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []LocalMCPToolSet
	for id, client := range m.clients {
		if !client.IsRunning() {
			continue
		}
		tools := client.GetTools()
		if len(tools) > 0 {
			result = append(result, LocalMCPToolSet{
				ServerID:   id,
				ServerName: client.entry.Name,
				Tools:      tools,
			})
		}
	}
	return result
}

// CallTool dispatches a tool call to the appropriate local MCP client.
func (m *LocalMCPManager) CallTool(serverID, toolName string, args map[string]interface{}) (string, error) {
	return m.CallToolForOwner("", serverID, toolName, args)
}

// CallToolForOwner dispatches to an owner-dedicated local MCP process when an
// owner is available. That keeps independent agent loops from serializing on a
// single stdio server process.
func (m *LocalMCPManager) CallToolForOwner(ownerID, serverID, toolName string, args map[string]interface{}) (string, error) {
	startedAt := time.Now()
	if coretool.IsDisabledExternalCodingSessionTool(toolName) {
		return "", fmt.Errorf("external coding-session tool %q is disabled", toolName)
	}
	ownerID = strings.TrimSpace(ownerID)
	if m.registry != nil && m.registry.app != nil && m.registry.app.mcpRuntimeSyncPending(serverID) {
		return "", fmt.Errorf("local MCP runtime for %q is not ready; runtime synchronization requires reconciliation", strings.TrimSpace(serverID))
	}
	defer func() {
		if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
			log.Printf("[LocalMCP] call_tool slow server=%s owner=%q tool=%s elapsed=%s", serverID, ownerID, toolName, elapsed.Round(time.Millisecond))
		}
	}()
	if ownerID != "" {
		client, err := m.clientForOwner(serverID, ownerID)
		if err != nil {
			return "", err
		}
		return client.CallTool(toolName, args)
	}
	m.mu.RLock()
	client, ok := m.clients[serverID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("local MCP server %s not running", serverID)
	}
	return client.CallTool(toolName, args)
}

func (m *LocalMCPManager) clientForOwner(serverID, ownerID string) (*LocalMCPClient, error) {
	serverID = strings.TrimSpace(serverID)
	ownerID = strings.TrimSpace(ownerID)
	if serverID == "" || ownerID == "" {
		return nil, fmt.Errorf("local MCP server and owner are required")
	}
	entry, ok := m.localServerEntry(serverID)
	if !ok {
		return nil, fmt.Errorf("local MCP server %s not configured", serverID)
	}
	if entry.Disabled {
		return nil, fmt.Errorf("local MCP server %s disabled", serverID)
	}
	m.mu.RLock()
	if byOwner := m.ownerClients[serverID]; byOwner != nil {
		if client := byOwner[ownerID]; client != nil && client.IsRunning() {
			m.mu.RUnlock()
			return client, nil
		}
	}
	m.mu.RUnlock()

	startLock := m.startLockForKey(serverID + "\x00" + ownerID)
	lockWaitStart := time.Now()
	startLock.Lock()
	if waited := time.Since(lockWaitStart); waited > 100*time.Millisecond {
		log.Printf("[LocalMCP] owner start lock waited server=%s owner=%q waited=%s", serverID, ownerID, waited.Round(time.Millisecond))
	}
	defer startLock.Unlock()
	m.mu.RLock()
	if byOwner := m.ownerClients[serverID]; byOwner != nil {
		if client := byOwner[ownerID]; client != nil && client.IsRunning() {
			m.mu.RUnlock()
			return client, nil
		}
	}
	m.mu.RUnlock()

	client := NewLocalMCPClient(entry)
	start := time.Now()
	if err := client.Start(m.ctx); err != nil {
		startErr := fmt.Errorf("start owner local MCP %s for %s: %w", serverID, ownerID, err)
		// Owner-scoped clients are an independent runtime boundary. A
		// background/shared client may be healthy while this dedicated process
		// fails, so managed marketplace servers must persist the same blocker
		// used by SyncFromConfig instead of returning an ephemeral error only.
		if stateErr := m.recordRuntimeSyncFailure(serverID, "stdio", startErr); stateErr != nil {
			return nil, fmt.Errorf("%v; persist runtime sync state: %w", startErr, stateErr)
		}
		return nil, startErr
	}
	if _, err := client.DiscoverTools(); err != nil {
		client.Stop()
		discoverErr := fmt.Errorf("discover owner local MCP %s for %s: %w", serverID, ownerID, err)
		if stateErr := m.recordRuntimeSyncFailure(serverID, "stdio", discoverErr); stateErr != nil {
			return nil, fmt.Errorf("%v; persist runtime sync state: %w", discoverErr, stateErr)
		}
		return nil, discoverErr
	}
	m.mu.Lock()
	if m.ownerClients[serverID] == nil {
		m.ownerClients[serverID] = make(map[string]*LocalMCPClient)
	}
	if existing := m.ownerClients[serverID][ownerID]; existing != nil && existing.IsRunning() {
		m.mu.Unlock()
		client.Stop()
		return existing, nil
	}
	m.ownerClients[serverID][ownerID] = client
	m.mu.Unlock()
	// Successful checked startup + tools discovery is durable readiness
	// evidence for a managed server, including when it was first started only
	// for this owner-scoped call.
	if stateErr := m.recordRuntimeSyncReady(serverID); stateErr != nil {
		// The process is now started but its readiness evidence could not be
		// persisted. Remove the owner entry and stop it so callers cannot use
		// an untracked runtime; the durable fence remains fail-closed.
		m.mu.Lock()
		if byOwner := m.ownerClients[serverID]; byOwner != nil {
			delete(byOwner, ownerID)
			if len(byOwner) == 0 {
				delete(m.ownerClients, serverID)
			}
		}
		m.mu.Unlock()
		client.Stop()
		return nil, fmt.Errorf("persist runtime readiness for %s: %w", serverID, stateErr)
	}
	log.Printf("[LocalMCP] started owner-scoped client server=%s owner=%q elapsed=%s", serverID, ownerID, time.Since(start).Round(time.Millisecond))
	return client, nil
}

func (m *LocalMCPManager) localServerEntry(serverID string) (corelib.LocalMCPServerEntry, bool) {
	if m == nil || m.registry == nil {
		return corelib.LocalMCPServerEntry{}, false
	}
	for _, entry := range m.registry.ListLocalServers() {
		if entry.ID == serverID {
			return entry, true
		}
	}
	return corelib.LocalMCPServerEntry{}, false
}

// StopAll terminates all running local MCP server processes.
func (m *LocalMCPManager) StopAll() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, client := range m.clients {
		log.Printf("[LocalMCP] stopping %s", id)
		client.Stop()
	}
	for id, byOwner := range m.ownerClients {
		for owner, client := range byOwner {
			log.Printf("[LocalMCP] stopping %s owner=%q", id, owner)
			client.Stop()
		}
	}
	m.clients = make(map[string]*LocalMCPClient)
	m.ownerClients = make(map[string]map[string]*LocalMCPClient)
	m.startLocks = make(map[string]*sync.Mutex)
}

// StopServer terminates the shared and owner-scoped clients for one server.
// It is used by an explicit checked retry so a stale process cannot make a
// freshly re-armed runtime blocker look ready without repeating startup and
// tools discovery against the current configuration.
func (m *LocalMCPManager) StopServer(serverID string) {
	serverID = strings.TrimSpace(serverID)
	if m == nil || serverID == "" {
		return
	}
	var clients []*LocalMCPClient
	m.mu.Lock()
	if client := m.clients[serverID]; client != nil {
		clients = append(clients, client)
		delete(m.clients, serverID)
	}
	if byOwner := m.ownerClients[serverID]; byOwner != nil {
		for owner, client := range byOwner {
			if client != nil {
				clients = append(clients, client)
			}
			delete(byOwner, owner)
		}
		delete(m.ownerClients, serverID)
	}
	m.mu.Unlock()
	for _, client := range clients {
		client.Stop()
	}
}

// StopOwner terminates owner-scoped local MCP clients for one agent instance.
// Shared clients stay alive for tool discovery and manual calls.
func (m *LocalMCPManager) StopOwner(ownerID string) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return
	}
	type clientToStop struct {
		serverID string
		client   *LocalMCPClient
	}
	var toStop []clientToStop
	m.mu.Lock()
	for serverID, byOwner := range m.ownerClients {
		client := byOwner[ownerID]
		if client == nil {
			continue
		}
		delete(byOwner, ownerID)
		toStop = append(toStop, clientToStop{serverID: serverID, client: client})
		if len(byOwner) == 0 {
			delete(m.ownerClients, serverID)
		}
	}
	m.mu.Unlock()
	for _, item := range toStop {
		log.Printf("[LocalMCP] stopping owner-scoped client server=%s owner=%q", item.serverID, ownerID)
		item.client.Stop()
	}
}

// IsRunning checks if a specific local MCP server is running.
func (m *LocalMCPManager) IsRunning(serverID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[serverID]
	if ok && client.IsRunning() {
		return true
	}
	for _, client := range m.ownerClients[serverID] {
		if client.IsRunning() {
			return true
		}
	}
	return false
}

// ResolveServerID resolves a local MCP server reference by exact ID first, then exact name.
func (m *LocalMCPManager) ResolveServerID(serverRef string) (string, error) {
	serverRef = strings.TrimSpace(serverRef)
	if serverRef == "" {
		return "", fmt.Errorf("local MCP server reference is required")
	}

	m.mu.RLock()

	if client, ok := m.clients[serverRef]; ok && client.IsRunning() {
		m.mu.RUnlock()
		return serverRef, nil
	}

	matches := make([]string, 0, 1)
	for id, client := range m.clients {
		if !client.IsRunning() {
			continue
		}
		if strings.TrimSpace(client.entry.Name) == serverRef {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		m.mu.RUnlock()
		return matches[0], nil
	}
	if len(matches) > 1 {
		m.mu.RUnlock()
		return "", fmt.Errorf("local MCP server name %q is ambiguous; please use server id", serverRef)
	}
	m.mu.RUnlock()

	configured := m.registry.ListLocalServers()
	for _, entry := range configured {
		if entry.Disabled {
			continue
		}
		if entry.ID == serverRef {
			return entry.ID, nil
		}
	}
	matches = matches[:0]
	for _, entry := range configured {
		if entry.Disabled {
			continue
		}
		if strings.TrimSpace(entry.Name) == serverRef {
			matches = append(matches, entry.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("local MCP server name %q is ambiguous; please use server id", serverRef)
	}
	return "", fmt.Errorf("local MCP server %q not running", serverRef)
}

// LocalMCPToolSet groups tools from a single local MCP server.
type LocalMCPToolSet struct {
	ServerID   string
	ServerName string
	Tools      []MCPToolView
}
