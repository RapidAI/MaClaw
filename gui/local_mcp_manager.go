package main

import (
	"context"
	"fmt"
	"log"
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
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	// Don't start new processes if the manager is shutting down.
	select {
	case <-m.ctx.Done():
		return
	default:
	}

	entries := m.registry.ListLocalServers()

	// Build a set of desired server IDs
	desired := make(map[string]corelib.LocalMCPServerEntry, len(entries))
	for _, e := range entries {
		if !e.Disabled {
			desired[e.ID] = e
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
		if _, ok := desired[id]; !ok {
			delete(m.clients, id)
			toStop = append(toStop, clientToStop{id: id, client: client})
		}
	}
	for id, byOwner := range m.ownerClients {
		if _, ok := desired[id]; ok {
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
		toStart = append(toStart, entry)
	}
	m.mu.Unlock()

	for _, item := range toStop {
		log.Printf("[LocalMCP] stopping removed/disabled/crashed server %s", item.id)
		item.client.Stop()
	}

	for _, entry := range toStart {
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		client := NewLocalMCPClient(entry)
		if err := client.Start(m.ctx); err != nil {
			log.Printf("[LocalMCP] failed to start %s (%s): %v", entry.Name, entry.Command, err)
			continue
		}
		// Discover tools with retry — some servers need a moment after
		// the handshake before tools/list is ready.
		var tools []MCPToolView
		var discoverErr error
		for attempt := 1; attempt <= 3; attempt++ {
			tools, discoverErr = client.DiscoverTools()
			if discoverErr == nil {
				break
			}
			log.Printf("[LocalMCP] discover tools for %s attempt %d/3 failed: %v", entry.Name, attempt, discoverErr)
			if attempt < 3 {
				select {
				case <-m.ctx.Done():
					discoverErr = m.ctx.Err()
				case <-time.After(time.Duration(attempt) * time.Second):
				}
			}
		}
		if discoverErr != nil {
			log.Printf("[LocalMCP] giving up tool discovery for %s: %v", entry.Name, discoverErr)
			client.Stop()
			continue
		}
		log.Printf("[LocalMCP] started %s with %d tools", entry.Name, len(tools))
		select {
		case <-m.ctx.Done():
			client.Stop()
			return
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
	}
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
		return nil, fmt.Errorf("start owner local MCP %s for %s: %w", serverID, ownerID, err)
	}
	if _, err := client.DiscoverTools(); err != nil {
		client.Stop()
		return nil, fmt.Errorf("discover owner local MCP %s for %s: %w", serverID, ownerID, err)
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
