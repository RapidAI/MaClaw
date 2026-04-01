package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// LocalMCPManager manages the lifecycle of all local (stdio) MCP server
// processes. It starts/stops clients based on the config and provides
// tool discovery and invocation for the agent pipeline.
type LocalMCPManager struct {
	registry *MCPRegistry
	mu       sync.RWMutex
	clients  map[string]*LocalMCPClient // keyed by server ID
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewLocalMCPManager creates a new manager.
func NewLocalMCPManager(registry *MCPRegistry) *LocalMCPManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &LocalMCPManager{
		registry: registry,
		clients:  make(map[string]*LocalMCPClient),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// SyncFromConfig reads the local MCP server config and starts/stops
// clients as needed. Enabled servers are started whenever a sync happens.
// App startup decides whether to trigger the initial sync based on AutoStart.
func (m *LocalMCPManager) SyncFromConfig() {
	// Don't start new processes if the manager is shutting down.
	select {
	case <-m.ctx.Done():
		return
	default:
	}

	entries := m.registry.ListLocalServers()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Build a set of desired server IDs
	desired := make(map[string]LocalMCPServerEntry, len(entries))
	for _, e := range entries {
		if !e.Disabled {
			desired[e.ID] = e
		}
	}

	// Stop clients that are no longer in config or are disabled
	for id, client := range m.clients {
		if _, ok := desired[id]; !ok {
			log.Printf("[LocalMCP] stopping removed/disabled server %s", id)
			client.Stop()
			delete(m.clients, id)
		}
	}

	// Also remove clients whose processes have crashed
	for id, client := range m.clients {
		if !client.IsRunning() {
			log.Printf("[LocalMCP] removing crashed server %s", id)
			client.Stop()
			delete(m.clients, id)
		}
	}

	// Start new clients (or restart crashed ones)
	for id, entry := range desired {
		if _, exists := m.clients[id]; exists {
			continue
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
		m.clients[id] = client
	}
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
	m.mu.RLock()
	client, ok := m.clients[serverID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("local MCP server %s not running", serverID)
	}
	return client.CallTool(toolName, args)
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
	m.clients = make(map[string]*LocalMCPClient)
}

// IsRunning checks if a specific local MCP server is running.
func (m *LocalMCPManager) IsRunning(serverID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[serverID]
	return ok && client.IsRunning()
}

// LocalMCPToolSet groups tools from a single local MCP server.
type LocalMCPToolSet struct {
	ServerID   string
	ServerName string
	Tools      []MCPToolView
}
